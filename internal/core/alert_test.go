package core

import (
	"testing"
	"time"

	"cc-usage/internal/config"
	"cc-usage/internal/store"
)

func alertProfile(t *testing.T) *config.Config {
	t.Helper()
	p := &config.Config{}
	p.ApplyDefaults() // alert_percent 기본 90
	return p
}

func TestAlertsEdgeFiresOnce(t *testing.T) {
	p := alertProfile(t)
	st := &store.StateFile{}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)

	below := Limits{FiveHour: &store.Window{Percent: 80, ResetsAt: reset}}
	if a, dirty := Alerts(p, below, st, now); a.Level != AlertNone || dirty {
		t.Fatalf("80%%는 조용해야 한다: %+v dirty=%v", a, dirty)
	}

	near := Limits{FiveHour: &store.Window{Percent: 93, ResetsAt: reset}}
	a, dirty := Alerts(p, near, st, now)
	if a.Level != AlertNear || !a.Burst || !dirty {
		t.Fatalf("임박 엣지: %+v dirty=%v", a, dirty)
	}
	// 같은 틱의 두 번째 호출(statusline은 초당 여러 번 돈다)에서는 다시 쏘지 않는다.
	a, dirty = Alerts(p, near, st, now.Add(80*time.Millisecond))
	if dirty {
		t.Fatalf("같은 단계에서 state를 다시 쓰지 않는다: %+v dirty=%v", a, dirty)
	}
	if !a.Burst {
		t.Error("burst 구간 안에서는 계속 깜빡여야 한다")
	}

	// burst가 끝나면 움직임은 멈추고 단계만 남는다.
	if a, _ := Alerts(p, near, st, now.Add(AlertBurst)); a.Burst || a.Level != AlertNear {
		t.Errorf("burst 종료 후: %+v", a)
	}

	// 임박 → 소진은 새 엣지다.
	over := Limits{FiveHour: &store.Window{Percent: 100, ResetsAt: reset}}
	if a, d := Alerts(p, over, st, now.Add(time.Minute)); a.Level != AlertOver || !d {
		t.Errorf("소진 엣지: %+v", a)
	}
}

func TestAlertsRearmsOnReset(t *testing.T) {
	p := alertProfile(t)
	st := &store.StateFile{}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	first := Limits{FiveHour: &store.Window{Percent: 100, ResetsAt: now.Add(time.Hour)}}
	if _, d := Alerts(p, first, st, now); !d {
		t.Fatal("첫 소진은 새 단계라 state에 기록돼야 한다")
	}
	// 창이 리셋돼 알림 조건이 사라지면 state를 비운다.
	if a, dirty := Alerts(p, Limits{}, st, now.Add(time.Hour)); a.Level != AlertNone || !dirty || st.AlertKey != "" {
		t.Fatalf("리셋 후 무장 해제: %+v dirty=%v key=%q", a, dirty, st.AlertKey)
	}
	// 다음 창에서 다시 100%면 새 엣지다 (resets_at이 달라 키가 다르다).
	second := Limits{FiveHour: &store.Window{Percent: 100, ResetsAt: now.Add(6 * time.Hour)}}
	if _, d := Alerts(p, second, st, now.Add(2*time.Hour)); !d {
		t.Error("다음 창은 새 키라 다시 기록돼야 한다")
	}
}

func TestAlertsWorstWindowWins(t *testing.T) {
	p := alertProfile(t)
	lim := Limits{
		FiveHour: &store.Window{Percent: 95},
		SevenDay: &store.Window{Percent: 100},
	}
	a, _ := Alerts(p, lim, &store.StateFile{}, time.Now())
	if a.Level != AlertOver || a.Window != "7d" {
		t.Errorf("소진이 임박을 이겨야 한다: %+v", a)
	}
}

func TestAlertsSameLevelPrefersSevenDay(t *testing.T) {
	p := alertProfile(t)
	st := &store.StateFile{}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	// 5h가 먼저 임박에 닿아 경보를 잡는다.
	only5h := Limits{FiveHour: &store.Window{Percent: 95, ResetsAt: now.Add(time.Hour)}}
	if a, _ := Alerts(p, only5h, st, now); a.Window != "5h" {
		t.Fatalf("5h만 임박: %+v", a)
	}
	// 나중에 7d가 같은 단계에 닿으면 7d로 넘어가고 새 엣지가 된다.
	// (풀리는 데 더 오래 걸리는 쪽이 아프다.)
	both := Limits{
		FiveHour: &store.Window{Percent: 95, ResetsAt: now.Add(time.Hour)},
		SevenDay: &store.Window{Percent: 95, ResetsAt: now.Add(72 * time.Hour)},
	}
	a, _ := Alerts(p, both, st, now.Add(time.Minute))
	if a.Window != "7d" {
		t.Errorf("같은 단계면 7d가 이겨야 한다: %+v", a)
	}
	if !a.Burst {
		t.Error("7d로 넘어간 것은 새 단계라 깜빡임이 다시 시작돼야 한다")
	}
}

func TestAlertsNearDisabled(t *testing.T) {
	off := 0.0
	p := &config.Config{AlertPercent: &off} // 0 → 임박 경고 끔
	p.ApplyDefaults()
	lim := Limits{FiveHour: &store.Window{Percent: 99}}
	if a, _ := Alerts(p, lim, &store.StateFile{}, time.Now()); a.Level != AlertNone {
		t.Errorf("임박을 껐으면 99%%도 조용해야 한다: %+v", a)
	}
	// 소진 강조는 남는다.
	lim = Limits{FiveHour: &store.Window{Percent: 100}}
	if a, _ := Alerts(p, lim, &store.StateFile{}, time.Now()); a.Level != AlertOver {
		t.Errorf("소진은 여전히 잡아야 한다: %+v", a)
	}
}
