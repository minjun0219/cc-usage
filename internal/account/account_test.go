package account

import (
	"os"
	"path/filepath"
	"testing"

	"cc-usage/internal/config"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEmail(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".claude")

	// 기본 설치: 홈 루트에만 있다. <config_dir>/.claude.json 은 없다.
	write(t, filepath.Join(home, ".claude.json"), `{"oauthAccount":{"emailAddress":"me@example.com","organizationName":"ACME"}}`)
	if got := Email(&config.Config{ConfigDir: cfgDir}); got != "me@example.com" {
		t.Errorf("홈 루트 폴백: %q", got)
	}

	// config_dir 쪽이 있으면 그쪽이 이긴다 (CLAUDE_CONFIG_DIR 을 쓰는 경우 대비).
	write(t, filepath.Join(cfgDir, ".claude.json"), `{"oauthAccount":{"emailAddress":"work@example.com"}}`)
	if got := Email(&config.Config{ConfigDir: cfgDir}); got != "work@example.com" {
		t.Errorf("config_dir 우선: %q", got)
	}
}

func TestEmailUnknownIsEmpty(t *testing.T) {
	// 파일 없음 · 깨진 JSON · 필드 없음이 모두 "모른다"로 떨어져야 한다.
	// 배지가 안 뜰 뿐 statusline 은 그대로 나간다.
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := &config.Config{ConfigDir: filepath.Join(home, ".claude")}

	if got := Email(cfg); got != "" {
		t.Errorf("파일 없음: %q", got)
	}
	write(t, filepath.Join(home, ".claude.json"), `{ 깨진 json`)
	if got := Email(cfg); got != "" {
		t.Errorf("깨진 JSON: %q", got)
	}
	write(t, filepath.Join(home, ".claude.json"), `{"oauthAccount":{"organizationName":"ACME"}}`)
	if got := Email(cfg); got != "" {
		t.Errorf("emailAddress 없음: %q", got)
	}
	if got := Email(nil); got != "" {
		t.Errorf("nil config: %q", got)
	}
}
