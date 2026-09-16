# AGENTS.md — cc-usage 작업 방식

프로젝트 구조와 불변 조건은 [CLAUDE.md](CLAUDE.md)에 있다. 여기는 **어떻게 일하는가**만 적는다.

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
