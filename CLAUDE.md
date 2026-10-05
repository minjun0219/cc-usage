# cc-usage: Claude Code 작업 가이드

> **아카이브됨 (2026-10-06).** 기능은 [rocky](https://github.com/minjun0219/rocky)로 옮겼다 — 설계는 rocky `docs/design/specs/2026-10-05-cc-usage-mirror-design.md`. 이 repo에는 변경을 받지 않는다. 아래 실측 기록은 rocky 쪽 판단의 근거로 남긴다.

Claude Code statusline + 크레딧 guard. Go 표준 라이브러리만 사용 (외부 의존성 추가 금지).

**새 머신에 설치·적용하는 작업이면 [SETUP.md](SETUP.md)를 본다.** 이 문서와 AGENTS.md는 repo를 고치는 작업용이다.

## 구조

```
cmd/cc-usage/          subcommand 진입점 (statusline, guard, allow, refresh, probe, doctor, config, update) · 도움말(help.go)
internal/config/    설정 로딩, 기본값
internal/store/     cache 파일 모델, atomic write, flock
internal/auth/      token 읽기 (read-only), keychain 후보 목록
internal/api/       비공식 /api/oauth/usage 호출 및 파싱
internal/core/      순수 판단 로직 (stdin 파싱, 모드 선택, refresh 필요 여부, 크레딧 baseline, guard)
internal/render/    statusline 문자열 생성
examples/           config, settings.json 예시
```

## 불변 조건 (반드시 지킬 것)

1. `statusline`은 **network를 기다리지 않는다.** cc-usage 자신은 이 경로에서 API를 호출하지 않고, 갱신이 필요하면 `refresh`를 detached로 실행한 뒤 즉시 끝낸다.
   로컬 subprocess(`git status`, `extra_commands`)는 **반드시 타임아웃을 걸고** 실행한다. 실패·타임아웃은 그 세그먼트만 생략하고 나머지 줄은 그대로 출력한다.
   `extra_commands`에는 사용자가 network를 쓰는 명령을 넣을 수 있다. 그래서 타임아웃은 반드시 지킬 이 조건의 일부다. 타임아웃은 프로세스 **그룹째** 끊어야 한다. 자식이 stdout을 물려받으면 직접 프로세스만 죽여서는 pipe가 닫히지 않아 그대로 지연이 된다.
2. `usage.json`은 `refresh`만, `state.json`은 `statusline`만 쓴다. writer를 섞지 않는다.
3. credential 파일과 keychain은 읽기만 한다. token refresh 구현 금지.
4. 비공식 API 응답의 모든 필드는 optional로 취급한다. 파싱 실패로 statusline이 비면 안 된다.
5. `guard`는 설정 오류나 데이터 없음에서 차단하지 않는다 (fail-open).
6. 판단 로직은 `internal/core`에 두고 `now time.Time`을 인자로 받아 테스트 가능하게 유지한다.

## 개발

작업 방식(브랜치 정책 · 게이트 · preview · 설치 규칙)은 [AGENTS.md](AGENTS.md)에 있다.

## 실제 Team 계정으로 확인한 것 (2026-09-17)

`cc-usage probe` 1회 + 그 응답을 `CC_USAGE_API_URL`로 다시 넣어 end-to-end 확인.

- **`extra_usage`는 온다.** `is_enabled` · `used_credits` · `monthly_limit` · `utilization` 모두 채워져 온다.
- **`utilization`은 0-100이 맞다.** window(`five_hour`/`seven_day`)와 `extra_usage` 모두 같은 범위다.
- **`used_credits`는 cent가 맞다.** 응답이 `decimal_places: 2`를 같이 주고, `used_credits / monthly_limit`이 `extra_usage.utilization`과 정확히 맞아떨어진다. 즉 `credit_divisor: 100` 기본값이 옳다.
- window의 `limit_dollars` · `used_dollars` · `remaining_dollars`는 전부 `null`로 와서 금액 대신 `utilization`만 쓸 수 있다.
- `resets_at`은 epoch 대신 ISO8601 문자열로 온다 (`ParseInput`이 이미 양쪽을 받는다).

**`context_window`에는 `remaining_percentage`도 온다** (공식 문서 확인). 지금은 `used_percentage`만 쓰지만, ctx를 window처럼 "남은 비율"로 바꿔 표시하고 싶으면 `100 -` 계산 없이 그 필드를 쓰면 된다. `context_window_size`도 온다. 기본 200000이고 확장 모델은 1000000이다. **compaction 임계는 오지 않는다.** 그래서 ctx 색은 "위험"을 말하지 않고 차오르는 정도만 나타낸다.

**Team 계정과 개인 계정은 `organizationUuid`로 갈리지 않는다.** 개인 계정에도 이 필드가 있다(2026-09-18 양쪽 실측). `.claude.json`만으로 계정을 구분하려면 `oauthAccount.emailAddress`뿐이고, 두 계정 모두 채워져 온다. 확실히 구분하는 `organization_type`(`claude_team`)·`seat_tier`(`team_tier_1`)는 **`/api/oauth/profile`에** 있어서 API 호출이 필요하다. cc-usage는 아직 이 엔드포인트를 쓰지 않는다.

**`.claude.json`의 `oauthAccount.hasExtraUsageEnabled`로** 크레딧 활성 여부를 API 없이 알 수 있다. **guard가 쓴다** (`account.ExtraUsageEnabled` → `core.Guard`). `usage.json`의 관측값이 있으면 그 값이 우선하고 없을 때만 이 힌트로 판단한다.

그 전에 이 문단에 적혀 있던 "cache 가 비면 fail-open 으로 통과"는 **틀린 서술이었다.** 실제로는 `uf.Usage == nil`이 `!Enabled` 분기로 들어가지 못해 **차단**했다. `source: "stdin"`은 한도에 닿기 전까지 API를 호출하지 않아 `usage.json`이 평소에 없으므로, 크레딧이 꺼진 계정도 소진 직후 첫 prompt에서 "크레딧이 차감됩니다"로 막혔다. 재현은 임시 `XDG_CONFIG_HOME`/`XDG_CACHE_HOME`에 `five_hour.percent: 100`인 `state.json`을 넣고 `cc-usage guard`를 실행하면 된다.

"필드 없음"과 `false`는 반드시 구분한다(그래서 `*bool`이다). 같게 다루면 이 필드가 없는 설치에서 크레딧이 켜져 있어도 꺼진 것으로 단정해 **막아야 할 때 통과시킨다**.

**Team 계정은 `extra_usage.is_enabled: true`다** (2026-09-18 확인). 개인 계정의 `false`(`out_of_credits`)와 다르다. guard가 실제로 막을 상황은 Team 계정 쪽에 있다.

**`COLUMNS`·`LINES`는 실제로 온다.** 이 맥의 Ghostty에서 `COLUMNS=127 LINES=72`로 확인했다(`extra_commands`로 환경을 출력해 봤다). statusline은 stdout이 pipe라 `tput`·ioctl로는 폭을 읽지 못한다. 대신 Claude Code가 렌더링 직전에 이 둘을 넣어 준다. 공식 문서에도 명시돼 있다. `LINES`는 아직 쓰지 않는다.

아직 쓰지 않는 필드: `decimal_places` · `currency`("USD") · `spend_limit_reached` · `user_disabled` · `disabled_reason`. 앞 둘은 `credit_divisor`/`currency` 설정을 없앨 근거가 되고, `spend_limit_reached`는 guard가 볼 만하다.

## keychain service 이름 (2026-09-19 확인)

기본 `~/.claude`에서는 **접미사 없는 `Claude Code-credentials`다.** `doctor`로 token이 실제로
keychain에서 읽히는 것을 확인했다(`source=keychain`).

접미사가 붙은 항목(`Claude Code-credentials-28907b45` 등)이 이 맥에 **12개 실존한다.**
**다만 접미사가 무엇의 해시인지는 알아내지 못했다.** keychain 메타데이터에 경로 힌트가 없고(`acct`는 사용자명)
CLI가 Mach-O 바이너리라 문자열도 잡히지 않는다. 알아내려면 새 config dir로 한 번 로그인해야 하는데
token이 새로 발급되는 부작용이 있다.

**규칙을 몰라도 막히지 않는다.** `cc-usage doctor`가 실제 keychain을 조회해 후보를 전부 나열하므로,
보고 `keychain_service`에 적으면 된다. 규칙을 추론해 자동으로 고르려 들지 않는다.
틀린 추론으로 다른 계정의 token을 가져오는 것보다 사용자가 보고 고르는 편이 낫다.

### `CLAUDE_CONFIG_DIR`가 설정값보다 우선한다 (고침)

`claude auth status`를 다른 `CLAUDE_CONFIG_DIR`로 실행하면 **`loggedIn: false`가** 나온다. Claude Code는
config dir마다 credential을 따로 둔다. 그래서 cc-usage가 설정 파일의 `config_dir`를 우선하면,
환경변수를 바꾼 세션에서 **다른 계정의 token**으로 API를 호출한다. 숫자가 통째로 남의 것이 되는데
그럴듯해서 티도 안 난다. `ApplyDefaults`가 환경변수를 우선하게 고쳤다.

**그것만으로는 모자랐다.** `keychain_service` 기본값 `Claude Code-credentials`는 접미사가 없어
config dir와 무관하게 같은 값이다. `token.go`는 keychain을 먼저 본다. 그래서 `config_dir`를 옳게 맞춰도
keychain이 기본 계정 token을 먼저 가져와서 creds 파일 폴백까지 가지도 않았다. **비기본 config_dir이면
keychain 기본값을 주지 않는다**(건너뛰고 `<config_dir>/.credentials.json`만 본다). 못 찾으면 숫자가
나오지 않는다. 그래도 틀린 계정의 숫자보다 낫다. 그 dir의 keychain 이름을 아는 사용자가 설정에 적으면
그 이름을 그대로 쓴다.

## Antigravity(`agy`) statusLine 입력 (2026-10-01, agy 1.2.14 실측)

tmux로 agy를 실행해 statusLine에 stdin을 덤프하는 스크립트를 지정해 확인했다. 설정은
`~/.gemini/antigravity-cli/settings.json`의 `statusLine: {type: "command", command}`이다.

- **Claude Code와 같은 꼴이다.** `model.display_name` · `workspace.current_dir` · `cwd` ·
  `context_window.used_percentage`(+`remaining_percentage`·`context_window_size`=1048576) · `session_id` · `transcript_path`.
- **`rate_limits`는 없고 `quota`가 온다.** `gemini-5h` · `gemini-weekly` · `3p-5h` · `3p-weekly`, 각각
  `remaining_fraction`(0-1, **남은** 비율) · `reset_time`(ISO8601) · `reset_in_seconds`. 3p는 이름으로 보아
  Gemini가 아닌 모델(Claude·GPT-OSS) 몫이다. 대응표가 오지 않아 모델 이름으로 고른다.
- `product: "antigravity"`로 호스트를 구분한다. Claude Code payload에는 이 필드가 없다.
- `model.effort`는 **문자열**이다(Claude Code는 최상위 `effort.level` 객체). `display_name`에 이미
  "(High)"가 붙어 와서 쓰지 않는다.
- **`COLUMNS`·`LINES`를 넣어 주지 않는다.** 대신 stdin에 `terminal_width`가 온다. 지금 폭을 쓰는 곳은
  크레딧 줄 배치뿐이고 agy에는 크레딧이 없어 쓰지 않는다. `COLORTERM=truecolor`는 온다(Claude Code에는 오지 않음).
- `plan_tier`("Google AI Pro") · `email` · `agent_state` · `vcs` · `sandbox`도 온다. 쓰지 않는다.

## 미확인 사항 (실제 계정으로 검증 필요)

- 크레딧으로 넘어간 뒤에도 stdin `rate_limits`가 100%로 유지되는지
