package extra

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"cc-usage/internal/config"
)

func TestRun(t *testing.T) {
	cmds := []config.ExtraCommand{
		{Command: []string{"sh", "-c", `printf 'lm {{session_id}}\n'`}},
		{Command: []string{"sh", "-c", "echo boom >&2; echo leaked; exit 1"}}, // 실패 → 출력 없음
		{Command: []string{"sh", "-c", "sleep 5"}, TimeoutMS: 50},             // 타임아웃 → 출력 없음
		{Command: []string{"cc-usage-no-such-binary-xyz"}},                    // 미설치 → 출력 없음
		{Command: []string{"sh", "-c", `printf 'board {{cwd}}\n\n'`}},         // 빈 줄은 버린다
	}
	got := Run(context.Background(), cmds, Vars{SessionID: "s1", Cwd: "/tmp/x"})
	want := []string{"lm s1", "board /tmp/x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRunSkipsEmptyPlaceholder(t *testing.T) {
	cmds := []config.ExtraCommand{
		{Command: []string{"sh", "-c", `printf 'lm\n'`, "--", "{{session_id}}"}},
		{Command: []string{"sh", "-c", `printf 'plain\n'`}},
	}
	got := Run(context.Background(), cmds, Vars{})
	if !reflect.DeepEqual(got, []string{"plain"}) {
		t.Errorf("got %q, want only the command without placeholders", got)
	}
}

func TestExpand(t *testing.T) {
	argv, ok := Expand([]string{"curl", "--data-urlencode", "cwd={{cwd}}", "-s"}, Vars{Cwd: "/a b"}.vals())
	if !ok || !reflect.DeepEqual(argv, []string{"curl", "--data-urlencode", "cwd=/a b", "-s"}) {
		t.Errorf("got %q ok=%v", argv, ok)
	}
	if _, ok := Expand(nil, nil); ok {
		t.Error("empty argv should be skipped")
	}
}

func TestRunTimeoutKillsDescendants(t *testing.T) {
	// 자식이 stdout을 물려받으면 직접 프로세스만 죽여서는 pipe가 안 닫힌다.
	// 그룹째 죽이지 않으면 timeout_ms를 무시하고 5초를 기다린다.
	cmds := []config.ExtraCommand{
		{Command: []string{"sh", "-c", "sleep 5 &"}, TimeoutMS: 50},
	}
	start := time.Now()
	if out := Run(context.Background(), cmds, Vars{}); out != nil {
		t.Errorf("타임아웃된 명령은 출력이 없어야 한다: %q", out)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("timeout_ms=50인데 %v 걸렸다 — 자식이 pipe를 붙잡고 있다", d)
	}
}

func TestProbeSaysWhyEachCommandIsMissing(t *testing.T) {
	// statusline 은 실패를 전부 삼킨다. doctor 가 쓰는 Probe 는 그 이유를
	// 항목별로 갈라 말해야 한다 — 같은 "안 붙음" 이라도 고칠 곳이 다르다.
	cmds := []config.ExtraCommand{
		{Command: []string{"sh", "-c", `printf 'a\nb\n'`}},
		{Command: []string{"sh", "-c", "true"}},
		{Command: []string{"sh", "-c", "echo nope >&2; exit 3"}},
		{Command: []string{"sh", "-c", "sleep 5"}, TimeoutMS: 50},
		{Command: []string{"cc-usage-no-such-binary-xyz"}},
		{Command: []string{"/no/such/path/xyz"}},
		{Command: []string{"x", "{{session_id}}"}},
		{},
		{Command: []string{"sh", "-c", "sleep 5 & echo hi"}, TimeoutMS: 3000}, // 자식이 stdout 을 붙잡음
	}
	got := Probe(context.Background(), cmds, Vars{Cwd: "/tmp"})
	want := []Status{OK, Empty, Failed, Timeout, NotFound, NotFound, Skipped, Skipped, Failed}
	for i, w := range want {
		if got[i].Status != w {
			t.Errorf("[%d] status %v, want %v (%+v)", i, got[i].Status, w, got[i])
		}
	}
	if got[2].Stderr != "nope" {
		t.Errorf("stderr 첫 줄을 남겨야 한다: %q", got[2].Stderr)
	}
	if got[6].Missing != "{{session_id}}" {
		t.Errorf("비어 있던 placeholder 를 말해야 한다: %q", got[6].Missing)
	}
	for i, frag := range []string{"ok", "출력 없음", "nope", "타임아웃", "미설치", "미설치", "{{session_id}}", "command 가 비어", "stdout 을 붙잡고"} {
		if d := Describe(got[i], cmds[i].Timeout()); !strings.Contains(d, frag) {
			t.Errorf("[%d] Describe = %q, want %q 포함", i, d, frag)
		}
	}
	if d := Describe(got[0], 0); !strings.Contains(d, "→ a (외 1줄)") {
		t.Errorf("첫 줄과 나머지 줄 수: %q", d)
	}
}
