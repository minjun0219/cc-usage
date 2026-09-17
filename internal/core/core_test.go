package core

import (
	"strings"
	"testing"
	"time"

	"cc-usage/internal/config"
	"cc-usage/internal/store"
)

func f(v float64) *float64 { return &v }

func profile(src string) *config.Config {
	p := &config.Config{Source: src, Guard: true}
	p.ApplyDefaults()
	return p
}

func TestParseInputResetsAtFormats(t *testing.T) {
	in, err := ParseInput([]byte(`{"rate_limits":{
		"five_hour":{"used_percentage":23.5,"resets_at":1738425600},
		"seven_day":{"used_percentage":41,"resets_at":"2026-04-03T00:00:00Z"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	five, seven := in.StdinLimits()
	if five == nil || five.ResetsAt.Unix() != 1738425600 {
		t.Fatalf("five: %+v", five)
	}
	if seven == nil || seven.ResetsAt.Year() != 2026 {
		t.Fatalf("seven: %+v", seven)
	}
}

func TestParseInputWorkspace(t *testing.T) {
	in, err := ParseInput([]byte(`{"workspace":{"current_dir":"/Users/x/dev/y"},"session_id":"s1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if in.Workspace.CurrentDir != "/Users/x/dev/y" || in.SessionID != "s1" {
		t.Errorf("got %+v", in)
	}
	// workspace가 없는 payload도 그대로 통과해야 한다.
	in, err = ParseInput([]byte(`{"model":{"display_name":"Opus"}}`))
	if err != nil || in.Workspace.CurrentDir != "" {
		t.Errorf("got %+v err=%v", in, err)
	}
}

func TestParseInputRejectsEpochLeak(t *testing.T) {
	in, _ := ParseInput([]byte(`{"rate_limits":{"five_hour":{"used_percentage":1776950400,"resets_at":1776950400}}}`))
	if five, _ := in.StdinLimits(); five != nil {
		t.Fatalf("expected nil, got %+v", five)
	}
}

func TestParseInputEmpty(t *testing.T) {
	in, err := ParseInput(nil)
	if err != nil || in == nil {
		t.Fatal(err)
	}
	if a, b := in.StdinLimits(); a != nil || b != nil {
		t.Fatal("expected no limits")
	}
}

func TestUseStdinAuto(t *testing.T) {
	now := time.Now()
	p := profile(config.SourceAuto)
	st := &store.StateFile{}
	if UseStdin(p, st, false, now) {
		t.Fatal("team-like profile should use API")
	}
	st.StdinLimitsSeen = now.Add(-time.Hour)
	if !UseStdin(p, st, false, now) {
		t.Fatal("recently seen stdin limits should keep stdin mode")
	}
}

func TestNeedRefreshStdinOnlyWhenExhausted(t *testing.T) {
	now := time.Now()
	p := profile(config.SourceStdin)
	st, uf := &store.StateFile{}, &store.UsageFile{}
	lim := Limits{FiveHour: &store.Window{Percent: 80}, FromStdin: true}
	if NeedRefresh(p, true, lim, st, uf, now) {
		t.Fatal("should not call API below 100%")
	}
	lim.FiveHour.Percent = 100
	if !NeedRefresh(p, true, lim, st, uf, now) {
		t.Fatal("should call API at 100%")
	}
	st.SpawnedAt = now.Add(-10 * time.Second)
	if NeedRefresh(p, true, lim, st, uf, now) {
		t.Fatal("spawn gap not respected")
	}
}

func TestNeedRefreshAPIPolling(t *testing.T) {
	now := time.Now()
	p := profile(config.SourceAPI)
	st := &store.StateFile{}
	uf := &store.UsageFile{Usage: &store.Usage{FetchedAt: now.Add(-time.Minute)}}
	if NeedRefresh(p, false, Limits{}, st, uf, now) {
		t.Fatal("fresh cache should not refresh")
	}
	uf.Usage.FetchedAt = now.Add(-6 * time.Minute)
	if !NeedRefresh(p, false, Limits{}, st, uf, now) {
		t.Fatal("stale cache should refresh")
	}
	uf.BackoffUntil = now.Add(time.Minute)
	if NeedRefresh(p, false, Limits{}, st, uf, now) {
		t.Fatal("backoff not respected")
	}
}

func usage(credits float64, pct float64) *store.Usage {
	return &store.Usage{
		FiveHour: &store.Window{Percent: pct},
		Extra:    &store.Extra{Enabled: true, UsedCredits: f(credits), MonthlyLimit: f(5000)},
	}
}

func TestCreditBaselineAndSpending(t *testing.T) {
	now := time.Now()
	p := profile(config.SourceAPI)
	uf := &store.UsageFile{}

	ApplyFetch(uf, usage(1000, 50), false, "", now)
	if uf.Baseline != nil {
		t.Fatal("no baseline below limit")
	}
	lim := Limits{FiveHour: &store.Window{Percent: 100}}
	ApplyFetch(uf, usage(1000, 100), true, "5h", now.Add(time.Minute))
	if uf.Baseline == nil || uf.Baseline.Credits != 1000 {
		t.Fatalf("baseline: %+v", uf.Baseline)
	}
	cv := Credits(p, lim, uf, now.Add(time.Minute))
	if !cv.Show || cv.Spending || cv.Used != 10 {
		t.Fatalf("before spend: %+v", cv)
	}
	ApplyFetch(uf, usage(1080, 100), true, "5h", now.Add(6*time.Minute))
	cv = Credits(p, lim, uf, now.Add(6*time.Minute))
	if !cv.Spending || cv.SpentWindow < 0.79 || cv.SpentWindow > 0.81 {
		t.Fatalf("after spend: %+v", cv)
	}
	ApplyFetch(uf, usage(1080, 3), false, "", now.Add(5*time.Hour))
	if uf.Baseline != nil {
		t.Fatal("baseline should clear after window reset")
	}
}

// API 모드는 이미 usage 를 폴링하므로 크레딧을 숨길 이유가 없다. stdin 모드는
// 한도 전에 보여주려면 API 를 더 불러야 하므로 설정을 켰을 때만 보인다.
func TestCreditsShownInAPIMode(t *testing.T) {
	now := time.Now()
	uf := &store.UsageFile{}
	ApplyFetch(uf, usage(1000, 30), false, "", now)

	api := Credits(profile(config.SourceAPI), Limits{
		FiveHour: &store.Window{Percent: 30}, FromStdin: false}, uf, now)
	if !api.Show || !api.Enabled || api.Used != 10 {
		t.Errorf("API 모드는 한도 전에도 크레딧을 보여야 한다: %+v", api)
	}

	stdin := Credits(profile(config.SourceStdin), Limits{
		FiveHour: &store.Window{Percent: 30}, FromStdin: true}, uf, now)
	if stdin.Show {
		t.Errorf("stdin 모드는 설정 없이 보이면 안 된다: %+v", stdin)
	}

	p := profile(config.SourceStdin)
	p.AlwaysShowCredits = true
	if on := Credits(p, Limits{FiveHour: &store.Window{Percent: 30}, FromStdin: true}, uf, now); !on.Show {
		t.Errorf("always_show_credits 를 켜면 stdin 모드도 보여야 한다: %+v", on)
	}
}

// 크레딧이 비활성이거나 응답에 없으면 API 모드여도 줄을 내지 않는다 — 매 렌더마다
// "크레딧 비활성" 이 붙으면 그게 노이즈다.
func TestCreditsHiddenWhenExtraAbsent(t *testing.T) {
	now := time.Now()
	uf := &store.UsageFile{}
	ApplyFetch(uf, &store.Usage{FetchedAt: now}, false, "", now) // extra 없음
	cv := Credits(profile(config.SourceAPI), Limits{
		FiveHour: &store.Window{Percent: 30}, FromStdin: false}, uf, now)
	if cv.Show || cv.Enabled {
		t.Errorf("크레딧 정보가 없으면 줄을 내지 않는다: %+v", cv)
	}
}

func TestExpiredWindowDropped(t *testing.T) {
	now := time.Now()
	st := &store.StateFile{ObservedAt: now.Add(-time.Hour), FiveHour: &store.Window{Percent: 100, ResetsAt: now.Add(-time.Minute)}}
	lim := Merge(true, nil, nil, st, &store.UsageFile{}, now)
	if hit, _ := lim.Exhausted(); hit {
		t.Fatal("reset window must not count as exhausted")
	}
}

func TestGuard(t *testing.T) {
	now := time.Now()
	p := profile(config.SourceAPI)
	lim := Limits{FiveHour: &store.Window{Percent: 100}}
	uf := &store.UsageFile{}
	allow := &store.AllowFile{}

	if d := Guard(p, lim, uf, allow, now); !d.Block || !strings.Contains(d.Reason, "5h") {
		t.Fatalf("should block unknown-credit exhaustion: %+v", d)
	}
	allow.AllowUntil = now.Add(time.Minute)
	if Guard(p, lim, uf, allow, now).Block {
		t.Fatal("allow window ignored")
	}
	allow.AllowUntil = time.Time{}
	uf.Usage = &store.Usage{Extra: &store.Extra{Enabled: false}}
	if Guard(p, lim, uf, allow, now).Block {
		t.Fatal("credits disabled: nothing to guard")
	}
	p.Guard = false
	uf.Usage = nil
	if Guard(p, lim, uf, allow, now).Block {
		t.Fatal("guard disabled")
	}
}

func TestBackoff(t *testing.T) {
	now := time.Now()
	uf := &store.UsageFile{}
	for i := 0; i < 10; i++ {
		ApplyFailure(uf, errTest{}, 0, now)
	}
	if d := uf.BackoffUntil.Sub(now); d != 30*time.Minute {
		t.Fatalf("cap: %v", d)
	}
	ApplyFailure(uf, errTest{}, 2*time.Hour, now)
	if d := uf.BackoffUntil.Sub(now); d != 2*time.Hour {
		t.Fatalf("retry-after: %v", d)
	}
}

type errTest struct{}

func (errTest) Error() string { return "x" }
