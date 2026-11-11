//! cc-usage — Claude Code statusline + 크레딧 guard.
//!
//! 순수 판정(`limits` · `claude_account` · `statusline` · `config`)과 파일·프로세스를 다루는 쪽(`bounded` ·
//! `git_status` · `statusline_*` · `commands` · `update`)으로 나뉜다. 판정은 "지금"을 인자로 받아 테스트한다.
//! 코드는 rocky(`crates/rocky-core` · `crates/rocky-cli`)의 `rocky statusline` 에서 옮겨 왔다. 이제 rocky 가 이 크레이트를
//! 라이브러리로 싣는다(`default-features = false`) — 이름은 `profile`, 설정은 부르는 쪽이 넘긴다.

pub mod bounded;
pub mod claude_account;
pub mod commands;
pub mod config;
pub mod git_status;
#[cfg(feature = "cli")]
pub mod help;
pub mod limits;
pub mod profile;
pub mod statusline;
pub mod statusline_cache;
pub mod statusline_doctor;
pub mod statusline_guard;
pub mod statusline_refresh;
#[cfg(feature = "cli")]
pub mod update;

/// 빌드한 체크아웃의 `git describe` — 없으면 `dev`.
#[cfg(feature = "cli")]
pub const VERSION: &str = env!("CC_USAGE_VERSION");
/// 빌드한 소스 경로 — `update` 가 거기서 pull·빌드·설치한다.
#[cfg(feature = "cli")]
pub const REPO_PATH: &str = env!("CARGO_MANIFEST_DIR");
