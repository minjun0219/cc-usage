//! cc-usage — Claude Code statusline + 크레딧 guard.
//!
//! 순수 판정(`limits` · `claude_account` · `statusline` · `config`)과 파일·프로세스를 다루는 쪽(`bounded` ·
//! `git_status` · `statusline_*` · `commands` · `update`)으로 나뉜다. 판정은 "지금"을 인자로 받아 테스트한다.
//! 코드는 rocky(`crates/rocky-core` · `crates/rocky-cli`)의 `rocky statusline` 에서 옮겨 왔다 — 모듈 이름을 그대로 둬
//! 두 레포를 견주기 쉽게 한다.

pub mod bounded;
pub mod claude_account;
pub mod commands;
pub mod config;
pub mod git_status;
pub mod help;
pub mod limits;
pub mod statusline;
pub mod statusline_cache;
pub mod statusline_doctor;
pub mod statusline_guard;
pub mod statusline_refresh;
pub mod update;

/// 빌드한 체크아웃의 `git describe` — 없으면 `dev`.
pub const VERSION: &str = env!("CC_USAGE_VERSION");
/// 빌드한 소스 경로 — `update` 가 거기서 pull·빌드·설치한다.
pub const REPO_PATH: &str = env!("CARGO_MANIFEST_DIR");
