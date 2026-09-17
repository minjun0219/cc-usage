// Package core holds the pure decision logic shared by statusline, refresh and guard.
package core

import (
	"time"

	"cc-usage/internal/config"
	"cc-usage/internal/store"
)

const (
	// stdinRecent: in auto mode, a profile that showed stdin rate_limits this recently is treated as stdin-backed.
	stdinRecent = 6 * time.Hour
	// minSpawnGap avoids spawning refresh on every statusline tick.
	minSpawnGap = 30 * time.Second
	// risingWindow keeps the "credits being spent" warning visible after an increase.
	risingWindow = 15 * time.Minute
	// StaleAfter marks API data as stale.
	StaleAfter = 30 * time.Minute
)

// Limits is the merged view the UI and guard work from.
type Limits struct {
	FiveHour  *store.Window
	SevenDay  *store.Window
	FromStdin bool
}

// Peak is the highest usage percentage across windows. 폴링 간격을 고를 때
// "가장 급한 창" 하나만 보면 되므로 이 값으로 충분하다.
func (l Limits) Peak() float64 {
	var peak float64
	for _, w := range []*store.Window{l.FiveHour, l.SevenDay} {
		if w != nil && w.Percent > peak {
			peak = w.Percent
		}
	}
	return peak
}

// UseStdin reports whether limits should come from stdin for this profile.
func UseStdin(p *config.Config, st *store.StateFile, stdinPresent bool, now time.Time) bool {
	switch p.Source {
	case config.SourceStdin:
		return true
	case config.SourceAPI:
		return false
	}
	if stdinPresent {
		return true
	}
	return !st.StdinLimitsSeen.IsZero() && now.Sub(st.StdinLimitsSeen) < stdinRecent
}

// Merge picks stdin windows when available, otherwise the last recent state, otherwise API data.
func Merge(useStdin bool, five, seven *store.Window, st *store.StateFile, uf *store.UsageFile, now time.Time) Limits {
	if useStdin {
		if five != nil || seven != nil {
			return Limits{FiveHour: dropExpired(five, now), SevenDay: dropExpired(seven, now), FromStdin: true}
		}
		if now.Sub(st.ObservedAt) < stdinRecent {
			return Limits{FiveHour: dropExpired(st.FiveHour, now), SevenDay: dropExpired(st.SevenDay, now), FromStdin: true}
		}
		return Limits{FromStdin: true}
	}
	if uf.Usage == nil {
		return Limits{}
	}
	return Limits{FiveHour: dropExpired(uf.Usage.FiveHour, now), SevenDay: dropExpired(uf.Usage.SevenDay, now)}
}

func dropExpired(w *store.Window, now time.Time) *store.Window {
	if w == nil {
		return nil
	}
	if !w.ResetsAt.IsZero() && now.After(w.ResetsAt) {
		return nil // the window has reset; the old percentage no longer applies
	}
	return w
}

// Exhausted reports whether any window is at 100% and returns a key identifying that window.
func (l Limits) Exhausted() (bool, string) {
	for _, c := range []struct {
		name string
		w    *store.Window
	}{{"7d", l.SevenDay}, {"5h", l.FiveHour}} {
		if c.w != nil && c.w.Percent >= 100 {
			return true, windowKey(c.name, c.w)
		}
	}
	return false, ""
}

// windowKey identifies a window *instance* — 같은 5h라도 리셋되면 다른 키다.
func windowKey(name string, w *store.Window) string {
	if w.ResetsAt.IsZero() {
		return name
	}
	return name + "@" + w.ResetsAt.UTC().Truncate(time.Minute).Format(time.RFC3339)
}

// NeedRefresh decides whether statusline should spawn `cc-usage refresh`.
// 여유 구간에서 간격을 늘리는 배수와 그 경계. 경계를 alert_percent 가 아니라
// 따로 둔 것은, 임박 경고를 꺼도(alert_percent=0) 폴링은 여전히 촘촘해져야 하기
// 때문이다. 70 은 색이 노래지기 시작하는 지점과 같다 — 화면과 동작이 같은 말을 한다.
const (
	idlePollFactor = 3
	idleBelow      = 70
)

// pollInterval decides how often the usage API may be called. 한도에 여유가
// 있으면 간격을 늘린다 — 크레딧이 깎일 일이 없는 구간에서 같은 주기로 부를 이유가
// 없고, 이 API 는 rate limit 이 낮다.
func pollInterval(p *config.Config, lim Limits) time.Duration {
	switch peak := lim.Peak(); {
	case peak >= 100:
		return p.CreditPoll() // 소진 — 크레딧이 실제로 깎이는 중이다
	case peak >= idleBelow:
		return p.Poll()
	default:
		iv := p.Poll() * idlePollFactor
		// 우리가 늘린 간격이 StaleAfter 를 넘으면 여유 구간 내내 "⚠︎ stale" 이
		// 뜬다 — 화면은 데이터가 낡았다고 말하는데 폴링은 쉬고 있는 상태다.
		// 사용자가 직접 정한 Poll() 이 이미 그보다 길면 그건 그쪽 선택이다.
		if limit := max(p.Poll(), StaleAfter); iv > limit {
			iv = limit
		}
		return iv
	}
}

func NeedRefresh(p *config.Config, useStdin bool, lim Limits, st *store.StateFile, uf *store.UsageFile, now time.Time) bool {
	if now.Before(uf.BackoffUntil) || now.Sub(st.SpawnedAt) < minSpawnGap || now.Sub(uf.LastAttempt) < minSpawnGap {
		return false
	}
	age := time.Duration(1<<62 - 1)
	if uf.Usage != nil {
		age = now.Sub(uf.Usage.FetchedAt)
	}
	if !useStdin {
		return age >= pollInterval(p, lim)
	}
	hit, _ := lim.Exhausted()
	if hit || p.AlwaysShowCredits {
		return age >= pollInterval(p, lim)
	}
	return false
}

// ApplyFetch updates credit tracking after a successful fetch. hit/key describe the limit state at fetch time.
func ApplyFetch(uf *store.UsageFile, u *store.Usage, hit bool, key string, now time.Time) {
	uf.Usage = u
	uf.LastAttempt = now
	uf.LastError = ""
	uf.Failures = 0
	uf.BackoffUntil = time.Time{}

	credits, ok := usedCredits(u)
	if !ok {
		uf.PrevCredits = nil
		if !hit {
			uf.Baseline = nil
		}
		return
	}
	if uf.PrevCredits != nil && credits > *uf.PrevCredits {
		uf.CreditsRisingAt = now
	}
	if credits < 0 || (uf.PrevCredits != nil && credits < *uf.PrevCredits) {
		uf.Baseline = nil // monthly reset or correction
	}
	c := credits
	uf.PrevCredits = &c

	switch {
	case !hit:
		uf.Baseline = nil
	case uf.Baseline == nil || uf.Baseline.WindowKey != key:
		uf.Baseline = &store.Baseline{WindowKey: key, Credits: credits, At: now}
	}
}

// ApplyFailure records an error with exponential backoff (or Retry-After when given).
func ApplyFailure(uf *store.UsageFile, err error, retryAfter time.Duration, now time.Time) {
	uf.LastAttempt = now
	uf.LastError = err.Error()
	uf.Failures++
	d := time.Minute << min(uf.Failures-1, 5) // 1m .. 32m
	if d > 30*time.Minute {
		d = 30 * time.Minute
	}
	if retryAfter > d {
		d = retryAfter
	}
	uf.BackoffUntil = now.Add(d)
}

func usedCredits(u *store.Usage) (float64, bool) {
	if u == nil || u.Extra == nil || !u.Extra.Enabled || u.Extra.UsedCredits == nil {
		return 0, false
	}
	return *u.Extra.UsedCredits, true
}

// CreditView is what the UI shows on the credit line.
type CreditView struct {
	Show        bool
	Enabled     bool
	Used        float64 // converted with CreditDivisor
	Limit       *float64
	SpentWindow float64 // since baseline, converted
	Spending    bool
}

func Credits(p *config.Config, lim Limits, uf *store.UsageFile, now time.Time) CreditView {
	v := CreditView{}
	credits, ok := usedCredits(uf.Usage)
	hit, _ := lim.Exhausted()
	if !ok {
		v.Show = hit // limit hit but credits unknown yet
		return v
	}
	v.Enabled = true
	v.Used = credits / p.CreditDivisor
	if l := uf.Usage.Extra.MonthlyLimit; l != nil {
		conv := *l / p.CreditDivisor
		v.Limit = &conv
	}
	if uf.Baseline != nil && hit {
		v.SpentWindow = (credits - uf.Baseline.Credits) / p.CreditDivisor
	}
	rising := !uf.CreditsRisingAt.IsZero() && now.Sub(uf.CreditsRisingAt) < risingWindow
	v.Spending = v.SpentWindow > 0 || rising
	// 쓴 크레딧이 있으면 상시 보여준다. 0 이면 줄을 내지 않는다 — "$0.00" 은
	// 자리만 먹고 아무것도 말하지 않는다. 임박·소진에서 강조가 붙는 것은 그대로다
	// (creditLine 이 판단한다).
	v.Show = hit || v.Spending || p.AlwaysShowCredits || v.Used > 0
	return v
}

// GuardDecision says whether a prompt should be blocked.
type GuardDecision struct {
	Block  bool
	Reason string
}

func Guard(p *config.Config, lim Limits, uf *store.UsageFile, allow *store.AllowFile, now time.Time) GuardDecision {
	if !p.Guard || now.Before(allow.AllowUntil) {
		return GuardDecision{}
	}
	hit, key := lim.Exhausted()
	cv := Credits(p, lim, uf, now)
	if hit {
		if uf.Usage != nil && uf.Usage.Extra != nil && !uf.Usage.Extra.Enabled {
			return GuardDecision{} // no credits to spend; Claude Code will block by itself
		}
		return GuardDecision{Block: true, Reason: "사용량 한도 소진 (" + key + ") — 이 prompt부터 크레딧이 차감됩니다"}
	}
	if cv.Spending {
		return GuardDecision{Block: true, Reason: "최근 크레딧 소진이 감지됐습니다"}
	}
	return GuardDecision{}
}
