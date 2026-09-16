// Package extra runs the statusline segments a profile borrows from other tools
// (profile.extra_commands) and appends their output verbatim.
//
// cc-usage는 그 명령들이 무엇인지 알지 않는다 — 명령·플레이스홀더·타임아웃만 알고,
// 어떤 도구를 부를지는 전부 ~/.config/cc-usage/config.json 에 있다.
package extra

import (
	"context"
	"os/exec"
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

// Run executes the commands in parallel and returns their stdout lines in config
// order. A command that is missing, times out, or exits non-zero contributes
// nothing: 추가 세그먼트가 statusline을 깨뜨려선 안 된다.
func Run(ctx context.Context, cmds []config.ExtraCommand, v Vars) []string {
	out := make([][]string, len(cmds))
	var wg sync.WaitGroup
	for i, c := range cmds {
		argv, ok := Expand(c.Command, v.vals())
		if !ok {
			continue
		}
		wg.Add(1)
		go func(i int, c config.ExtraCommand, argv []string) {
			defer wg.Done()
			out[i] = run(ctx, c, argv)
		}(i, c, argv)
	}
	wg.Wait()

	var lines []string
	for _, o := range out {
		lines = append(lines, o...)
	}
	return lines
}

func run(ctx context.Context, c config.ExtraCommand, argv []string) []string {
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
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func (v Vars) vals() map[string]string {
	return map[string]string{"{{session_id}}": v.SessionID, "{{cwd}}": v.Cwd}
}

// Expand substitutes the placeholders in every argv element. ok is false when the
// command should be skipped: 빈 자리를 채워 부르면 무의미한 조회가 된다.
func Expand(argv []string, vals map[string]string) ([]string, bool) {
	if len(argv) == 0 {
		return nil, false
	}
	out := make([]string, len(argv))
	for i, a := range argv {
		for k, val := range vals {
			if !strings.Contains(a, k) {
				continue
			}
			if val == "" {
				return nil, false
			}
			a = strings.ReplaceAll(a, k, val)
		}
		out[i] = a
	}
	return out, true
}
