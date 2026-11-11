//! 이 라이브러리를 실행 파일에 싣는 쪽의 이름 묶음 — 환경 변수·캐시 폴더·메시지 머리·갱신 명령줄.
//!
//! 기본은 cc-usage 자신(`CC_USAGE`)이다. 다른 실행 파일(예: rocky 의 `rocky statusline`)이 이 크레이트를 라이브러리로
//! 쓰면 `main` 첫머리에서 [`set`] 으로 자기 이름을 한 번 건다 — 그 뒤로 모든 모듈이 [`get`] 으로 같은 값을 본다.
//! 판정·렌더 규칙은 같고 이름만 갈리므로, 함수마다 인자로 끌고 다니지 않고 프로세스에 하나만 둔다.

use std::sync::OnceLock;

/// 실행 파일 하나의 이름들.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Profile {
    /// 메시지 머리(`[name]`)와 usage API 의 `User-Agent`.
    pub name: &'static str,
    /// 캐시 루트 아래의 폴더 — `<cache>/<cache_dir...>/<config_dir 해시>/…`, `allow.json` 도 여기.
    pub cache_dir: &'static [&'static str],
    /// statusline 이 detached 로 띄우는 갱신의 인자(실행 파일은 자기 자신).
    pub refresh_args: &'static [&'static str],
    /// guard 가 막을 때 안내하는 해제 명령(인자 없이 — 뒤에 ` 30m` 이 붙는다).
    pub allow_command: &'static str,
    /// `doctor` 가 `extraCommands` 가 없을 때 가리키는 안내.
    pub extra_commands_hint: &'static str,
    /// 이번 실행의 `source` 를 바꾸는 환경 변수 — 부모 statusline 이 갱신 프로세스로 넘길 때도 쓴다.
    pub source_env: &'static str,
    /// 하위 프로세스 표식 — 이것이 걸린 statusline 은 아무것도 그리지 않는다(자기를 끝없이 다시 띄우지 않게).
    pub nested_env: &'static str,
    /// 테스트 전용 — RFC3339 로 "지금" 을 고정한다.
    pub now_env: &'static str,
    /// 테스트 전용 — usage API 주소를 바꾼다.
    pub usage_url_env: &'static str,
}

/// cc-usage 자신.
pub const CC_USAGE: Profile = Profile {
    name: "cc-usage",
    cache_dir: &["cc-usage"],
    refresh_args: &["refresh"],
    allow_command: "cc-usage allow",
    extra_commands_hint: "cc-usage config",
    source_env: "CC_USAGE_SOURCE",
    nested_env: "CC_USAGE_NESTED",
    now_env: "CC_USAGE_NOW",
    usage_url_env: "CC_USAGE_API_URL",
};

static CURRENT: OnceLock<Profile> = OnceLock::new();

/// 이 프로세스의 이름을 건다 — 첫 호출만 먹는다. 이미 걸려 있으면(또는 [`get`] 이 먼저 불려 기본값으로 굳었으면)
/// `false` 를 돌려주고 바꾸지 않는다. `main` 첫머리, 다른 모듈을 부르기 전에 부른다.
pub fn set(profile: Profile) -> bool {
    CURRENT.set(profile).is_ok()
}

/// 이 프로세스의 이름 — [`set`] 을 안 불렀으면 [`CC_USAGE`].
pub fn get() -> &'static Profile {
    CURRENT.get_or_init(|| CC_USAGE)
}

// 프로필은 프로세스에 하나라 다른 이름을 거는 검사는 여기(lib 단위 테스트 실행 파일)에만 둔다 — 통합 테스트(`tests/it`)는
// cc-usage 실행 파일을 띄워 기본 이름을 본다. 이 실행 파일의 다른 테스트는 이름에 기대지 않는다(`statusline::full` 의 셋).
#[cfg(test)]
mod tests {
    use super::*;
    use std::path::Path;

    const OTHER: Profile = Profile {
        name: "other",
        cache_dir: &["other", "statusline"],
        refresh_args: &["statusline", "refresh"],
        allow_command: "other statusline allow",
        extra_commands_hint: "docs",
        source_env: "OTHER_SOURCE",
        nested_env: "OTHER_NESTED",
        now_env: "OTHER_NOW",
        usage_url_env: "OTHER_URL",
    };

    #[test]
    fn set_profile_renames_cache_messages_and_env() {
        assert!(set(OTHER), "처음 거는 이름은 먹는다");
        assert!(!set(CC_USAGE), "두 번째부터는 바꾸지 않는다");
        assert_eq!(get(), &OTHER);

        let root = Path::new("/c");
        let slot = crate::claude_account::cache_slot(root, Path::new("/home/me/.claude"));
        assert!(slot.starts_with("/c/other/statusline"), "{slot:?}");
        assert_eq!(
            crate::claude_account::allow_file(root),
            Path::new("/c/other/statusline/allow.json")
        );
        assert_eq!(
            crate::statusline_guard::block_message("한도 소진"),
            "[other] 한도 소진.\n계속하려면 터미널에서: other statusline allow 30m"
        );
        assert_eq!(crate::bounded::nested_env(), "OTHER_NESTED");
    }
}
