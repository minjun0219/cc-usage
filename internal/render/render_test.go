package render

import (
	"strings"
	"testing"
	"time"

	"cc-usage/internal/config"
	"cc-usage/internal/core"
	"cc-usage/internal/git"
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
	label := "work"
	p := &config.Profile{Name: "work", Label: &label}
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
	if !strings.Contains(lines[0], "5h 100% 1h20m→") || !strings.Contains(lines[0], "ctx 42%") {
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

func TestAbbrevHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cases := map[string]string{
		home:             "~",
		home + "/dev/x":  "~/dev/x",
		"/opt/other":     "/opt/other",
		home + "-work/x": home + "-work/x", // 접두사만 같은 다른 경로
	}
	for in, want := range cases {
		if got := AbbrevHome(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestDirLine(t *testing.T) {
	now := time.Now()
	p := &config.Profile{Name: "me"}
	p.ApplyDefaults()
	uf := &store.UsageFile{}
	lim := core.Limits{FiveHour: &store.Window{Percent: 30}, FromStdin: true}

	view := func(dir string, gs *git.Status) View {
		return View{Profile: p, Dir: dir, Git: gs, Limits: lim, Usage: uf,
			Credits: core.Credits(p, lim, uf, now), Now: now}
	}

	if lines := Lines(view("", nil), Style{}); len(lines) != 1 {
		t.Fatalf("no current_dir should drop the path row: %q", lines)
	}
	// git repo가 아니면 경로만, 브랜치 세그먼트는 생략한다.
	lines := Lines(view("/opt/x", nil), Style{})
	if len(lines) != 2 || lines[0] != "/opt/x" {
		t.Fatalf("lines: %q", lines)
	}
	lines = Lines(view("/opt/x", &git.Status{Branch: "main", Unstaged: 8,
		HasUpstream: true, Ahead: 1, Behind: 2}), Style{})
	if lines[0] != "/opt/x  ⎇ main !8 ⇡1⇣2" {
		t.Errorf("branch segment: %q", lines[0])
	}
	lines = Lines(view("/opt/x", &git.Status{Branch: "main", Conflicted: 1, Staged: 3, Unstaged: 5}), Style{})
	if lines[0] != "/opt/x  ⎇ main =1 +3 !5" {
		t.Errorf("counts: %q", lines[0])
	}
	// 업스트림이 없으면 ahead/behind는 나오지 않는다.
	lines = Lines(view("/opt/x", &git.Status{Branch: "main"}), Style{})
	if lines[0] != "/opt/x  ⎇ main" {
		t.Errorf("no upstream: %q", lines[0])
	}
	lines = Lines(view("/opt/x", &git.Status{Branch: "(detached)", Detached: true, OID: "1a2b3c4d5e"}), Style{})
	if lines[0] != "/opt/x  ⎇ @1a2b3c4" {
		t.Errorf("detached: %q", lines[0])
	}
}

func TestResetText(t *testing.T) {
	now := time.Date(2026, 9, 16, 16, 40, 0, 0, time.Local)
	if got := resetText(now.Add(80*time.Minute), now); got != "1h20m→18:00" {
		t.Errorf("within a day: %s", got)
	}
	// 하루를 넘기면 시각만으로 어느 날인지 알 수 없으니 남은 시간만 남긴다.
	if got := resetText(now.Add(52*time.Hour), now); got != "2d4h" {
		t.Errorf("beyond a day: %s", got)
	}
}

func TestLinesLabelHidden(t *testing.T) {
	now := time.Now()
	hidden := ""
	p := &config.Profile{Name: "personal", Label: &hidden}
	p.ApplyDefaults()
	uf := &store.UsageFile{}
	lim := core.Limits{FiveHour: &store.Window{Percent: 30}, FromStdin: true}
	lines := Lines(View{Profile: p, Model: "Opus 5", Limits: lim, Usage: uf,
		Credits: core.Credits(p, lim, uf, now), Now: now}, Style{})
	if lines[0] != "Opus 5 · 5h 30%" {
		t.Errorf("빈 label은 세그먼트를 통째로 생략해야 한다: %q", lines[0])
	}
	// 설정에 없으면 profile 이름이 라벨이 된다 (기존 동작).
	p2 := &config.Profile{Name: "personal"}
	p2.ApplyDefaults()
	lines = Lines(View{Profile: p2, Model: "Opus 5", Limits: lim, Usage: uf,
		Credits: core.Credits(p2, lim, uf, now), Now: now}, Style{})
	if lines[0] != "[personal] · Opus 5 · 5h 30%" {
		t.Errorf("기본 라벨: %q", lines[0])
	}
}

func TestWindowAlertEmphasis(t *testing.T) {
	now := time.Now()
	p := &config.Profile{Name: "me"}
	p.ApplyDefaults()
	uf := &store.UsageFile{}
	lim := core.Limits{
		FiveHour:  &store.Window{Percent: 95},
		SevenDay:  &store.Window{Percent: 40},
		FromStdin: true,
	}
	line := func(a core.Alert) string {
		return Lines(View{Profile: p, Limits: lim, Alert: a, Usage: uf,
			Credits: core.Credits(p, lim, uf, now), Now: now}, Style{Color: true})[0]
	}
	// burst의 켜진 프레임만 배지, 꺼진 프레임과 burst 종료 후에는 굵은 빨강.
	on := line(core.Alert{Level: core.AlertNear, Window: "5h", Burst: true, On: true})
	off := line(core.Alert{Level: core.AlertNear, Window: "5h", Burst: true})
	rest := line(core.Alert{Level: core.AlertNear, Window: "5h"})
	if !strings.Contains(on, redBG+"95%") {
		t.Errorf("켜진 프레임은 배지여야 한다: %q", on)
	}
	if !strings.Contains(off, bold+red+"95%") || !strings.Contains(rest, bold+red+"95%") {
		t.Errorf("꺼진 프레임·burst 종료 후는 굵은 빨강: off=%q rest=%q", off, rest)
	}
	// 경보를 올리지 않은 window는 평소 색 그대로다.
	if !strings.Contains(on, green+"40%") {
		t.Errorf("7d는 건드리지 않아야 한다: %q", on)
	}
	// 경보가 없으면 임계값 색 규칙만 적용된다.
	if plain := line(core.Alert{}); !strings.Contains(plain, red+"95%") || strings.Contains(plain, redBG) {
		t.Errorf("경보 없음: %q", plain)
	}
}
