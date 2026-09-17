package core

import (
	"fmt"
	"time"

	"cc-usage/internal/config"
	"cc-usage/internal/store"
)

// AlertLevel escalates with the worst window.
type AlertLevel int

const (
	AlertNone AlertLevel = iota
	AlertNear            // 임박
	AlertOver            // 소진
)

const (
	// AlertBurst: 단계가 올라간 직후 이만큼만 깜빡인다. 계속 움직이면 눈에
	// 띄는 게 아니라 무시하게 되므로, 그 뒤에는 굵은 빨강으로 가만히 남는다.
	AlertBurst = 6 * time.Second
	// alertFrame: statusline은 초당 여러 번 도니 벽시계에서 프레임을 고르면
	// 프레임 카운터를 저장하지 않고도 애니메이션이 된다.
	alertFrame = 500 * time.Millisecond
)

// Alert drives the statusline 강조.
type Alert struct {
	Level   AlertLevel
	Window  string // "5h" / "7d" — 강조할 세그먼트
	Percent float64
	Burst   bool // 지금이 깜빡이는 구간인가
	On      bool // burst 중 현재 프레임이 "켜짐"인가
}

// Alerts reports the current alert and updates st. dirty가 true면 state를 써야
// 한다. 단계가 올라간 시점(AlertAt)을 기록해 두어야 burst 구간을 알 수 있다.
func Alerts(p *config.Config, lim Limits, st *store.StateFile, now time.Time) (Alert, bool) {
	level, name, pct, wkey := worstWindow(p, lim)
	if level == AlertNone {
		if st.AlertKey == "" {
			return Alert{}, false
		}
		st.AlertKey, st.AlertAt = "", time.Time{} // 창이 리셋됐다 — 다시 무장
		return Alert{}, true
	}

	a := Alert{Level: level, Window: name, Percent: pct}
	key := fmt.Sprintf("%d@%s", level, wkey)
	dirty := false
	if st.AlertKey != key {
		st.AlertKey, st.AlertAt = key, now
		dirty = true
	}
	if d := now.Sub(st.AlertAt); d >= 0 && d < AlertBurst {
		a.Burst = true
		a.On = int(d/alertFrame)%2 == 0
	}
	return a, dirty
}

// worstWindow picks the most urgent window: 소진이 임박을 이기고, 같은 단계면
// 7d가 5h보다 아프다 (풀리는 데 더 오래 걸린다).
func worstWindow(p *config.Config, lim Limits) (AlertLevel, string, float64, string) {
	var (
		best AlertLevel
		name string
		pct  float64
		wkey string
	)
	for _, c := range []struct {
		name string
		w    *store.Window
		// 7d를 먼저 본다 — 같은 단계면 뒤에 오는 5h가 이기지 못해 7d가 남는다.
		// Exhausted()와 같은 순서다.
	}{{"7d", lim.SevenDay}, {"5h", lim.FiveHour}} {
		if c.w == nil {
			continue
		}
		l := AlertNone
		switch {
		case c.w.Percent >= 100:
			l = AlertOver
		case p.Alert() > 0 && c.w.Percent >= p.Alert():
			l = AlertNear
		}
		if l > best {
			best, name, pct, wkey = l, c.name, c.w.Percent, windowKey(c.name, c.w)
		}
	}
	return best, name, pct, wkey
}
