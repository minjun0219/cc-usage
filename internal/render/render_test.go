package render

import (
	"fmt"
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
		80 * time.Minute: "1h 20m",
		52 * time.Hour:   "2d 4h",
		// 0인 아랫단위는 떼어 낸다.
		24 * time.Hour: "1d",
		2 * time.Hour:  "2h",
		48 * time.Hour: "2d",
	}
	for d, want := range cases {
		if got := Duration(d); got != want {
			t.Errorf("%v: got %s want %s", d, got, want)
		}
	}
}

func TestLinesSpending(t *testing.T) {
	now := time.Now()
	p := &config.Config{}
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
		Config: p, Model: "Sonnet", ContextPct: &pct, Limits: lim, Usage: uf,
		Credits: core.Credits(p, lim, uf, now), Now: now,
	}, Style{})
	if len(lines) != 2 {
		t.Fatalf("lines: %q", lines)
	}
	if !strings.Contains(lines[0], "5h 0% (↻") || !strings.Contains(lines[0], "ctx 42%") {
		t.Errorf("line1: %s", lines[0])
	}
	// 금액은 **남은 값**이다 — 쓴 값 $10.80 이 아니라 $50-$10.80.
	// 한도는 .00 을 떼고, 금액은 폭이 흔들리지 않게 소수점을 유지한다.
	if !strings.Contains(lines[1], "$39.20 ($50)") || !strings.Contains(lines[1], "+$0.80") || !strings.Contains(lines[1], "소진 중") {
		t.Errorf("line2: %s", lines[1])
	}
}

func TestLinesQuietBelowLimit(t *testing.T) {
	now := time.Now()
	p := &config.Config{}
	p.ApplyDefaults()
	lim := core.Limits{FiveHour: &store.Window{Percent: 30}, FromStdin: true}
	uf := &store.UsageFile{}
	lines := Lines(View{Config: p, Limits: lim, Usage: uf, Credits: core.Credits(p, lim, uf, now), Now: now}, Style{})
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
	p := &config.Config{}
	p.ApplyDefaults()
	uf := &store.UsageFile{}
	lim := core.Limits{FiveHour: &store.Window{Percent: 30}, FromStdin: true}

	view := func(dir string, gs *git.Status) View {
		return View{Config: p, Dir: dir, Git: gs, Limits: lim, Usage: uf,
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
	if lines[0] != "/opt/x · ⎇ main !8 ⇡1⇣2" {
		t.Errorf("branch segment: %q", lines[0])
	}
	lines = Lines(view("/opt/x", &git.Status{Branch: "main", Conflicted: 1, Staged: 3, Unstaged: 5}), Style{})
	if lines[0] != "/opt/x · ⎇ main =1 +3 !5" {
		t.Errorf("counts: %q", lines[0])
	}
	// 업스트림이 없으면 ahead/behind는 나오지 않는다.
	lines = Lines(view("/opt/x", &git.Status{Branch: "main"}), Style{})
	if lines[0] != "/opt/x · ⎇ main" {
		t.Errorf("no upstream: %q", lines[0])
	}
	lines = Lines(view("/opt/x", &git.Status{Branch: "(detached)", Detached: true, OID: "1a2b3c4d5e"}), Style{})
	if lines[0] != "/opt/x · ⎇ @1a2b3c4" {
		t.Errorf("detached: %q", lines[0])
	}
}

func TestResetText(t *testing.T) {
	now := time.Date(2026, 9, 16, 16, 40, 0, 0, time.Local)
	if got := resetText(now.Add(80*time.Minute), now); got != "↻18:00" {
		t.Errorf("within a day: %s", got)
	}
	// 하루를 넘기면 시각만으로 어느 날인지 알 수 없으니 남은 시간만 남긴다.
	if got := resetText(now.Add(52*time.Hour), now); got != "2d 4h" {
		t.Errorf("beyond a day: %s", got)
	}
}

// 트루컬러면 퍼센트 색이 끊김 없이 변하고, 아니면 기존 3단계로 떨어진다.
func TestPctColorGradientAndFallback(t *testing.T) {
	tc := Style{Color: true, TrueColor: true}
	var prevR int
	for i, used := range []float64{0, 30, 60, 90, 100} {
		got := tc.pctColor(used)
		if !strings.HasPrefix(got, "\033[38;2;") {
			t.Fatalf("트루컬러는 24bit 이스케이프여야 한다: %q", got)
		}
		var r, g, b int
		if _, err := fmt.Sscanf(got, "\033[38;2;%d;%d;%dm", &r, &g, &b); err != nil {
			t.Fatalf("파싱 실패 %q: %v", got, err)
		}
		// 사용률이 오를수록 빨강이 세진다 — 숫자와 색이 같은 방향.
		if i > 0 && r <= prevR {
			t.Errorf("used=%v 에서 빨강이 더 세지지 않았다: %d → %d", used, prevR, r)
		}
		prevR = r
	}

	// 지원하지 않는 터미널에서는 이스케이프가 새면 안 되고 3단계로 떨어진다.
	plain := Style{Color: true}
	for used, want := range map[float64]string{10: green, 75: yellow, 95: red} {
		if got := plain.pctColor(used); got != want {
			t.Errorf("폴백 used=%v: got %q want %q", used, got, want)
		}
	}
}

// 평소 크레딧은 상태 줄 끝에 붙고, 강조가 붙는 상태만 줄을 따로 쓴다.
func TestCreditRowPlacement(t *testing.T) {
	now := time.Now()
	c := &config.Config{}
	c.ApplyDefaults()
	used, limit := 1160.0, 5000.0
	uf := &store.UsageFile{Usage: &store.Usage{FetchedAt: now,
		Extra: &store.Extra{Enabled: true, UsedCredits: &used, MonthlyLimit: &limit}}}

	// 여유 — 한 줄로 끝난다.
	idle := core.Limits{FiveHour: &store.Window{Percent: 30}, FromStdin: true}
	lines := Lines(View{Config: c, Model: "Opus 5", Limits: idle, Usage: uf,
		Credits: core.Credits(c, idle, uf, now), Now: now}, Style{})
	if len(lines) != 1 {
		t.Fatalf("여유에서는 크레딧이 상태 줄에 붙어야 한다: %q", lines)
	}
	if !strings.Contains(lines[0], "5h 70%") || !strings.Contains(lines[0], "💳 $38.40") {
		t.Errorf("한 줄에 둘 다 있어야 한다: %q", lines[0])
	}

	// 소진 — 문장이 길어지므로 줄을 따로 쓴다.
	hit := core.Limits{FiveHour: &store.Window{Percent: 100}, FromStdin: true}
	lines = Lines(View{Config: c, Model: "Opus 5", Limits: hit, Usage: uf,
		Credits: core.Credits(c, hit, uf, now), Now: now}, Style{})
	if len(lines) != 2 {
		t.Fatalf("소진에서는 크레딧이 제 줄을 가져야 한다: %q", lines)
	}
	if strings.Contains(lines[0], "💳") || !strings.Contains(lines[1], "💳") {
		t.Errorf("크레딧은 둘째 줄이어야 한다: %q", lines)
	}
}

func TestDisplayWidth(t *testing.T) {
	for in, want := range map[string]int{
		"":                              0,
		"5h 70%":                        6,
		"\033[32m70%\033[0m":            3,  // ANSI 는 폭이 없다
		"💳 $11.60":                      9,  // 이모지 2 + 공백 1 + "$11.60" 6
		"크레딧 소진 중":                      14, // 한글 6자 × 2 + 공백 2
		"\033[90m(1h 20m→10:17)\033[0m": 14, // → 는 1칸
	} {
		if got := displayWidth(in); got != want {
			t.Errorf("%q: got %d want %d", in, got, want)
		}
	}
	// 화살표는 Ambiguous 라 1칸으로 센다 (대부분의 터미널이 그렇게 그린다).
	if got := displayWidth("→"); got != 1 {
		t.Errorf("→: got %d want 1", got)
	}
	if got := displayWidth("⎇ main"); got != 6 {
		t.Errorf("⎇ main: got %d want 6", got)
	}
}

// 폭을 알면(COLUMNS) 붙였을 때 넘칠 크레딧은 내린다. 모르면 기존 규칙만 쓴다.
func TestCreditFallsBelowWhenTooWide(t *testing.T) {
	now := time.Now()
	c := &config.Config{}
	c.ApplyDefaults()
	used, limit := 1160.0, 5000.0
	uf := &store.UsageFile{Usage: &store.Usage{FetchedAt: now,
		Extra: &store.Extra{Enabled: true, UsedCredits: &used, MonthlyLimit: &limit}}}
	lim := core.Limits{FiveHour: &store.Window{Percent: 30}, FromStdin: true}
	view := View{Config: c, Model: "Opus 5 (1M context)", Limits: lim, Usage: uf,
		Credits: core.Credits(c, lim, uf, now), Now: now}

	wide := Lines(view, Style{Width: 200})
	if len(wide) != 1 {
		t.Errorf("넓으면 한 줄이어야 한다: %q", wide)
	}
	narrow := Lines(view, Style{Width: 30})
	if len(narrow) != 2 || !strings.Contains(narrow[1], "💳") {
		t.Errorf("좁으면 크레딧이 내려가야 한다: %q", narrow)
	}
	unknown := Lines(view, Style{}) // COLUMNS 없음 → 폭 판단 생략
	if len(unknown) != 1 {
		t.Errorf("폭을 모르면 기존대로 붙인다: %q", unknown)
	}
	// 경계 — 판단 기준은 COLUMNS 가 아니라 COLUMNS-rightMargin 이다. 오른쪽은
	// Claude Code 의 배지·알림 몫이라 우리가 쓰면 그것들이 아래로 밀린다.
	row := displayWidth(Lines(view, Style{Width: 500})[0])
	if got := Lines(view, Style{Width: row + rightMargin}); len(got) != 1 {
		t.Errorf("여백까지 확보되면 붙인다(%d칸): %q", row+rightMargin, got)
	}
	if got := Lines(view, Style{Width: row + rightMargin - 1}); len(got) != 2 {
		t.Errorf("여백이 한 칸 모자라면 내린다(%d칸): %q", row+rightMargin-1, got)
	}
	// 줄 자체는 들어가지만 여백이 없는 폭 — 여기서 붙이면 배지가 밀린다.
	if got := Lines(view, Style{Width: row + 1}); len(got) != 2 {
		t.Errorf("줄은 들어가도 여백이 없으면 내린다(%d칸): %q", row+1, got)
	}
}

// 7d 는 평소에 숨고, 사용률이 올라가거나 경보를 올리면 나타난다.
func TestSevenDayHiddenUntilItMatters(t *testing.T) {
	now := time.Now()
	c := &config.Config{}
	c.ApplyDefaults()
	uf := &store.UsageFile{}
	row := func(pct float64, a core.Alert) string {
		lim := core.Limits{FiveHour: &store.Window{Percent: 10},
			SevenDay: &store.Window{Percent: pct}, FromStdin: true}
		return Lines(View{Config: c, Model: "Opus 5", Limits: lim, Alert: a, Usage: uf,
			Credits: core.Credits(c, lim, uf, now), Now: now}, Style{})[0]
	}
	if got := row(sevenDayShowAt-1, core.Alert{}); strings.Contains(got, "7d") {
		t.Errorf("여유로운 7d 는 숨어야 한다: %q", got)
	}
	if got := row(sevenDayShowAt, core.Alert{}); !strings.Contains(got, "7d") {
		t.Errorf("임계에 닿으면 나타나야 한다: %q", got)
	}
	// alert_percent 를 이보다 낮게 잡은 설정에서 경보가 뜬 창이 숨으면 안 된다.
	if got := row(30, core.Alert{Level: core.AlertNear, Window: "7d"}); !strings.Contains(got, "7d") {
		t.Errorf("경보를 올린 7d 는 임계 아래여도 나타나야 한다: %q", got)
	}
}

func TestWindowAlertEmphasis(t *testing.T) {
	now := time.Now()
	p := &config.Config{}
	p.ApplyDefaults()
	uf := &store.UsageFile{}
	lim := core.Limits{
		FiveHour: &store.Window{Percent: 95},
		// 7d 는 sevenDayShowAt 위여야 화면에 올라온다 — 75% 사용 → 남은 25%.
		SevenDay:  &store.Window{Percent: 75},
		FromStdin: true,
	}
	line := func(a core.Alert) string {
		return Lines(View{Config: p, Limits: lim, Alert: a, Usage: uf,
			Credits: core.Credits(p, lim, uf, now), Now: now}, Style{Color: true})[0]
	}
	// 경보가 붙은 창은 배지다 — burst 가 끝나도 배지로 남는다. 굵은 빨강 글씨는
	// 같은 줄의 다른 임계색에 묻힌다.
	on := line(core.Alert{Level: core.AlertNear, Window: "5h", Burst: true, On: true})
	off := line(core.Alert{Level: core.AlertNear, Window: "5h", Burst: true})
	rest := line(core.Alert{Level: core.AlertNear, Window: "5h"})
	if !strings.Contains(on, redBG+" 5% ") || !strings.Contains(rest, redBG+" 5% ") {
		t.Errorf("켜진 프레임과 쉼 상태는 배지: on=%q rest=%q", on, rest)
	}
	// 깜빡임의 꺼진 프레임만 글씨로 떨어진다. 여백은 그대로 남겨 폭이 바뀌지
	// 않게 한다 — 프레임마다 폭이 달라지면 줄 전체가 좌우로 출렁인다.
	if !strings.Contains(off, bold+red+" 5% ") {
		t.Errorf("꺼진 프레임은 여백을 유지한 글씨: %q", off)
	}
	if displayWidth(on) != displayWidth(off) || displayWidth(on) != displayWidth(rest) {
		t.Errorf("프레임마다 폭이 달라지면 안 된다: on=%d off=%d rest=%d",
			displayWidth(on), displayWidth(off), displayWidth(rest))
	}
	// 경보를 올리지 않은 window는 평소 색 그대로다.
	if !strings.Contains(on, yellow+"25%") {
		t.Errorf("7d는 건드리지 않아야 한다: %q", on)
	}
	// 경보가 없으면 임계값 색 규칙만 적용된다.
	if plain := line(core.Alert{}); !strings.Contains(plain, red+"5%") || strings.Contains(plain, redBG) {
		t.Errorf("경보 없음: %q", plain)
	}
}

func TestLinesNeverEmpty(t *testing.T) {
	// 빈 stdin + cache 없음 → 모든 세그먼트가 빈다.
	c := &config.Config{}
	c.ApplyDefaults()
	uf := &store.UsageFile{}
	lim := core.Limits{FromStdin: true}
	lines := Lines(View{Config: c, Limits: lim, Usage: uf,
		Credits: core.Credits(c, lim, uf, time.Now()), Now: time.Now()}, Style{})
	if len(lines) != 1 || lines[0] != "[cc-usage]" {
		t.Errorf("statusline이 통째로 비면 안 된다: %q", lines)
	}
}

func TestCreditAmountSaysWhichDirection(t *testing.T) {
	now := time.Now()
	cfg := &config.Config{}
	cfg.ApplyDefaults()
	used, limit := 983.0, 10000.0
	lim := core.Limits{FiveHour: &store.Window{Percent: 30}, FromStdin: true}
	line := func(uf *store.UsageFile) string {
		return creditLine(View{Config: cfg, Limits: lim, Usage: uf,
			Credits: core.Credits(cfg, lim, uf, now), Now: now}, Style{})
	}
	withLimit := &store.UsageFile{Usage: &store.Usage{FetchedAt: now,
		Extra: &store.Extra{Enabled: true, UsedCredits: &used, MonthlyLimit: &limit}}}
	if got := line(withLimit); got != "💳 $90.17 ($100)" {
		t.Errorf("한도가 있으면 남은 금액: %q", got)
	}
	// monthly_limit 은 optional 이다. 없으면 남은 금액을 낼 수 없으므로 쓴 금액이
	// 나가는데, 같은 서식이면 남은 금액으로 읽힌다 — 무엇인지 밝혀야 한다.
	noLimit := &store.UsageFile{Usage: &store.Usage{FetchedAt: now,
		Extra: &store.Extra{Enabled: true, UsedCredits: &used}}}
	if got := line(noLimit); got != "💳 $9.83 사용" {
		t.Errorf("한도가 없으면 쓴 금액임을 밝힌다: %q", got)
	}
}

func TestResetTextUsesCalendarDay(t *testing.T) {
	now := time.Date(2026, 9, 17, 15, 0, 0, 0, time.Local)
	if got := resetText(now.Add(20*time.Minute), now); got != "↻15:20" {
		t.Errorf("같은 날이면 시각: %q", got)
	}
	// 23시간 뒤는 24시간 안이지만 내일이다. 시각으로 내면 오늘 14시로 읽힌다.
	if got := resetText(now.Add(23*time.Hour), now); got != "23h" {
		t.Errorf("날이 바뀌면 남은 시간: %q", got)
	}
	// 자정을 막 넘기는 경우도 마찬가지다.
	if got := resetText(time.Date(2026, 9, 18, 0, 30, 0, 0, time.Local), now); got != "9h 30m" {
		t.Errorf("자정 넘김: %q", got)
	}
	if got := resetText(now.Add(50*time.Hour), now); got != "2d 2h" {
		t.Errorf("이틀 뒤: %q", got)
	}
}

func TestMoneyShort(t *testing.T) {
	// 한도는 잘 변하지 않으므로 .00 을 뗀다. 소수점이 있으면 그대로 둔다.
	cases := map[float64]string{100: "$100", 100.5: "$100.50", 0: "$0", 33.33: "$33.33"}
	for in, want := range cases {
		if got := moneyShort("$", in); got != want {
			t.Errorf("moneyShort(%v) = %q want %q", in, got, want)
		}
	}
	// 금액 쪽은 폭이 흔들리지 않게 소수점을 유지한다.
	if got := money("$", 100); got != "$100.00" {
		t.Errorf("money(100) = %q", got)
	}
}

func TestCtxColorStaysOutOfTheRedAxis(t *testing.T) {
	// ctx 는 빨강 축을 쓰지 않는다 — 빨강은 "여기서 멈춘다"(한도·경보) 자리다.
	for _, s := range []Style{{Color: true}, {Color: true, TrueColor: true}} {
		for _, p := range []float64{0, 41, 70, 90, 100} {
			got := s.ctxColor(p)
			if got == red || got == yellow || got == green || got == redBG {
				t.Errorf("TrueColor=%v ctx %v%% 가 한도 색을 쓴다: %q", s.TrueColor, p, got)
			}
		}
	}
	// 차오를수록 밝아진다 (truecolor 세 채널 모두 단조 증가).
	prev := [3]int{-1, -1, -1}
	for p := 0.0; p <= 100; p += 10 {
		var r, g, b int
		fmt.Sscanf((Style{Color: true, TrueColor: true}).ctxColor(p), "\033[38;2;%d;%d;%dm", &r, &g, &b)
		if r < prev[0] || g < prev[1] || b < prev[2] {
			t.Errorf("%v%% 에서 밝기가 꺾인다: %d,%d,%d ← %v", p, r, g, b, prev)
		}
		prev = [3]int{r, g, b}
	}
}

func TestColorTiers(t *testing.T) {
	// statusline 프로세스에는 COLORTERM 이 오지 않고 TERM 만 온다(실측).
	// 24bit 가 없다고 바로 3단계로 떨어지면 71% 와 89% 가 같은 색이 된다.
	tiers := []struct {
		name   string
		style  Style
		prefix string
	}{
		{"24bit", Style{Color: true, TrueColor: true}, "\033[38;2;"},
		{"256색", Style{Color: true, Color256: true}, "\033[38;5;"},
	}
	for _, tier := range tiers {
		seen := map[string]bool{}
		for _, p := range []float64{10, 30, 50, 71, 80, 89, 95} {
			c := tier.style.pctColor(p)
			if !strings.HasPrefix(c, tier.prefix) {
				t.Errorf("%s: %v%% → %q", tier.name, p, c)
			}
			seen[c] = true
		}
		// 71 과 89 가 갈리는지가 이 계층을 둔 이유다.
		if a, b := tier.style.pctColor(71), tier.style.pctColor(89); a == b {
			t.Errorf("%s: 71%%와 89%%가 같은 색이다 (%q)", tier.name, a)
		}
		if len(seen) < 5 {
			t.Errorf("%s: 7개 값에서 색이 %d가지뿐 — 그라데이션이 뭉갠다", tier.name, len(seen))
		}
	}
	// 둘 다 없으면 기존 3단계로 떨어진다.
	plain := Style{Color: true}
	if got := plain.pctColor(50); got != green {
		t.Errorf("폴백: %q", got)
	}
}

func TestCube256Axis(t *testing.T) {
	// 큐브 축은 등간격이 아니다 (0·95·135·175·215·255). 가장 가까운 단계를 고른다.
	// 정확히 중간인 값(115 = 95와 135의 중간)은 어느 쪽이든 임의라 넣지 않는다.
	for in, want := range map[int]int{0: 0, 40: 0, 60: 1, 95: 1, 120: 2, 175: 3, 255: 5} {
		if got := cubeAxis(in); got != want {
			t.Errorf("cubeAxis(%d) = %d want %d", in, got, want)
		}
	}
	if got := cube256(0, 0, 0); got != 16 {
		t.Errorf("검정 = %d want 16", got)
	}
	if got := cube256(255, 255, 255); got != 231 {
		t.Errorf("흰색 = %d want 231", got)
	}
}

func TestBadge(t *testing.T) {
	s := Style{Color: true}
	cases := []struct {
		name  string
		badge *config.Badge
		want  string
	}{
		{"없으면 표시 없음", nil, ""},
		{"이모지는 그대로", &config.Badge{Emoji: "🏢"}, "🏢"},
		{"이모지가 색을 이긴다", &config.Badge{Emoji: "🏢", Color: "blue"}, "🏢"},
		{"글리프 기본값은 ●", &config.Badge{Color: "blue"}, "\033[34m●\033[0m"},
		{"글리프 교체", &config.Badge{Color: "cyan", Glyph: "◆"}, "\033[36m◆\033[0m"},
		{"256 인덱스", &config.Badge{Color: "33", Glyph: "◆"}, "\033[38;5;33m◆\033[0m"},
		// 오타로 글리프가 사라지는 것보다 색 없이 뜨는 편이 낫다.
		{"모르는 색이면 색만 빠짐", &config.Badge{Color: "purpel"}, "●"},
	}
	for _, c := range cases {
		if got := badgeText(c.badge, s); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestBadgeIsPrefixNotSegment(t *testing.T) {
	now := time.Now()
	cfg := &config.Config{}
	cfg.ApplyDefaults()
	uf := &store.UsageFile{}
	lim := core.Limits{FiveHour: &store.Window{Percent: 30}, FromStdin: true}
	line := func(b *config.Badge) string {
		return Lines(View{Config: cfg, Badge: b, Model: "Opus 5", Limits: lim, Usage: uf,
			Credits: core.Credits(cfg, lim, uf, now), Now: now}, Style{})[0]
	}
	// 배지 뒤에는 구분자가 아니라 공백 하나다 — 세그먼트가 아니라 머리표다.
	if got := line(&config.Badge{Emoji: "🏢"}); got != "🏢 Opus 5 · 5h 70%" {
		t.Errorf("배지 있음: %q", got)
	}
	if got := line(nil); got != "Opus 5 · 5h 70%" {
		t.Errorf("배지 없음: %q", got)
	}
	// 배지 폭이 크레딧 배치 판단에 들어가야 한다 (이모지는 2칸).
	if displayWidth(line(&config.Badge{Emoji: "🏢"}))-displayWidth(line(nil)) != 3 {
		t.Errorf("배지 폭이 반영되지 않는다")
	}
}

func TestBadgeSurvivesEmptyRow(t *testing.T) {
	// stdin 도 cache 도 빈 렌더가 정보가 가장 적은 순간이다. 하필 거기서
	// "여기는 평소 자리가 아니다" 신호가 사라지면 안 된다.
	now := time.Now()
	cfg := &config.Config{}
	cfg.ApplyDefaults()
	uf := &store.UsageFile{}
	lim := core.Limits{FromStdin: true}
	lines := Lines(View{Config: cfg, Badge: &config.Badge{Emoji: "🏢"},
		Limits: lim, Usage: uf, Credits: core.Credits(cfg, lim, uf, now), Now: now}, Style{})
	if len(lines) != 1 || lines[0] != "🏢" {
		t.Errorf("배지만 남아야 한다: %q", lines)
	}
}

func TestBadgeNeverLooksLikeSegment(t *testing.T) {
	// 배지는 머리표라 구분자를 붙이지 않는다. 나머지가 다 비고 크레딧만 남는
	// 렌더에서 "🏢 · 💳 …" 가 되면 배지가 세그먼트처럼 보인다.
	now := time.Now()
	cfg := &config.Config{}
	cfg.ApplyDefaults()
	used, limit := 1230.0, 10000.0
	uf := &store.UsageFile{Usage: &store.Usage{FetchedAt: now,
		Extra: &store.Extra{Enabled: true, UsedCredits: &used, MonthlyLimit: &limit}}}
	lim := core.Limits{FromStdin: true} // 모델·ctx·한도 전부 없음
	lines := Lines(View{Config: cfg, Badge: &config.Badge{Emoji: "🏢"},
		Limits: lim, Usage: uf, Credits: core.Credits(cfg, lim, uf, now), Now: now}, Style{})
	if strings.Contains(lines[0], "🏢 · ") {
		t.Errorf("배지 뒤에 구분자가 붙었다: %q", lines[0])
	}
	if !strings.HasPrefix(lines[0], "🏢 💳") {
		t.Errorf("배지 + 공백 + 크레딧: %q", lines[0])
	}
}

func TestBadgeWidthCountsTowardCreditPlacement(t *testing.T) {
	// 배지를 마지막에 붙이더라도 줄에는 들어가는 폭이다. 폭 판단에서 빠지면
	// 경계 근처에서 크레딧이 붙을지 내려갈지가 한 칸씩 어긋난다.
	now := time.Now()
	cfg := &config.Config{}
	cfg.ApplyDefaults()
	used, limit := 1230.0, 10000.0
	uf := &store.UsageFile{Usage: &store.Usage{FetchedAt: now,
		Extra: &store.Extra{Enabled: true, UsedCredits: &used, MonthlyLimit: &limit}}}
	lim := core.Limits{FiveHour: &store.Window{Percent: 30}, FromStdin: true}
	view := func(b *config.Badge) View {
		return View{Config: cfg, Badge: b, Model: "Opus 5", Limits: lim, Usage: uf,
			Credits: core.Credits(cfg, lim, uf, now), Now: now}
	}
	// 배지 없이는 딱 붙고, 배지 폭(2+1)이 더해지면 넘치는 폭을 고른다.
	plain := Lines(view(nil), Style{})
	w := displayWidth(plain[0])
	s := Style{Width: w + rightMargin + 1} // 배지 없으면 들어가고 있으면 넘친다
	if got := Lines(view(nil), s); len(got) != 1 {
		t.Fatalf("배지 없을 때는 한 줄이어야 한다: %q", got)
	}
	if got := Lines(view(&config.Badge{Emoji: "🏢"}), s); len(got) != 2 {
		t.Errorf("배지 폭이 반영되면 크레딧이 내려가야 한다: %q", got)
	}
}

func TestEmojiWidths(t *testing.T) {
	// 배지가 임의의 사용자 이모지를 이 계산에 태운다. 흔한 구간이 빠지면
	// 폭이 1칸씩 틀린다.
	for _, e := range []string{"💳", "🏢", "🚀", "🟠", "🧠", "🪄", "⚡", "✅", "⌛"} {
		if got := displayWidth(e); got != 2 {
			t.Errorf("%s (U+%04X): %d칸 (2여야 함)", e, []rune(e)[0], got)
		}
	}
	// 화살표·기호류는 대부분의 터미널에서 1칸이다.
	for _, a := range []string{"⎇", "⇡", "⇣", "↻", "·"} {
		if got := displayWidth(a); got != 1 {
			t.Errorf("%s: %d칸 (1이여야 함)", a, got)
		}
	}
}
