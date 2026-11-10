//! 설정 파일 — `$CC_USAGE_CONFIG`, 없으면 `$XDG_CONFIG_HOME/cc-usage/config.json`(기본 `~/.config/cc-usage/config.json`).
//!
//! 필드 이름은 rocky `rocky.json` 의 `statusline` 블록과 같다(camelCase) — 그 블록의 내용을 이 파일의 최상위에 그대로 옮기면 된다.
//! 파일이 없거나 깨져도 기본값으로 돈다(fail-open) — statusline 이 설정 한 줄 때문에 비면 안 된다.

use std::path::{Path, PathBuf};

/// 설정 파일 경로를 바꾸는 환경 변수.
pub const CONFIG_ENV: &str = "CC_USAGE_CONFIG";

/// 이 프로세스가 읽을 설정 파일 경로. 홈을 모르면 None.
pub fn path() -> Option<PathBuf> {
    if let Some(p) = std::env::var_os(CONFIG_ENV).filter(|p| !p.is_empty()) {
        return Some(PathBuf::from(p));
    }
    if let Some(x) = std::env::var_os("XDG_CONFIG_HOME").filter(|p| !p.is_empty()) {
        return Some(PathBuf::from(x).join("cc-usage").join("config.json"));
    }
    let home = std::env::var_os("HOME").filter(|p| !p.is_empty())?;
    Some(
        PathBuf::from(home)
            .join(".config")
            .join("cc-usage")
            .join("config.json"),
    )
}

/// 지금 경로의 설정 — 경로를 모르면 기본값.
pub fn load_user() -> StatuslineConfig {
    path().map(|p| load(&p)).unwrap_or_default()
}

/// 설정 파일(`config.json`) — 모양은 rocky `rocky.json` 의 `statusline` 블록과 같다(그 블록을 그대로 옮겨 쓴다).
#[derive(Debug, Clone, Default, PartialEq)]
pub struct StatuslineConfig {
    /// 한도 판정(`source` · `alertPercent`).
    pub limits: crate::limits::LimitsConfig,
    /// 아래에 덧붙일 다른 도구의 줄(`extraCommands[]`).
    pub extra_commands: Vec<crate::statusline::extra::ExtraCommand>,
    /// Claude Code 설정 폴더(`configDir`) — 세션의 `CLAUDE_CONFIG_DIR` 가 있으면 그쪽이 이긴다. 기본 `~/.claude`.
    pub config_dir: Option<String>,
    /// 토큰 keychain 항목(`keychainService`) — 없으면 기본 설정 폴더일 때만 `Claude Code-credentials`.
    pub keychain_service: Option<String>,
    /// 토큰 파일(`credentialsFile`) — 없으면 `<config_dir>/.credentials.json`.
    pub credentials_file: Option<String>,
    /// 이 환경 변수에 토큰이 있으면 그것을 먼저 쓴다(`tokenEnv`).
    pub token_env: Option<String>,
    /// 로그인된 계정(이메일)별 표시(`badges`) — 목록에 없는 계정은 아무것도 붙지 않는다.
    pub badges: std::collections::HashMap<String, crate::statusline::full::Badge>,
    /// 경로를 Cmd+클릭으로 여는 링크 템플릿(`dirLink`, 예: `vscode://file{{cwd}}`). 없으면 링크 없음.
    pub dir_link: Option<String>,
}

/// 파일 없음 / 파싱 실패 / 블록 없음 / 모르는 값은 기본값(fail-open). `extraCommands` 에서 모양이 틀린 항목(`command` 가
/// 문자열 배열이 아님)은 건너뛴다 — statusline 이 설정 한 줄 때문에 비면 안 된다.
pub fn load(config_path: &Path) -> StatuslineConfig {
    let mut cfg = StatuslineConfig::default();
    let Ok(raw) = std::fs::read_to_string(config_path) else {
        return cfg;
    };
    let Ok(parsed) = serde_json::from_str::<serde_json::Value>(&raw) else {
        return cfg;
    };
    let Some(block) = parsed.as_object() else {
        return cfg;
    };
    if let Some(source) = block
        .get("source")
        .and_then(|v| v.as_str())
        .and_then(crate::limits::Source::parse)
    {
        cfg.limits.source = source;
    }
    cfg.limits.alert_percent = block.get("alertPercent").and_then(|v| v.as_f64());
    let text = |key: &str| {
        block
            .get(key)
            .and_then(|v| v.as_str())
            .filter(|s| !s.is_empty())
            .map(str::to_string)
    };
    cfg.config_dir = text("configDir");
    cfg.keychain_service = text("keychainService");
    cfg.credentials_file = text("credentialsFile");
    cfg.token_env = text("tokenEnv");
    cfg.dir_link = text("dirLink");
    cfg.limits.currency = text("currency");
    cfg.limits.credit_divisor = block.get("creditDivisor").and_then(|v| v.as_f64());
    cfg.limits.poll_seconds = block.get("pollSeconds").and_then(|v| v.as_u64());
    cfg.limits.credit_poll_seconds = block.get("creditPollSeconds").and_then(|v| v.as_u64());
    // 모양이 틀린 항목(객체가 아님, 필드가 문자열이 아님)은 건너뛴다.
    cfg.badges = block
        .get("badges")
        .and_then(|v| v.as_object())
        .into_iter()
        .flatten()
        .filter_map(|(email, v)| {
            let obj = v.as_object()?;
            let field = |k: &str| match obj.get(k) {
                None | Some(serde_json::Value::Null) => Some(String::new()),
                Some(serde_json::Value::String(s)) => Some(s.clone()),
                Some(_) => None,
            };
            Some((
                email.clone(),
                crate::statusline::full::Badge {
                    emoji: field("emoji")?,
                    glyph: field("glyph")?,
                    color: field("color")?,
                },
            ))
        })
        .collect();
    cfg.limits.credit_fade = block.get("creditFade").and_then(|v| v.as_bool());
    cfg.limits.always_show_credits = block
        .get("alwaysShowCredits")
        .and_then(|v| v.as_bool())
        .unwrap_or(false);
    cfg.limits.guard = block
        .get("guard")
        .and_then(|v| v.as_bool())
        .unwrap_or(false);
    let extras = block.get("extraCommands").and_then(|v| v.as_array());
    cfg.extra_commands = extras
        .into_iter()
        .flatten()
        .filter_map(|item| {
            let command = item
                .get("command")?
                .as_array()?
                .iter()
                .map(|a| a.as_str().map(str::to_string))
                .collect::<Option<Vec<_>>>()?;
            Some(crate::statusline::extra::ExtraCommand {
                command,
                timeout_ms: item.get("timeoutMs").and_then(|v| v.as_u64()),
            })
        })
        .collect();
    cfg
}
