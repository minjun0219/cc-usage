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

func TestEmailFollowsClaudeConfigDir(t *testing.T) {
	// CLAUDE_CONFIG_DIR 로 연 세션은 그 계정으로 돌고 있다. 설정의 config_dir 이
	// 뭐라고 적혀 있든 이 세션의 계정은 환경변수 쪽이다.
	home := t.TempDir()
	t.Setenv("HOME", home)
	write(t, filepath.Join(home, ".claude.json"), `{"oauthAccount":{"emailAddress":"default@example.com"}}`)
	workDir := filepath.Join(home, ".claude-work")
	write(t, filepath.Join(workDir, ".claude.json"), `{"oauthAccount":{"emailAddress":"work@example.com"}}`)

	cfg := &config.Config{ConfigDir: filepath.Join(home, ".claude")} // 설정은 기본값 그대로
	if got := Email(cfg); got != "default@example.com" {
		t.Errorf("환경변수 없으면 기본: %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", workDir)
	if got := Email(cfg); got != "work@example.com" {
		t.Errorf("환경변수가 이겨야 한다: %q", got)
	}
	// 환경변수가 가리키는 곳에 파일이 없으면 다음 후보로 떨어진다.
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "nowhere"))
	if got := Email(cfg); got != "default@example.com" {
		t.Errorf("없는 경로면 폴백: %q", got)
	}
}
