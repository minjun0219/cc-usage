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
// $CLAUDE_CONFIG_DIR 가 맨 앞이다 — 그것이 **지금 도는 세션이 실제로 쓰는 값**
// 이라, 설정에 뭐라고 적혀 있든 이 세션의 계정은 그쪽이다. 이걸 안 보면
// `CLAUDE_CONFIG_DIR=~/.claude-work claude` 로 연 세션에서 배지가 기본 계정을
// 가리킨다(조용히 틀린다).
//
// 기본 설치에서는 홈 루트(`~/.claude.json`)에만 있고 앞의 두 경로는 없다 —
// 그래서 자연히 마지막으로 떨어진다. CLAUDE_CONFIG_DIR 를 쓸 때 .claude.json
// 이 그 안에 놓이는지는 확인하지 못했으므로, 후보를 늘려 모르는 채로도 맞게
// 동작하게 한다.
//
// 주의: 이것은 배지만 고친다. CLAUDE_CONFIG_DIR 만 바꾸고 XDG_CACHE_HOME 을
// 그대로 두면 cache 는 여전히 두 계정이 공유해서 한도·크레딧이 섞인다.
// 계정을 나누는 문서화된 방법은 CC_USAGE_CONFIG 와 XDG_CACHE_HOME 을 함께
// 나누는 것이다.
func paths(c *config.Config) []string {
	var out []string
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		out = append(out, filepath.Join(config.Expand(d), ".claude.json"))
	}
	if c != nil && c.ConfigDir != "" {
		out = append(out, filepath.Join(c.ConfigDir, ".claude.json"))
	}
	// 홈 루트 폴백은 **기본 설치일 때만** 쓴다.
	//
	// config_dir 에 다른 값을 적었거나 CLAUDE_CONFIG_DIR 가 설정돼 있으면, 이
	// 세션의 계정은 그쪽이다. 거기서 파일을 못 찾았다고 홈 루트를 읽으면 **다른
	// 계정의 이메일**을 집어 온다 — token 은 config_dir 쪽 credentials 를 쓰므로
	// 배지와 숫자가 서로 다른 계정을 가리키게 된다.
	//
	// 그 경우에는 배지를 내지 않는다. 틀린 배지보다 배지 없음이 낫다.
	home, err := os.UserHomeDir()
	if err != nil {
		return out
	}
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		return out
	}
	if c != nil && c.ConfigDir != "" && filepath.Clean(c.ConfigDir) != filepath.Join(home, ".claude") {
		return out
	}
	return append(out, filepath.Join(home, ".claude.json"))
}
