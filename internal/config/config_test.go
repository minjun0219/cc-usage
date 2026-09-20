package config

import (
	"path/filepath"
	"testing"
)

func TestAlertPercent(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	// 설정에 없으면 90, 0이면 끔. 0이 "끔"이 되려면 없음과 구분돼야 한다 —
	// 예전에는 0이 기본값 90으로 덮여서 README 가 거짓말을 하고 있었다.
	cases := []struct {
		name string
		in   *float64
		want float64
	}{
		{"설정 없음 → 기본 90", nil, 90},
		{"0 → 임박 경고 끔", f(0), 0},
		{"80 → 그대로", f(80), 80},
		{"범위 밖(-1) → 끔", f(-1), 0},
		{"범위 밖(101) → 끔", f(101), 0},
	}
	for _, c := range cases {
		cfg := &Config{AlertPercent: c.in}
		cfg.ApplyDefaults()
		if got := cfg.Alert(); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestClaudeConfigDirWinsOverSetting(t *testing.T) {
	// $CLAUDE_CONFIG_DIR 가 걸린 세션은 Claude Code 자신이 그쪽 자격 증명을
	// 쓴다(실측: 다른 값을 주면 loggedIn 이 false 가 된다). 설정에 적어 둔
	// config_dir 이 이기면 cc-usage 는 **다른 계정의 token** 을 집는다.
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := filepath.Join(home, ".claude-work")
	t.Setenv("CLAUDE_CONFIG_DIR", work)

	c := &Config{ConfigDir: filepath.Join(home, ".claude-personal")}
	c.ApplyDefaults()
	if c.ConfigDir != work {
		t.Errorf("config_dir: %q (환경변수 %q 를 따라야 한다)", c.ConfigDir, work)
	}
	// creds 는 그 디렉터리에서 찾는다. keychain 이름 규칙이 불확실해도 이
	// 폴백이 맞는 계정의 token 을 집어 준다.
	if want := filepath.Join(work, ".credentials.json"); c.CredentialsFile != want {
		t.Errorf("credentials_file: %q, want %q", c.CredentialsFile, want)
	}
}

func TestConfigDirDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	// 환경변수가 없으면 설정값이 그대로 산다.
	c := &Config{ConfigDir: filepath.Join(home, ".claude-personal")}
	c.ApplyDefaults()
	if want := filepath.Join(home, ".claude-personal"); c.ConfigDir != want {
		t.Errorf("설정값이 살아야 한다: %q", c.ConfigDir)
	}
	// 둘 다 없으면 기본값.
	d := &Config{}
	d.ApplyDefaults()
	if want := filepath.Join(home, ".claude"); d.ConfigDir != want {
		t.Errorf("기본값: %q, want %q", d.ConfigDir, want)
	}
}

func TestKeychainDefaultOnlyForDefaultDir(t *testing.T) {
	// 접미사 없는 이름은 config_dir 과 무관하게 같은 값이라, 비기본 dir 에서
	// 읽으면 반드시 기본 계정의 token 을 집는다. token.go 가 keychain 을 먼저
	// 보므로 creds 파일 폴백까지 가지도 않는다.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	d := &Config{}
	d.ApplyDefaults()
	if d.KeychainService != "Claude Code-credentials" {
		t.Errorf("기본 dir 은 keychain 을 쓴다: %q", d.KeychainService)
	}

	w := &Config{ConfigDir: filepath.Join(home, ".claude-work")}
	w.ApplyDefaults()
	if w.KeychainService != "" {
		t.Errorf("비기본 dir 은 keychain 을 건너뛴다: %q", w.KeychainService)
	}

	// 사용자가 적었으면 그대로 쓴다 — 그 dir 의 이름을 아는 경우다.
	m := &Config{ConfigDir: filepath.Join(home, ".claude-work"), KeychainService: "Claude Code-credentials-abc12345"}
	m.ApplyDefaults()
	if m.KeychainService != "Claude Code-credentials-abc12345" {
		t.Errorf("명시값이 살아야 한다: %q", m.KeychainService)
	}

	// 환경변수로 비기본이 된 경우에도 같다.
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-env"))
	e := &Config{}
	e.ApplyDefaults()
	if e.KeychainService != "" {
		t.Errorf("환경변수로 비기본이 되면 건너뛴다: %q", e.KeychainService)
	}
}
