//! 명령 입구 — 도움말·버전·config·update 의 인자 처리. 판정·렌더는 다른 테스트 파일이 본다.

use std::process::Command;

fn run(args: &[&str]) -> (i32, String, String) {
    let dir = tempfile::tempdir().unwrap();
    let out = Command::new(env!("CARGO_BIN_EXE_cc-usage"))
        .env_clear()
        .env("PATH", std::env::var("PATH").unwrap_or_default())
        .env("HOME", dir.path())
        .args(args)
        .output()
        .unwrap();
    (
        out.status.code().unwrap_or(-1),
        String::from_utf8_lossy(&out.stdout).into_owned(),
        String::from_utf8_lossy(&out.stderr).into_owned(),
    )
}

#[test]
fn no_args_or_unknown_command_print_help_and_exit_2() {
    for args in [&[][..], &["bogus"][..]] {
        let (code, out, err) = run(args);
        assert_eq!(code, 2, "{args:?}");
        assert_eq!(out, "");
        assert!(err.contains("cc-usage statusline"), "{args:?}: {err}");
    }
}

#[test]
fn help_flag_is_help_for_that_command() {
    let (code, out, _) = run(&["statusline", "--help"]);
    assert_eq!(code, 0);
    assert!(out.starts_with("cc-usage statusline — "), "{out}");
    // 설정 경로 줄은 임시 HOME 마다 다르다 — 그 앞까지만 견준다.
    let head = |s: &str| s.split("설정 파일").next().unwrap().to_string();
    let (_, via_help, _) = run(&["help", "statusline"]);
    assert_eq!(head(&out), head(&via_help));
}

#[test]
fn version_prints_one_line() {
    let (code, out, _) = run(&["version"]);
    assert_eq!(code, 0);
    assert_eq!(out.lines().count(), 1, "{out}");
    assert_eq!(out.trim(), cc_usage::VERSION);
}

/// `config` 는 지금 읽을 경로(홈 기준)와 camelCase 필드를 보인다.
#[test]
fn config_shows_the_path_and_fields() {
    let (code, out, _) = run(&["config"]);
    assert_eq!(code, 0);
    assert!(
        out.contains("/.config/cc-usage/config.json  (없음"),
        "{out}"
    );
    for field in [
        "configDir",
        "extraCommands",
        "dirLink",
        "badges",
        "creditFade",
    ] {
        assert!(out.contains(field), "{field}: {out}");
    }
    let (code, _, err) = run(&["config", "extra"]);
    assert_eq!(code, 1);
    assert!(err.contains("인자를 받지 않습니다"), "{err}");
}

#[test]
fn update_rejects_unknown_flags_before_touching_git() {
    let (code, out, err) = run(&["update", "--force"]);
    assert_eq!(code, 1);
    assert_eq!(out, "");
    assert!(err.contains("unknown flag: --force"), "{err}");
}

#[test]
fn rev_list_counts_parse_as_behind_ahead() {
    use cc_usage::update::parse_counts;
    assert_eq!(parse_counts("2\t5\n"), Some((5, 2)));
    assert_eq!(parse_counts("0\t0"), Some((0, 0)));
    assert_eq!(parse_counts(""), None);
    assert_eq!(parse_counts("1"), None);
    assert_eq!(parse_counts("1\t2\t3"), None);
}

#[test]
fn allow_takes_at_most_one_argument() {
    let (code, _, err) = run(&["allow", "1h", "2h"]);
    assert_eq!(code, 1);
    assert!(err.contains("하나만"), "{err}");
}
