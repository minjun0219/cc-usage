package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"cc-usage/internal/config"
)

// 도움말은 **바이너리만 보는 쪽**을 위한 것이다. settings.json 에 걸린 이 바이너리를
// 만난 에이전트는 소스 repo 가 어디 있는지 모른다 — README 에만 있던 extra_commands 를
// 못 찾고 statusLine 을 래퍼 스크립트로 갈아 끼우려 한 일이 실제로 있었다.
//
// 예시에 특정 도구 이름을 넣지 않는다. cc-usage 는 extra_commands 로 부르는 것이
// 무엇인지 모른다(AGENTS.md).

const usageCommands = `cc-usage — Claude Code statusline / 크레딧 guard

사용법:
  cc-usage statusline [--source none]
                               statusLine command (stdin JSON → stdout)
  cc-usage guard               UserPromptSubmit hook (한도 소진 시 exit 2)
  cc-usage allow [DURATION|off]   guard 일시 해제 (기본 30m)
  cc-usage refresh             usage API 1회 조회 (statusline이 자동 호출)
  cc-usage probe               usage API 원본 응답 출력 (필드 확인용)
  cc-usage doctor [--session-id ID]
                               설정/token/cache/keychain/extra_commands 진단
  cc-usage config              설정 파일 경로와 전체 필드
  cc-usage update [--check]    소스를 받아 다시 설치 (--check: 뒤처졌는지만 확인)
  cc-usage version
  cc-usage <명령> --help        명령별 도움말
`

const extraHelp = `다른 도구의 statusline 줄 붙이기 — extra_commands
  statusLine command 를 래퍼 스크립트로 바꾸지 말고, 설정 파일의 extra_commands 에
  적는다. 각 항목의 stdout 이 cc-usage 줄 아래에 설정 순서대로 그대로 붙는다.

    "extra_commands": [
      { "command": ["my-statusline-tool", "line", "-s", "{{session_id}}"] },
      { "command": ["curl", "-sf", "--get", "--data-urlencode", "cwd={{cwd}}",
                    "http://127.0.0.1:PORT/statusline"],
        "timeout_ms": 300 }
    ]

  - command 는 argv 배열이다. 셸을 거치지 않는다 (파이프·리다이렉트 없음).
  - placeholder 는 {{session_id}}, {{cwd}} 둘. 값이 빈 것이 하나라도 있으면 그 명령은
    실행하지 않는다.
  - timeout_ms 기본 300. 넘기거나, 0 이 아닌 코드로 끝나거나, 명령이 없으면 그 줄만
    빠진다 — statusline 은 실패를 조용히 삼킨다.
  - 항목들은 병렬로 돈다.
  - 줄이 안 붙으면 cc-usage doctor 가 항목별 실행 결과를 보여준다.
`

// configSection is where the settings live — 경로는 환경에 따라 달라지므로
// 고정 문자열이 아니라 지금 이 프로세스가 읽을 경로를 낸다.
func configSection() string {
	var b strings.Builder
	b.WriteString("설정 파일\n")
	fmt.Fprintf(&b, "  %s", config.Path())
	switch _, err := os.Stat(config.Path()); {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		b.WriteString("  (없음 — 기본값으로 동작한다. 만들면 된다)")
	default:
		fmt.Fprintf(&b, "  (%v)", err)
	}
	b.WriteString("\n  기본 ~/.config/cc-usage/config.json ($XDG_CONFIG_HOME 을 따른다). $CC_USAGE_CONFIG 로 바꿀 수 있다.\n")
	b.WriteString("  전체 필드: cc-usage config")
	if repoPath != "" {
		fmt.Fprintf(&b, "  ·  README: %s/README.md", repoPath)
	}
	b.WriteString("\n")
	return b.String()
}

func helpText() string {
	return usageCommands + "\n" + configSection() + "\n" + extraHelp
}

const statuslineHelp = `cc-usage statusline — Claude Code 의 statusLine command

  settings.json:
    "statusLine": { "type": "command", "command": "cc-usage statusline" }

  입력  stdin 으로 Claude Code 가 주는 statusLine JSON
        (model · workspace.current_dir · context_window · rate_limits · session_id)
  출력  stdout 으로 여러 줄
        1. 작업 경로 · git 브랜치/변경 (git repo 밖이면 경로만)
        2. [계정 배지] 모델 · ctx · 5h/7d 한도 · 크레딧
           (한도가 소진되거나 폭이 모자라면 크레딧이 제 줄로 내려간다)
        3. 그 아래로 extra_commands 출력

  network 를 기다리지 않는다 — usage 갱신이 필요하면 refresh 를 detached 로 띄우고
  바로 끝난다. 어떤 실패에도 무언가를 출력한다. 세그먼트가 안 보이면 cc-usage doctor.

  --source auto|stdin|api|none
        설정의 source 를 이 실행에서만 바꾼다 ($CC_USAGE_SOURCE 도 같다. 플래그가 이긴다).
        none 은 token·API·cache 를 일절 보지 않고 1번 줄과 모델 · ctx 만 그린다 —
        Claude Code 가 아닌 호스트에 쓴다. Antigravity(agy) 에서는 /statusline 으로 걸거나
        ~/.gemini/antigravity-cli/settings.json 에:
          "statusLine": { "type": "command", "command": "cc-usage statusline --source none" }
`

func statuslineHelpText() string {
	return statuslineHelp + "\n" + configSection() + "\n" + extraHelp
}

// configFields mirrors README 의 필드 표. 바이너리만 가진 쪽이 README 없이 볼 수 있게
// 여기 둔다 — 필드를 더하면 README 와 함께 고친다.
const configFields = `필드 (모두 생략 가능)
  config_dir           ~/.claude       Claude Code 의 CLAUDE_CONFIG_DIR ($CLAUDE_CONFIG_DIR 가 이긴다)
  source               auto            stdin (Pro/Max) / api (Team) / auto / none (한도 끔)
                                       $CC_USAGE_SOURCE · statusline --source 가 이긴다
  keychain_service     Claude Code-credentials
                                       macOS keychain 항목. 비기본 config_dir 이면 기본값 없음
                                       — 후보는 cc-usage doctor 가 나열한다
  credentials_file     <config_dir>/.credentials.json
  token_env            –               이 환경변수에 token 이 있으면 우선
  poll_seconds         300             api 모드 조회 주기
  credit_poll_seconds  300             한도 소진 후 크레딧 조회 주기
  credit_divisor       100             used_credits 단위 환산 (cent)
  currency             $               표시 통화 기호
  always_show_credits  false           크레딧이 0 이어도 표시
  alert_percent        90              임박 강조 임계. 0 이면 소진만 강조
  guard                false           cc-usage guard 활성화
  badges               –               { "<email>": { "emoji": "…" } 또는 { "glyph": "◆", "color": "blue" } }
                                       로그인된 계정별 머리표
  extra_commands       –               다른 도구의 statusline 줄 (아래)
`

func runConfig(args []string) error {
	if len(args) > 0 && !isHelp(args[0]) {
		return fmt.Errorf("config 는 인자를 받지 않습니다: %q", args[0])
	}
	fmt.Print(configSection() + "\n" + configFields + "\n" + extraHelp)
	return nil
}

// printHelp prints cmd's help — 전용 도움말이 있는 명령은 그것을, 나머지는 전체를.
func printHelp(cmd string) {
	switch cmd {
	case "statusline":
		fmt.Print(statuslineHelpText())
	case "config":
		_ = runConfig(nil)
	default:
		fmt.Print(helpText())
	}
}

// parseFlags parses args and reports whether help was asked for anywhere in them
// (`doctor --session-id x --help` 처럼 첫 인자가 아닐 때). flag 패키지의 자체
// Usage 출력은 끈다 — 도움말은 printHelp 한 곳에서 낸다.
func parseFlags(fs *flag.FlagSet, args []string) (help bool, err error) {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printHelp(fs.Name())
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func isHelp(a string) bool {
	switch a {
	case "-h", "-help", "--help", "help":
		return true
	}
	return false
}
