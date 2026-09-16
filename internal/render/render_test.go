package render

import (
	"strings"
	"testing"
	"time"

	"cc-usage/internal/config"
	"cc-usage/internal/core"
	"cc-usage/internal/store"
)

func TestDuration(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second: "<1m",
		45 * time.Minute: "45m",
		80 * time.Minute: "1h20m",
		52 * time.Hour:   "2d4h",
	}
	for d, want := range cases {
		if got := Duration(d); got != want {
			t.Errorf("%v: got %s want %s", d, got, want)
		}
	}
}

func TestLinesSpending(t *testing.T) {
	now := time.Now()
	p := &config.Profile{Name: "work", Label: "work"}
	p.ApplyDefaults()
	used, limit := 1080.0, 5000.0
	uf := &store.UsageFile{
		Usage:           &store.Usage{FetchedAt: now, Extra: &store.Extra{Enabled: true, UsedCredits: &used, MonthlyLimit: &limit}},
		Baseline:        &store.Baseline{WindowKey: "5h", Credits: 1000},
		CreditsRisingAt: now,
	}
	lim := core.Limits{FiveHour: &store.Window{Percent: 100, ResetsAt: now.Add(80 * time.Minute)}}
	pct := 42.0
	lines := Lines(View{
		Profile: p, Model: "Sonnet", ContextPct: &pct, Limits: lim, Usage: uf,
		Credits: core.Credits(p, lim, uf, now), Now: now,
	}, Style{})
	if len(lines) != 2 {
		t.Fatalf("lines: %q", lines)
	}
	if !strings.Contains(lines[0], "5h 100% 1h20m") || !strings.Contains(lines[0], "ctx 42%") {
		t.Errorf("line1: %s", lines[0])
	}
	if !strings.Contains(lines[1], "$10.80 / $50.00") || !strings.Contains(lines[1], "+$0.80") || !strings.Contains(lines[1], "소진 중") {
		t.Errorf("line2: %s", lines[1])
	}
}

func TestLinesQuietBelowLimit(t *testing.T) {
	now := time.Now()
	p := &config.Profile{Name: "me"}
	p.ApplyDefaults()
	lim := core.Limits{FiveHour: &store.Window{Percent: 30}, FromStdin: true}
	uf := &store.UsageFile{}
	lines := Lines(View{Profile: p, Limits: lim, Usage: uf, Credits: core.Credits(p, lim, uf, now), Now: now}, Style{})
	if len(lines) != 1 {
		t.Fatalf("expected single line, got %q", lines)
	}
}
