package core

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"cc-usage/internal/store"
)

// Input is the subset of the Claude Code statusline JSON that cc-usage uses.
type Input struct {
	SessionID string `json:"session_id"`
	Model     struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
	ContextWindow struct {
		UsedPercentage *float64 `json:"used_percentage"`
	} `json:"context_window"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
	RateLimits *struct {
		FiveHour *stdinWindow `json:"five_hour"`
		SevenDay *stdinWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

type stdinWindow struct {
	UsedPercentage *float64        `json:"used_percentage"`
	ResetsAt       json.RawMessage `json:"resets_at"`
}

// ParseInput tolerates empty or partial input.
func ParseInput(b []byte) (*Input, error) {
	in := &Input{}
	if len(strings.TrimSpace(string(b))) == 0 {
		return in, nil
	}
	err := json.Unmarshal(b, in)
	return in, err
}

// StdinLimits returns validated 5h/7d windows from stdin (nil when absent or invalid).
func (in *Input) StdinLimits() (five, seven *store.Window) {
	if in.RateLimits == nil {
		return nil, nil
	}
	return convert(in.RateLimits.FiveHour), convert(in.RateLimits.SevenDay)
}

func convert(w *stdinWindow) *store.Window {
	if w == nil || w.UsedPercentage == nil {
		return nil
	}
	p := *w.UsedPercentage
	// Guard against a known glitch where an epoch timestamp leaked into used_percentage.
	if p < 0 || p > 200 {
		return nil
	}
	if p > 100 {
		p = 100
	}
	return &store.Window{Percent: p, ResetsAt: parseResets(w.ResetsAt)}
}

// parseResets accepts epoch seconds (documented) or an RFC3339 string.
func parseResets(raw json.RawMessage) time.Time {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return time.Time{}
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if json.Unmarshal(raw, &str) == nil {
			if t, err := time.Parse(time.RFC3339Nano, str); err == nil {
				return t
			}
			s = str
		}
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
		if f > 1e12 { // milliseconds
			return time.UnixMilli(int64(f))
		}
		return time.Unix(int64(f), 0)
	}
	return time.Time{}
}
