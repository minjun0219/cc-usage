# SETUP.md: 새 환경에 cc-usage 적용하기 (에이전트용)

> **새로 적용하지 않는다 (2026-10-06 아카이브).** 같은 기능은 [rocky](https://github.com/minjun0219/rocky)의 `rocky statusline --full`이다. "cc-usage를 적용해 줘"를 받으면 rocky를 설치하고 `rocky.json`의 `statusline` 블록을 설정한다. 아래는 기록으로만 남긴다.

이 문서는 **"cc-usage를 이 머신에 적용해 줘"** 같은 요청을 받은 에이전트가 읽는다.
repo를 고치는 작업이면 [CLAUDE.md](CLAUDE.md)·[AGENTS.md](AGENTS.md)를 본다.

## 소개

- **개인용 도구다.** 주인 한 사람이 자기 머신(회사 맥 · 집 맥)에 깔아 쓰려고 만들었다. 릴리스·패키지·Homebrew 배포가 없고, 소스에서 빌드해 설치하는 것이 유일한 방법이다.
- 하는 일은 둘이다.
  1. **Claude Code `statusLine` command**: 경로·git·모델·ctx·5h/7d 한도·크레딧을 2-3줄로 표시한다.
  2. **`UserPromptSubmit` hook (`guard`)**: 한도가 소진돼 크레딧이 차감되기 시작하면 prompt를 막는다. 기본은 꺼져 있다.
- Go 표준 라이브러리만 쓴 단일 바이너리다. 런타임 의존성(`jq`·`python` 등)이 없다.
- Team 계정의 5h/7d·크레딧은 **비공식** `/api/oauth/usage`에서 읽는다. rate limit이 매우 낮다.
- 동작은 [README.md](README.md)에 자세히 나온다. 바이너리만 있을 때는 `cc-usage --help` · `cc-usage config`를 본다.

## 전제

| | |
| --- | --- |
| OS | macOS가 주 대상(token을 keychain에서 읽는다). Linux는 `<config_dir>/.credentials.json`으로 동작한다 |
| 빌드 도구 | `git`, `make`, Go 1.22 이상 |
| 설치 위치 | `~/.local/bin/cc-usage` (`PREFIX`로 바꿀 수 있다). 이 디렉터리가 `PATH`에 있는지 확인한다 |

## 순서

### 0. 이미 깔려 있는지 먼저 본다

```bash
command -v cc-usage && cc-usage version && cc-usage update --check
```

이미 있으면 새로 clone하지 않는다. `cc-usage update`로 갱신하고 3번(설정 파일)부터 본다.

### 1. 지우지 않을 위치에 clone

```bash
git clone https://github.com/minjun0219/cc-usage.git ~/dev/workspaces/cc-usage
cd ~/dev/workspaces/cc-usage
```

**임시 디렉터리에 clone하지 않는다.** `make install`이 빌드한 경로를 바이너리에 기록하고, `cc-usage update`가 그 경로에서 pull·빌드한다. clone을 지우면 update가 동작하지 않는다. 위 경로는 주인이 쓰는 위치의 예시다. 사용자가 다른 위치를 원하면 거기에 둔다.

### 2. 테스트 → 설치

```bash
make test       # go vet + go test. 실패하면 설치하지 않는다
make install    # → ~/.local/bin/cc-usage
cc-usage version
```

`version`에 `-dirty`가 붙으면 커밋하지 않은 변경이 섞인 것이다. 새 clone이면 붙지 않아야 한다.

### 3. 설정 파일: `~/.config/cc-usage/config.json`

**이미 있으면 덮어쓰지 않는다.** 주인이 이 머신에 맞춰 적어 둔 값이다. `cc-usage doctor`의 `config:` 줄에 읽는 경로가 나온다.

없으면 만든다. 정해야 하는 것은 사실상 `source` 하나다.

| 계정 | `source` |
| --- | --- |
| Team (회사) | `api` |
| Pro / Max (개인) | `stdin` |
| 모르겠다 | 사용자에게 묻는다. 묻기 어려우면 생략(`auto`) |
| 한도 표시가 필요 없다 | `none`: token·API·cache를 아예 보지 않는다 |

```json
{
  "source": "stdin"
}
```

나머지 필드는 전부 생략 가능하고 기본값이 맞다. 전체 목록은 `cc-usage config`로 본다.

> **주의: [`examples/config.json`](examples/config.json)을 그대로 복사하지 않는다.** `badges`의 이메일과 `extra_commands`의 `my-statusline-tool`·`PORT`는 자리표시자다. 그대로 두면 statusline을 렌더링할 때마다 없는 명령을 실행한다.

### 4. Claude Code settings: `~/.claude/settings.json`

**파일을 통째로 바꾸지 않고 병합한다.** 다른 설정·hook이 들어 있다.

```json
{
  "statusLine": {
    "type": "command",
    "command": "~/.local/bin/cc-usage statusline",
    "refreshInterval": 60
  }
}
```

- **이미 `statusLine`이 있으면** 바꾸기 전에 사용자에게 확인한다. 기존 command가 다른 도구의 줄을 출력했다면 그 도구를 래퍼 스크립트로 감싸지 말고 `config.json`의 `extra_commands`로 옮긴다(`cc-usage config` 하단에 형식이 있다). 래퍼로 감싸면 cc-usage의 타임아웃 보호가 사라진다.
- `refreshInterval: 60`은 cache를 다시 읽는 주기일 뿐 API 호출 주기가 아니다.
- `CLAUDE_CONFIG_DIR`로 Claude Code를 따로 실행하는 계정이 있으면 그 디렉터리의 `settings.json`에도 같은 `statusLine` 설정을 넣는다. 그때는 cache도 나눠야 한다. 방법은 README의 [계정 나누기](README.md#계정-나누기)에 있다.

**guard는 사용자가 원할 때만** 켠다. 켜려면 둘 다 필요하다.

1. `config.json`에 `"guard": true`
2. `settings.json`의 `hooks.UserPromptSubmit` 배열에 **항목을 추가**한다(기존 hook을 지우지 않는다)

```json
{ "hooks": [ { "type": "command", "command": "~/.local/bin/cc-usage guard" } ] }
```

guard는 설정 오류·데이터 없음에서 막지 않는다(fail-open). 막혔을 때 사용자가 풀려면 `cc-usage allow 30m`을 실행한다.

### 5. 확인

```bash
cc-usage doctor
```

- `token:` 줄에서 token을 읽었는지 본다. macOS에서 처음이면 keychain 허용 창이 뜰 수 있다. 사용자가 눌러야 한다.
- macOS면 첫 줄에 `keychain 후보` 목록이 나온다. token을 읽지 못했을 때 **후보를 에이전트가 골라 적지 않는다.** 이름 규칙이 밝혀지지 않았다. 틀리게 고르면 다른 계정의 token으로 숫자를 표시한다. 목록을 사용자에게 보여 주고 고르게 한 뒤 `keychain_service`에 적는다.
- `source: stdin`이면 평소에 token을 쓰지 않는다. 한도가 100%에 닿을 때만 크레딧을 조회한다.

렌더링 결과는 API를 호출하지 않는 `--source none`으로 확인한다.

```bash
echo '{"model":{"display_name":"Opus"},"workspace":{"current_dir":"'"$PWD"'"},"context_window":{"used_percentage":12}}' \
  | cc-usage statusline --source none
```

경로·git 줄과 `Opus · ctx 12%` 줄이 나오면 바이너리는 정상이다. 실제 모습은 Claude Code를 새로 실행해 확인한다.

### 6. (선택) Antigravity `agy`

사용자가 agy도 쓰면 같은 명령을 설정한다. agy 안에서 `/statusline cc-usage statusline`을 입력하거나 `~/.gemini/antigravity-cli/settings.json`에 같은 `statusLine` 블록을 넣는다. 자세한 설명은 README의 Antigravity 절에 있다.

## 하지 말 것

- **`cc-usage probe`를 반복하지 않는다.** 비공식 API를 그대로 호출한다. 이 API는 rate limit이 매우 낮다. 필드 확인은 이미 끝났다(CLAUDE.md의 "실제 Team 계정으로 확인한 것").
- **credential을 만들거나 고치지 않는다.** cc-usage는 token을 읽기만 한다. token이 없으면 Claude Code로 로그인하게 안내한다.
- **`statusLine` command를 래퍼 스크립트로 바꾸지 않는다.** 줄을 덧붙이려면 `extra_commands`를 쓴다.
- **기존 `settings.json` · `config.json`을 덮어쓰지 않는다.** 병합하고, 충돌하면 묻는다.
- **설치 김에 repo를 고치지 않는다.** 고칠 것이 보이면 사용자에게 알린다. 고친다면 [AGENTS.md](AGENTS.md)의 규칙(브랜치 → PR, `make test`)을 따른다.

## 막혔을 때

| 증상 | 볼 것 |
| --- | --- |
| statusline이 비어 있다 | `cc-usage doctor`, settings.json의 command 경로 |
| 5h/7d가 나오지 않는다 | `source`가 계정에 맞는지(3번 표), `doctor`의 `token:` 줄 |
| 다른 계정의 숫자처럼 보인다 | `CLAUDE_CONFIG_DIR` · `keychain_service` · `XDG_CACHE_HOME`이 같은 계정을 가리키는지 |
| `extra_commands` 줄이 붙지 않는다 | `cc-usage doctor`의 항목별 결과 |
| `cc-usage update`가 거부한다 | 출력 이유 그대로다(소스 경로 없음 · 커밋하지 않은 변경 · 갈라짐 · 테스트 실패). 억지로 넘기지 않는다 |
