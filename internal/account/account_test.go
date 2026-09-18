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

	// config_dir 쪽이 있으면 그쪽이 이긴다.
	write(t, filepath.Join(cfgDir, ".claude.json"), `{"oauthAccount":{"emailAddress":"work@example.com"}}`)
	if got := Email(&config.Config{ConfigDir: cfgDir}); got != "work@example.com" {
		t.Errorf("config_dir 우선: %q", got)
	}
}

func TestNoFallbackAwayFromTheActiveProfile(t *testing.T) {
	// 기본이 아닌 config_dir 을 적었는데 거기 .claude.json 이 없으면, 홈 루트를
	// 읽어선 안 된다 — 그건 **다른 계정**의 파일이다. token 은 config_dir 쪽
	// credentials 를 쓰므로 배지와 숫자가 서로 다른 계정을 가리키게 된다.
	// 틀린 배지보다 배지 없음이 낫다.
	home := t.TempDir()
	t.Setenv("HOME", home)
	write(t, filepath.Join(home, ".claude.json"), `{"oauthAccount":{"emailAddress":"default@example.com"}}`)

	custom := filepath.Join(home, ".claude-work") // 파일을 두지 않는다
	if got := Email(&config.Config{ConfigDir: custom}); got != "" {
		t.Errorf("다른 계정으로 새면 안 된다: %q", got)
	}
	// CLAUDE_CONFIG_DIR 이 걸려 있을 때도 같다.
	t.Setenv("CLAUDE_CONFIG_DIR", custom)
	if got := Email(&config.Config{ConfigDir: filepath.Join(home, ".claude")}); got != "" {
		t.Errorf("환경변수가 걸려 있으면 홈 루트로 안 떨어진다: %q", got)
	}
	// 기본 설치(config_dir 이 ~/.claude)에서는 종전대로 홈 루트를 읽는다.
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got := Email(&config.Config{ConfigDir: filepath.Join(home, ".claude")}); got != "default@example.com" {
		t.Errorf("기본 설치는 종전대로: %q", got)
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
	// 환경변수가 가리키는 곳에 파일이 없어도 홈 루트로 떨어지지 않는다.
	// 이 세션의 계정은 환경변수 쪽이고, 홈 루트 파일은 다른 계정의 것이다.
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "nowhere"))
	if got := Email(cfg); got != "" {
		t.Errorf("없는 경로면 배지 없음이어야 한다 (다른 계정으로 새면 안 됨): %q", got)
	}
}

func TestClaudeConfigDirIsExclusive(t *testing.T) {
	// CLAUDE_CONFIG_DIR 가 걸려 있는데 그쪽 파일이 있어도 oauthAccount 가 비어
	// 있는 경우(API key 인증 · 갓 만든 디렉터리 · 원자적 재작성 중). 다른 후보로
	// 새면 **다른 계정**의 이메일을 집어 온다.
	home := t.TempDir()
	t.Setenv("HOME", home)
	personal := filepath.Join(home, ".claude-personal")
	work := filepath.Join(home, ".claude-work")
	write(t, filepath.Join(home, ".claude.json"), `{"oauthAccount":{"emailAddress":"default@example.com"}}`)
	write(t, filepath.Join(personal, ".claude.json"), `{"oauthAccount":{"emailAddress":"personal@example.com"}}`)
	write(t, filepath.Join(work, ".claude.json"), `{"someOtherKey":1}`) // oauthAccount 없음

	t.Setenv("CLAUDE_CONFIG_DIR", work)
	if got := Email(&config.Config{ConfigDir: personal}); got != "" {
		t.Errorf("config_dir 프로필로 새면 안 된다: %q", got)
	}
	// 환경변수가 걸리면 후보는 그것 하나뿐이다.
	if p := paths(&config.Config{ConfigDir: personal}); len(p) != 1 {
		t.Errorf("후보가 %d개 (1개여야 한다): %v", len(p), p)
	}
}

func TestSourceDoesNotRead(t *testing.T) {
	// Source 는 매 렌더 불린다 — 파일을 읽으면 캐시를 둔 의미가 없다.
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := &config.Config{ConfigDir: filepath.Join(home, ".claude")}
	// 파일이 하나도 없어도 경로는 나온다 (읽지 않으니까). 실제로 읽히는 파일이
	// 아니라 **어느 자리를 보고 있나**를 나타내는 식별자다 — 캐시가 갈리는 데
	// 필요한 것은 그것뿐이다.
	if got := Source(cfg); got != filepath.Join(home, ".claude", ".claude.json") {
		t.Errorf("Source: %q", got)
	}
	// 보는 자리가 바뀌면 Source 도 바뀐다 — 캐시 키가 갈리는 근거다.
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-work"))
	if got := Source(cfg); got != filepath.Join(home, ".claude-work", ".claude.json") {
		t.Errorf("Source(env): %q", got)
	}
}
