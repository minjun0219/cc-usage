# cc-usage — Claude Code 작업 가이드

Claude Code statusline + 크레딧 guard. Go 표준 라이브러리만 사용 (외부 의존성 추가 금지).

## 구조

```
cmd/cc-usage/          subcommand 진입점 (statusline, guard, allow, refresh, probe, doctor)
internal/config/    설정 로딩, 기본값
internal/store/     cache 파일 모델, atomic write, flock
internal/auth/      token 읽기 (read-only), keychain 후보 목록
internal/api/       비공식 /api/oauth/usage 호출 및 파싱
internal/core/      순수 판단 로직 (stdin 파싱, 모드 선택, refresh 필요 여부, 크레딧 baseline, guard)
internal/render/    statusline 문자열 생성
examples/           config, settings.json 예시
```

## 불변 조건 (반드시 지킬 것)

1. `statusline`은 **network를 기다리지 않는다.** cc-usage 자신은 이 경로에서 API를 호출하지 않고, 갱신이 필요하면 `refresh`를 detached로 띄운 뒤 즉시 끝낸다.
   로컬 subprocess(`git status`, `extra_commands`)는 **반드시 타임아웃을 걸고** 부른다. 실패·타임아웃은 그 세그먼트만 생략하고 나머지 줄은 그대로 낸다.
   `extra_commands`에는 사용자가 network를 타는 명령을 넣을 수 있다 — 그래서 타임아웃은 권장이 아니라 이 조건의 일부다. 타임아웃은 프로세스 **그룹째** 끊어야 한다. 자식이 stdout을 물려받으면 직접 프로세스만 죽여서는 pipe가 닫히지 않아 그대로 지연이 된다.
2. `usage.json`은 `refresh`만, `state.json`은 `statusline`만 쓴다. writer를 섞지 않는다.
3. credential 파일과 keychain은 읽기만 한다. token refresh 구현 금지.
4. 비공식 API 응답의 모든 필드는 optional로 취급한다. 파싱 실패로 statusline이 비면 안 된다.
5. `guard`는 설정 오류나 데이터 없음에서 차단하지 않는다 (fail-open).
6. 판단 로직은 `internal/core`에 두고 `now time.Time`을 인자로 받아 테스트 가능하게 유지한다.

## 개발

작업 방식(브랜치 정책 · 게이트 · preview · 설치 규칙)은 [AGENTS.md](AGENTS.md)에 있다.

## 실제 Team 계정으로 확인한 것 (2026-09-17)

`cc-usage probe` 1회 + 그 응답을 `CC_USAGE_API_URL`로 되먹여 end-to-end 확인.

- **`extra_usage`는 온다.** `is_enabled` · `used_credits` · `monthly_limit` · `utilization` 모두 채워져 온다.
- **`utilization`은 0-100이 맞다.** 창(`five_hour`/`seven_day`)과 `extra_usage` 모두 같은 범위다.
- **`used_credits`는 cent가 맞다.** 응답이 `decimal_places: 2`를 같이 주고, `used_credits / monthly_limit`이 `extra_usage.utilization`과 정확히 맞아떨어진다. 즉 `credit_divisor: 100` 기본값이 옳다.
- 창의 `limit_dollars` · `used_dollars` · `remaining_dollars`는 전부 `null`로 온다 — 금액이 아니라 `utilization`만 쓸 수 있다.
- `resets_at`은 epoch가 아니라 ISO8601 문자열이다 (`ParseInput`이 이미 양쪽을 받는다).

**`context_window`에는 `remaining_percentage`도 온다** (공식 문서 확인). 지금은 `used_percentage`만 쓰지만, ctx를 한도 창처럼 "남은 비율"로 뒤집고 싶어지면 `100 -` 계산 없이 그 필드를 쓰면 된다. `context_window_size`도 온다 — 기본 200000, 확장 모델은 1000000. **compaction 임계는 오지 않는다** — ctx 색이 "위험"을 말하지 않고 차오르는 정도만 나타내는 이유다.

**Team 계정과 개인 계정은 `organizationUuid` 로 갈리지 않는다** — 개인 계정도 이 필드를 갖는다(2026-09-18 양쪽 실측). `.claude.json` 만으로 계정을 가리려면 `oauthAccount.emailAddress` 뿐이고, 두 계정 모두 채워져 온다. 확실히 가르는 `organization_type`(`claude_team`)·`seat_tier`(`team_tier_1`)는 **`/api/oauth/profile`** 에 있는데 그건 API 호출이 필요하다 — cc-usage 는 아직 이 엔드포인트를 쓰지 않는다.

**`.claude.json` 의 `oauthAccount.hasExtraUsageEnabled`** 로 크레딧 활성 여부를 API 없이 알 수 있다. 지금 guard 는 cache 가 비면 fail-open 으로 통과시키는데, 이걸 쓰면 그 구간을 줄일 수 있다. 아직 쓰지 않는다.

**Team 계정은 `extra_usage.is_enabled: true` 다** (2026-09-18 확인). 개인 계정의 `false`(`out_of_credits`)와 다르다 — guard 가 실제로 막을 상황은 Team 계정 쪽에 있다.

**`COLUMNS`·`LINES`는 실제로 온다.** 이 맥의 Ghostty 에서 `COLUMNS=127 LINES=72` 로 확인했다(`extra_commands` 로 환경을 찍어 봤다). statusline 은 stdout 이 파이프라 `tput`·ioctl 로는 폭을 못 읽지만, Claude Code 가 렌더 직전에 이 둘을 넣어 준다 — 공식 문서에도 명시돼 있다. `LINES` 는 아직 쓰지 않는다.

아직 쓰지 않는 필드: `decimal_places` · `currency`("USD") · `spend_limit_reached` · `user_disabled` · `disabled_reason`. 앞 둘은 `credit_divisor`/`currency` 설정을 없앨 근거가 되고, `spend_limit_reached`는 guard가 볼 만하다.

## keychain service 이름 (2026-09-19 확인)

기본 `~/.claude` 에서는 **접미사 없는 `Claude Code-credentials`** 다. `doctor` 로 token 이 실제로
keychain 에서 읽히는 것을 확인했다(`source=keychain`).

접미사가 붙은 항목(`Claude Code-credentials-28907b45` 등)이 이 맥에 **12개 실존한다.** Claude Code 가
상황에 따라 접미사를 붙이는 것은 사실이다. **다만 접미사가 무엇의 해시인지는 알아내지 못했다** —
keychain 메타데이터에 경로 힌트가 없고(`acct` 는 사용자명), CLI 가 Mach-O 바이너리라 문자열도 잡히지
않는다. 알아내려면 새 config dir 로 로그인을 한 번 태워야 하는데 토큰이 새로 발급되는 부작용이 있다.

**규칙을 몰라도 막히지 않는다.** `cc-usage doctor` 가 실제 keychain 을 긁어 후보를 전부 나열하므로,
보고 `keychain_service` 에 적으면 된다. 규칙을 추론해 자동으로 고르려 들지 않는 이유가 이것이다 —
틀린 추론으로 다른 계정의 token 을 집는 것보다 사용자가 보고 고르는 편이 낫다.

**`config_dir` 은 `CLAUDE_CONFIG_DIR` 를 따르지 않는다.** 설정 파일의 값만 본다. 반면
`internal/account` 는 그 환경변수를 배타적으로 본다 — **배지는 환경변수를 따라가고 token·creds 경로는
따라가지 않는다.** 환경변수만 바꾸면 배지는 회사 계정인데 token 은 개인 계정 keychain 에서 읽는다.
README 의 "계정 나누기" 가 설정을 함께 나누라고 안내해서 실사용에서는 드러나지 않지만, 두 축이 서로
다른 것을 보고 있다는 사실은 그대로다. 아직 맞추지 않았다.

## 미확인 사항 (실제 계정으로 검증 필요)

- 크레딧으로 넘어간 뒤에도 stdin `rate_limits`가 100%로 유지되는지
