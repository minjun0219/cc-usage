package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// repoPath is where this binary was built from, injected at build time by the
// Makefile. `cc-usage update` pulls and reinstalls from there.
//
// 설정 항목으로 두지 않은 이유: 빌드한 자리가 곧 답이라 사용자가 적어 줄 것이
// 없다. 비어 있으면(직접 go build 한 경우 등) update 는 그 사실을 말하고 멈춘다.
var repoPath = ""

// runUpdate pulls the source and reinstalls.
//
// 자동 업데이트가 아니다 — 부를 때만 돈다. 세션 도중에 동작이 조용히 바뀌는
// 것을 피하려는 것이고, 그런 어긋남은 실제로 사고를 냈다(설정이 옛 플래그를
// 들고 있는데 바이너리가 그것을 모르는 상태).
func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fs.Bool("check", false, "받지 않고 뒤처졌는지만 본다")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if repoPath == "" {
		return errors.New("이 바이너리에는 소스 경로가 박혀 있지 않습니다 (make install 로 설치하지 않았습니다)")
	}
	// git 에게 물어본다. .git 이 디렉터리인지로 판단하면 linked worktree 와
	// submodule 이 걸린다 — 거기서는 .git 이 메타데이터를 가리키는 **파일**이다.
	if out, err := gitIn(repoPath, "rev-parse", "--git-dir"); err != nil {
		return fmt.Errorf("git 저장소가 아닙니다: %s (옮겼거나 지웠습니까?)%s", repoPath, indent(out))
	}
	fmt.Printf("소스: %s\n", repoPath)

	// 원격 상태를 먼저 본다. fetch 만으로는 작업 트리가 바뀌지 않는다.
	// remote 를 적지 않는다. 브랜치의 upstream 이 origin 이 아닌 저장소에서
	// "origin" 을 박으면 upstream 이 멀쩡한데도 여기서 죽거나, 무관한 origin 을
	// 받아 와 비교가 낡은 데이터 위에서 돈다.
	if out, err := gitIn(repoPath, "fetch", "--quiet"); err != nil {
		return fmt.Errorf("fetch 실패: %w%s", err, indent(out))
	}
	behind, ahead, ok := counts(repoPath)
	if !ok {
		// upstream 이 없으면 비교할 대상이 없다. 모르는 것을 "최신" 이라고
		// 말하지 않는다 — 그 말을 믿고 넘어가면 뒤처진 채로 남는다.
		return errors.New("upstream 이 없어 비교할 수 없습니다 (브랜치가 원격을 추적하고 있습니까?)")
	}
	switch {
	case behind == 0 && ahead == 0:
		fmt.Println("최신입니다.")
	case behind == 0:
		fmt.Printf("원격보다 %d커밋 앞서 있습니다 (받을 것 없음).\n", ahead)
	default:
		fmt.Printf("%d커밋 뒤처져 있습니다.\n", behind)
	}
	if *check || behind == 0 {
		return nil
	}

	// 커밋하지 않은 변경이 있으면 멈춘다. 그 상태로 설치하면 돌고 있는
	// 바이너리가 하는 일의 소스가 어디에도 없게 된다.
	// status 실패를 무시하면 빈 출력이 "깨끗함" 으로 읽혀 그대로 설치까지 간다.
	out, err := gitIn(repoPath, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("작업 트리 상태를 확인할 수 없습니다: %w%s", err, indent(out))
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("커밋하지 않은 변경이 있습니다 — 먼저 정리하세요:%s", indent(out))
	}
	if out, err := gitIn(repoPath, "pull", "--ff-only", "--quiet"); err != nil {
		return fmt.Errorf("pull 실패 (갈라졌을 수 있습니다): %w%s", err, indent(out))
	}

	// 게이트를 통과하지 못한 것을 설치하지 않는다.
	fmt.Println("make test …")
	if out, err := run(repoPath, "make", "test"); err != nil {
		return fmt.Errorf("테스트 실패 — 설치하지 않습니다: %w%s", err, indent(out))
	}
	fmt.Println("make build …")
	if out, err := run(repoPath, "make", "build"); err != nil {
		return fmt.Errorf("빌드 실패: %w%s", err, indent(out))
	}
	exe, err := installTarget()
	if err != nil {
		return err
	}
	if out, err := run(repoPath, "install", "-m", "0755", filepath.Join(repoPath, "bin", "cc-usage"), exe); err != nil {
		return fmt.Errorf("설치 실패 (%s): %w%s", exe, err, indent(out))
	}
	now, _ := gitIn(repoPath, "describe", "--tags", "--always", "--dirty")
	fmt.Printf("%s → %s  (%s)\n", version, strings.TrimSpace(now), exe)
	return nil
}

// installTarget is the path this update must overwrite — 지금 돌고 있는 바로 그
// 파일이다.
//
// `make install` 을 부르지 않는 이유: 그러면 Makefile 의 기본 PREFIX 로 간다.
// 기본이 아닌 자리에 설치해 뒀거나 바이너리를 옮겼으면 **엉뚱한 파일을 갱신하고
// 성공이라 보고한다** — 돌고 있는 바이너리는 옛 버전 그대로다. Makefile 의
// install 타깃이 하는 일은 build + install -m 0755 가 전부라, 자리를 직접
// 정해 주는 것으로 잃는 게 없다.
//
// 심링크는 실체까지 따라간다. `~/.local/bin/cc-usage` 가 링크면 링크를 파일로
// 덮어써 버리는 대신 가리키는 실체를 갱신해야 링크가 살아남는다.
func installTarget() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("실행 파일 경로를 알 수 없습니다: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// counts returns how far HEAD is behind and ahead of its upstream.
//
// ok 가 false 면 비교 자체가 안 된 것이다(upstream 없음 등). 0/0 으로 뭉개면
// 호출자가 그것을 "최신" 으로 읽는다 — 모르는 것과 같은 것은 다르다.
func counts(dir string) (behind, ahead int, ok bool) {
	out, err := gitIn(dir, "rev-list", "--left-right", "--count", "HEAD...@{u}")
	if err != nil {
		return 0, 0, false
	}
	if n, err := fmt.Sscanf(strings.TrimSpace(out), "%d\t%d", &ahead, &behind); n != 2 || err != nil {
		return 0, 0, false
	}
	return behind, ahead, true
}

func gitIn(dir string, args ...string) (string, error) {
	return run(dir, "git", append([]string{"-C", dir}, args...)...)
}

func run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func indent(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return "\n  " + strings.ReplaceAll(s, "\n", "\n  ")
}
