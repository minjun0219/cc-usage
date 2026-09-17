// Package config loads the cc-usage settings file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Source decides where 5h/7d limits come from.
const (
	SourceAuto  = "auto"  // stdin rate_limits if seen recently, otherwise OAuth usage API
	SourceStdin = "stdin" // Pro/Max: stdin only; the API is called only after a limit is hit
	SourceAPI   = "api"   // Team/Enterprise: poll the OAuth usage API
)

// Config is the whole settings file — 계정 하나를 상정한다. 한 머신에서 여러
// 계정을 보려면 설정을 나누는 게 아니라 프로세스를 나눈다: CC_USAGE_CONFIG 와
// XDG_CACHE_HOME 을 다른 곳으로 주면 설정도 cache 도 통째로 갈린다.
type Config struct {
	ConfigDir         string  `json:"config_dir"`
	Source            string  `json:"source"`
	KeychainService   string  `json:"keychain_service"`
	CredentialsFile   string  `json:"credentials_file"`
	TokenEnv          string  `json:"token_env"`
	PollSeconds       int     `json:"poll_seconds"`
	CreditPollSeconds int     `json:"credit_poll_seconds"`
	CreditDivisor     float64 `json:"credit_divisor"`
	Currency          string  `json:"currency"`
	AlwaysShowCredits bool    `json:"always_show_credits"`
	Guard             bool    `json:"guard"`

	// AlertPercent is the "임박" threshold. 설정에 없으면 90, **0이면 임박 경고를
	// 끈다**(소진 강조는 남는다). 0이 "끔" 이 되려면 "설정에 없음" 과 구분해야 해서
	// 포인터다. 읽을 때는 Alert() 를 쓴다.
	AlertPercent *float64 `json:"alert_percent"`

	// ExtraCommands append other tools' statusline output below cc-usage's own
	// lines. 배(repo)별 사정을 코드가 아니라 설정에 두기 위한 창구다.
	ExtraCommands []ExtraCommand `json:"extra_commands"`
}

// ExtraCommand is one external statusline segment. Command is an argv list (no
// shell); the placeholders {{session_id}} and {{cwd}} are substituted in each
// element. 값이 빈 placeholder가 하나라도 있으면 그 명령은 건너뛴다.
type ExtraCommand struct {
	Command   []string `json:"command"`
	TimeoutMS int      `json:"timeout_ms"`
}

// Timeout bounds one extra command; statusline은 매 렌더마다 도니 짧게 둔다.
func (e ExtraCommand) Timeout() time.Duration {
	if e.TimeoutMS <= 0 {
		return 300 * time.Millisecond
	}
	return time.Duration(e.TimeoutMS) * time.Millisecond
}

// Alert is the 임박 threshold to compare 사용률 against. 0이면 임박 경고를
// 쓰지 않는다 — 범위를 벗어난 값도 0으로 본다(경보를 통째로 잃는 대신 소진만 남는다).
func (c *Config) Alert() float64 {
	if c.AlertPercent == nil {
		return 90
	}
	p := *c.AlertPercent
	if p < 0 || p > 100 {
		return 0
	}
	return p
}

func (c *Config) Poll() time.Duration       { return time.Duration(c.PollSeconds) * time.Second }
func (c *Config) CreditPoll() time.Duration { return time.Duration(c.CreditPollSeconds) * time.Second }

// Path returns the config file path ($CC_USAGE_CONFIG or ~/.config/cc-usage/config.json).
func Path() string {
	if v := os.Getenv("CC_USAGE_CONFIG"); v != "" {
		return Expand(v)
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "cc-usage", "config.json")
	}
	return Expand("~/.config/cc-usage/config.json")
}

// Load reads the config. 파일이 없으면 기본값만으로 동작한다 — statusline은
// 설정이 없다고 비면 안 된다.
func Load() (*Config, error) {
	cfg := &Config{}
	b, err := os.ReadFile(Path())
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf("config %s: %w", Path(), err)
		}
	}
	cfg.ApplyDefaults()
	return cfg, nil
}

func (c *Config) ApplyDefaults() {
	if c.ConfigDir == "" {
		c.ConfigDir = "~/.claude"
	}
	c.ConfigDir = Expand(c.ConfigDir)
	if c.Source == "" {
		c.Source = SourceAuto
	}
	if c.KeychainService == "" {
		c.KeychainService = "Claude Code-credentials"
	}
	if c.CredentialsFile == "" {
		c.CredentialsFile = filepath.Join(c.ConfigDir, ".credentials.json")
	}
	c.CredentialsFile = Expand(c.CredentialsFile)
	if c.PollSeconds <= 0 {
		c.PollSeconds = 300
	}
	if c.CreditPollSeconds <= 0 {
		c.CreditPollSeconds = 300
	}
	if c.CreditDivisor <= 0 {
		c.CreditDivisor = 100
	}
	if c.Currency == "" {
		c.Currency = "$"
	}
}

// Expand replaces a leading ~ with the home directory.
func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
