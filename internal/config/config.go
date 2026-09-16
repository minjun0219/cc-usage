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
	Label             string  `json:"label"`
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
}

type Config struct {
	DefaultProfile string              `json:"default_profile"`
	Profiles       map[string]*Profile `json:"profiles"`
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
	if p.Label == "" {
		p.Label = p.Name
	}
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
