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
	yellow = "\033[33m"
	red    = "\033[31m"
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
	Model      string
	ContextPct *float64
	Limits     core.Limits
	Usage      *store.UsageFile
	Credits    core.CreditView
	Now        time.Time
}

// Lines returns one or two statusline rows.
func Lines(v View, s Style) []string {
	parts := []string{s.c(dim, "["+v.Profile.Label+"]")}
	if v.Model != "" {
		parts = append(parts, v.Model)
	}
	if v.ContextPct != nil {
		parts = append(parts, "ctx "+s.c(pctColor(*v.ContextPct), fmt.Sprintf("%.0f%%", *v.ContextPct)))
	}
	if w := v.Limits.FiveHour; w != nil {
		parts = append(parts, windowText("5h", w, v.Now, s))
	}
	if w := v.Limits.SevenDay; w != nil {
		parts = append(parts, windowText("7d", w, v.Now, s))
	}
	if note := statusNote(v, s); note != "" {
		parts = append(parts, note)
	}
	lines := []string{strings.Join(parts, s.c(dim, " · "))}
	if cl := creditLine(v, s); cl != "" {
		lines = append(lines, cl)
	}
	return lines
}

func windowText(name string, w *store.Window, now time.Time, s Style) string {
	t := name + " " + s.c(pctColor(w.Percent), fmt.Sprintf("%.0f%%", w.Percent))
	if !w.ResetsAt.IsZero() && w.ResetsAt.After(now) {
		t += " " + s.c(dim, Duration(w.ResetsAt.Sub(now)))
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
