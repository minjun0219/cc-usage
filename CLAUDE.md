# cc-usage — Claude Code 작업 가이드

Claude Code statusline + 크레딧 guard. Go 표준 라이브러리만 사용 (외부 의존성 추가 금지).

## 구조

```
cmd/cc-usage/          subcommand 진입점 (statusline, guard, allow, refresh, probe, doctor)
internal/config/    profile 로딩, 기본값, profile 선택
internal/store/     cache 파일 모델, atomic write, flock
internal/auth/      token 읽기 (read-only), keychain 후보 목록
internal/api/       비공식 /api/oauth/usage 호출 및 파싱
internal/core/      순수 판단 로직 (stdin 파싱, 모드 선택, refresh 필요 여부, 크레딧 baseline, guard)
internal/render/    statusline 문자열 생성
examples/           config, settings.json 예시
```

## 불변 조건 (반드시 지킬 것)

1. `statusline`은 **network를 기다리지 않는다.** cc-usage 자신은 이 경로에서 API를 호출하지 않고, 갱신이 필요하면 `refresh`를 detached로 띄운 뒤 즉시 끝낸다. 알림(`notify`)도 detached다.
   로컬 subprocess(`git status`, `extra_commands`)는 **반드시 타임아웃을 걸고** 부른다. 실패·타임아웃은 그 세그먼트만 생략하고 나머지 줄은 그대로 낸다.
   `extra_commands`에는 사용자가 network를 타는 명령을 넣을 수 있다 — 그래서 타임아웃은 권장이 아니라 이 조건의 일부다. 타임아웃은 프로세스 **그룹째** 끊어야 한다. 자식이 stdout을 물려받으면 직접 프로세스만 죽여서는 pipe가 닫히지 않아 그대로 지연이 된다.
2. `usage.json`은 `refresh`만, `state.json`은 `statusline`만 쓴다. writer를 섞지 않는다.
3. credential 파일과 keychain은 읽기만 한다. token refresh 구현 금지.
4. 비공식 API 응답의 모든 필드는 optional로 취급한다. 파싱 실패로 statusline이 비면 안 된다.
5. `guard`는 설정 오류나 데이터 없음에서 차단하지 않는다 (fail-open).
6. 판단 로직은 `internal/core`에 두고 `now time.Time`을 인자로 받아 테스트 가능하게 유지한다.

## 개발

작업 방식(브랜치 정책 · 게이트 · preview · 설치 규칙)은 [AGENTS.md](AGENTS.md)에 있다.

## 미확인 사항 (실제 계정으로 검증 필요)

- Team plan 계정에서 `/api/oauth/usage`가 `extra_usage`를 반환하는지
- `utilization` 범위(0-100 가정)와 `used_credits` 단위(cent 가정)
- `CLAUDE_CONFIG_DIR` 사용 시 keychain service 이름 규칙 (`cc-usage doctor`로 확인)
- 크레딧으로 넘어간 뒤에도 stdin `rate_limits`가 100%로 유지되는지
