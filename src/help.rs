//! 도움말은 **바이너리만 보는 쪽**을 위한 것이다. settings.json 에 걸린 이 바이너리를 만난 에이전트는 소스 레포가 어디
//! 있는지 모른다 — README 에만 있던 extraCommands 를 못 찾고 statusLine 을 래퍼 스크립트로 갈아 끼우려 한 일이 실제로 있었다.
//!
//! 예시에 특정 도구 이름을 넣지 않는다. cc-usage 는 extraCommands 로 부르는 것이 무엇인지 모른다(AGENTS.md).

pub const USAGE: &str = "cc-usage — Claude Code statusline / 크레딧 guard

사용법:
  cc-usage statusline [--source S]
                               statusLine command (stdin JSON → stdout)
  cc-usage guard               UserPromptSubmit hook (한도 소진 시 exit 2)
  cc-usage allow [DURATION|off]   guard 일시 해제 (기본 30m, 예: 2h · 1h30m)
  cc-usage probe               usage API 원본 응답 출력 (필드 확인용)
  cc-usage doctor [--session ID]
                               설정·계정·token·cache·keychain 후보·extraCommands 진단
  cc-usage config              설정 파일 경로와 전체 필드
  cc-usage update [--check]    소스를 받아 다시 설치 (--check: 뒤처졌는지만 확인)
  cc-usage version
  cc-usage <명령> --help        명령별 도움말
";

pub const EXTRA: &str = r#"다른 도구의 statusline 줄 붙이기 — extraCommands
  statusLine command 를 래퍼 스크립트로 바꾸지 말고, 설정 파일의 extraCommands 에
  적는다. 각 항목의 stdout 이 cc-usage 줄 아래에 설정 순서대로 그대로 붙는다.

    "extraCommands": [
      { "command": ["my-statusline-tool", "line", "-s", "{{session_id}}"] },
      { "command": ["curl", "-sf", "--get", "--data-urlencode", "cwd={{cwd}}",
                    "http://127.0.0.1:PORT/statusline"],
        "timeoutMs": 300 }
    ]

  - command 는 argv 배열이다. 셸을 거치지 않는다 (파이프·리다이렉트 없음).
  - placeholder 는 {{session_id}}, {{cwd}} 둘. 쓰인 값이 비었으면 그 명령은 실행하지 않는다.
  - timeoutMs 기본 300. 넘기거나, 0 이 아닌 코드로 끝나거나, 명령이 없으면 그 줄만
    빠진다 — statusline 은 실패를 조용히 삼킨다. 넘기면 프로세스 그룹째 끊는다.
  - 항목들은 나란히 돈다.
  - 줄이 안 붙으면 cc-usage doctor 가 항목별 실행 결과를 보여준다.
"#;

pub const STATUSLINE: &str = r#"cc-usage statusline — Claude Code 의 statusLine command

  settings.json:
    "statusLine": { "type": "command", "command": "cc-usage statusline" }

  입력  stdin 으로 Claude Code 가 주는 statusLine JSON
  출력  stdout 으로 여러 줄
        1. 작업 경로 · git 브랜치/변경 (git repo 밖이면 경로만)
        2. [계정 배지] 모델 · ctx · 5h/7d 한도 · 크레딧
           (한도가 소진되거나 폭이 모자라면 크레딧이 제 줄로 내려간다)
        3. 그 아래로 extraCommands 출력

  network 를 기다리지 않는다 — usage 갱신이 필요하면 refresh 를 detached 로 띄우고
  바로 끝난다. 어떤 실패에도 무언가를 출력한다. 세그먼트가 안 보이면 cc-usage doctor.

  --source auto|stdin|api|none
        설정의 source 를 이 statusline 실행에서만 바꾼다 ($CC_USAGE_SOURCE 도 같다. 플래그가
        이긴다). guard 에는 먹지 않는다 — guard 는 설정 파일의 source 만 본다.
        none 은 token·API·cache 를 일절 보지 않고 1번 줄과 모델 · ctx 만 그린다
        (extraCommands 는 그대로 붙는다).

  Antigravity(agy) 에서도 같은 명령을 건다 — stdin 의 product 로 알아본다.
        Claude 쪽 token·API·cache 는 source 와 무관하게 보지 않고, 한도는 agy 가 주는
        quota 로 그린다(Gemini 모델이면 gemini-*, 아니면 3p-* 버킷).
"#;

/// README 의 필드 표와 같다 — 필드를 더하면 README · config.schema.json 과 함께 고친다.
pub const FIELDS: &str = r#"필드 (모두 생략 가능, 이름은 rocky.json 의 statusline 블록과 같다)
  configDir            ~/.claude       Claude Code 설정 폴더 ($CLAUDE_CONFIG_DIR 가 이긴다)
  source               auto            stdin (Pro/Max) / api (Team) / auto / none (한도 끔)
                                       statusline 에서는 $CC_USAGE_SOURCE · --source 가 이긴다
  keychainService      Claude Code-credentials
                                       macOS keychain 항목. configDir 세션에만 쓴다.
                                       없으면 Claude Code 이름 규칙(-<sha256(dir)[:8]> 접미사)
                                       — 후보는 cc-usage doctor 가 나열한다
  credentialsFile      <configDir>/.credentials.json
  tokenEnv             –               이 환경 변수에 token 이 있으면 먼저
  pollSeconds          300             api 모드 조회 주기
  creditPollSeconds    300             한도 소진 후 크레딧 조회 주기
  creditDivisor        100             used_credits 단위 환산 (cent)
  currency             $               표시 통화 기호
  creditFade           true            안 쓸 때 금액을 옅게, 쓰기 시작하면 3초에 걸쳐 원래 색
  alwaysShowCredits    false           크레딧이 0 이어도 표시
  alertPercent         90              임박 강조 임계. 0 이면 소진만 강조
  guard                false           cc-usage guard 활성화
  dirLink              –               경로를 OSC 8 링크로 (예: vscode://file{{cwd}})
  badges               –               { "<email>": { "emoji": "…" } 또는 { "glyph": "◆", "color": "blue" } }
                                       로그인된 계정별 머리표
  extraCommands        –               다른 도구의 statusline 줄 (아래)
  스키마: 소스 레포의 config.schema.json
"#;

/// 설정 파일 자리 — 환경에 따라 달라지므로 지금 이 프로세스가 읽을 경로를 낸다.
pub fn config_section() -> String {
    let mut out = String::from("설정 파일\n");
    match crate::config::path() {
        Some(p) => {
            out.push_str(&format!("  {}", p.display()));
            match std::fs::metadata(&p) {
                Ok(_) => {}
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                    out.push_str("  (없음 — 기본값으로 동작한다. 만들면 된다)")
                }
                Err(e) => out.push_str(&format!("  ({e})")),
            }
        }
        None => out.push_str("  (HOME 을 몰라 정할 수 없다)"),
    }
    out.push_str(
        "\n  기본 ~/.config/cc-usage/config.json ($XDG_CONFIG_HOME 을 따른다). $CC_USAGE_CONFIG 로 바꿀 수 있다.\n",
    );
    out.push_str(&format!(
        "  전체 필드: cc-usage config  ·  README: {}/README.md\n",
        crate::REPO_PATH
    ));
    out
}

/// 전체 도움말.
pub fn full() -> String {
    format!("{USAGE}\n{}\n{EXTRA}", config_section())
}

/// 명령별 도움말 — 전용 도움말이 있는 명령은 그것을, 나머지는 전체를.
pub fn for_command(cmd: &str) -> String {
    match cmd {
        "statusline" => format!("{STATUSLINE}\n{}\n{EXTRA}", config_section()),
        "config" => config_text(),
        _ => full(),
    }
}

/// `cc-usage config` 의 출력.
pub fn config_text() -> String {
    format!("{}\n{FIELDS}\n{EXTRA}", config_section())
}
