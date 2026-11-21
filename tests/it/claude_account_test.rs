//! `cc_usage::claude_account` — 이 세션이 어느 계정인가의 규칙(cc-usage `internal/config` · `internal/account`).

use std::path::{Path, PathBuf};

use cc_usage::claude_account::*;

const HOME: &str = "/home/me";

fn home() -> &'static Path {
    Path::new(HOME)
}

#[test]
fn config_dir_prefers_the_session_env_then_config_then_default() {
    assert_eq!(
        config_dir(Some("/w"), Some("~/x"), home()),
        PathBuf::from("/w")
    );
    assert_eq!(
        config_dir(None, Some("~/x"), home()),
        PathBuf::from("/home/me/x")
    );
    assert_eq!(
        config_dir(Some(""), None, home()),
        PathBuf::from("/home/me/.claude")
    );
    assert_eq!(
        config_dir(Some("~"), None, home()),
        PathBuf::from("/home/me")
    );
}

#[test]
fn account_paths_never_leak_into_another_accounts_file() {
    // CLAUDE_CONFIG_DIR 가 있으면 그 폴더의 파일 하나뿐.
    assert_eq!(
        account_paths(Some("/w"), Path::new("/w"), home()),
        [PathBuf::from("/w/.claude.json")]
    );
    // 기본 설치만 홈 루트 폴백.
    assert_eq!(
        account_paths(None, Path::new("/home/me/.claude/"), home()),
        [
            PathBuf::from("/home/me/.claude/.claude.json"),
            PathBuf::from("/home/me/.claude.json")
        ]
    );
    assert_eq!(
        account_paths(None, Path::new("/home/me/other"), home()),
        [PathBuf::from("/home/me/other/.claude.json")]
    );
}

#[test]
fn email_parse_tells_unreadable_from_no_email() {
    assert_eq!(
        email_from_account_file(
            r#"{"oauthAccount":{"emailAddress":"a@x","hasExtraUsageEnabled":true}}"#
        ),
        Some("a@x".into())
    );
    for no_email in [
        r#"{}"#,
        r#"{"oauthAccount":null}"#,
        r#"{"oauthAccount":{}}"#,
    ] {
        assert_eq!(
            email_from_account_file(no_email),
            Some(String::new()),
            "{no_email}"
        );
    }
    for unreadable in [
        "{ not json",
        r#"{"oauthAccount":"x"}"#,
        r#"{"oauthAccount":{"emailAddress":3}}"#,
        r#"{"oauthAccount":{"emailAddress":"a@x","hasExtraUsageEnabled":"yes"}}"#,
    ] {
        assert_eq!(email_from_account_file(unreadable), None, "{unreadable}");
    }
}

#[test]
fn cache_dirs_split_by_config_dir_and_account() {
    let root = cache_root(None, home());
    assert_eq!(root, PathBuf::from("/home/me/.cache"));
    assert_eq!(cache_root(Some("/c"), home()), PathBuf::from("/c"));
    let a = cache_slot(&root, Path::new("/home/me/.claude"));
    let b = cache_slot(&root, Path::new("/home/me/work"));
    assert_ne!(a, b);
    assert!(a.starts_with("/home/me/.cache/cc-usage"));
    // 같은 폴더면 끝의 `/` 와 무관하게 같은 자리.
    assert_eq!(a, cache_slot(&root, Path::new("/home/me/.claude/")));
    assert_ne!(cache_bucket(&a, Some("a@x")), cache_bucket(&a, Some("b@x")));
    assert_eq!(cache_bucket(&a, None), a.join("_"));
    assert_eq!(cache_bucket(&a, Some("")), a.join("_"));
    // 이메일이 경로에 그대로 남지 않는다.
    assert!(!cache_bucket(&a, Some("a@x"))
        .to_string_lossy()
        .contains("a@x"));
}

#[test]
fn keychain_name_follows_claude_codes_rule_and_the_setting_stays_in_its_dir() {
    let default = Path::new("/home/me/.claude");
    let work = Path::new("/home/me/work");
    // 환경 변수 없이 띄운 기본 폴더 세션 — 접미사 없음.
    assert_eq!(
        keychain_service(None, default, default, None, home()),
        DEFAULT_KEYCHAIN_SERVICE
    );
    // CLAUDE_CONFIG_DIR 로 띄운 세션 — sha256(그 값) 앞 8자리(실측: /Users/minjun/.claude → 8f9c6e44).
    assert_eq!(
        keychain_service(
            None,
            Path::new("/Users/minjun/.claude"),
            Path::new("/Users/minjun/.claude"),
            Some("/Users/minjun/.claude"),
            Path::new("/Users/minjun")
        ),
        "Claude Code-credentials-8f9c6e44"
    );
    let work_name = keychain_service(None, default, work, Some("/home/me/work"), home());
    assert!(
        work_name.starts_with("Claude Code-credentials-") && work_name.len() == 32,
        "{work_name}"
    );
    // 환경 변수 없이 설정의 configDir 이 비기본 폴더면 그 경로로 같은 규칙.
    assert_eq!(keychain_service(None, work, work, None, home()), work_name);
    // 설정값은 그것이 가리키는 폴더의 세션에만 — 다른 폴더 세션이 그 토큰을 집으면 남의 숫자다.
    assert_eq!(
        keychain_service(Some("Mine"), work, work, Some("/home/me/work"), home()),
        "Mine"
    );
    assert_eq!(
        keychain_service(Some("Mine"), work, default, None, home()),
        DEFAULT_KEYCHAIN_SERVICE
    );
    assert_eq!(
        keychain_service(Some("Mine"), default, work, Some("/home/me/work"), home()),
        work_name
    );

    assert_eq!(
        credentials_file(None, default, work, home()),
        PathBuf::from("/home/me/work/.credentials.json")
    );
    assert_eq!(
        credentials_file(Some("~/c.json"), work, work, home()),
        PathBuf::from("/home/me/c.json")
    );
    assert_eq!(
        credentials_file(Some("~/c.json"), default, work, home()),
        PathBuf::from("/home/me/work/.credentials.json")
    );
}

#[test]
fn token_parse_accepts_bare_and_oauth_json_and_checks_expiry() {
    let bare = parse_token("  tok\n").unwrap();
    assert_eq!((bare.access_token.as_str(), bare.expires_at), ("tok", None));
    let json =
        parse_token(r#"{"claudeAiOauth":{"accessToken":"a","expiresAt":1757997600000}}"#).unwrap();
    assert_eq!(json.access_token, "a");
    let at = json.expires_at.unwrap();
    assert!(check_token(json.clone(), at - chrono::TimeDelta::seconds(1)).is_ok());
    assert_eq!(
        check_token(json, at + chrono::TimeDelta::seconds(1)).unwrap_err(),
        TOKEN_EXPIRED
    );
    for bad in [
        "",
        "{}",
        r#"{"claudeAiOauth":{"accessToken":""}}"#,
        "{ broken",
    ] {
        assert!(parse_token(bad).is_err(), "{bad:?}");
    }
}

/// 크레딧 힌트 — "필드 없음"(모름)과 `false` 를 가른다. 타입이 틀리면 파일 전체를 못 읽은 것이다.
#[test]
fn account_file_carries_the_credit_hint() {
    let hint = |raw: &str| parse_account_file(raw).map(|a| a.extra_usage_enabled);
    assert_eq!(
        hint(r#"{"oauthAccount":{"emailAddress":"a@x","hasExtraUsageEnabled":true}}"#),
        Some(Some(true))
    );
    assert_eq!(
        hint(r#"{"oauthAccount":{"hasExtraUsageEnabled":false}}"#),
        Some(Some(false))
    );
    assert_eq!(
        hint(r#"{"oauthAccount":{"emailAddress":"a@x"}}"#),
        Some(None)
    );
    assert_eq!(hint(r#"{}"#), Some(None));
    assert_eq!(
        hint(r#"{"oauthAccount":{"hasExtraUsageEnabled":"yes"}}"#),
        None
    );
    assert_eq!(hint("not json"), None);
}

/// keychain 토큰이 만료됐으면 유효한 파일 토큰이 이긴다 — 만료 시각을 모르는 파일 토큰도 유효로 본다.
#[test]
fn a_fresh_file_token_beats_an_expired_keychain_token() {
    let now = chrono::DateTime::from_timestamp(2_000_000, 0).unwrap();
    let token = |at: Option<i64>| OauthToken {
        access_token: "t".into(),
        expires_at: at.and_then(|s| chrono::DateTime::from_timestamp(s, 0)),
    };
    assert!(file_beats_expired_keychain(&token(Some(2_000_100)), now));
    assert!(file_beats_expired_keychain(&token(None), now));
    assert!(!file_beats_expired_keychain(&token(Some(1_999_000)), now));
}

/// 로그인 기록(`oauthAccount`)이 있나와 `billingType` — `oauthAccount` 가 없거나 null 이면 로그인 기록이 없다(API key 로만 도는
/// 설정 폴더의 실측 꼴). `billingType` 은 보이기만 하는 값이라 타입이 틀려도 이메일은 지킨다.
#[test]
fn account_file_tells_whether_a_claude_login_exists() {
    let read =
        |raw: &str| parse_account_file(raw).map(|a| (a.oauth_account, a.email, a.billing_type));
    // API key 로 도는 설정 폴더(2026-11-20 실측 — 계정 관련 키는 이 둘뿐이었다).
    assert_eq!(
        read(
            r#"{"customApiKeyResponses":{"approved":[],"rejected":[]},"mcpNeedsAuthNoticed":true}"#
        ),
        Some((false, String::new(), None))
    );
    assert_eq!(
        read(r#"{"oauthAccount":null}"#),
        Some((false, String::new(), None))
    );
    assert_eq!(
        read(r#"{"oauthAccount":{"emailAddress":"a@x","billingType":"stripe_subscription"}}"#),
        Some((true, "a@x".into(), Some("stripe_subscription".into())))
    );
    assert_eq!(
        read(r#"{"oauthAccount":{"emailAddress":"a@x"}}"#),
        Some((true, "a@x".into(), None))
    );
    assert_eq!(
        read(r#"{"oauthAccount":{"emailAddress":"a@x","billingType":7}}"#),
        Some((true, "a@x".into(), None))
    );
}

/// API key 가 있는 자리 — `env.ANTHROPIC_API_KEY`(실측) 또는 `apiKeyHelper`. 비었거나 문자열이 아니거나 읽을 수 없으면 없다.
#[test]
fn settings_api_key_names_where_the_key_is() {
    assert_eq!(
        settings_api_key(r#"{"env":{"ANTHROPIC_API_KEY":"sk-ant-x"}}"#),
        Some("env.ANTHROPIC_API_KEY")
    );
    assert_eq!(
        settings_api_key(r#"{"apiKeyHelper":"~/bin/key.sh"}"#),
        Some("apiKeyHelper")
    );
    for none in [
        r#"{}"#,
        r#"{"env":{"OTHER":"x"}}"#,
        r#"{"env":{"ANTHROPIC_API_KEY":""}}"#,
        r#"{"env":{"ANTHROPIC_API_KEY":"  "}}"#,
        r#"{"env":{"ANTHROPIC_API_KEY":1}}"#,
        r#"{"apiKeyHelper":""}"#,
        "not json",
    ] {
        assert_eq!(settings_api_key(none), None, "{none}");
    }
}

/// API 과금은 로그인 기록이 없고 API key 가 있을 때만이다. 로그인 기록이 있으면(구독으로 로그인해 둔 세션) API key 가 있어도,
/// API key 가 없으면 로그인 기록이 없어도(`CLAUDE_CODE_OAUTH_TOKEN` 구독 세션일 수 있다) 아니다 — 구독 세션의 한도를 경고
/// 없이 지우면 안 된다. 로그인 기록이 있으면 설정 파일을 보지도 않는다.
#[test]
fn api_billing_needs_no_login_and_an_api_key() {
    let no_login = AccountFile::default();
    let logged_in = AccountFile {
        email: "a@x".into(),
        oauth_account: true,
        ..AccountFile::default()
    };
    assert!(is_api_billing(&no_login, || true));
    assert!(!is_api_billing(&no_login, || false));
    assert!(!is_api_billing(&logged_in, || panic!(
        "로그인 기록이 있으면 설정 파일을 보지 않는다"
    )));
}
