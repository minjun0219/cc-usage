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

	// Badges marks the statusline by **which account is logged in**, keyed by
	// email. 목록에 없는 계정은 아무것도 붙지 않는다 — 평소 쓰는 계정을 안 적으면
	// 표시가 뜨는 것 자체가 "여기는 평소 자리가 아니다" 라는 신호가 된다.
	//
	// 계정 전환은 지원하지 않는다. 지금 로그인된 계정을 알아보게만 한다.
	Badges map[string]Badge `json:"badges"`

	// ExtraCommands append other tools' statusline output below cc-usage's own
	// lines. 배(repo)별 사정을 코드가 아니라 설정에 두기 위한 창구다.
	ExtraCommands []ExtraCommand `json:"extra_commands"`
}

// Badge is how one account marks the statusline.
//
// Emoji 가 있으면 그것만 쓴다 — 이모지는 제 색을 가지고 있어 Color 를 볼 이유가
// 없다. 없으면 Glyph(기본 ●)를 Color 로 칠한다.
//
// 빨강 계열은 피하는 게 좋다. 이 줄에서 빨강은 "여기서 멈춘다"(한도·경보)를
// 뜻하도록 축을 갈라 뒀는데, 배지가 빨강이면 같은 말을 두 가지로 쓰게 된다.
// 막지는 않는다 — 고르는 쪽이 알고 고르면 될 일이다.
type Badge struct {
	Emoji string `json:"emoji"`
	Glyph string `json:"glyph"`
	Color string `json:"color"`
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
	// $CLAUDE_CONFIG_DIR 가 설정값을 **이긴다.** 그것이 지금 도는 세션이 실제로
	// 쓰는 값이기 때문이다 — Claude Code 는 이 변수를 절대적으로 따른다. 다른
	// 값을 주면 로그인 상태부터 갈린다(`claude auth status` 가 `loggedIn: false`
	// 를 낸다, 2026-09-19 실측). 즉 자격 증명을 공유하지 않는다.
	//
	// 여기서 설정값을 우선하면, 환경변수를 바꾼 세션에서 cc-usage 가 **다른
	// 계정의 token** 으로 API 를 부른다. 배지만 틀리는 게 아니라 5h/7d·크레딧
	// 숫자가 통째로 남의 것이 되고, 그럴듯해서 티도 나지 않는다.
	//
	// internal/account 가 같은 이유로 이 변수를 배타적으로 본다. 두 축이 같은
	// 것을 보게 하는 쪽이 여기다.
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		c.ConfigDir = d
	} else if c.ConfigDir == "" {
		c.ConfigDir = "~/.claude"
	}
	c.ConfigDir = Expand(c.ConfigDir)
	if c.Source == "" {
		c.Source = SourceAuto
	}
	// keychain 기본값은 **기본 config_dir 일 때만** 준다.
	//
	// `Claude Code-credentials` 는 접미사가 없어 config_dir 과 무관하게 같은
	// 값이다. 그래서 비기본 dir 에서 이걸 읽으면 **반드시 기본 계정의 token**
	// 을 집는다 — token.go 가 keychain 을 먼저 보므로 creds 파일 폴백까지 가지도
	// 않는다. config_dir 을 옳게 맞춰도 숫자가 남의 것이 되는 이유가 이것이었다.
	//
	// 비워 두면 keychain 을 건너뛰고 `<config_dir>/.credentials.json` 을 본다.
	// 못 찾으면 숫자가 안 나오는데, **틀린 계정의 숫자보다 낫다.** 그 dir 의
	// keychain 이름을 아는 사용자는 설정에 적으면 그대로 쓰인다.
	//
	// 현재 Claude Code 가 비기본 dir 에서 어떤 이름을 쓰는지는 확인하지 못했다
	// (알아내려면 로그인을 새로 태워야 한다). 옛 규칙은 sha256(config_dir)[:8]
	// 접미사였고 그 항목이 이 맥에 실존하지만, 지금은 기본 dir 에서 접미사 없는
	// 이름을 쓴다 — 규칙이 바뀌었다. 추론해서 고르지 않는 이유다.
	if c.KeychainService == "" && c.ConfigDir == defaultConfigDir() {
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

// defaultConfigDir is ~/.claude — Claude Code 가 손대지 않았을 때 쓰는 자리다.
func defaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
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
