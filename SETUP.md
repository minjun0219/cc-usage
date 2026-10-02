# SETUP.md — 새 환경에 cc-usage 적용하기 (에이전트용)

이 문서는 **"cc-usage 를 이 머신에 적용해 줘"** 같은 요청을 받은 에이전트를 위한 것이다.
repo 를 고치는 작업이면 이 문서가 아니라 [CLAUDE.md](CLAUDE.md)·[AGENTS.md](AGENTS.md) 를 본다.

## 이게 뭔가

- **개인용 도구다.** 주인 한 사람이 자기 머신(회사 맥 · 집 맥)에 깔아 쓰려고 만들었다. 릴리스·패키지·Homebrew 배포가 없고, **소스에서 빌드해 설치하는 것이 유일한 방법**이다.
- 하는 일은 둘이다.
  1. **Claude Code `statusLine` command** — 경로·git·모델·ctx·5h/7d 한도·크레딧을 2-3줄로 그린다.
  2. **`UserPromptSubmit` hook (`guard`)** — 한도가 소진돼 크레딧이 깎이기 시작하면 prompt 를 막는다. 기본은 꺼져 있다.
- Go 표준 라이브러리만 쓴 단일 바이너리다. 런타임 의존성(`jq`·`python` 등)이 없다.
- Team 계정의 5h/7d·크레딧은 **비공식** `/api/oauth/usage` 를 읽는다. rate limit 이 매우 낮다.
- 동작의 자세한 설명은 [README.md](README.md), 바이너리만 있을 때는 `cc-usage --help` · `cc-usage config`.

## 전제

| | |
| --- | --- |
| OS | macOS 가 주 대상(token 을 keychain 에서 읽는다). Linux 는 `<config_dir>/.credentials.json` 으로 동작한다 |
| 빌드 도구 | `git`, `make`, Go 1.22 이상 |
| 설치 위치 | `~/.local/bin/cc-usage` (`PREFIX` 로 바꿀 수 있다). 이 디렉터리가 `PATH` 에 있는지 확인한다 |

## 순서

### 0. 이미 깔려 있는지 먼저 본다

```bash
command -v cc-usage && cc-usage version && cc-usage update --check
```

이미 있으면 새로 clone 하지 않는다. `cc-usage update` 로 갱신하고 3번(설정 확인)부터 본다.

### 1. clone — 지울 일 없는 자리에

```bash
git clone https://github.com/minjun0219/cc-usage.git ~/dev/workspaces/cc-usage
cd ~/dev/workspaces/cc-usage
```

**임시 디렉터리에 clone 하지 않는다.** `make install` 이 빌드한 경로를 바이너리에 박고, `cc-usage update` 가 그 경로에서 pull·빌드한다. clone 을 지우면 update 가 끊긴다. 위 경로는 주인이 쓰는 자리의 예시다 — 사용자가 다른 자리를 원하면 거기에 둔다.

### 2. 테스트 → 설치

```bash
make test       # go vet + go test. 실패하면 설치하지 않는다
make install    # → ~/.local/bin/cc-usage
cc-usage version
```

`version` 에 `-dirty` 가 붙으면 커밋하지 않은 변경이 섞인 것이다. 새 clone 이면 붙지 않아야 한다.

### 3. 설정 파일 — `~/.config/cc-usage/config.json`

**이미 있으면 덮어쓰지 않는다.** 주인이 이 머신에 맞춰 적어 둔 값이다. `cc-usage doctor` 첫 줄에 읽는 경로가 나온다.

없으면 만든다. 정해야 하는 것은 사실상 `source` 하나다.

| 계정 | `source` |
| --- | --- |
| Team (회사) | `api` |
| Pro / Max (개인) | `stdin` |
| 모르겠다 | 사용자에게 묻는다. 묻기 어려우면 생략(`auto`) |
| 한도 표시가 필요 없다 | `none` — token·API·cache 를 아예 보지 않는다 |

```json
{
  "source": "stdin"
}
```

나머지 필드는 전부 생략 가능하고 기본값이 맞다. 전체 목록은 `cc-usage config`.

> ⚠️ **[`examples/config.json`](examples/config.json) 을 그대로 복사하지 않는다.** `badges` 의 이메일과 `extra_commands` 의 `my-statusline-tool`·`PORT` 는 자리표시자다. 그대로 두면 없는 명령을 매 렌더마다 부른다.

### 4. Claude Code settings — `~/.claude/settings.json`

**병합한다. 파일을 통째로 바꾸지 않는다.** 다른 설정·hook 이 들어 있다.

```json
{
  "statusLine": {
    "type": "command",
    "command": "~/.local/bin/cc-usage statusline",
    "refreshInterval": 60
  }
}
```

- **이미 `statusLine` 이 있으면** 바꾸기 전에 사용자에게 확인한다. 기존 command 가 다른 도구의 줄을 그리고 있었다면, 그 도구를 **래퍼 스크립트로 감싸지 말고** `config.json` 의 `extra_commands` 로 옮긴다(`cc-usage config` 하단에 형식이 있다). 래퍼로 감싸면 cc-usage 의 타임아웃 보호가 사라진다.
- `refreshInterval: 60` 은 cache 를 다시 읽는 주기일 뿐 API 호출 주기가 아니다.
- `CLAUDE_CONFIG_DIR` 로 Claude Code 를 따로 돌리는 계정이 있으면 그 디렉터리의 `settings.json` 에도 같은 것을 넣는다. 그때는 cache 도 갈라야 한다 — README 의 [계정 나누기](README.md#계정-나누기).

**guard 는 사용자가 원할 때만** 켠다. 켜려면 둘 다 필요하다.

1. `config.json` 에 `"guard": true`
2. `settings.json` 의 `hooks.UserPromptSubmit` 배열에 **항목을 추가**한다(기존 hook 을 지우지 않는다)

```json
{ "hooks": [ { "type": "command", "command": "~/.local/bin/cc-usage guard" } ] }
```

guard 는 설정 오류·데이터 없음에서 막지 않는다(fail-open). 막혔을 때 사용자가 풀려면 `cc-usage allow 30m`.

### 5. 확인

```bash
cc-usage doctor
```

- `token:` 줄이 읽혔는지 본다. macOS 에서 처음이면 keychain 허용 창이 뜰 수 있다 — 사용자가 눌러야 한다.
- macOS 면 첫 줄에 `keychain 후보` 목록이 나온다. token 을 못 읽었을 때 **후보를 에이전트가 골라 적지 않는다.** 이름 규칙이 밝혀지지 않았고, 틀리게 고르면 **다른 계정의 token** 으로 숫자를 그린다. 목록을 사용자에게 보여 주고 고르게 한 뒤 `keychain_service` 에 적는다.
- `source: stdin` 이면 평소에 token 을 쓰지 않는다. 한도가 100% 에 닿을 때만 크레딧을 조회한다.

렌더는 API 를 건드리지 않는 `--source none` 으로 본다.

```bash
echo '{"model":{"display_name":"Opus"},"workspace":{"current_dir":"'"$PWD"'"},"context_window":{"used_percentage":12}}' \
  | cc-usage statusline --source none
```

경로·git 줄과 `Opus · ctx 12%` 줄이 나오면 바이너리는 정상이다. 실제 모습은 Claude Code 를 새로 띄워 확인한다.

### 6. (선택) Antigravity `agy`

사용자가 agy 도 쓰면 같은 명령을 건다. agy 안에서 `/statusline cc-usage statusline`, 또는 `~/.gemini/antigravity-cli/settings.json` 에 같은 `statusLine` 블록. 자세한 것은 README 의 Antigravity 절.

## 하지 말 것

- **`cc-usage probe` 를 반복하지 않는다.** 비공식 API 를 그대로 부르고, rate limit 이 매우 낮다. 필드 확인은 이미 끝났다(CLAUDE.md 의 "실제 Team 계정으로 확인한 것").
- **credential 을 만들거나 고치지 않는다.** cc-usage 는 token 을 읽기만 한다. token 이 없으면 Claude Code 로 로그인하게 안내한다.
- **`statusLine` command 를 래퍼 스크립트로 바꾸지 않는다.** 줄을 덧붙이는 자리는 `extra_commands` 다.
- **기존 `settings.json` · `config.json` 을 덮어쓰지 않는다.** 병합하고, 충돌하면 묻는다.
- **설치 김에 repo 를 고치지 않는다.** 고칠 것이 보이면 사용자에게 알린다. 고친다면 [AGENTS.md](AGENTS.md) 의 규칙(브랜치 → PR, `make test`)을 따른다.

## 막혔을 때

| 증상 | 볼 것 |
| --- | --- |
| statusline 이 비어 있다 | `cc-usage doctor`, settings.json 의 command 경로 |
| 5h/7d 가 안 나온다 | `source` 가 계정에 맞는지(3번 표), `doctor` 의 `token:` 줄 |
| 다른 계정의 숫자처럼 보인다 | `CLAUDE_CONFIG_DIR` · `keychain_service` · `XDG_CACHE_HOME` 이 같은 계정을 가리키는지 |
| `extra_commands` 줄이 안 붙는다 | `cc-usage doctor` 의 항목별 결과 |
| `cc-usage update` 가 거부한다 | 출력 이유 그대로다(소스 경로 없음 · 커밋 안 한 변경 · 갈라짐 · 테스트 실패). 억지로 넘기지 않는다 |
