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

func TestAgyLimitsUnknownModelDrawsNothing(t *testing.T) {
	// 모델 이름을 모르면 어느 버킷인지 모른다 — 3p 숫자를 Gemini 세션에 그리지 않는다.
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, raw := range []string{
		`{"product":"antigravity","quota":{"3p-5h":{"remaining_fraction":0.25}}}`,
		`{"product":"antigravity","model":"gemini","quota":{"3p-5h":{"remaining_fraction":0.25}}}`, // 타입 어긋남 → 이름 빔
	} {
		in, _ := ParseInput([]byte(raw))
		if lim := AgyLimits(in, now); lim.FiveHour != nil || lim.SevenDay != nil {
			t.Errorf("%s: got %+v", raw, lim)
		}
	}
	// 접두사가 달라도 gemini 를 품으면 gemini 버킷이다.
	in := agyInput(t, "Google Gemini 4 Ultra")
	if lim := AgyLimits(in, now); lim.FiveHour == nil || !near(lim.FiveHour.Percent, 14.04) {
		t.Errorf("Google Gemini: got %+v", lim.FiveHour)
	}
}

func TestAgyWindowClampsEdgeNoise(t *testing.T) {
	f := func(v float64) *agyQuota { return &agyQuota{RemainingFraction: &v} }
	cases := []struct {
		q    *agyQuota
		want float64 // 사용률, -1 이면 nil
	}{
		{f(-1e-9), 100},   // 소진 직후의 음수 오차 — 창이 사라지면 안 된다
		{f(1.0000001), 0}, // 미사용의 오차
		{f(0.5), 50},      //
		{f(1.5), -1},      // 단위를 모른다
		{f(-0.2), -1},     //
		{&agyQuota{}, -1}, // 값 없음
	}
	for _, c := range cases {
		w := agyWindow(c.q)
		switch {
		case c.want < 0 && w != nil:
			t.Errorf("%v: got %+v want nil", *c.q, w)
		case c.want >= 0 && (w == nil || !near(w.Percent, c.want)):
			t.Errorf("%v: got %+v want %.0f", *c.q, w, c.want)
		}
	}
}

func TestLocalLimits(t *testing.T) {
	// none·agy 만 local 이다. Claude Code payload 는 source 가 무엇이든 local 이 아니다.
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	agy := agyInput(t, "Gemini 3.8 Flash (High)")
	claude, _ := ParseInput([]byte(`{"model":{"display_name":"Opus 5"}}`))
	cases := []struct {
		name      string
		source    string
		in        *Input
		local     bool
		hasLimits bool
	}{
		{"claude auto", config.SourceAuto, claude, false, false},
		{"claude api", config.SourceAPI, claude, false, false},
		{"claude none", config.SourceNone, claude, true, false},
		{"agy auto", config.SourceAuto, agy, true, true},
		{"agy api", config.SourceAPI, agy, true, true},
		{"agy none", config.SourceNone, agy, true, false},
	}
	for _, c := range cases {
		p := &config.Config{Source: c.source}
		p.ApplyDefaults()
		lim, _, local := LocalLimits(p, c.in, now)
		if local != c.local || (lim.FiveHour != nil) != c.hasLimits {
			t.Errorf("%s: local=%v limits=%+v", c.name, local, lim)
		}
	}
}
