# cc-usage

Claude Code statusline + 크레딧 guard. 계정 하나를 상정합니다 — 설정도 cache도 한 벌입니다.

```
~/dev/workspaces/cc-usage · ⎇ main +3 !5 ⇡1
Sonnet · ctx 40% · 5h 0% (1h20m→18:00) · 7d 45% (2d4h)
💳 $11.60 / $50.00 · 이번 window +$0.80 · 크레딧 소진 중
```

> ⚠️ Team 계정의 5h/7d 및 크레딧 정보는 비공식 `GET https://api.anthropic.com/api/oauth/usage` 에 의존합니다. 응답 구조는 예고 없이 바뀔 수 있고, rate limit이 매우 낮습니다.

## 동작 방식

| `source` | 5h/7d 출처 | usage API 호출 시점 |
| --- | --- | --- |
| `stdin` (Pro/Max) | statusline stdin의 `rate_limits` | 5h 또는 7d가 **100%일 때만** (크레딧 조회) |
| `api` (Team) | usage API | `poll_seconds` 주기 (기본 300초) |
| `auto` | 최근 6시간 내 stdin에 `rate_limits`가 보였으면 stdin, 아니면 api | 위 규칙 |

- `cc-usage statusline`은 **network를 기다리지 않습니다.** usage API는 이 경로에서 호출하지 않고, 갱신이 필요하면 `cc-usage refresh`를 detached로 띄운 뒤 즉시 종료합니다. 로컬 subprocess(`git status`, `extra_commands`)는 타임아웃을 걸고 부르며, 느리거나 실패하면 그 세그먼트만 빠집니다. `extra_commands`에 network를 타는 명령을 넣는 것은 설정하는 쪽의 선택이고, 그 지연은 타임아웃이 막습니다.
- `refresh`는 lock으로 동시에 하나만 실행되고, 실패 시 1m → 32m(최대 30m) backoff, 429의 `Retry-After`를 존중합니다.
- 한도 100%가 처음 관측된 시점의 `used_credits`를 baseline으로 저장해서 "이번 window에서 쓴 크레딧"을 계산합니다. 첫 조회 전 소진분은 포함되지 않습니다.
- `5h`/`7d` 숫자는 **남은 비율**입니다(100 − 사용률). 쓴 양보다 남은 양이 "지금 뭘 할 수 있나"에 바로 답하기 때문입니다. 색은 사용률로 고르므로 숫자가 작아질수록 빨개집니다. 괄호는 리셋까지 남은 시간이고, 하루를 넘으면 어느 날인지 모호해지므로 시각을 빼고 남은 시간만 냅니다.
- 경로 줄은 stdin의 `workspace.current_dir`에서 나옵니다. 브랜치 상태는 `git status --porcelain=v2 --branch --untracked-files=no` **한 번**으로 읽습니다 — 브랜치명, 변경 파일 수(`=` conflict / `+` staged / `!` unstaged), ahead(`⇡`)/behind(`⇣`). 개수는 porcelain=v2가 파일당 한 줄을 뱉고 `XY` 필드가 staged/unstaged를 구분해 주므로 추가 git 호출이 없습니다. untracked는 스캔 비용 때문에 세지 않습니다(`--untracked-files=no`). git repo가 아니거나 500ms를 넘기면 세그먼트만 빠집니다.
- token은 **읽기 전용**입니다 (token_env → macOS keychain → `<config_dir>/.credentials.json`). 만료 시 갱신하지 않고, Claude Code가 다음 요청에서 갱신합니다.

## 설치

```bash
make test
make install            # ~/.local/bin/cc-usage
```

## 설정

### 1. `~/.config/cc-usage/config.json`

[`examples/config.json`](examples/config.json) 참고. 주요 필드:

| 필드 | 기본값 | 설명 |
| --- | --- | --- |
| `config_dir` | `~/.claude` | Claude Code의 `CLAUDE_CONFIG_DIR` |
| `source` | `auto` | `stdin` / `api` / `auto` |
| `keychain_service` | `Claude Code-credentials` | macOS keychain 항목 이름 |
| `credentials_file` | `<config_dir>/.credentials.json` | Linux 등 keychain이 없을 때 |
| `token_env` | – | 이 환경변수에 token이 있으면 우선 사용 |
| `poll_seconds` | 300 | api 모드 polling 주기 |
| `credit_poll_seconds` | 300 | 한도 소진 후 크레딧 조회 주기 |
| `credit_divisor` | 100 | `used_credits` 단위 환산 (cent 가정) |
| `currency` | `$` | 표시 통화 기호 |
| `always_show_credits` | false | 한도 전에도 크레딧 줄 표시 (stdin 모드에서는 API 호출이 늘어남) |
| `alert_percent` | 90 | 이 %를 넘으면 "임박" 강조. 범위 밖(예: `-1`)이면 임박 경고를 끄고 소진만 강조 |
| `notify` | – | 한도 단계가 올라갈 때 1회 실행할 명령 (아래 참고) |
| `guard` | false | `cc-usage guard` 활성화 |
| `extra_commands` | – | 다른 도구의 statusline 줄을 아래에 덧붙임 (아래 참고) |

머신이 여럿이면(회사·집) **각 머신에 `config.json`을 하나씩** 둡니다. 계정 타입이 달라도 `source`만 각자 적으면 됩니다 — 회사 Team 계정은 `api`, 집 Pro/Max는 `stdin`. 나머지 설정(색은 코드, 표기는 코드)은 같은 바이너리를 쓰는 한 저절로 같습니다.

한 머신에서 계정을 여럿 보려면 설정을 나누는 게 아니라 **프로세스를 나눕니다** — `CC_USAGE_CONFIG`와 `XDG_CACHE_HOME`을 다른 경로로 주면 설정도 cache도 통째로 갈립니다.

```bash
CC_USAGE_CONFIG=~/.config/cc-usage/work.json \
XDG_CACHE_HOME=~/.cache/cc-usage-work \
  cc-usage statusline
```

#### `alert_percent` / `notify` — 한도 임박 알리기

한도가 `alert_percent`를 넘으면(임박) 또는 100%에 닿으면(소진), 해당 window 세그먼트를 **몇 초간 빨간 배지로 깜빡인 뒤 굵은 빨강으로 고정**합니다. 계속 움직이는 표시는 결국 눈에 안 들어오기 때문에, 움직임은 단계가 올라간 직후 `AlertBurst`(6초) 동안만입니다. 프레임은 벽시계에서 고르므로 상태 저장이 없고, 터미널의 blink(SGR 5) 지원 여부와 무관합니다.

> statusline은 초당 여러 번 호출됩니다. 그래서 강조와 알림 모두 **엣지 트리거**입니다 — 단계가 올라간 그 순간 한 번만 발동하고, 창이 리셋되면 다시 무장합니다. 어디까지 알렸는지는 `state.json`에 남습니다(`statusline`이 유일한 writer라는 불변 조건 그대로).

`notify`를 두면 그 엣지에서 명령을 **detached로 1회** 실행합니다. `enabled`를 `false`로 두면 명령은 그대로 둔 채 끕니다 — 끄자고 블록을 지우면 다시 켤 때 명령을 기억해 내야 하기 때문입니다. `enabled`를 생략하면 켜진 것으로 봅니다. 현재 상태는 `cc-usage doctor`가 보여줍니다.

```json
"alert_percent": 90,
"notify": {
  "enabled": true,
  "command": ["osascript", "-e",
              "display notification \"{{message}}\" with title \"Claude 한도\" subtitle \"{{window}} {{percent}}%\""]
}
```

placeholder는 `{{level}}`(`near`/`over`), `{{window}}`(`5h`/`7d`), `{{percent}}`, `{{message}}`입니다. `extra_commands`와 같은 이유로 argv 배열이고, 어떤 알림 수단을 쓸지는 설정에만 있습니다 — 코드에는 `osascript`가 없습니다.

수단은 환경에 따라 갈립니다. `osascript`·`say`·`terminal-notifier`는 **cc-usage가 도는 그 머신**에서 울리므로, SSH 너머에서 쓰는 중이라면 아무도 못 봅니다. 그 경우 `curl`로 푸시 서비스(ntfy, Pushover, Slack webhook 등)에 쏘는 편이 실제로 도착합니다. 다만 한도 수치가 외부로 나가므로 토픽/엔드포인트 관리가 필요합니다.

> 터미널 벨(`\a`)이나 OSC 9/777은 SSH를 타고 실제로 보고 있는 터미널까지 가지만, `notify`는 detached라 stdout이 터미널에 없어서 이 경로로는 안 됩니다. 쓰려면 statusline 출력 자체에 섞는 별도 경로가 필요합니다.

#### `extra_commands` — 다른 도구의 줄 덧붙이기

cc-usage가 모르는 세그먼트(로컬 위임 표시, todo 보드 등)는 코드가 아니라 설정으로 붙입니다. 각 항목의 stdout이 cc-usage 줄 **아래에 그대로** 출력됩니다.

```json
"extra_commands": [
  { "command": ["my-statusline-tool", "line", "-s", "{{session_id}}"] },
  {
    "command": ["curl", "-sf", "--get",
                "--data-urlencode", "cwd={{cwd}}",
                "--data-urlencode", "session={{session_id}}",
                "http://127.0.0.1:PORT/api/statusline"],
    "timeout_ms": 300
  }
]
```

- `command`는 **argv 배열**입니다 (shell을 거치지 않으므로 따옴표·공백 문제가 없습니다).
- placeholder는 `{{session_id}}`, `{{cwd}}` 둘입니다. **값이 빈 placeholder가 하나라도 있으면 그 명령은 실행하지 않습니다** — 예를 들어 session이 없는 호출에서는 위임 줄이 뜨지 않습니다.
- 명령이 없거나, 0이 아닌 코드로 끝나거나, `timeout_ms`(기본 300ms)를 넘기면 **아무것도 출력하지 않습니다.** statusline은 어떤 경우에도 나머지 줄을 출력합니다. 위 `curl -sf`의 `-f`도 같은 목적입니다 — 이 라우트가 없는 구버전 데몬의 404 JSON 본문이 statusline에 새지 않게 합니다.
- 항목들은 병렬로 실행되고, 출력은 설정에 적은 순서대로 붙습니다.
- 여기서 무엇을 부를지는 전적으로 이 설정 파일에만 있습니다. cc-usage 코드에는 `my-statusline-tool`도 보드 데몬도 등장하지 않습니다.

### 2. keychain 항목 확인 (macOS)

```bash
cc-usage doctor
```

`keychain 후보` 목록에서 맞는 항목을 골라 `keychain_service`에 넣으세요. `CLAUDE_CONFIG_DIR`를 쓸 때 keychain 항목 이름이 어떻게 정해지는지는 공식 문서로 확인하지 못했으므로, 반드시 `doctor`로 확인하는 것을 권장합니다. 처음 접근 시 keychain 허용 창이 뜰 수 있습니다.

### 3. 필드 검증 (처음 한 번)

```bash
cc-usage probe
```

원본 응답을 보고 다음을 확인하세요.

- `five_hour.utilization`이 0-100 범위인지 (cc-usage는 0-100으로 가정)
- `extra_usage.used_credits` 단위 (cent라면 `credit_divisor: 100` 유지)

rate limit이 낮으니 반복 실행은 피하세요.

### 4. Claude Code settings

[`examples/settings.json`](examples/settings.json) → `~/.claude/settings.json`

`refreshInterval: 60`은 cache를 다시 읽는 주기일 뿐, API 호출 주기가 아닙니다.

## Guard (선택)

`guard: true`로 두고 `UserPromptSubmit` hook으로 등록하면, 다음 경우 prompt를 차단합니다 (exit 2).

- 5h 또는 7d가 100%이고, 크레딧이 켜져 있거나 아직 모르는 경우
- 최근 15분 내 `used_credits` 증가가 관측된 경우

크레딧을 쓰고 싶을 때:

```bash
cc-usage allow 30m   # 30분 허용
cc-usage allow off   # 즉시 다시 차단
```

한계:
- cache 기반이라 감지 전 소진분은 막지 못합니다.
- 이미 진행 중인 agent turn은 멈추지 않습니다.
- usage 정보를 한 번도 못 가져온 상태(api 모드 오류)에서는 차단하지 않습니다.

## 명령

```
cc-usage statusline
cc-usage guard
cc-usage allow [DURATION|off]
cc-usage refresh
cc-usage probe
cc-usage doctor
cc-usage version
```

`NO_COLOR=1`이면 색상을 끕니다. cache는 `~/.cache/cc-usage/` (0600).

## 확장 아이디어

같은 cache(`usage.json`, `state.json`)를 읽는 subcommand로 붙이면 API 호출이 늘지 않습니다.

- `cc-usage tui` — cmux pane용 Bubble Tea 대시보드
- `cc-usage cmux` — `cmux set-status`로 sidebar pill 갱신
- `--forecast` — 현재 속도로 window 소진 시각 예측
