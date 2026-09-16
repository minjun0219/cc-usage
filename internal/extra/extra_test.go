package extra

import (
	"context"
	"reflect"
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
