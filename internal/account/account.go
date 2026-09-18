// Package account reads which Claude account is logged in.
//
// 출처는 Claude Code 자신의 설정 파일(`.claude.json`)의 oauthAccount 다. 이것이
// 누가 계정을 바꾸든 결과가 드러나는 단일 진실 원천이다 — claude-swap 이든
// /login 이든 keychain 을 직접 건드리든 여기에 반영된다. 특정 전환 도구를 아는
// 대신 결과만 본다.
//
// 읽기만 한다. 이 파일은 Claude Code 소유다.
package account

import (
	"encoding/json"
	"os"
	"path/filepath"

	"cc-usage/internal/config"
)

// claudeConfig is the sliver of .claude.json we need. 나머지 필드는 무시한다 —
// organizationName 같은 것은 화면에 낼 일이 없고, 들고 있을 이유도 없다.
type claudeConfig struct {
	OAuthAccount struct {
		EmailAddress string `json:"emailAddress"`
	} `json:"oauthAccount"`
}

// Email returns the logged-in account's email, or "" when it cannot be read.
//
// 빈 문자열은 실패가 아니라 "모른다" 다 — 호출자는 배지를 생략하면 된다.
// 파일 없음·파싱 실패·필드 없음이 모두 같은 결과로 떨어진다 (불변 조건 4번과
// 같은 자세).
func Email(c *config.Config) string {
	for _, p := range paths(c) {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cc claudeConfig
		if json.Unmarshal(b, &cc) != nil {
			continue
		}
		if cc.OAuthAccount.EmailAddress != "" {
			return cc.OAuthAccount.EmailAddress
		}
	}
	return ""
}

// paths is where .claude.json can live, most specific first.
//
// 기본 설치에서는 홈 루트(`~/.claude.json`)에 있고 `<config_dir>/.claude.json`
// 은 없다 — 그래서 첫 경로가 비면 자연히 두 번째로 떨어진다. CLAUDE_CONFIG_DIR
// 를 쓸 때 어디로 가는지는 확인하지 못했으므로, 양쪽을 다 보는 것으로 모르는
// 채로도 맞게 동작하게 한다.
func paths(c *config.Config) []string {
	var out []string
	if c != nil && c.ConfigDir != "" {
		out = append(out, filepath.Join(c.ConfigDir, ".claude.json"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, ".claude.json"))
	}
	return out
}
