// Package extra runs the statusline segments a profile borrows from other tools
// (profile.extra_commands) and appends their output verbatim.
//
// cc-usage는 그 명령들이 무엇인지 알지 않는다 — 명령·플레이스홀더·타임아웃만 알고,
// 어떤 도구를 부를지는 전부 ~/.config/cc-usage/config.json 에 있다.
package extra

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"cc-usage/internal/config"
)

// Vars are the placeholder values available to extra commands.
type Vars struct {
	SessionID string // {{session_id}}
	Cwd       string // {{cwd}}
}

// Status is how one extra command ended. statusline 은 OK 가 아닌 것을 전부
// 조용히 삼키므로, 어느 쪽이었는지는 doctor 에서만 보인다.
type Status int

const (
	OK       Status = iota // 0 으로 끝났고 출력이 있다
	Empty                  // 0 으로 끝났지만 출력이 없다
	Skipped                // 실행하지 않았다 (placeholder 가 비었거나 command 가 비었다)
	NotFound               // 실행 파일이 없다
	Timeout                // timeout_ms 를 넘겼다
	Failed                 // 0 이 아닌 코드로 끝났거나 실행 자체가 실패했다
)

// Result is one command's outcome. statusline 은 Lines 만 쓰고, 나머지는
// doctor 가 "왜 안 붙는가" 를 말하는 데 쓴다.
type Result struct {
	Status  Status
	Argv    []string // 치환한 argv. Skipped 면 nil
	Lines   []string // OK 일 때만 채워진다
	Missing string   // Skipped: 비어 있던 placeholder ("" 면 command 자체가 비었다)
	Stderr  string   // Failed: stderr 첫 줄
	Err     error    // NotFound / Failed
	Elapsed time.Duration
}

// Run executes the commands in parallel and returns their stdout lines in config
// order. A command that is missing, times out, or exits non-zero contributes
// nothing: 추가 세그먼트가 statusline을 깨뜨려선 안 된다.
func Run(ctx context.Context, cmds []config.ExtraCommand, v Vars) []string {
	var lines []string
	for _, r := range Probe(ctx, cmds, v) {
		lines = append(lines, r.Lines...)
	}
	return lines
}

// Probe runs the commands exactly as Run does and reports each outcome, in
// config order. statusline 과 **같은 경로**로 돌린다 — doctor 가 따로 돌리면
// 둘이 어긋났을 때 진단이 실제 동작과 다른 말을 한다.
func Probe(ctx context.Context, cmds []config.ExtraCommand, v Vars) []Result {
	out := make([]Result, len(cmds))
	vals := v.vals()
	var wg sync.WaitGroup
	for i, c := range cmds {
		argv, missing, ok := expand(c.Command, vals)
		if !ok {
			out[i] = Result{Status: Skipped, Missing: missing}
			continue
		}
		wg.Add(1)
		go func(i int, c config.ExtraCommand, argv []string) {
			defer wg.Done()
			out[i] = run(ctx, c, argv)
		}(i, c, argv)
	}
	wg.Wait()
	return out
}

func run(ctx context.Context, c config.ExtraCommand, argv []string) Result {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, c.Timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// 자식이 stdout을 물려받으면 직접 프로세스만 죽여서는 pipe가 닫히지 않아
	// Output()이 계속 기다린다 (`sh -c "sleep 5 &"`가 timeout_ms=50에도 5초를
	// 잡아먹었다). 그룹째 죽이고, 그래도 남는 경우를 위해 취소 후 대기도 묶는다.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 100 * time.Millisecond
	// Output() drops stdout on a non-zero exit, which is what `curl -sf` and the
	// old script's `|| :` bought us: 실패한 명령의 에러 본문이 새어 나오지 않는다.
	b, err := cmd.Output()
	r := Result{Argv: argv, Elapsed: time.Since(start)}
	var ee *exec.ExitError
	switch {
	// 타임아웃을 먼저 본다. 그룹째 죽인 결과는 signal 종료(ExitError)로도
	// 오므로, 순서를 바꾸면 타임아웃이 "비정상 종료" 로 읽힌다.
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		r.Status = Timeout
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		r.Status, r.Err = NotFound, err
	case errors.As(err, &ee):
		r.Status, r.Err, r.Stderr = Failed, err, firstLine(string(ee.Stderr))
	case err != nil:
		r.Status, r.Err = Failed, err
	default:
		for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			if strings.TrimSpace(l) != "" {
				r.Lines = append(r.Lines, l)
			}
		}
		r.Status = OK
		if len(r.Lines) == 0 {
			r.Status = Empty
		}
	}
	return r
}

// Describe is the one-line verdict doctor prints for r.
func Describe(r Result, timeout time.Duration) string {
	ms := func(d time.Duration) string { return fmt.Sprintf("%dms", d.Milliseconds()) }
	switch r.Status {
	case OK:
		more := ""
		if n := len(r.Lines); n > 1 {
			more = fmt.Sprintf(" (외 %d줄)", n-1)
		}
		return fmt.Sprintf("ok %s → %s%s", ms(r.Elapsed), r.Lines[0], more)
	case Empty:
		return fmt.Sprintf("출력 없음 — exit 0 이지만 stdout 이 비었다 (%s)", ms(r.Elapsed))
	case Skipped:
		if r.Missing == "" {
			return "건너뜀 — command 가 비어 있다"
		}
		return fmt.Sprintf("건너뜀 — %s 가 비어 있다", r.Missing)
	case NotFound:
		return fmt.Sprintf("미설치 — %v", r.Err)
	case Timeout:
		return fmt.Sprintf("타임아웃 — %s 를 넘겼다 (timeout_ms 로 늘릴 수 있다)", ms(timeout))
	default:
		s := fmt.Sprintf("비정상 종료 — %v", r.Err)
		if r.Stderr != "" {
			s += ": " + r.Stderr
		}
		return s
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func (v Vars) vals() map[string]string {
	return map[string]string{"{{session_id}}": v.SessionID, "{{cwd}}": v.Cwd}
}

// Expand substitutes the placeholders in every argv element. ok is false when the
// command should be skipped: 빈 자리를 채워 부르면 무의미한 조회가 된다.
func Expand(argv []string, vals map[string]string) ([]string, bool) {
	out, _, ok := expand(argv, vals)
	return out, ok
}

// expand is Expand that also names the empty placeholder that caused a skip.
// 키를 정렬해 돈다 — map 순서대로면 빈 것이 둘일 때 doctor 가 매번 다른 이름을 댄다.
func expand(argv []string, vals map[string]string) ([]string, string, bool) {
	if len(argv) == 0 {
		return nil, "", false
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, len(argv))
	for i, a := range argv {
		for _, k := range keys {
			if !strings.Contains(a, k) {
				continue
			}
			if vals[k] == "" {
				return nil, k, false
			}
			a = strings.ReplaceAll(a, k, vals[k])
		}
		out[i] = a
	}
	return out, "", true
}
