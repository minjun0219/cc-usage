# cc-usage

Claude Code statusline + 크레딧 guard. **개인 계정(Pro/Max)** 과 **회사 Team 계정**을 profile로 나눠 한 binary로 처리합니다.

```
[work] · Sonnet · ctx 40% · 5h 100% 1h20m · 7d 55% 2d4h
💳 $11.60 / $50.00 · 이번 window +$0.80 · 크레딧 소진 중
```

> ⚠️ Team 계정의 5h/7d 및 크레딧 정보는 비공식 `GET https://api.anthropic.com/api/oauth/usage` 에 의존합니다. 응답 구조는 예고 없이 바뀔 수 있고, rate limit이 매우 낮습니다.

## 동작 방식

| profile `source` | 5h/7d 출처 | usage API 호출 시점 |
| --- | --- | --- |
| `stdin` (Pro/Max) | statusline stdin의 `rate_limits` | 5h 또는 7d가 **100%일 때만** (크레딧 조회) |
| `api` (Team) | usage API | `poll_seconds` 주기 (기본 300초) |
| `auto` | 최근 6시간 내 stdin에 `rate_limits`가 보였으면 stdin, 아니면 api | 위 규칙 |

- `cc-usage statusline`은 **network 호출을 하지 않습니다.** cache만 읽고, 갱신이 필요하면 `cc-usage refresh`를 detached로 띄운 뒤 즉시 종료합니다.
- `refresh`는 profile별 lock으로 동시에 하나만 실행되고, 실패 시 1m → 32m(최대 30m) backoff, 429의 `Retry-After`를 존중합니다.
- 한도 100%가 처음 관측된 시점의 `used_credits`를 baseline으로 저장해서 "이번 window에서 쓴 크레딧"을 계산합니다. 첫 조회 전 소진분은 포함되지 않습니다.
- token은 **읽기 전용**입니다 (token_env → macOS keychain → `<config_dir>/.credentials.json`). 만료 시 갱신하지 않고, Claude Code가 다음 요청에서 갱신합니다.

## 설치

```bash
make test
make install            # ~/.local/bin/cc-usage
```

## 설정

### 1. 계정 분리 (권장)

두 계정은 `CLAUDE_CONFIG_DIR`로 분리하는 것을 전제로 합니다.

```bash
claude                                   # 개인 (~/.claude)
CLAUDE_CONFIG_DIR=~/.claude-work claude  # 회사
```

`/login`으로 같은 config dir에서 계정을 바꿔 쓰면 credential이 하나라 profile 구분이 불가능합니다.

### 2. `~/.config/cc-usage/config.json`

[`examples/config.json`](examples/config.json) 참고. 주요 필드:

| 필드 | 기본값 | 설명 |
| --- | --- | --- |
| `label` | profile 이름 | statusline 앞에 표시 |
| `config_dir` | `~/.claude` | 이 profile의 `CLAUDE_CONFIG_DIR` |
| `source` | `auto` | `stdin` / `api` / `auto` |
| `keychain_service` | `Claude Code-credentials` | macOS keychain 항목 이름 |
| `credentials_file` | `<config_dir>/.credentials.json` | Linux 등 keychain이 없을 때 |
| `token_env` | – | 이 환경변수에 token이 있으면 우선 사용 |
| `poll_seconds` | 300 | api 모드 polling 주기 |
| `credit_poll_seconds` | 300 | 한도 소진 후 크레딧 조회 주기 |
| `credit_divisor` | 100 | `used_credits` 단위 환산 (cent 가정) |
| `currency` | `$` | 표시 통화 기호 |
| `always_show_credits` | false | 한도 전에도 크레딧 줄 표시 (stdin 모드에서는 API 호출이 늘어남) |
| `guard` | false | `cc-usage guard` 활성화 |

profile 선택 순서: `--profile` → `$CC_USAGE_PROFILE` → `$CLAUDE_CONFIG_DIR`와 `config_dir` 일치 → `default_profile`.

### 3. keychain 항목 확인 (macOS)

```bash
cc-usage doctor --profile work
```

`keychain 후보` 목록에서 회사 계정 항목을 골라 `keychain_service`에 넣으세요. 두 profile이 같은 항목을 가리키면 경고가 표시됩니다. `CLAUDE_CONFIG_DIR`를 쓸 때 keychain 항목 이름이 어떻게 정해지는지는 공식 문서로 확인하지 못했으므로, 반드시 `doctor`로 확인하는 것을 권장합니다. 처음 접근 시 keychain 허용 창이 뜰 수 있습니다.

### 4. 필드 검증 (처음 한 번)

```bash
cc-usage probe --profile work
```

원본 응답을 보고 다음을 확인하세요.

- `five_hour.utilization`이 0-100 범위인지 (cc-usage는 0-100으로 가정)
- `extra_usage.used_credits` 단위 (cent라면 `credit_divisor: 100` 유지)

rate limit이 낮으니 반복 실행은 피하세요.

### 5. Claude Code settings

- 개인: [`examples/settings.personal.json`](examples/settings.personal.json) → `~/.claude/settings.json`
- 회사: [`examples/settings.work.json`](examples/settings.work.json) → `~/.claude-work/settings.json`

`refreshInterval: 60`은 cache를 다시 읽는 주기일 뿐, API 호출 주기가 아닙니다.

## Guard (선택)

`guard: true`인 profile에서 `UserPromptSubmit` hook으로 등록하면, 다음 경우 prompt를 차단합니다 (exit 2).

- 5h 또는 7d가 100%이고, 크레딧이 켜져 있거나 아직 모르는 경우
- 최근 15분 내 `used_credits` 증가가 관측된 경우

크레딧을 쓰고 싶을 때:

```bash
cc-usage allow 30m --profile work   # 30분 허용
cc-usage allow off --profile work   # 즉시 다시 차단
```

한계:
- cache 기반이라 감지 전 소진분은 막지 못합니다.
- 이미 진행 중인 agent turn은 멈추지 않습니다.
- usage 정보를 한 번도 못 가져온 상태(api 모드 오류)에서는 차단하지 않습니다.

## 명령

```
cc-usage statusline [--profile NAME]
cc-usage guard      [--profile NAME]
cc-usage allow      [DURATION|off] [--profile NAME]
cc-usage refresh    [--profile NAME]
cc-usage probe      [--profile NAME]
cc-usage doctor     [--profile NAME]
cc-usage version
```

`NO_COLOR=1`이면 색상을 끕니다. cache는 `~/.cache/cc-usage/<profile>/` (0600).

## 확장 아이디어

같은 cache(`usage.json`, `state.json`)를 읽는 subcommand로 붙이면 API 호출이 늘지 않습니다.

- `cc-usage tui` — cmux pane용 Bubble Tea 대시보드
- `cc-usage cmux` — `cmux set-status`로 sidebar pill 갱신
- `--forecast` — 현재 속도로 window 소진 시각 예측
