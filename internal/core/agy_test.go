package core

import (
	"fmt"
	"math"
	"testing"
	"time"

	"cc-usage/internal/config"
	"cc-usage/internal/store"
)

// agyPayload 는 agy 1.2.14 가 실제로 준 stdin 에서 식별 정보를 뺀 것이다.
const agyPayload = `{
  "cwd": "/w", "session_id": "s",
  "model": {"id": "%s", "display_name": "%s", "effort": "high"},
  "workspace": {"current_dir": "/w", "project_dir": "/w"},
  "context_window": {"context_window_size": 1048576, "used_percentage": 0, "remaining_percentage": 100},
  "product": "antigravity",
  "quota": {
    "3p-5h":         {"remaining_fraction": 1,          "reset_time": "2026-10-01T05:15:36Z", "reset_in_seconds": 17967},
    "3p-weekly":     {"remaining_fraction": 0.25,       "reset_time": "2026-10-08T00:15:36Z", "reset_in_seconds": 604767},
    "gemini-5h":     {"remaining_fraction": 0.8596034,  "reset_time": "2026-10-01T04:15:38Z", "reset_in_seconds": 14369},
    "gemini-weekly": {"remaining_fraction": 0.93826973, "reset_time": "2026-10-07T08:19:44Z", "reset_in_seconds": 547415}
  },
  "plan_tier": "Google AI Pro", "terminal_width": 140
}`

func agyInput(t *testing.T, model string) *Input {
	t.Helper()
	in, err := ParseInput([]byte(fmt.Sprintf(agyPayload, model, model)))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestAgyLimitsPicksBucketByModel(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		model       string
		five, seven float64 // 사용률(%)
	}{
		{"Gemini 3.8 Flash (High)", 14.04, 6.17},
		{"Claude Opus 4.6 (Thinking)", 0, 75},
		{"GPT-OSS 120B (Medium)", 0, 75},
	}
	for _, c := range cases {
		in := agyInput(t, c.model)
		if !in.IsAgy() {
			t.Fatalf("%s: IsAgy false", c.model)
		}
		lim := AgyLimits(in, now)
		if lim.FiveHour == nil || lim.SevenDay == nil {
			t.Fatalf("%s: got %+v", c.model, lim)
		}
		if !near(lim.FiveHour.Percent, c.five) || !near(lim.SevenDay.Percent, c.seven) {
			t.Errorf("%s: 5h %.2f 7d %.2f, want %.2f %.2f",
				c.model, lim.FiveHour.Percent, lim.SevenDay.Percent, c.five, c.seven)
		}
		if lim.FiveHour.ResetsAt.IsZero() || !lim.FromStdin {
			t.Errorf("%s: resets_at/FromStdin 누락 %+v", c.model, lim.FiveHour)
		}
	}
}

func TestAgyLimitsTolerant(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name, raw string
	}{
		{"quota 없음", `{"product":"antigravity","model":{"display_name":"Gemini"}}`},
		{"고른 버킷 없음 — 다른 버킷으로 대신하지 않는다",
			`{"product":"antigravity","model":{"display_name":"Gemini"},"quota":{"3p-5h":{"remaining_fraction":0.5}}}`},
		{"범위 밖 fraction", `{"product":"antigravity","model":{"display_name":"Gemini"},"quota":{"gemini-5h":{"remaining_fraction":42}}}`},
		{"이미 리셋된 창", `{"product":"antigravity","model":{"display_name":"Gemini"},"quota":{"gemini-5h":{"remaining_fraction":0.1,"reset_time":"2026-09-30T00:00:00Z"}}}`},
	}
	for _, c := range cases {
		in, _ := ParseInput([]byte(c.raw))
		if lim := AgyLimits(in, now); lim.FiveHour != nil || lim.SevenDay != nil {
			t.Errorf("%s: got %+v", c.name, lim)
		}
	}
}

func TestIsAgyOnlyForAntigravity(t *testing.T) {
	// Claude Code payload 에는 product 가 없다 — 기존 경로를 그대로 타야 한다.
	in, _ := ParseInput([]byte(`{"model":{"display_name":"Opus"},"rate_limits":{}}`))
	if in.IsAgy() {
		t.Error("Claude Code payload 를 agy 로 봤다")
	}
}

func TestSteadyAlertNeverBursts(t *testing.T) {
	p := &config.Config{}
	p.ApplyDefaults()
	lim := Limits{FiveHour: &store.Window{Percent: 100}}
	a := SteadyAlert(p, lim)
	if a.Level != AlertOver || a.Window != "5h" || a.Burst {
		t.Errorf("got %+v", a)
	}
	if a := SteadyAlert(p, Limits{FiveHour: &store.Window{Percent: 10}}); a.Level != AlertNone {
		t.Errorf("여유 구간인데 경보: %+v", a)
	}
}
