//! `cc-usage update [--check]` — 빌드한 소스 체크아웃(`REPO_PATH`)에서 받아 다시 설치한다.
//!
//! 자동 업데이트가 아니다 — 부를 때만 돈다. 세션 도중에 동작이 조용히 바뀌는 것을 피하려는 것이고, 그런 어긋남은 실제로
//! 사고를 냈다(설정이 옛 플래그를 들고 있는데 바이너리가 그것을 모르는 상태).

use std::path::{Path, PathBuf};
use std::process::Command;

/// 받고 → 게이트(`cargo test`)를 통과하면 → release 빌드를 지금 돌고 있는 바로 그 파일 자리에 설치한다.
pub fn run(check: bool) -> Result<(), String> {
    let repo = Path::new(crate::REPO_PATH);
    // .git 이 디렉터리인지로 판단하면 linked worktree 와 submodule 이 걸린다 — git 에게 묻는다.
    git(repo, &["rev-parse", "--git-dir"]).map_err(|out| {
        format!(
            "git 저장소가 아닙니다: {} (옮겼거나 지웠습니까?){}",
            repo.display(),
            indent(&out)
        )
    })?;
    println!("소스: {}", repo.display());

    // remote 를 적지 않는다 — upstream 이 origin 이 아닌 저장소에서 "origin" 을 박으면 낡은 데이터 위에서 비교한다.
    git(repo, &["fetch", "--quiet"]).map_err(|out| format!("fetch 실패{}", indent(&out)))?;
    // upstream 이 없으면 비교할 대상이 없다. 모르는 것을 "최신" 이라고 말하지 않는다.
    let (behind, ahead) = counts(repo).ok_or(
        "upstream 이 없어 비교할 수 없습니다 (브랜치가 원격을 추적하고 있습니까?)".to_string(),
    )?;
    match (behind, ahead) {
        (0, 0) => println!("최신입니다."),
        (0, a) => println!("원격보다 {a}커밋 앞서 있습니다 (받을 것 없음)."),
        (b, _) => println!("{b}커밋 뒤처져 있습니다."),
    }
    if check || behind == 0 {
        return Ok(());
    }

    // 커밋하지 않은 변경이 있으면 멈춘다 — 돌고 있는 바이너리가 하는 일의 소스가 어디에도 없게 된다.
    // status 실패를 무시하면 빈 출력이 "깨끗함" 으로 읽혀 그대로 설치까지 간다.
    let status = git(repo, &["status", "--porcelain"])
        .map_err(|out| format!("작업 트리 상태를 확인할 수 없습니다{}", indent(&out)))?;
    if !status.trim().is_empty() {
        return Err(format!(
            "커밋하지 않은 변경이 있습니다 — 먼저 정리하세요:{}",
            indent(&status)
        ));
    }
    git(repo, &["pull", "--ff-only", "--quiet"])
        .map_err(|out| format!("pull 실패 (갈라졌을 수 있습니다){}", indent(&out)))?;

    // 게이트를 통과하지 못한 것을 설치하지 않는다.
    println!("cargo test …");
    run_in(repo, "cargo", &["test", "--quiet"])
        .map_err(|out| format!("테스트 실패 — 설치하지 않습니다{}", indent(&out)))?;
    println!("cargo build --release …");
    run_in(repo, "cargo", &["build", "--release", "--quiet"])
        .map_err(|out| format!("빌드 실패{}", indent(&out)))?;
    let exe = install_target()?;
    let built = repo.join("target").join("release").join("cc-usage");
    let (built_s, exe_s) = (built.to_string_lossy(), exe.to_string_lossy());
    run_in(repo, "install", &["-m", "0755", &built_s, &exe_s])
        .map_err(|out| format!("설치 실패 ({exe_s}){}", indent(&out)))?;
    let now = git(repo, &["describe", "--tags", "--always", "--dirty"]).unwrap_or_default();
    println!("{} → {}  ({exe_s})", crate::VERSION, now.trim());
    Ok(())
}

/// 이 업데이트가 덮어쓸 파일 — 지금 돌고 있는 바로 그 파일이다. 기본 자리(`make install` 의 PREFIX)에 설치하면, 바이너리를
/// 옮겨 뒀을 때 엉뚱한 파일을 갱신하고 성공이라 보고한다. 심링크는 실체까지 따라간다 — 링크를 파일로 덮어쓰지 않게.
fn install_target() -> Result<PathBuf, String> {
    let exe =
        std::env::current_exe().map_err(|e| format!("실행 파일 경로를 알 수 없습니다: {e}"))?;
    Ok(std::fs::canonicalize(&exe).unwrap_or(exe))
}

/// HEAD 가 upstream 보다 몇 커밋 뒤/앞인가. 비교 자체가 안 되면 None — 0/0 으로 뭉개면 호출자가 "최신" 으로 읽는다.
fn counts(repo: &Path) -> Option<(u32, u32)> {
    let out = git(
        repo,
        &["rev-list", "--left-right", "--count", "HEAD...@{u}"],
    )
    .ok()?;
    parse_counts(&out)
}

/// `rev-list --left-right --count HEAD...@{u}` 의 출력(`<ahead>\t<behind>`) → (behind, ahead).
pub fn parse_counts(out: &str) -> Option<(u32, u32)> {
    let mut it = out.split_whitespace();
    let ahead = it.next()?.parse().ok()?;
    let behind = it.next()?.parse().ok()?;
    it.next().is_none().then_some((behind, ahead))
}

fn git(repo: &Path, args: &[&str]) -> Result<String, String> {
    let repo_s = repo.to_string_lossy();
    let mut full = vec!["-C", &repo_s];
    full.extend_from_slice(args);
    run_in(repo, "git", &full)
}

/// 성공이면 stdout, 실패면 stdout+stderr(사람에게 보일 것).
fn run_in(dir: &Path, program: &str, args: &[&str]) -> Result<String, String> {
    let out = Command::new(program)
        .args(args)
        .current_dir(dir)
        .output()
        .map_err(|e| format!("{program}: {e}"))?;
    let text = format!(
        "{}{}",
        String::from_utf8_lossy(&out.stdout),
        String::from_utf8_lossy(&out.stderr)
    );
    if out.status.success() {
        Ok(String::from_utf8_lossy(&out.stdout).into_owned())
    } else {
        Err(format!(
            "{program} {}: {}\n{text}",
            args.join(" "),
            out.status
        ))
    }
}

/// 여러 줄 출력을 들여 쓴다 — 비었으면 빈 문자열.
pub fn indent(s: &str) -> String {
    let s = s.trim_end_matches('\n');
    if s.is_empty() {
        return String::new();
    }
    format!("\n  {}", s.replace('\n', "\n  "))
}
