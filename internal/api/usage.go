// Package api calls the undocumented Claude OAuth usage endpoint.
// The response shape is not a public contract; every field is treated as optional.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"cc-usage/internal/store"
)

const (
	URL        = "https://api.anthropic.com/api/oauth/usage"
	betaHeader = "oauth-2025-04-20"
)

// RateLimitedError carries Retry-After when the endpoint returns 429.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string { return "rate limited (429)" }

var ErrUnauthorized = errors.New("unauthorized (401/403)")

// FetchRaw returns the raw response body.
func FetchRaw(ctx context.Context, token string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	url := URL
	if v := os.Getenv("CC_USAGE_API_URL"); v != "" { // tests only
		url = v
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", betaHeader)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "cc-usage")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &RateLimitedError{RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrUnauthorized
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(body), 120))
	}
	return body, nil
}

// Fetch returns the parsed usage.
func Fetch(ctx context.Context, token string) (*store.Usage, error) {
	body, err := FetchRaw(ctx, token)
	if err != nil {
		return nil, err
	}
	return Parse(body, time.Now())
}

type rawWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

type rawExtra struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *float64 `json:"monthly_limit"`
	UsedCredits  *float64 `json:"used_credits"`
	Utilization  *float64 `json:"utilization"`
}

type rawResponse struct {
	FiveHour   *rawWindow `json:"five_hour"`
	SevenDay   *rawWindow `json:"seven_day"`
	ExtraUsage *rawExtra  `json:"extra_usage"`
}

// Parse converts the response body. utilization is assumed to be 0-100 (verify with `cc-usage probe`).
func Parse(body []byte, now time.Time) (*store.Usage, error) {
	var r rawResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse usage: %w", err)
	}
	u := &store.Usage{FetchedAt: now, FiveHour: window(r.FiveHour), SevenDay: window(r.SevenDay)}
	if e := r.ExtraUsage; e != nil {
		u.Extra = &store.Extra{
			Enabled:      e.IsEnabled,
			UsedCredits:  e.UsedCredits,
			MonthlyLimit: e.MonthlyLimit,
			Utilization:  e.Utilization,
		}
	}
	return u, nil
}

func window(w *rawWindow) *store.Window {
	if w == nil || w.Utilization == nil {
		return nil
	}
	out := &store.Window{Percent: *w.Utilization}
	if w.ResetsAt != nil {
		if t, err := time.Parse(time.RFC3339Nano, *w.ResetsAt); err == nil {
			out.ResetsAt = t
		}
	}
	return out
}

func retryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(v); err == nil {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
