# AGENTS.md — cc-usage 작업 방식

프로젝트 구조와 불변 조건은 [CLAUDE.md](CLAUDE.md)에 있다. 여기는 **어떻게 일하는가**만 적는다.

## 판단 기준

갈리면 이 순서로 본다: **의존성 최소화 → 성능 → 둘이 부딪히면 측정.**

### 의존성 최소화

`CLAUDE.md`의 "외부 의존성 추가 금지"를 **런타임까지** 같은 기준으로 본다. `jq`·`python` 같은 도구를 statusline 경로에서 부르지 않는다. 그 도구가 없는 환경에서 줄이 비면 안 되고, 프로세스 하나가 그대로 지연이다. JSON은 `encoding/json`으로 in-process에서 읽는다.

예외는 `extra_commands`와 `notify`다. 거기서 부르는 명령은 사용자가 고른 것이고, 없으면 그 세그먼트만 조용히 빠지게 되어 있다. 그래서 **cc-usage 코드는 그 명령들이 무엇인지 몰라야 한다** — 특정 도구를 아는 지식이 들어오는 순간 그 repo의 변경이 이 repo를 깬다.

### 성능

`statusline`은 초당 두 번 돈다. 비용 단위는 알고리즘이 아니라 **프로세스 개수와 기동 시간**이다. 같은 정보를 두 번 물어보지 말고 한 호출로 합친다 (`git status --porcelain=v2`가 브랜치·업스트림·ahead/behind·변경 수를 한 번에 주는 것처럼).

외부 호출을 하나 늘리는 것은 대략 10ms를 늘리는 일이다. 늘릴 값이 있는지 먼저 따진다.

### 측정

성능 이야기는 **재기 전에 하지 않는다.** 추정으로 정한 것은 추정으로 뒤집힌다. 눈에 보이는 변화(표기·색)는 `make preview`, 시간은 직접 잰다.

아래는 이 맥에서 실측한 기준선이다. 절대 목표가 아니라 **비교용**이다 — 새 값이 이보다 크게 벗어나면 이유를 찾는다.

| | |
|---|---|
| 빈 프로세스 (`/usr/bin/true`) | 2.7 ms |
| 빈 Go 바이너리 | 3.5 ms |
| `echo \| jq` 1회 | 4.2 ms |
| `git status --porcelain=v2 -uno` | 7.2 ms |
| cc-usage 코어 (extra 없음) | 14.4 ms |
| cc-usage 전체 (extra 2개) | 23.1 ms |
| statusline 호출 빈도 | 초당 약 2회 |

`-unormal`(untracked 포함)은 3천 파일 repo에서 `-uno`보다 +5.5 ms다. 그래서 지금은 untracked를 세지 않는다.

**전체 렌더가 50 ms를 넘기 시작하면** extra 출력을 cache에 적재하고 statusline은 cache만 읽는 층을 넣을 때다. 그전에는 라이브 반응성이 더 값이 있다.

## 브랜치: `main` 직접

기능 브랜치·PR·워크트리를 쓰지 않는다. 커밋도 푸시도 `main`에 바로 한다.

리뷰받을 상대가 없고, 배포 대상이 없으며(로컬 바이너리 하나), 깨져도 statusline은 fail-open이라 PR 층이 값을 내지 못한다.

공개로 돌리거나 배포 대상이 생기면 그때 다시 정한다. 그전에는 재제안하지 않는다 — 이 항목을 적어 두는 이유가 그것이다.

## 게이트: `make test`

PR이 없으므로 이것이 **유일한 층**이다. 권장이 아니라 필수다.

```bash
make test     # go vet ./... + go test ./...
```

통과하기 전에는 커밋하지 않는다. "통과했다"는 실제로 돌려 본 뒤에만 쓴다.

## 눈으로 봐야 하는 변경: `make preview`

표기·색·정렬은 테스트로 잡히지 않고, 여기에는 PR 프리뷰도 없다.

```bash
make preview
```

대표 상태(평소 / 경보 임박 / 소진+크레딧 / git repo 밖 / 빈 payload)를 실제 빌드 산출물에 물려 렌더한다. 실제 cache와 token은 건드리지 않는다 — 임시 `XDG_CACHE_HOME`을 쓰고 조회 불가능한 credential을 가리켜 refresh가 network에 닿지 못하게 한다.

상태를 더하거나 고칠 일이 생기면 `examples/preview.sh`에 넣는다. 매번 손으로 JSON을 만들어야 하면 결국 안 보게 된다.

## 커밋하지 않은 트리를 설치하지 않는다

`make install` 전에 커밋한다. 돌고 있는 바이너리가 하는 일의 소스가 어디에도 없는 상태를 만들지 않기 위해서다.

```bash
cc-usage version
```

여기 `-dirty`가 보이면 규칙을 어긴 것이다. `Makefile`의 `git describe --dirty`가 이미 그 신호를 낸다.

## API 없이 end-to-end 확인

- `CC_USAGE_API_URL`로 가짜 서버 지정 (테스트 전용)
- profile에 `token_env`를 지정해 keychain 우회
- `HOME` / `XDG_CACHE_HOME`을 임시 디렉터리로 지정
