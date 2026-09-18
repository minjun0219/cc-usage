#!/bin/sh
# make preview — 대표 상태들을 실제 바이너리로 렌더해 눈으로 확인한다.
#
# 표기·색·정렬 변경은 테스트로 잡히지 않는다. 손으로 JSON을 만들어야 하면
# 결국 안 보게 되므로 여기 고정해 둔다.
#
# 사용자의 실제 cache/token은 건드리지 않는다: 임시 XDG_CACHE_HOME을 쓰고,
# 조회가 불가능한 keychain/credentials를 가리켜 refresh가 network에 닿지
# 못하게 한다.
set -e
BIN=${1:-./bin/cc-usage}
DIR=$(mktemp -d)
trap 'rm -rf "$DIR"' EXIT
export XDG_CACHE_HOME="$DIR/cache" CC_USAGE_CONFIG="$DIR/config.json"

# 배지는 로그인된 계정(.claude.json)으로 정해지므로 가짜 계정 파일을 심는다.
# HOME 을 임시 디렉터리로 돌려서 실제 계정을 건드리지 않는다.
mkdir -p "$DIR/home"
cat > "$DIR/home/.claude.json" <<'JSON'
{"oauthAccount":{"emailAddress":"preview@example.com"}}
JSON
export HOME="$DIR/home"

cat > "$DIR/config.json" <<JSON
{"source":"stdin", "alert_percent":90,
 "keychain_service":"cc-usage-preview-absent",
 "credentials_file":"$DIR/absent.json",
 "badges":{"preview@example.com":{"emoji":"🏢"}}}
JSON

# 배지 없는 설정 — 같은 상태를 배지 유무로 비교하려고 따로 둔다.
# 위 설정에서 badges 만 뺀 것을 그대로 적는다. 파생시키지 않는 이유는 의존성이다
# — Go 와 make 만 있는 머신에서 make preview 가 돌아야 한다.
cat > "$DIR/config-nobadge.json" <<JSON
{"source":"stdin", "alert_percent":90,
 "keychain_service":"cc-usage-preview-absent",
 "credentials_file":"$DIR/absent.json"}
JSON

NOW=$(date +%s)
mkdir -p "$XDG_CACHE_HOME/cc-usage"
cat > "$XDG_CACHE_HOME/cc-usage/usage.json" <<JSON
{"usage":{"fetched_at":"$(date -u -r "$NOW" +%Y-%m-%dT%H:%M:%SZ)",
  "extra":{"enabled":true,"used_credits":1160,"monthly_limit":5000}},
 "baseline":{"window_key":"5h","credits":1080,"at":"$(date -u -r "$NOW" +%Y-%m-%dT%H:%M:%SZ)"}}
JSON

show() {
	printf '\n\033[1m── %s\033[0m\n' "$1"
	shift
	printf '%s' "$1" | "$BIN" statusline
}

CWD=$(pwd)
R5=$((NOW + 4800))
R7=$((NOW + 187000))

show "평소" \
 "{\"session_id\":\"s\",\"model\":{\"display_name\":\"Opus 5\"},\"context_window\":{\"used_percentage\":41},\"workspace\":{\"current_dir\":\"$CWD\"},\"rate_limits\":{\"five_hour\":{\"used_percentage\":30,\"resets_at\":$R5},\"seven_day\":{\"used_percentage\":15,\"resets_at\":$R7}}}"

show "ctx·5h 높음 (경보 전)" \
 "{\"session_id\":\"s\",\"model\":{\"display_name\":\"Opus 5\"},\"context_window\":{\"used_percentage\":88},\"workspace\":{\"current_dir\":\"$CWD\"},\"rate_limits\":{\"five_hour\":{\"used_percentage\":72,\"resets_at\":$R5},\"seven_day\":{\"used_percentage\":55,\"resets_at\":$R7}}}"

show "임박 93% (경보 — 6초간 깜빡임)" \
 "{\"session_id\":\"s\",\"model\":{\"display_name\":\"Opus 5\"},\"workspace\":{\"current_dir\":\"$CWD\"},\"rate_limits\":{\"five_hour\":{\"used_percentage\":93,\"resets_at\":$R5}}}"

show "소진 100% + 크레딧 소진 중" \
 "{\"session_id\":\"s\",\"model\":{\"display_name\":\"Opus 5\"},\"workspace\":{\"current_dir\":\"$CWD\"},\"rate_limits\":{\"five_hour\":{\"used_percentage\":100,\"resets_at\":$R5},\"seven_day\":{\"used_percentage\":80,\"resets_at\":$R7}}}"

show "git repo 밖" \
 "{\"session_id\":\"s\",\"model\":{\"display_name\":\"Opus 5\"},\"workspace\":{\"current_dir\":\"/tmp\"},\"rate_limits\":{\"five_hour\":{\"used_percentage\":30,\"resets_at\":$R5}}}"

show "빈 payload — 직전 stdin 값을 cache에서 그대로 쓴다" ""

# ── 배지 ──────────────────────────────────────────────────────────
# 배지는 붙는 자리와 줄바꿈 판단을 바꾼다. 테스트로는 폭만 잡히고 색·간격은
# 눈으로 봐야 하므로, 경계 상태를 여기 고정해 둔다.
printf '\n\033[1m── 배지 없음 (같은 상태 비교용)\033[0m\n'
CC_USAGE_CONFIG="$DIR/config-nobadge.json" sh -c "printf '%s' '{\"session_id\":\"s\",\"model\":{\"display_name\":\"Opus 5\"},\"context_window\":{\"used_percentage\":41},\"workspace\":{\"current_dir\":\"$CWD\"},\"rate_limits\":{\"five_hour\":{\"used_percentage\":30,\"resets_at\":$R5}}}' | $BIN statusline"

show "배지 + 평소" \
 "{\"session_id\":\"s\",\"model\":{\"display_name\":\"Opus 5\"},\"context_window\":{\"used_percentage\":41},\"workspace\":{\"current_dir\":\"$CWD\"},\"rate_limits\":{\"five_hour\":{\"used_percentage\":30,\"resets_at\":$R5}}}"

show "배지 + 경보 (배지와 빨간 배지가 한 줄에)" \
 "{\"session_id\":\"s\",\"model\":{\"display_name\":\"Opus 5\"},\"workspace\":{\"current_dir\":\"$CWD\"},\"rate_limits\":{\"five_hour\":{\"used_percentage\":93,\"resets_at\":$R5}}}"

# 직전 케이스가 남긴 한도 cache 를 지워야 진짜로 "아무것도 없는" 렌더가 된다.
rm -f "$XDG_CACHE_HOME/cc-usage/state.json" "$XDG_CACHE_HOME/cc-usage/usage.json"
show "배지만 (stdin 도 cache 도 빈 렌더)" \
 "{\"session_id\":\"s\"}"

# Team(api) 모드 — 한도 전에도 크레딧 줄이 뜬다. stdin 이 아니라 cache 의 usage 에서
# 한도가 오는 상황이라, rate_limits 없는 payload 와 창이 담긴 usage.json 으로 만든다.
API_DIR=$(mktemp -d)
export XDG_CACHE_HOME="$API_DIR/cache" CC_USAGE_CONFIG="$API_DIR/config.json"
cat > "$API_DIR/config.json" <<JSON
{"source":"api", "alert_percent":90,
 "keychain_service":"cc-usage-preview-absent",
 "credentials_file":"$API_DIR/absent.json"}
JSON
mkdir -p "$XDG_CACHE_HOME/cc-usage"
cat > "$XDG_CACHE_HOME/cc-usage/usage.json" <<JSON
{"usage":{"fetched_at":"$(date -u -r "$NOW" +%Y-%m-%dT%H:%M:%SZ)",
  "five_hour":{"percent":35,"resets_at":"$(date -u -r "$R5" +%Y-%m-%dT%H:%M:%SZ)"},
  "seven_day":{"percent":60,"resets_at":"$(date -u -r "$R7" +%Y-%m-%dT%H:%M:%SZ)"},
  "extra":{"enabled":true,"used_credits":1160,"monthly_limit":5000}}}
JSON

show "Team(api) — 한도 전에도 크레딧" \
 "{\"session_id\":\"s\",\"model\":{\"display_name\":\"Opus 5\"},\"context_window\":{\"used_percentage\":41},\"workspace\":{\"current_dir\":\"$CWD\"}}"
rm -rf "$API_DIR"
printf '\n'
