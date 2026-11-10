//! 설치된 바이너리가 자기 버전(`git describe`)을 안다 — `cc-usage version`, `update` 가 쓴다.
//! 소스 경로는 `CARGO_MANIFEST_DIR` 로 이미 알 수 있어 여기서 넣지 않는다.

use std::process::Command;

fn main() {
    let version = Command::new("git")
        .args(["describe", "--tags", "--always", "--dirty"])
        .output()
        .ok()
        .filter(|o| o.status.success())
        .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
        .filter(|v| !v.is_empty())
        .unwrap_or_else(|| "dev".to_string());
    println!("cargo:rustc-env=CC_USAGE_VERSION={version}");
    // 커밋·체크아웃이 바뀌면 다시 계산한다.
    println!("cargo:rerun-if-changed=.git/HEAD");
    println!("cargo:rerun-if-changed=.git/index");
    println!("cargo:rerun-if-changed=.git/refs");
}
