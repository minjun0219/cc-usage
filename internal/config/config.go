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

	// AlertPercent is the "임박" threshold. 0이면 소진(100%)에서만 강조한다.
	AlertPercent float64 `json:"alert_percent"`
	// Notify fires once per 단계 when a limit is hit. nil이면 알림 없음.
	Notify *Notify `json:"notify"`

	// ExtraCommands append other tools' statusline output below cc-usage's own
	// lines. 배(repo)별 사정을 코드가 아니라 설정에 두기 위한 창구다.
	ExtraCommands []ExtraCommand `json:"extra_commands"`
}

// Notify runs once when a limit first reaches 임박 or 소진. Command is an argv
// list with {{level}} · {{window}} · {{percent}} · {{message}} 치환.
// 어떤 알림 수단을 쓸지는 설정에만 있다 — osascript든 무엇이든.
type Notify struct {
	// Enabled를 따로 두는 이유: 명령은 적어 둔 채 껐다 켰다 하기 위해서다.
	// 끄자고 블록을 지우면 다시 켤 때 명령을 기억해 내야 한다.
	Enabled *bool    `json:"enabled"`
	Command []string `json:"command"`
}

// On reports whether notifications should fire. 블록이 있고 명령이 있으면
// 기본은 켜짐이고, enabled를 false로 적었을 때만 꺼진다.
func (n *Notify) On() bool {
	if n == nil || len(n.Command) == 0 {
		return false
	}
	return n.Enabled == nil || *n.Enabled
}

// NotifyState explains why notifications will or will not fire. 인자는 숨긴다 —
// webhook URL이나 토큰을 넣는 게 흔한데 doctor 출력은 로그에 남는다.
func (c *Config) NotifyState() string {
	switch {
	case c.Notify == nil:
		return "설정 없음"
	case len(c.Notify.Command) == 0:
		return "command 없음"
	case !c.Notify.On():
		return "enabled=false (명령은 보존됨)"
	}
	return fmt.Sprintf("on — %s (인자 %d개, 내용은 숨김)",
		c.Notify.Command[0], len(c.Notify.Command)-1)
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
	if c.AlertPercent == 0 {
		c.AlertPercent = 90
	}
	if c.AlertPercent < 0 || c.AlertPercent > 100 {
		c.AlertPercent = 0 // 범위를 벗어나면 임박 경고를 끈다 (소진 강조는 남는다)
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
