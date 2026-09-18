// Package store persists cache files with atomic writes.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"cc-usage/internal/config"
)

// Window is one rate-limit window. Percent is 0-100.
type Window struct {
	Percent  float64   `json:"percent"`
	ResetsAt time.Time `json:"resets_at,omitempty"`
}

// Extra is the usage-credits (extra usage) block. UsedCredits and MonthlyLimit are raw API units.
type Extra struct {
	Enabled      bool     `json:"enabled"`
	UsedCredits  *float64 `json:"used_credits,omitempty"`
	MonthlyLimit *float64 `json:"monthly_limit,omitempty"`
	Utilization  *float64 `json:"utilization,omitempty"`
}

type Usage struct {
	FetchedAt time.Time `json:"fetched_at"`
	FiveHour  *Window   `json:"five_hour,omitempty"`
	SevenDay  *Window   `json:"seven_day,omitempty"`
	Extra     *Extra    `json:"extra,omitempty"`
}

// Baseline is the credit count when a limit window was first seen exhausted.
type Baseline struct {
	WindowKey string    `json:"window_key"`
	Credits   float64   `json:"credits"`
	At        time.Time `json:"at"`
}

// UsageFile is written only by `cc-usage refresh`.
type UsageFile struct {
	Usage           *Usage    `json:"usage,omitempty"`
	LastAttempt     time.Time `json:"last_attempt,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	Failures        int       `json:"failures,omitempty"`
	BackoffUntil    time.Time `json:"backoff_until,omitempty"`
	PrevCredits     *float64  `json:"prev_credits,omitempty"`
	CreditsRisingAt time.Time `json:"credits_rising_at,omitempty"`
	Baseline        *Baseline `json:"baseline,omitempty"`
}

// StateFile is written only by `cc-usage statusline` (what Claude Code passed on stdin).
type StateFile struct {
	ObservedAt      time.Time `json:"observed_at"`
	StdinLimitsSeen time.Time `json:"stdin_limits_seen,omitempty"`
	FiveHour        *Window   `json:"five_hour,omitempty"`
	SevenDay        *Window   `json:"seven_day,omitempty"`
	SpawnedAt       time.Time `json:"spawned_at,omitempty"`
	// AlertKey는 "<단계>@<window>@<리셋시각>"이다. 창이 리셋되거나 단계가
	// 올라가면 값이 달라져 강조와 알림이 다시 무장한다.
	AlertKey string    `json:"alert_key,omitempty"`
	AlertAt  time.Time `json:"alert_at,omitempty"`

	// AccountEmail 과 AccountAt 은 배지용 캐시다. .claude.json 을 매 렌더 읽지
	// 않기 위해, 한도 값이 움직였을 때만 다시 읽는다 — 계정이 바뀌면 한도도
	// 바뀌기 때문이다. AccountAt 은 그때의 한도 스냅샷이다.
	AccountEmail string `json:"account_email,omitempty"`
	AccountAt    string `json:"account_at,omitempty"`
}

// AllowFile is written by `cc-usage allow`.
type AllowFile struct {
	AllowUntil time.Time `json:"allow_until"`
}

// Dir is the cache directory. 계정을 나누는 축은 여기가 아니라 XDG_CACHE_HOME 이다
// — 다른 계정으로 돌리려면 설정과 cache 를 통째로 다른 경로에 두고 프로세스를 나눈다.
func Dir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		base = config.Expand("~/.cache")
	}
	return filepath.Join(base, "cc-usage")
}

func UsagePath() string { return filepath.Join(Dir(), "usage.json") }
func StatePath() string { return filepath.Join(Dir(), "state.json") }
func AllowPath() string { return filepath.Join(Dir(), "allow.json") }
func LockPath() string  { return filepath.Join(Dir(), "refresh.lock") }

// Read decodes a JSON file; a missing file leaves v untouched and returns nil.
func Read(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Write encodes v and atomically replaces path (mode 0600).
func Write(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// TryLock takes a non-blocking exclusive lock. ok=false means another process holds it.
func TryLock(path string) (unlock func(), ok bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, true, nil
}
