//! 설정 파일 — camelCase 필드(rocky `rocky.json` 의 `statusline` 블록과 같은 모양)를 읽고, 틀린 값은 그 항목만 버린다.
//! rocky `crates/rocky-core/tests/it/config_test.rs` 의 `statusline_block_*` 를 옮겼다(블록을 최상위로 푼 것만 다르다).

use cc_usage::config::*;

fn write_config(content: &str) -> (tempfile::TempDir, std::path::PathBuf) {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("config.json");
    std::fs::write(&path, content).unwrap();
    (dir, path)
}

#[test]
fn config_reads_source_and_alert_percent() {
    use cc_usage::limits::Source;
    let (_dir, path) = write_config(r#"{ "source": "none", "alertPercent": 80 }"#);
    let c = load(&path).limits;
    assert_eq!(c.source, Source::None);
    assert_eq!(c.alert_percent, Some(80.0));
    // 모르는 source 는 기본값(auto), 파일이 없거나 JSON 이 아니어도 기본값.
    for raw in [r#"{ "source": "STDIN" }"#, "not json"] {
        let (_dir, path) = write_config(raw);
        assert_eq!(load(&path), StatuslineConfig::default(), "{raw}");
    }
    assert_eq!(
        load(std::path::Path::new("/nonexistent/config.json")),
        StatuslineConfig::default()
    );
}

#[test]
fn config_reads_extra_commands_and_skips_malformed_ones() {
    use cc_usage::statusline::extra::ExtraCommand;
    let (_dir, path) = write_config(
        r#"{ "extraCommands": [
            { "command": ["harness-lm", "line", "-s", "{{session_id}}"] },
            { "command": ["curl", "-sf"], "timeoutMs": 500 },
            { "command": "not an array" },
            { "command": ["ok", 3] },
            { "timeoutMs": 10 },
            { "command": [] }
        ] }"#,
    );
    let got = load(&path).extra_commands;
    let cmd = |argv: &[&str], timeout_ms| ExtraCommand {
        command: argv.iter().map(|a| a.to_string()).collect(),
        timeout_ms,
    };
    assert_eq!(
        got,
        vec![
            cmd(&["harness-lm", "line", "-s", "{{session_id}}"], None),
            cmd(&["curl", "-sf"], Some(500)),
            // 빈 command 는 남는다 — 실행 단계에서 건너뛴다.
            cmd(&[], None),
        ]
    );
}

#[test]
fn config_reads_refresh_and_credit_settings() {
    let (_dir, path) = write_config(
        r#"{ "configDir": "~/work", "keychainService": "K", "credentialsFile": "~/c.json",
             "tokenEnv": "T", "pollSeconds": 600, "creditPollSeconds": 60, "creditDivisor": 1,
             "currency": "₩", "alwaysShowCredits": true }"#,
    );
    let c = load(&path);
    assert_eq!(
        (
            c.config_dir.as_deref(),
            c.keychain_service.as_deref(),
            c.credentials_file.as_deref(),
            c.token_env.as_deref()
        ),
        (Some("~/work"), Some("K"), Some("~/c.json"), Some("T"))
    );
    assert_eq!(c.limits.poll(), chrono::TimeDelta::seconds(600));
    assert_eq!(c.limits.credit_poll(), chrono::TimeDelta::seconds(60));
    assert_eq!((c.limits.credit_divisor(), c.limits.currency()), (1.0, "₩"));
    assert!(c.limits.always_show_credits);
    // 빈 문자열·0 은 기본값.
    let (_dir, path) = write_config(r#"{ "configDir": "", "pollSeconds": 0, "currency": "" }"#);
    let c = load(&path);
    assert_eq!(c.config_dir, None);
    assert_eq!(c.limits.poll(), chrono::TimeDelta::seconds(300));
    assert_eq!(c.limits.currency(), "$");
}

#[test]
fn config_reads_badges_and_skips_malformed_ones() {
    let (_dir, path) = write_config(
        r#"{ "badges": {
            "work@example.com": { "emoji": "🏢" },
            "me@example.com": { "glyph": "◆", "color": "33" },
            "bad@example.com": { "emoji": 3 },
            "worse@example.com": "x"
        } }"#,
    );
    let badges = load(&path).badges;
    assert_eq!(badges.len(), 2);
    assert_eq!(badges["work@example.com"].emoji, "🏢");
    assert_eq!(
        (
            badges["me@example.com"].glyph.as_str(),
            badges["me@example.com"].color.as_str()
        ),
        ("◆", "33")
    );
}

#[test]
fn config_reads_credit_fade_defaulting_on() {
    let (_dir, path) = write_config(r#"{ "creditFade": false }"#);
    assert!(!load(&path).limits.credit_fade());
    let (_dir, path) = write_config(r#"{}"#);
    assert!(load(&path).limits.credit_fade());
}

#[test]
fn config_reads_guard_defaulting_off() {
    let (_dir, path) = write_config(r#"{ "guard": true }"#);
    assert!(load(&path).limits.guard);
    let (_dir, path) = write_config(r#"{}"#);
    assert!(!load(&path).limits.guard);
}

#[test]
fn config_reads_dir_link() {
    let (_dir, path) = write_config(r#"{ "dirLink": "vscode://file{{cwd}}" }"#);
    assert_eq!(
        load(&path).dir_link.as_deref(),
        Some("vscode://file{{cwd}}")
    );
    for raw in [r#"{}"#, r#"{ "dirLink": "" }"#] {
        let (_dir, path) = write_config(raw);
        assert_eq!(load(&path).dir_link, None, "{raw}");
    }
}
