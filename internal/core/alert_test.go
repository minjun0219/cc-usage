package core

import (
	"testing"
	"time"

	"cc-usage/internal/config"
	"cc-usage/internal/store"
)

func alertProfile(t *testing.T) *config.Profile {
	t.Helper()
	p := &config.Profile{Name: "p"}
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
	if a.Level != AlertNear || !a.Fired || !a.Burst || !dirty {
		t.Fatalf("임박 엣지: %+v dirty=%v", a, dirty)
	}
	// 같은 틱의 두 번째 호출(statusline은 초당 여러 번 돈다)에서는 다시 쏘지 않는다.
	a, dirty = Alerts(p, near, st, now.Add(80*time.Millisecond))
	if a.Fired || dirty {
		t.Fatalf("엣지는 한 번만: %+v dirty=%v", a, dirty)
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
	if a, _ := Alerts(p, over, st, now.Add(time.Minute)); a.Level != AlertOver || !a.Fired {
		t.Errorf("소진 엣지: %+v", a)
	}
}

func TestAlertsRearmsOnReset(t *testing.T) {
	p := alertProfile(t)
	st := &store.StateFile{}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	first := Limits{FiveHour: &store.Window{Percent: 100, ResetsAt: now.Add(time.Hour)}}
	if a, _ := Alerts(p, first, st, now); !a.Fired {
		t.Fatal("첫 소진에서 쏴야 한다")
	}
	// 창이 리셋돼 알림 조건이 사라지면 state를 비운다.
	if a, dirty := Alerts(p, Limits{}, st, now.Add(time.Hour)); a.Level != AlertNone || !dirty || st.AlertKey != "" {
		t.Fatalf("리셋 후 무장 해제: %+v dirty=%v key=%q", a, dirty, st.AlertKey)
	}
	// 다음 창에서 다시 100%면 새 엣지다 (resets_at이 달라 키가 다르다).
	second := Limits{FiveHour: &store.Window{Percent: 100, ResetsAt: now.Add(6 * time.Hour)}}
	if a, _ := Alerts(p, second, st, now.Add(2*time.Hour)); !a.Fired {
		t.Error("다음 창에서 다시 쏴야 한다")
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

func TestAlertsNearDisabled(t *testing.T) {
	p := &config.Profile{Name: "p", AlertPercent: -1} // 범위 밖 → 임박 경고 끔
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
