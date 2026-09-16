// Package git reads the working-tree state shown on the statusline.
//
// statusline은 매 렌더마다 도니 git 호출은 하나뿐이다 — `status --porcelain=v2
// --branch`가 브랜치명·업스트림·ahead/behind·dirty를 한 번에 준다.
package git

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// timeout caps the git call. 넘기면 브랜치 세그먼트만 빠지고 statusline은 그대로 나간다.
const timeout = 500 * time.Millisecond

// Status is the subset of `git status` the statusline shows.
type Status struct {
	Branch      string // "(detached)"면 Detached가 true
	Detached    bool
	OID         string // detached일 때 보여줄 커밋 (초기 커밋 전이면 빈 값)
	HasUpstream bool
	Ahead       int
	Behind      int

	// 변경 파일 수. porcelain=v2가 이미 파일당 한 줄을 뱉으니 세는 데 드는
	// 추가 비용은 없다 — XY 필드의 X가 staged, Y가 unstaged다.
	Staged     int
	Unstaged   int
	Conflicted int
}

// Dirty reports whether the working tree has any change to show.
func (s Status) Dirty() bool { return s.Staged+s.Unstaged+s.Conflicted > 0 }

// Read returns the status of the repo containing dir. ok is false when dir is
// empty, is not a repo, or git failed — 그 경우 호출자는 세그먼트를 생략한다.
func Read(ctx context.Context, dir string) (Status, bool) {
	if dir == "" {
		return Status{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// --no-optional-locks: statusline이 인덱스 락을 잡으면 안 된다.
	// --untracked-files=no: untracked 스캔이 status에서 제일 비싸다. 대신
	// untracked 파일만 있는 트리는 clean으로 보인다.
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "--no-optional-locks",
		"status", "--porcelain=v2", "--branch", "--untracked-files=no").Output()
	if err != nil {
		return Status{}, false
	}
	return Parse(string(out)), true
}

// Parse reads `git status --porcelain=v2 --branch` output.
func Parse(out string) Status {
	var s Status
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "# ") {
			s.count(line)
			continue
		}
		key, val, _ := strings.Cut(strings.TrimPrefix(line, "# "), " ")
		switch key {
		case "branch.oid":
			if val != "(initial)" {
				s.OID = val
			}
		case "branch.head":
			if val == "(detached)" {
				s.Detached = true
			}
			s.Branch = val
		case "branch.upstream":
			s.HasUpstream = val != ""
		case "branch.ab":
			// "+1 -2" — 업스트림이 없으면 이 줄 자체가 오지 않는다.
			a, b, ok := strings.Cut(val, " ")
			if !ok {
				continue
			}
			s.Ahead, s.Behind = signed(a), signed(b)
		}
	}
	return s
}

// count classifies one entry line: "1"/"2"는 일반·rename 변경, "u"는 conflict.
// untracked("?")는 --untracked-files=no 때문에 애초에 오지 않는다.
func (s *Status) count(line string) {
	kind, rest, ok := strings.Cut(line, " ")
	if !ok {
		return
	}
	if kind == "u" {
		s.Conflicted++
		return
	}
	if kind != "1" && kind != "2" {
		return
	}
	xy, _, _ := strings.Cut(rest, " ")
	if len(xy) != 2 {
		return
	}
	if xy[0] != '.' {
		s.Staged++
	}
	if xy[1] != '.' {
		s.Unstaged++
	}
}

func signed(v string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimPrefix(v, "+"), "-"))
	if err != nil {
		return 0
	}
	return n
}
