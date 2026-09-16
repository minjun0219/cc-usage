// Package config loads cc-usage profiles (one per Claude account).
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

type Profile struct {
	Name              string  `json:"-"`
	Label             *string `json:"label"`
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

type Config struct {
	DefaultProfile string              `json:"default_profile"`
	Profiles       map[string]*Profile `json:"profiles"`
}

// StatusLabel is the "[…]" segment on the statusline. 설정에 없으면 profile 이름,
// 빈 문자열로 지정하면 세그먼트 자체를 내지 않는다 — profile이 하나뿐인 설치에서
// 매 줄 앞에 붙는 라벨은 구분이 아니라 노이즈다.
func (p *Profile) StatusLabel() string {
	if p.Label == nil {
		return p.Name
	}
	return *p.Label
}

// Display is the profile name used in messages; 라벨을 껐어도 비지 않는다.
func (p *Profile) Display() string {
	if l := p.StatusLabel(); l != "" {
		return l
	}
	return p.Name
}

func (p *Profile) Poll() time.Duration       { return time.Duration(p.PollSeconds) * time.Second }
func (p *Profile) CreditPoll() time.Duration { return time.Duration(p.CreditPollSeconds) * time.Second }

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

// Load reads the config. A missing file yields a single "default" profile.
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
	if len(cfg.Profiles) == 0 {
		cfg.Profiles = map[string]*Profile{"default": {}}
		cfg.DefaultProfile = "default"
	}
	for name, p := range cfg.Profiles {
		if p == nil {
			p = &Profile{}
			cfg.Profiles[name] = p
		}
		p.Name = name
		p.ApplyDefaults()
	}
	return cfg, nil
}

func (p *Profile) ApplyDefaults() {
	if p.ConfigDir == "" {
		p.ConfigDir = "~/.claude"
	}
	p.ConfigDir = Expand(p.ConfigDir)
	if p.Source == "" {
		p.Source = SourceAuto
	}
	if p.KeychainService == "" {
		p.KeychainService = "Claude Code-credentials"
	}
	if p.CredentialsFile == "" {
		p.CredentialsFile = filepath.Join(p.ConfigDir, ".credentials.json")
	}
	p.CredentialsFile = Expand(p.CredentialsFile)
	if p.PollSeconds <= 0 {
		p.PollSeconds = 300
	}
	if p.CreditPollSeconds <= 0 {
		p.CreditPollSeconds = 300
	}
	if p.AlertPercent == 0 {
		p.AlertPercent = 90
	}
	if p.AlertPercent < 0 || p.AlertPercent > 100 {
		p.AlertPercent = 0 // 범위를 벗어나면 임박 경고를 끈다 (소진 강조는 남는다)
	}
	if p.CreditDivisor <= 0 {
		p.CreditDivisor = 100
	}
	if p.Currency == "" {
		p.Currency = "$"
	}
}

// Resolve picks a profile: explicit > $CC_USAGE_PROFILE > $CLAUDE_CONFIG_DIR match > default.
func (c *Config) Resolve(explicit string) (*Profile, error) {
	for _, name := range []string{explicit, os.Getenv("CC_USAGE_PROFILE")} {
		if name == "" {
			continue
		}
		if p, ok := c.Profiles[name]; ok {
			return p, nil
		}
		return nil, fmt.Errorf("unknown profile %q", name)
	}
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = "~/.claude"
	}
	dir = filepath.Clean(Expand(dir))
	for _, p := range c.Profiles {
		if filepath.Clean(p.ConfigDir) == dir {
			return p, nil
		}
	}
	if p, ok := c.Profiles[c.DefaultProfile]; ok {
		return p, nil
	}
	if len(c.Profiles) == 1 {
		for _, p := range c.Profiles {
			return p, nil
		}
	}
	return nil, errors.New("no profile matched; pass --profile")
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
