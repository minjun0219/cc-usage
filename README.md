# cc-usage

Claude Code statusline + 크레딧 guard. 계정 하나를 상정합니다 — 설정도 cache도 한 벌입니다.

```
~/dev/workspaces/cc-usage · ⎇ main +3 !5 ⇡1
Sonnet · ctx 40% · 5h 70% (↻18:00) · 💳 $38.40 ($50.00)
```

7d는 여유로우면 나오지 않고, 한도가 소진되면 크레딧이 제 줄로 내려갑니다.

```
~/dev/workspaces/cc-usage · ⎇ main +3 !5 ⇡1
Sonnet · ctx 40% · 7d 25% (2d 4h) · 5h 0% (↻18:00)
💳 $38.40 ($50.00) · 이번 window +$0.80 · 크레딧 소진 중
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
- **7d는 사용률 70% 아래면 나오지 않습니다.** 주 관심사는 5h이고 7d 리셋은 며칠 뒤라 자리만 차지합니다. 색이 노래지기 시작하는 지점부터 올라오고, 경보를 올린 창은 임계와 무관하게 나옵니다.
- **크레딧 금액은 "남은 값"입니다.** 5h·7d가 남은 비율인데 크레딧만 쓴 금액이면 방향이 엇갈려 뒤집어 읽게 됩니다. 색도 한도 창과 같은 규칙으로 골라서, 90%를 쓴 상태가 흐린 회색으로 조용히 지나가지 않습니다. 한도는 괄호로 감싸 흐리게 둡니다(`$9.83 ($100.00)`) — 한도 창의 `86% (↻14:40)`과 같은 꼴이라, 값 뒤의 괄호는 부가 정보라는 규칙이 줄 전체에서 한결같습니다.
- **크레딧 줄은 쓴 크레딧이 0이 아니면 항상 나옵니다.** `$0.00`은 자리만 먹고 아무것도 말하지 않으므로 0이면 줄 자체를 내지 않습니다. 평소에는 **상태 줄 끝에 붙어** 두 줄로 끝나고, 다음 두 경우에 **제 줄로 내려갑니다**.

  1. 강조가 붙는 상태(크레딧 소진 중 · 한도 소진 · 조회 중) — 문장이 길어지는 데다, 줄이 하나 느는 것 자체가 신호가 됩니다.
  2. **붙이면 폭이 모자랄 때** — `COLUMNS`로 폭을 알 수 있으면 표시 폭을 계산해서 판단합니다. `COLUMNS`가 없으면 이 판단을 건너뜁니다.

     이때 기준은 `COLUMNS`가 아니라 **`COLUMNS - 40`**입니다. `COLUMNS`는 터미널 폭이지 statusline이 다 써도 되는 폭이 아닙니다 — Claude Code가 그 오른쪽에 배지와 알림을 얹고(`✔ Update installed · Restart to update`가 실측 38칸), 우리 줄이 길면 **그것들이 아래로 밀려납니다.** 실측값에 여유를 더해 40칸을 비워 둡니다. 배지 문구는 그때그때 달라서 정확한 값을 알 길은 없습니다. 크레딧이 비활성이거나 응답에 없을 때도 줄이 나오지 않습니다.
- **usage API 호출 간격은 한도 상황에 따라 달라집니다.** 이 API는 rate limit이 낮아서, 크레딧이 깎일 일이 없는 구간에서 같은 주기로 부를 이유가 없습니다.

  | 가장 급한 창의 사용률 | 간격 |
  | --- | --- |
  | 70% 미만 (여유) | `poll_seconds` × 3 |
  | 70% 이상 (임박) | `poll_seconds` |
  | 100% (소진) | `credit_poll_seconds` |

  경계를 `alert_percent`가 아니라 따로 둔 것은, 임박 경고를 꺼도(`alert_percent: 0`) 폴링은 여전히 촘촘해져야 하기 때문입니다. 70%는 색이 노래지기 시작하는 지점과 같습니다 — 화면과 동작이 같은 이야기를 합니다.
- 한도 100%가 처음 관측된 시점의 `used_credits`를 baseline으로 저장해서 "이번 window에서 쓴 크레딧"을 계산합니다. 첫 조회 전 소진분은 포함되지 않습니다.
- `5h`/`7d` 숫자는 **남은 비율**입니다(100 − 사용률). 쓴 양보다 남은 양이 "지금 뭘 할 수 있나"에 바로 답하기 때문입니다. 색은 사용률로 고르므로 숫자가 작아질수록 빨개집니다. 괄호는 **언제 풀리나**입니다 — `↻18:00`은 8칸 고정이라 남은 시간이 줄어도 뒤가 밀리지 않고, `↻`가 "여기서 다시 시작한다"를 바로 전합니다. 하루를 넘기면 시각만으로는 어느 날인지 모르므로 남은 시간(`2d 4h`)을 냅니다.
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
| `poll_seconds` | 300 | api 모드 polling 주기 (여유 구간에서는 3배로 늘어납니다) |
| `credit_poll_seconds` | 300 | 한도 소진 후 크레딧 조회 주기 |
| `credit_divisor` | 100 | `used_credits` 단위 환산 (cent 가정) |
| `currency` | `$` | 표시 통화 기호 |
| `always_show_credits` | false | 크레딧이 0이어도 줄 표시. stdin 모드에서는 한도 전에도 API를 부르게 됩니다 |
| `badges` | – | 로그인된 계정을 이메일로 알아보는 표시 (아래 참고) |
| `alert_percent` | 90 | 이 %를 넘으면 "임박" 강조. **`0`이면 임박 경고를 끄고** 소진(100%)만 강조 |
| `guard` | false | `cc-usage guard` 활성화 |
| `extra_commands` | – | 다른 도구의 statusline 줄을 아래에 덧붙임 (아래 참고) |

머신이 여럿이면(회사·집) **각 머신에 `config.json`을 하나씩** 둡니다. 계정 타입이 달라도 `source`만 각자 적으면 됩니다 — 회사 Team 계정은 `api`, 집 Pro/Max는 `stdin`. 나머지 설정(색은 코드, 표기는 코드)은 같은 바이너리를 쓰는 한 저절로 같습니다.

한 머신에서 계정을 여럿 보려면 설정을 나누는 게 아니라 **프로세스를 나눕니다** — `CC_USAGE_CONFIG`와 `XDG_CACHE_HOME`을 다른 경로로 주면 설정도 cache도 통째로 갈립니다.

```bash
CC_USAGE_CONFIG=~/.config/cc-usage/work.json \
XDG_CACHE_HOME=~/.cache/cc-usage-work \
  cc-usage statusline
```

#### `badges` — 어느 계정으로 돌고 있는지

계정마다 상태 줄 앞에 작은 표시를 붙입니다. **키는 이메일**입니다.

```json
"badges": {
  "work@example.com": { "emoji": "🏢" },
  "me@example.com":   { "color": "blue", "glyph": "◆" }
}
```

- `emoji`가 있으면 그것만 씁니다 (이모지는 제 색을 가지므로 `color`를 보지 않습니다)
- 없으면 `glyph`(기본 `●`)를 `color`로 칠합니다
- `color`는 이름(`blue`·`brightblue`·`cyan`·`green`·`yellow`·`magenta`·`red`·`gray`·`white`) 또는 256 인덱스(`"33"`)
- **목록에 없는 계정은 아무것도 붙지 않습니다.** 평소 쓰는 계정을 안 적어두면, 표시가 뜨는 것 자체가 "여기는 평소 자리가 아니다"라는 신호가 됩니다

> **빨강은 피하는 게 좋습니다.** 이 줄에서 빨강은 "여기서 멈춘다"(한도·경보)를 뜻하도록 축을 갈라 뒀습니다. 막지는 않습니다.

계정은 Claude Code 자신의 `.claude.json`에 있는 `oauthAccount.emailAddress`로 알아냅니다 — **누가 계정을 바꾸든 결과가 드러나는 단일 진실 원천**이라, 전환 도구를 알 필요가 없습니다. `<config_dir>/.claude.json`을 먼저 보고 없으면 `~/.claude.json`으로 떨어집니다. 읽기만 합니다.

매 렌더 읽지는 않습니다. **한도 값이 움직였을 때만** 다시 읽습니다 — 계정이 바뀌면 한도도 바뀌기 때문입니다. 그래서 두 계정의 사용률이 똑같은 순간에 전환하면 그 렌더에서는 안 잡히지만, 한도는 프롬프트 한 번이면 움직이므로 곧 따라옵니다. 읽기 실패·파싱 실패·필드 없음은 모두 "표시 없음"으로 떨어집니다.

**cc-usage는 계정을 바꾸지 않습니다.** 지금 로그인된 계정을 알아보게만 합니다.

#### `alert_percent` — 한도 임박 강조

한도가 `alert_percent`를 넘으면(임박) 또는 100%에 닿으면(소진), 해당 window 세그먼트가 **빨간 배지**가 됩니다. 단계가 올라간 직후 `AlertBurst`(6초) 동안은 배지와 굵은 빨강을 오가며 깜빡이고, 그 뒤에는 배지로 남습니다. 계속 움직이는 표시는 결국 눈에 안 들어오기 때문에 움직임은 그 6초뿐입니다.

깜빡임의 꺼진 프레임에도 배지의 양옆 여백은 남습니다 — 프레임마다 폭이 바뀌면 줄 전체가 좌우로 출렁입니다. 깜빡이는 것은 색이지 자리가 아닙니다.

> 프레임은 벽시계에서 고르므로 **프레임 카운터를 따로 저장하지 않고**, 터미널의 blink(SGR 5) 지원 여부와도 무관합니다. 다만 statusline은 초당 여러 번 호출되므로 단계가 올라간 **시점**은 `state.json`에 기록합니다 — 거기서부터 6초를 셉니다(`statusline`이 유일한 writer라는 불변 조건 그대로). 창이 리셋되면 키가 달라져 다시 무장합니다.

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

statusline은 stdout이 파이프라 `tput`·ioctl로 터미널 폭을 알 수 없습니다. Claude Code가 `COLUMNS`·`LINES`를 넣어 주므로 그걸 읽습니다. 표시 폭 계산은 ANSI를 걷어내고 한글·이모지를 2칸으로 세는데, 표준 라이브러리에 wcwidth가 없어 필요한 구간만 담은 근사입니다(`internal/render/width.go`).

`NO_COLOR=1`이면 색상을 끕니다. 퍼센트 색은 `COLORTERM`이 `truecolor`/`24bit`면 24bit 그라데이션으로 끊김 없이 변하고, 아니면 3단계(green/yellow/red)로 떨어집니다 — 지원하지 않는 터미널에서 이스케이프가 글자로 새는 것보다 계단식 색이 낫습니다. 경보(배지·굵은 빨강)는 임계를 넘어선 상태라 고정색입니다. cache는 `~/.cache/cc-usage/` (0600).

## 확장 아이디어

같은 cache(`usage.json`, `state.json`)를 읽는 subcommand로 붙이면 API 호출이 늘지 않습니다.

- `cc-usage tui` — cmux pane용 Bubble Tea 대시보드
- `cc-usage cmux` — `cmux set-status`로 sidebar pill 갱신
- `--forecast` — 현재 속도로 window 소진 시각 예측

## 라이선스

MIT — [LICENSE](LICENSE)
