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

cat > "$DIR/config.json" <<JSON
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
printf '\n'
