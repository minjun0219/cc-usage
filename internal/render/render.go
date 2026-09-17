// Package render builds the statusline text.
package render

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"cc-usage/internal/config"
	"cc-usage/internal/core"
	"cc-usage/internal/git"
	"cc-usage/internal/store"
)

type Style struct {
	Color bool
	// TrueColor turns the 퍼센트 색을 24bit 그라데이션으로 바꾼다.
	TrueColor bool
	// Color256 is the middle tier. statusline 프로세스에는 COLORTERM 이 오지
	// 않고 TERM 만 온다(실측) — 24bit 를 못 쓴다고 바로 3단계로 떨어뜨리면
	// 71% 와 89% 가 같은 색이 된다. 256색이면 그 사이가 보인다.
	Color256 bool
	// Width is the terminal column count, 0이면 모름. statusline 은 stdout 이
	// 파이프라 tput·ioctl 로는 폭을 알 수 없고, Claude Code 가 COLUMNS 로 넣어 준다.
	Width int
}

// rightMargin 은 statusline 이 쓰지 않고 비워 두는 오른쪽 칸이다.
//
// COLUMNS 는 터미널 폭이지 statusline 이 다 써도 되는 폭이 아니다 — Claude Code 가
// 그 오른쪽에 배지·알림을 얹는다("✔ Update installed · Restart to update" 가 실측
// 38칸이었다). 우리 줄이 길면 그것들이 아래로 밀린다. 실측 38 + 여유로 40 을 둔다.
// 정확한 값을 알 길은 없다 — 배지 문구는 그때그때 다르다.
const rightMargin = 40

// ctx 밝기 곡선. 지수와 계단 경계를 함께 뒤로 미뤄, 볼 일 없는 구간에서
// 눈을 끌지 않게 한다. payload 가 compaction 지점을 주지 않아 "위험" 을 말할
// 근거가 없으므로, 색은 위험이 아니라 차오르는 정도만 나타낸다.
const (
	ctxCurve  = 2.2
	ctxMidAt  = 80
	ctxHighAt = 95
)

// sevenDayShowAt 은 7d 세그먼트가 나타나는 사용률이다. pctColor 의 warn 임계와
// 같은 값이라, 노랗게 보일 만해지면 화면에도 올라온다.
const sevenDayShowAt = 70

func DefaultStyle() Style {
	ct := os.Getenv("COLORTERM")
	w, _ := strconv.Atoi(os.Getenv("COLUMNS")) // 없거나 이상하면 0 — 폭 판단을 건너뛴다
	if w < 0 {
		w = 0
	}
	return Style{
		Color:     os.Getenv("NO_COLOR") == "",
		TrueColor: ct == "truecolor" || ct == "24bit",
		Color256:  strings.Contains(os.Getenv("TERM"), "256color"),
		Width:     w,
	}
}

const (
	reset = "\033[0m"
	// dim은 밝기 속성(2)이 아니라 gray(90)다 — 터미널에 따라 2가 아예 무시되거나
	// 과하게 어두워진다. 90은 기존 셸 statusline이 쓰던 값이고 눈으로 맞춰 둔 것이다.
	dim     = "\033[90m"
	green   = "\033[32m"
	cyan    = "\033[36m"
	blue    = "\033[1;34m"
	ctxLow  = "\033[34m" // 파랑 — truecolor 가 없을 때의 ctx 단계
	ctxMid  = "\033[94m" // 밝은 파랑
	ctxHigh = "\033[96m" // 밝은 청록
	magenta = "\033[1;35m"
	yellow  = "\033[33m"
	red     = "\033[31m"
	redBG   = "\033[41;97m"
	bold    = "\033[1m"
)

// rgb renders a color in the best form the terminal supports. ok 가 false 면
// 호출자가 기본 ANSI 단계로 떨어진다 — 지원하지 않는 터미널에서 이스케이프가
// 글자로 새어 나오는 것보다 계단식 색이 낫다.
func (s Style) rgb(r, g, b int) (string, bool) {
	switch {
	case s.TrueColor:
		return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b), true
	case s.Color256:
		return fmt.Sprintf("\033[38;5;%dm", cube256(r, g, b)), true
	}
	return "", false
}

// cube256 maps RGB onto the 6×6×6 color cube (16-231). 큐브의 각 축은 등간격이
// 아니라 0·95·135·175·215·255 이므로 가장 가까운 단계를 고른다.
func cube256(r, g, b int) int {
	return 16 + 36*cubeAxis(r) + 6*cubeAxis(g) + cubeAxis(b)
}

func cubeAxis(v int) int {
	levels := [6]int{0, 95, 135, 175, 215, 255}
	best, bestD := 0, 1<<30
	for i, l := range levels {
		d := v - l
		if d < 0 {
			d = -d
		}
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

func (s Style) c(code, text string) string {
	if !s.Color || text == "" {
		return text
	}
	return code + text + reset
}

type View struct {
	Config     *config.Config
	Dir        string      // workspace.current_dir (빈 값이면 경로 줄 생략)
	Git        *git.Status // nil이면 git repo가 아니거나 조회 실패 — 세그먼트 생략
	Model      string
	ContextPct *float64
	Limits     core.Limits
	Alert      core.Alert
	Usage      *store.UsageFile
	Credits    core.CreditView
	Now        time.Time
}

// Lines returns the statusline rows: an optional cwd/branch row, the status
// row, and an optional credit row.
func Lines(v View, s Style) []string {
	var parts []string
	if v.Model != "" {
		parts = append(parts, s.c(magenta, v.Model))
	}
	if v.ContextPct != nil {
		parts = append(parts, "ctx "+s.c(s.ctxColor(*v.ContextPct), fmt.Sprintf("%.0f%%", *v.ContextPct)))
	}
	if w := v.Limits.FiveHour; w != nil {
		parts = append(parts, windowText("5h", w, v, s))
	}
	// 7d 는 평소에 볼 일이 없다 — 주 관심사는 5h 이고, 7d 리셋은 며칠 뒤라
	// 자리만 차지한다. 색이 노래지기 시작하는 지점부터 나타난다(폴링이 촘촘해지는
	// 경계와 같은 값). 경보를 올린 창은 임계와 무관하게 보여 준다 — alert_percent 를
	// 이보다 낮게 잡은 설정에서 경보가 뜬 창이 숨는 일이 없어야 한다.
	if w := v.Limits.SevenDay; w != nil && (w.Percent >= sevenDayShowAt || v.Alert.Window == "7d") {
		parts = append(parts, windowText("7d", w, v, s))
	}
	if note := statusNote(v, s); note != "" {
		parts = append(parts, note)
	}
	var lines []string
	if dl := dirLine(v, s); dl != "" {
		lines = append(lines, dl)
	}

	// 크레딧은 평소엔 상태 줄 끝에 붙는다 — statusline 이 세로로 차지하는 칸이
	// 곧 프롬프트가 밀리는 양이다. 내려야 할 때만 줄을 따로 쓴다.
	sep := s.c(dim, " · ")
	row := strings.Join(parts, sep)
	cl := creditLine(v, s)
	standalone := cl != "" && s.creditStandalone(v, row, cl, sep)
	if cl != "" && !standalone {
		if row == "" {
			row = cl
		} else {
			row += sep + cl
		}
	}
	if row != "" {
		lines = append(lines, row)
	}
	if standalone {
		lines = append(lines, cl)
	}
	if len(lines) == 0 {
		// stdin도 cache도 비면 모든 세그먼트가 빈다. statusline이 통째로 비면
		// 무엇이 도는지조차 알 수 없으므로 최소 한 줄은 낸다.
		lines = append(lines, s.c(dim, "[cc-usage]"))
	}
	return lines
}

func dirLine(v View, s Style) string {
	if v.Dir == "" {
		return ""
	}
	t := s.c(cyan, AbbrevHome(v.Dir))
	if b := branchText(v.Git, s); b != "" {
		t += s.c(dim, " · ") + b
	}
	return t
}

// branchText renders the branch segment. 표기 형태(마커·색·순서)는 여기 한 곳에만
// 있다 — 바꿀 때 이 함수만 갈아 끼우면 된다. git.Status는 이미 브랜치명·detached·
// 업스트림 유무·ahead/behind·dirty를 다 담고 있으니 파싱은 건드릴 일이 없다.
//
// 현재: "⎇ main !8" (unstaged 8) / "⎇ main =1 +3 !5" (conflict·staged·unstaged)
// / "⎇ main ⇡1⇣2" (업스트림 대비) / "⎇ @1a2b3c4" (detached)
// 글리프는 starship 계열을 따른다 — = conflict, + staged, ! unstaged, ⇡⇣ ahead/behind.
func branchText(st *git.Status, s Style) string {
	if st == nil || st.Branch == "" {
		return ""
	}
	name := st.Branch
	if st.Detached {
		name = "(detached)"
		if len(st.OID) >= 7 {
			name = "@" + st.OID[:7]
		}
	}
	t := s.c(blue, "⎇") + " " + s.c(green, name)
	for _, seg := range []struct {
		n     int
		glyph string
		color string
	}{
		{st.Conflicted, "=", red},
		{st.Staged, "+", green},
		{st.Unstaged, "!", yellow},
	} {
		if seg.n > 0 {
			t += " " + s.c(seg.color, fmt.Sprintf("%s%d", seg.glyph, seg.n))
		}
	}
	// 업스트림이 없으면(새 브랜치·detached) ahead/behind를 내지 않는다 — 없는
	// 것을 0으로 보여주면 최신인 것처럼 읽힌다.
	if st.HasUpstream {
		var sync string
		if st.Ahead > 0 {
			sync += fmt.Sprintf("⇡%d", st.Ahead)
		}
		if st.Behind > 0 {
			sync += fmt.Sprintf("⇣%d", st.Behind)
		}
		if sync != "" {
			t += " " + s.c(yellow, sync)
		}
	}
	return t
}

// AbbrevHome shortens a leading home directory to "~".
func AbbrevHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	switch {
	case p == home:
		return "~"
	case strings.HasPrefix(p, home+string(os.PathSeparator)):
		return "~" + p[len(home):]
	}
	return p
}

// windowText renders one limit window as **얼마나 남았나**(100-사용률)다. 쓴 양보다
// 남은 양이 지금 무엇을 할 수 있는지에 바로 답한다. 색은 그대로 사용률로 고르므로
// (pctColor·alertStyle) 숫자가 작아질수록 빨개진다 — 숫자와 색이 같은 방향이다.
func windowText(name string, w *store.Window, v View, s Style) string {
	pct, style := s.alertPct(name, w, v.Alert)
	t := name + " " + s.c(style, pct)
	if !w.ResetsAt.IsZero() && w.ResetsAt.After(v.Now) {
		t += " " + s.c(dim, "("+resetText(w.ResetsAt, v.Now)+")")
	}
	return t
}

// alertPct renders the percentage for one window, as a 배지 when this window
// raised the alert.
//
// 굵은 빨강 "글씨" 로는 묻힌다 — 같은 줄의 ctx 와 다른 창도 임계색을 쓰고 있어서,
// 경보가 "조금 더 빨간 글씨" 가 된다. 배지는 줄에서 유일한 색면(色面)이라 바로
// 눈에 걸린다. 양옆 한 칸은 배지가 이웃 글자에 붙어 답답해 보이는 것을 막는다.
//
// 여백은 burst 의 꺼진 프레임에도 남긴다 — 프레임마다 폭이 바뀌면 줄 전체가
// 좌우로 출렁인다. 깜빡이는 것은 색이지 자리가 아니다.
func (s Style) alertPct(name string, w *store.Window, a core.Alert) (string, string) {
	pct := fmt.Sprintf("%.0f%%", 100-w.Percent)
	if a.Level == core.AlertNone || a.Window != name {
		return pct, s.pctColor(w.Percent)
	}
	pct = " " + pct + " "
	if a.Burst && !a.On {
		return pct, bold + red // 깜빡임의 꺼진 프레임
	}
	return pct, redBG
}

// resetText answers "언제 풀리나". 오늘 안이면 시각 하나로 끝난다 — `↻14:40` 은
// 8칸 고정이라 남은 시간이 줄어도 뒤가 밀리지 않고, ↻ 가 "여기서 다시 시작한다"를
// 바로 전한다.
//
// 기준은 24시간이 아니라 **달력 날짜**다. 23시간 뒤 리셋을 `↻14:00` 으로 내면
// 오늘 14시로 읽힌다 — 7d 창은 늘 이 구간에 들어오고, 하필 사용률이 높아 화면에
// 올라왔을 때 그렇다. 날이 바뀌면 시각만으로 어느 날인지 알 수 없으므로 남은
// 시간을 낸다.
func resetText(at, now time.Time) string {
	at, n := at.Local(), now.Local()
	if at.Year() != n.Year() || at.YearDay() != n.YearDay() {
		return Duration(at.Sub(n))
	}
	return "↻" + at.Format("15:04")
}

func statusNote(v View, s Style) string {
	uf := v.Usage
	if v.Limits.FromStdin {
		if hit, _ := v.Limits.Exhausted(); !hit {
			return ""
		}
	}
	switch {
	case uf.Usage == nil && uf.LastError != "":
		return s.c(yellow, "usage: "+shortErr(uf.LastError))
	case uf.Usage == nil && !v.Limits.FromStdin:
		return s.c(dim, "usage …")
	case uf.Usage != nil && v.Now.Sub(uf.Usage.FetchedAt) > core.StaleAfter:
		return s.c(yellow, "⚠︎ stale "+Duration(v.Now.Sub(uf.Usage.FetchedAt)))
	}
	return ""
}

// creditStandalone reports whether the credit text needs its own row.
//
// 두 가지 이유로 내린다. 하나는 강조가 붙는 상태(크레딧 소진 중 · 한도 소진 ·
// 조회 중 · 비활성)로, 문장이 길어지는 데다 줄이 하나 느는 것 자체가 신호가 된다
// — creditLine 의 분기와 짝이므로 한쪽만 고치지 않는다. 다른 하나는 폭으로,
// 터미널을 알 때(COLUMNS) 붙이면 넘칠 경우다. 넘치면 터미널이 잘라 내거나 감아서
// 어차피 두 줄이 되는데, 그 두 줄은 우리가 고른 자리에서 갈리지 않는다.
func (s Style) creditStandalone(v View, row, credit, sep string) bool {
	cv := v.Credits
	if !cv.Enabled || cv.Spending {
		return true
	}
	if hit, _ := v.Limits.Exhausted(); hit {
		return true
	}
	if s.Width <= 0 {
		return false
	}
	w := displayWidth(row) + displayWidth(credit)
	if row != "" {
		w += displayWidth(sep)
	}
	return w > s.Width-rightMargin
}

func creditLine(v View, s Style) string {
	cv := v.Credits
	if !cv.Show {
		return ""
	}
	cur := v.Config.Currency
	if !cv.Enabled {
		if v.Usage.Usage != nil && v.Usage.Usage.Extra != nil && !v.Usage.Usage.Extra.Enabled {
			return s.c(dim, "💳 크레딧 비활성 — 한도 reset까지 대기")
		}
		return s.c(yellow, "💳 한도 소진 · 크레딧 조회 중…")
	}
	// 남은 금액을 낸다 — 5h·7d 가 남은 비율인데 크레딧만 쓴 금액이면 방향이 엇갈려
	// 읽는 사람이 뒤집어 본다. 색도 한도 창과 같은 규칙으로 골라서, 90% 를 쓴 상태가
	// 흐린 회색으로 조용히 지나가지 않게 한다.
	amount, tone := money(cur, cv.Used), dim
	hasLimit := cv.Limit != nil && *cv.Limit > 0
	if hasLimit {
		amount = money(cur, *cv.Limit-cv.Used)
		tone = s.pctColor(cv.Used / *cv.Limit * 100)
	}
	t := s.c(dim, "💳") + " " + s.c(tone, amount)
	// 한도는 괄호로 감싼다 — 한도 창의 "86% (↻14:40)" 과 같은 꼴이라 값 뒤의
	// 괄호는 부가 정보라는 규칙이 줄 전체에서 한결같아진다.
	//
	// 한도를 모르면 남은 금액을 계산할 수 없어 쓴 금액이 나간다. 같은 서식으로
	// 내면 남은 금액으로 읽히므로("$9.83" 이 9.83 남은 것으로 보인다) 무엇인지
	// 밝힌다. monthly_limit 은 비공식 API 의 optional 필드라 언제든 빠질 수 있다.
	if hasLimit {
		t += s.c(dim, " ("+moneyShort(cur, *cv.Limit)+")")
	} else {
		t += s.c(dim, " 사용")
	}
	tail := ""
	if cv.SpentWindow > 0 {
		tail += " · 이번 window +" + money(cur, cv.SpentWindow)
	}
	if cv.Spending {
		return t + s.c(bold+red, tail+" · 크레딧 소진 중")
	}
	if hit, _ := v.Limits.Exhausted(); hit {
		return t + s.c(yellow, tail+" · 다음 prompt부터 크레딧 사용")
	}
	return t + s.c(dim, tail)
}

func (s Style) pctColor(p float64) string {
	if c, ok := s.rgb(gradientRGB(p)); ok {
		return c
	}
	switch {
	case p >= 90:
		return red
	case p >= 70:
		return yellow
	default:
		return green
	}
}

// ctxColor is the 파랑 계열. 한도 창과 **다른 축**이라 색도 다른 축을 쓴다 —
// context 가 차는 것은 막히는 일이 아니라 곧 compaction 이 되는 일이고, 빨강은
// "여기서 멈춘다" 를 뜻하는 자리로 남겨 둔다(한도 창과 경보). 한 줄에 임계색이
// 셋(ctx · 5h · 7d)이면 경보 배지가 색으로 경쟁에서 진다.
//
// 임계를 숫자로 정하지 않은 이유도 있다. payload 에 compaction 지점이 오지 않아
// 몇 %가 위험한지 말할 근거가 없다 — 말할 수 없는 것을 색으로 말하지 않는다.
// 대신 차오를수록 밝아지기만 하되, **뒤로 몰아서** 밝아진다. 선형으로 올리면
// 40% 에서 벌써 중간 밝기가 되어 볼 일 없는 구간이 눈을 끈다. 지수를 씌워
// 80% 를 넘어서부터 확 밝아지게 한다 — 41% 는 0.14, 80% 는 0.60, 95% 는 0.89.
func (s Style) ctxColor(p float64) string {
	p = math.Max(0, math.Min(100, p))
	t := math.Pow(p/100, ctxCurve)
	if c, ok := s.rgb(int(75+45*t), int(95+95*t), int(130+125*t)); ok {
		return c
	}
	// truecolor 가 없을 때의 계단. 경계도 같은 이유로 뒤에 둔다.
	switch {
	case p >= ctxHighAt:
		return ctxHigh
	case p >= ctxMidAt:
		return ctxMid
	default:
		return ctxLow
	}
}

// gradientRGB goes green(여유) → 올리브 → red(소진) as 사용률 rises. 지수는 눈이
// 밝기를 선형으로 읽지 않아서 넣은 것이다 — 선형으로 섞으면 중간이 탁해진다.
func gradientRGB(used float64) (int, int, int) {
	t := (100 - math.Max(0, math.Min(100, used))) / 100 // 남은 비율
	return int(230 * math.Pow(1-t, 0.7)), int(200 * math.Pow(t, 0.6)), int(30 * t)
}

func money(cur string, v float64) string {
	return fmt.Sprintf("%s%.2f", cur, math.Max(v, 0))
}

// moneyShort drops a zero fraction: $100.00 → $100, $100.50 → $100.50.
//
// 한도처럼 잘 변하지 않는 값에만 쓴다. 매 렌더 바뀌는 금액에서 소수점을 떼면
// $38.40 ↔ $38 사이에서 폭이 3칸씩 오가며 뒤가 밀린다 — 리셋 표기를 시각으로
// 고정한 것과 같은 이유다.
func moneyShort(cur string, v float64) string {
	v = math.Max(v, 0)
	if v == math.Trunc(v) {
		return fmt.Sprintf("%s%.0f", cur, v)
	}
	return fmt.Sprintf("%s%.2f", cur, v)
}

func shortErr(e string) string {
	if len(e) > 40 {
		return e[:40] + "…"
	}
	return e
}

// Duration formats a positive duration compactly: 45m, 1h 20m, 2d 4h.
// 단위 사이를 띄우는 건 눈이 숫자와 단위를 한 덩어리로 묶어 읽기 때문이다 —
// 붙여 쓰면 "1h20m"이 한 토큰으로 보여 자릿수를 다시 세게 된다.
func Duration(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	m := int(d.Minutes())
	days, hours, mins := m/1440, (m%1440)/60, m%60
	// 0인 아랫단위는 떼어 낸다 — "1d 0h"의 0h는 자리만 먹고 아무것도 말하지 않는다.
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
