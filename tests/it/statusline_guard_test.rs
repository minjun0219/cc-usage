//! `cc-usage guard` · `allow` — 한도가 소진되면 prompt 를 막고(exit 2), allow 로 잠시 푼다. 판정 자체는
//! `limits_test` 가 보고, 여기는 계정·캐시 자리를 읽고 종료 코드·문구를 내는 배선을 본다.

use std::io::Write;
use std::path::Path;
use std::process::{Command, Stdio};

const NOW: &str = "2026-09-16T07:40:00Z";

struct Out {
    code: i32,
    stdout: String,
    stderr: String,
}

/// 환경을 비우고 `cc-usage <args>` 한 번 — 홈·설정은 `home` 아래.
fn cc_usage(home: &Path, statusline: &str, env: &[(&str, &str)], args: &[&str]) -> Out {
    let config = home.join("config.json");
    std::fs::write(&config, statusline).unwrap();
    let mut cmd = Command::new(env!("CARGO_BIN_EXE_cc-usage"));
    cmd.env_clear()
        .env("PATH", std::env::var("PATH").unwrap_or_default())
        .env("HOME", home)
        .env("CC_USAGE_CONFIG", &config)
        .env("CC_USAGE_NOW", NOW)
        .env("TZ", "UTC");
    for (k, v) in env {
        cmd.env(k, v);
    }
    let mut child = cmd
        .args(args)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    // 훅 입력 — guard 는 읽어서 버린다.
    child
        .stdin
        .take()
        .unwrap()
        .write_all(br#"{"session_id":"s","prompt":"hi"}"#)
        .unwrap();
    let out = child.wait_with_output().unwrap();
    Out {
        code: out.status.code().unwrap_or(-1),
        stdout: String::from_utf8_lossy(&out.stdout).into_owned(),
        stderr: String::from_utf8_lossy(&out.stderr).into_owned(),
    }
}

/// 로그인된 계정(`hint` 는 계정 파일의 `hasExtraUsageEnabled`)과 그 계정의 usage 캐시 — 5h 사용률 `used`(리셋 전).
fn seed(home: &Path, config_dir: &Path, used: f64, hint: Option<bool>) {
    std::fs::create_dir_all(config_dir).unwrap();
    let mut account = serde_json::json!({"emailAddress": "a@example.com"});
    if let Some(on) = hint {
        account["hasExtraUsageEnabled"] = on.into();
    }
    let file = if config_dir == home.join(".claude") {
        home.join(".claude.json")
    } else {
        config_dir.join(".claude.json")
    };
    std::fs::write(
        file,
        serde_json::json!({"oauthAccount": account}).to_string(),
    )
    .unwrap();
    let bucket = cc_usage::claude_account::cache_bucket(
        &cc_usage::claude_account::cache_slot(&home.join(".cache"), config_dir),
        Some("a@example.com"),
    );
    std::fs::create_dir_all(&bucket).unwrap();
    let usage = serde_json::json!({"usage": {
        "fetched_at": "2026-09-16T07:39:00Z",
        "five_hour": {"percent": used, "resets_at": "2026-09-16T09:00:00Z"}
    }});
    std::fs::write(bucket.join("usage.json"), usage.to_string()).unwrap();
}

const GUARD_ON: &str = r#"{"source":"api","guard":true}"#;
const BLOCKED: &str = "[cc-usage] 사용량 한도 소진 (5h@2026-09-16T09:00:00Z) — 이 prompt부터 크레딧이 차감됩니다.\n계속하려면 터미널에서: cc-usage allow 30m\n";

#[test]
fn guard_blocks_an_exhausted_limit_and_allow_lifts_it() {
    let dir = tempfile::tempdir().unwrap();
    let home = dir.path();
    seed(home, &home.join(".claude"), 100.0, None);

    let out = cc_usage(home, GUARD_ON, &[], &["guard"]);
    assert_eq!(
        (out.code, out.stdout.as_str(), out.stderr.as_str()),
        (2, "", BLOCKED)
    );

    let out = cc_usage(home, GUARD_ON, &[], &["allow"]);
    assert_eq!(
        (out.code, out.stdout.as_str()),
        (0, "[cc-usage] 08:10까지 크레딧 사용 허용\n")
    );
    let out = cc_usage(home, GUARD_ON, &[], &["guard"]);
    assert_eq!((out.code, out.stderr.as_str()), (0, ""));

    let out = cc_usage(home, GUARD_ON, &[], &["allow", "off"]);
    assert_eq!(
        (out.code, out.stdout.as_str()),
        (0, "[cc-usage] guard 다시 활성화\n")
    );
    assert_eq!(cc_usage(home, GUARD_ON, &[], &["guard"]).code, 2);

    let out = cc_usage(home, GUARD_ON, &[], &["allow", "1h30m"]);
    assert_eq!(out.stdout, "[cc-usage] 09:10까지 크레딧 사용 허용\n");
    // 허용 창 안(08:59)이면 통과.
    let later = [("CC_USAGE_NOW", "2026-09-16T08:59:00Z")];
    assert_eq!(cc_usage(home, GUARD_ON, &later, &["guard"]).code, 0);
    // 창이 지나면 다시 막는다.
    let out = cc_usage(home, GUARD_ON, &[], &["allow", "1s"]);
    assert_eq!(out.code, 0);
    let expired = [("CC_USAGE_NOW", "2026-09-16T07:40:02Z")];
    assert_eq!(cc_usage(home, GUARD_ON, &expired, &["guard"]).code, 2);
}

#[test]
fn allow_rejects_what_it_cannot_read() {
    let dir = tempfile::tempdir().unwrap();
    for arg in ["bogus", "30", "-5m", "0s"] {
        let out = cc_usage(dir.path(), GUARD_ON, &[], &["allow", arg]);
        assert_eq!(out.code, 1, "{arg}");
        assert!(
            out.stderr
                .contains(&format!("invalid duration {arg:?} (예: 30m, 2h)")),
            "{arg}: {}",
            out.stderr
        );
    }
}

/// fail-open — 꺼져 있거나, `source: none` 이거나, 데이터가 없거나, 크레딧이 꺼진 계정이면 막지 않는다.
#[test]
fn guard_fails_open() {
    for (statusline, used, hint) in [
        (r#"{"source":"api"}"#, 100.0, None),
        (r#"{"source":"none","guard":true}"#, 100.0, None),
        (GUARD_ON, 40.0, None),
        (GUARD_ON, 100.0, Some(false)),
    ] {
        let dir = tempfile::tempdir().unwrap();
        seed(dir.path(), &dir.path().join(".claude"), used, hint);
        let out = cc_usage(dir.path(), statusline, &[], &["guard"]);
        assert_eq!(
            (out.code, out.stderr.as_str()),
            (0, ""),
            "{statusline} {used} {hint:?}"
        );
    }
    // 캐시가 아예 없으면 모르는 것 — 막지 않는다.
    let dir = tempfile::tempdir().unwrap();
    let out = cc_usage(dir.path(), GUARD_ON, &[], &["guard"]);
    assert_eq!((out.code, out.stderr.as_str()), (0, ""));
}

/// guard 는 설정 파일의 source 만 본다 — 셸에 걸어 둔 `CC_USAGE_SOURCE=none` 이 조용히 끄지 못한다.
#[test]
fn guard_ignores_the_source_env() {
    let dir = tempfile::tempdir().unwrap();
    seed(dir.path(), &dir.path().join(".claude"), 100.0, None);
    let env = [("CC_USAGE_SOURCE", "none")];
    assert_eq!(cc_usage(dir.path(), GUARD_ON, &env, &["guard"]).code, 2);
}

/// guard 는 세션의 계정을 따라간다(`CLAUDE_CONFIG_DIR`). allow 는 계정과 상관없이 하나라, 다른 환경의 터미널에서 불러도 풀린다.
#[test]
fn guard_follows_the_session_account_and_allow_is_shared() {
    let dir = tempfile::tempdir().unwrap();
    let home = dir.path();
    let work = home.join("work-claude");
    seed(home, &home.join(".claude"), 40.0, None);
    seed(home, &work, 100.0, None);
    let work_env = [("CLAUDE_CONFIG_DIR", work.to_str().unwrap())];
    assert_eq!(cc_usage(home, GUARD_ON, &[], &["guard"]).code, 0);
    assert_eq!(cc_usage(home, GUARD_ON, &work_env, &["guard"]).code, 2);
    // 기본 환경의 터미널에서 푼다.
    assert_eq!(cc_usage(home, GUARD_ON, &[], &["allow", "10m"]).code, 0);
    assert_eq!(cc_usage(home, GUARD_ON, &work_env, &["guard"]).code, 0);
}
