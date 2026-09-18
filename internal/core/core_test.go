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
	p := profile(config.SourceAPI) // poll=300s, credit_poll=300s
	st := &store.StateFile{}
	busy := Limits{FiveHour: &store.Window{Percent: 80}} // 여유 아님 → 기본 주기
	uf := &store.UsageFile{Usage: &store.Usage{FetchedAt: now.Add(-time.Minute)}}
	if NeedRefresh(p, false, busy, st, uf, now) {
		t.Fatal("fresh cache should not refresh")
	}
	uf.Usage.FetchedAt = now.Add(-6 * time.Minute)
	if !NeedRefresh(p, false, busy, st, uf, now) {
		t.Fatal("stale cache should refresh")
	}
	uf.BackoffUntil = now.Add(time.Minute)
	if NeedRefresh(p, false, busy, st, uf, now) {
		t.Fatal("backoff not respected")
	}
}

// 한도에 여유가 있으면 간격을 늘려 API 를 아낀다. 이 API 는 rate limit 이 낮다.
func TestPollIntervalWidensWhenIdle(t *testing.T) {
	p := profile(config.SourceAPI)
	for _, c := range []struct {
		name string
		peak float64
		want time.Duration
	}{
		{"여유", 30, p.Poll() * idlePollFactor},
		{"경계 직전", idleBelow - 1, p.Poll() * idlePollFactor},
		{"임박 구간", idleBelow, p.Poll()},
		{"소진", 100, p.CreditPoll()},
	} {
		lim := Limits{FiveHour: &store.Window{Percent: c.peak}}
		if got := pollInterval(p, lim); got != c.want {
			t.Errorf("%s(peak=%v): got %v want %v", c.name, c.peak, got, c.want)
		}
	}
	// 두 창 중 급한 쪽을 본다.
	lim := Limits{FiveHour: &store.Window{Percent: 10}, SevenDay: &store.Window{Percent: 95}}
	if got := pollInterval(p, lim); got != p.Poll() {
		t.Errorf("7d 가 급하면 그쪽을 따라야 한다: %v", got)
	}
}

// 여유 구간에서는 같은 stale 정도라도 아직 부르지 않는다.
func TestNeedRefreshHoldsOffWhenIdle(t *testing.T) {
	now := time.Now()
	p := profile(config.SourceAPI)
	st := &store.StateFile{}
	idle := Limits{FiveHour: &store.Window{Percent: 20}}
	uf := &store.UsageFile{Usage: &store.Usage{FetchedAt: now.Add(-6 * time.Minute)}}
	if NeedRefresh(p, false, idle, st, uf, now) {
		t.Error("여유 구간에서 6분은 아직 이르다 (300s x3 = 15분)")
	}
	uf.Usage.FetchedAt = now.Add(-16 * time.Minute)
	if !NeedRefresh(p, false, idle, st, uf, now) {
		t.Error("16분이 지나면 부른다")
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

// 기준은 모드가 아니라 "쓴 크레딧이 있는가" 다. 0 이면 "$0.00" 이 자리만 먹으므로
// 내지 않고, 0 이 아니면 한도에 여유가 있어도 보여준다.
func TestCreditsShownWhenNonZero(t *testing.T) {
	now := time.Now()
	idle := Limits{FiveHour: &store.Window{Percent: 30}}

	spent := &store.UsageFile{}
	ApplyFetch(spent, usage(1000, 30), false, "", now)
	if cv := Credits(profile(config.SourceAPI), idle, spent, now); !cv.Show || cv.Used != 10 {
		t.Errorf("쓴 크레딧이 있으면 한도 전에도 보여야 한다: %+v", cv)
	}

	zero := &store.UsageFile{}
	ApplyFetch(zero, usage(0, 30), false, "", now)
	cv := Credits(profile(config.SourceAPI), idle, zero, now)
	if cv.Show {
		t.Errorf("크레딧 0 이면 줄을 내지 않는다: %+v", cv)
	}
	if !cv.Enabled {
		t.Errorf("0 이어도 크레딧 기능 자체는 켜져 있다: %+v", cv)
	}

	// 0 이라도 한도가 소진되면 기존대로 나온다 (크레딧으로 넘어가는 시점이라).
	hit := Limits{FiveHour: &store.Window{Percent: 100}}
	if cv := Credits(profile(config.SourceAPI), hit, zero, now); !cv.Show {
		t.Errorf("소진이면 크레딧 0 이어도 보여야 한다: %+v", cv)
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

func TestPollIntervalNeverOutlastsStale(t *testing.T) {
	lim := Limits{FiveHour: &store.Window{Percent: 30}} // 여유 구간 — idle 배수가 붙는다
	for _, c := range []struct {
		secs int
		want time.Duration
	}{
		{300, 15 * time.Minute},  // 300×3 = 15m, StaleAfter 안쪽
		{600, 30 * time.Minute},  // 600×3 = 30m 로 늘리면 임계와 같아진다 → 캡
		{900, 30 * time.Minute},  // 45m 가 될 것을 StaleAfter 로 자른다
		{2400, 40 * time.Minute}, // Poll 자체가 임계를 넘으면 그건 사용자 선택이다
	} {
		cfg := &config.Config{PollSeconds: c.secs}
		cfg.ApplyDefaults()
		if got := pollInterval(cfg, lim); got != c.want {
			t.Errorf("poll_seconds=%d: got %v want %v", c.secs, got, c.want)
		}
	}
}

func TestAccountCheckFollowsLimits(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	const src = "/home/u/.claude.json"
	st := &store.StateFile{}
	a := Limits{FiveHour: &store.Window{Percent: 40}, SevenDay: &store.Window{Percent: 20}}

	// 첫 렌더는 저장된 키가 비어 있으니 반드시 확인한다.
	if !NeedAccountCheck(src, a, st, now) {
		t.Fatal("첫 렌더에서 확인해야 한다")
	}
	st.AccountAt, st.AccountCheckedAt = AccountKey(src, a), now

	// 한도가 그대로고 TTL 안이면 파일을 읽지 않는다 — 여기서 성능을 산다.
	if NeedAccountCheck(src, a, st, now.Add(time.Second)) {
		t.Error("한도가 같고 TTL 안이면 다시 읽지 않는다")
	}
	// 값이 움직이면 계정이 바뀌었을 수 있으니 곧바로 다시 읽는다.
	b := Limits{FiveHour: &store.Window{Percent: 41}, SevenDay: &store.Window{Percent: 20}}
	if !NeedAccountCheck(src, b, st, now.Add(time.Second)) {
		t.Error("한도가 바뀌면 다시 읽는다")
	}
	// 창이 하나도 없어도 키가 비지 않아야 첫 렌더 판정이 성립한다.
	if k := (Limits{}).Key(); k == "" {
		t.Error("빈 Limits 의 키가 비면 첫 렌더를 구분할 수 없다")
	}
	// 창이 사라지는 것도 변화다 (stdin 이 끊긴 경우 등).
	if !NeedAccountCheck(src, Limits{}, st, now) {
		t.Error("창이 사라진 것도 변화로 본다")
	}
}

func TestAccountCheckHasCeiling(t *testing.T) {
	// Key() 는 퍼센트를 정수로 반올림한다. 저사용 구간에서는 값이 달라도 같은
	// 키가 되어(3.2% 와 3.4% 가 모두 "3"), 한도 변화만으로는 다시 읽는다는
	// 보장이 없다 — 계정이 바뀌어도 배지가 영영 옛 것으로 남을 수 있다.
	// 그래서 한도와 무관한 천장을 둔다.
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	low := Limits{FiveHour: &store.Window{Percent: 3.2}}
	same := Limits{FiveHour: &store.Window{Percent: 3.4}}
	if low.Key() != same.Key() {
		t.Fatal("이 테스트의 전제(반올림 충돌)가 깨졌다")
	}
	const src = "/home/u/.claude.json"
	st := &store.StateFile{AccountAt: AccountKey(src, low), AccountCheckedAt: now}

	if NeedAccountCheck(src, same, st, now.Add(30*time.Second)) {
		t.Error("TTL 안에서는 읽지 않는다")
	}
	if !NeedAccountCheck(src, same, st, now.Add(accountTTL)) {
		t.Error("TTL 을 넘기면 한도가 그대로여도 읽는다")
	}
}

func TestAccountKeyIncludesSource(t *testing.T) {
	// state.json 은 XDG_CACHE_HOME 을 나누지 않으면 두 세션이 공유한다.
	// 이메일만 캐시하면 한도 키가 우연히 같을 때(저사용·0% 구간은 흔하다)
	// 한쪽이 다른 쪽의 이메일로 배지를 그린다 — 이 기능이 막으려던 바로 그
	// "조용히 틀린 배지" 다. 보고 있는 계정 파일이 다르면 키도 달라야 한다.
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	lim := Limits{FiveHour: &store.Window{Percent: 0}} // 두 계정이 흔히 겹치는 구간
	work := "/home/u/.claude-work/.claude.json"
	personal := "/home/u/.claude.json"

	// 회사 세션이 먼저 캐시를 채운다.
	st := &store.StateFile{AccountEmail: "work@example.com",
		AccountAt: AccountKey(work, lim), AccountCheckedAt: now}
	// 같은 state.json 을 보는 개인 세션은 그 값을 그대로 쓰면 안 된다.
	if !NeedAccountCheck(personal, lim, st, now) {
		t.Error("보는 계정 파일이 다르면 캐시를 재사용하면 안 된다")
	}
	if NeedAccountCheck(work, lim, st, now) {
		t.Error("같은 계정 파일이면 캐시를 쓴다")
	}
}
