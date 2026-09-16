// Package render builds the statusline text.
package render

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"cc-usage/internal/config"
	"cc-usage/internal/core"
	"cc-usage/internal/git"
	"cc-usage/internal/store"
)

type Style struct{ Color bool }

func DefaultStyle() Style {
	return Style{Color: os.Getenv("NO_COLOR") == ""}
}

const (
	reset  = "\033[0m"
	dim    = "\033[2m"
	green  = "\033[32m"
	cyan   = "\033[36m"
	blue   = "\033[34m"
	yellow = "\033[33m"
	red    = "\033[31m"
	redBG  = "\033[41;97m"
	bold   = "\033[1m"
)

func (s Style) c(code, text string) string {
	if !s.Color || text == "" {
		return text
	}
	return code + text + reset
}

type View struct {
	Profile    *config.Profile
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
	if l := v.Profile.StatusLabel(); l != "" {
		parts = append(parts, s.c(dim, "["+l+"]"))
	}
	if v.Model != "" {
		parts = append(parts, v.Model)
	}
	if v.ContextPct != nil {
		parts = append(parts, "ctx "+s.c(pctColor(*v.ContextPct), fmt.Sprintf("%.0f%%", *v.ContextPct)))
	}
	if w := v.Limits.FiveHour; w != nil {
		parts = append(parts, windowText("5h", w, v, s))
	}
	if w := v.Limits.SevenDay; w != nil {
		parts = append(parts, windowText("7d", w, v, s))
	}
	if note := statusNote(v, s); note != "" {
		parts = append(parts, note)
	}
	var lines []string
	if dl := dirLine(v, s); dl != "" {
		lines = append(lines, dl)
	}
	if row := strings.Join(parts, s.c(dim, " · ")); row != "" {
		lines = append(lines, row)
	}
	if cl := creditLine(v, s); cl != "" {
		lines = append(lines, cl)
	}
	return lines
}

func dirLine(v View, s Style) string {
	if v.Dir == "" {
		return ""
	}
	t := s.c(cyan, AbbrevHome(v.Dir))
	if b := branchText(v.Git, s); b != "" {
		t += "  " + b
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

func windowText(name string, w *store.Window, v View, s Style) string {
	t := name + " " + s.c(alertStyle(name, w, v.Alert), fmt.Sprintf("%.0f%%", w.Percent))
	if !w.ResetsAt.IsZero() && w.ResetsAt.After(v.Now) {
		t += " " + s.c(dim, resetText(w.ResetsAt, v.Now))
	}
	return t
}

// alertStyle emphasises the window that raised the alert: 단계가 올라간 직후
// 몇 초만 배지로 깜빡이고(눈을 끌고), 그 뒤에는 굵은 빨강으로 가만히 남는다.
// 계속 움직이는 표시는 결국 배경이 된다.
func alertStyle(name string, w *store.Window, a core.Alert) string {
	if a.Level == core.AlertNone || a.Window != name {
		return pctColor(w.Percent)
	}
	if a.Burst && a.On {
		return redBG
	}
	return bold + red
}

// resetText is the time left until the window resets, plus the local wall clock
// of the reset itself — "언제 풀리나"는 남은 시간보다 시각이 쓸모 있다. 하루를
// 넘기면 시각만으로는 어느 날인지 모르니 남은 시간만 남긴다.
func resetText(at, now time.Time) string {
	d := at.Sub(now)
	t := Duration(d)
	if d < 24*time.Hour {
		t += "→" + at.Local().Format("15:04")
	}
	return t
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

func creditLine(v View, s Style) string {
	cv := v.Credits
	if !cv.Show {
		return ""
	}
	cur := v.Profile.Currency
	if !cv.Enabled {
		if v.Usage.Usage != nil && v.Usage.Usage.Extra != nil && !v.Usage.Usage.Extra.Enabled {
			return s.c(dim, "💳 크레딧 비활성 — 한도 reset까지 대기")
		}
		return s.c(yellow, "💳 한도 소진 · 크레딧 조회 중…")
	}
	t := "💳 " + money(cur, cv.Used)
	if cv.Limit != nil {
		t += " / " + money(cur, *cv.Limit)
	}
	if cv.SpentWindow > 0 {
		t += " · 이번 window +" + money(cur, cv.SpentWindow)
	}
	if cv.Spending {
		return s.c(bold+red, t+" · 크레딧 소진 중")
	}
	if hit, _ := v.Limits.Exhausted(); hit {
		return s.c(yellow, t+" · 다음 prompt부터 크레딧 사용")
	}
	return s.c(dim, t)
}

func pctColor(p float64) string {
	switch {
	case p >= 90:
		return red
	case p >= 70:
		return yellow
	default:
		return green
	}
}

func money(cur string, v float64) string {
	return fmt.Sprintf("%s%.2f", cur, math.Max(v, 0))
}

func shortErr(e string) string {
	if len(e) > 40 {
		return e[:40] + "…"
	}
	return e
}

// Duration formats a positive duration compactly: 45m, 1h20m, 2d4h.
func Duration(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	m := int(d.Minutes())
	days, hours, mins := m/1440, (m%1440)/60, m%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
