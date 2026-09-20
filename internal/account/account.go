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
//
// HasExtraUsageEnabled 가 포인터인 이유는 "필드 없음" 과 false 를 갈라야 하기
// 때문이다. Guard 가 이 값을 보고 차단을 건너뛰므로, 둘을 같게 다루면 이 필드를
// 쓰지 않는 설치에서 크레딧이 켜져 있어도 꺼진 것으로 단정하게 된다.
type claudeConfig struct {
	OAuthAccount struct {
		EmailAddress         string `json:"emailAddress"`
		HasExtraUsageEnabled *bool  `json:"hasExtraUsageEnabled"`
	} `json:"oauthAccount"`
}

// read parses the first candidate that can be read. ok is true only when a file
// was **read and parsed** — 파일이 없거나 JSON 이 깨졌으면 false 다.
func read(c *config.Config) (claudeConfig, bool) {
	for _, p := range paths(c) {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cc claudeConfig
		if json.Unmarshal(b, &cc) != nil {
			continue
		}
		return cc, true
	}
	return claudeConfig{}, false
}

// Email returns the logged-in account's email. ok is true only when a candidate
// file was **read and parsed**; 파일이 없거나 JSON 이 깨졌으면 false 다.
//
// 실패와 "읽었는데 이메일이 없다" 를 가른다. 이 둘을 같게 다루면 일시적 실패가
// 캐시에 "이메일 없음" 으로 확정 기록되고, 이 기능에서 "배지 없음" 은 그 자체로
// **기본 계정** 이라는 신호이므로 틀린 신호가 된다. Claude Code 는 이 파일을
// 원자적으로 재작성하므로(.claude.json.lock 이 함께 보인다) 그 창에 걸리는 일이
// 실제로 있다.
func Email(c *config.Config) (email string, ok bool) {
	cc, ok := read(c)
	if !ok {
		return "", false
	}
	// 여기까지 왔으면 계정 파일을 본 것이다. 이메일이 비어 있어도 그것이
	// 이 계정의 사실이다 (API key 인증 등).
	return cc.OAuthAccount.EmailAddress, true
}

// ExtraUsageEnabled reports whether usage credits are turned on for the logged-in
// account, **API 를 부르지 않고**. nil 은 모른다는 뜻이다 — 계정 파일을 못 읽었거나,
// 읽었는데 그 필드가 없는 경우다.
//
// guard 가 이것을 쓴다. `source: "stdin"` 에서는 한도에 닿기 전까지 API 를 부르지
// 않아 usage.json 이 아예 없고, 그 구간에서는 크레딧이 꺼진 계정까지 "크레딧이
// 차감됩니다" 로 막혔다. 이 파일은 그 답을 network 없이 들고 있다.
//
// 이 값은 계정 파일의 스냅샷이라 크레딧을 방금 켜고 끈 직후에는 뒤처질 수 있다.
// 그래서 usage.json 의 관측값이 있으면 그쪽이 이긴다 (core.Guard 가 그 순서를 안다).
func ExtraUsageEnabled(c *config.Config) *bool {
	cc, ok := read(c)
	if !ok {
		return nil
	}
	return cc.OAuthAccount.HasExtraUsageEnabled
}

// Source is which account slot this session is looking at. **파일을 읽지 않는다**
// — 경로만 조립하므로 매 렌더 불러도 공짜다.
//
// 캐시 키에 들어간다. 이메일만 캐시하면, cache 를 나누지 않은 설치
// (CLAUDE_CONFIG_DIR 만 나누고 XDG_CACHE_HOME 은 공유)에서 두 세션이 같은
// state.json 을 쓰면서 서로의 이메일을 읽어 간다 — 이 기능이 막으려던 "조용히
// 틀린 배지" 가 캐시 쪽에서 다시 생긴다.
func Source(c *config.Config) string {
	if p := paths(c); len(p) > 0 {
		return p[0]
	}
	return ""
}

// paths is where .claude.json can live, most specific first.
//
// $CLAUDE_CONFIG_DIR 가 걸려 있으면 **그것 하나뿐**이고, 아니면
// `<config_dir>/.claude.json` → `~/.claude.json` 순이다.
//
// 환경변수가 배타인 이유: 그것이 지금 도는 세션이 실제로 쓰는 값이라 그 세션의
// 계정은 그쪽이고, 거기서 못 읽었다고 다른 후보를 보면 **다른 계정**의 이메일을
// 집어 온다. `claude` 를 CLAUDE_CONFIG_DIR 로 돌리면 `.claude.json` 이 그 안에
// 생기는 것을 실측으로 확인했다(2026-09-18).
//
// 주의: 이것은 배지만 고친다. CLAUDE_CONFIG_DIR 만 바꾸고 XDG_CACHE_HOME 을
// 그대로 두면 cache 는 여전히 두 계정이 공유해서 한도·크레딧이 섞인다.
// 계정을 나누는 문서화된 방법은 셋을 함께 나누는 것이다.
func paths(c *config.Config) []string {
	// CLAUDE_CONFIG_DIR 가 걸려 있으면 **그것만** 본다. 다른 후보를 남겨 두면,
	// 그 디렉터리의 .claude.json 이 없거나 oauthAccount 가 비었을 때(API key
	// 인증 · 갓 만든 디렉터리 · 원자적 재작성 중) 다른 계정의 파일로 새어
	// 들어간다. 못 읽으면 배지를 내지 않는 쪽이 맞다.
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return []string{filepath.Join(config.Expand(d), ".claude.json")}
	}
	var out []string
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
	if c != nil && c.ConfigDir != "" && filepath.Clean(c.ConfigDir) != filepath.Join(home, ".claude") {
		return out
	}
	return append(out, filepath.Join(home, ".claude.json"))
}
