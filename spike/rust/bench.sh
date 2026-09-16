#!/bin/sh
# Go 핫패스와 Rust 스파이크를 같은 조건에서 비교한다.
#
#   sh spike/rust/bench.sh
#
# 먼저 출력이 바이트 단위로 같은지 확인하고, 다르면 멈춘다 — 다른 일을 하는
# 두 프로그램의 시간을 비교하는 것은 의미가 없다.
set -e
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"
go build -o bin/cc-usage ./cmd/cc-usage
(cd spike/rust && cargo build --release -q)
GO=./bin/cc-usage
RS=./spike/rust/target/release/hotpath

T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
cat > "$T/config.json" <<'JSON'
{"default_profile":"p","profiles":{"p":{"label":"","source":"stdin","alert_percent":90,
 "keychain_service":"absent","credentials_file":"/nonexistent"}}}
JSON
mkdir -p "$T/cache/cc-usage/p"
cat > "$T/cache/cc-usage/p/usage.json" <<JSON
{"usage":{"fetched_at":"$(date -u +%Y-%m-%dT%H:%M:%SZ)","extra":{"enabled":true,"used_credits":1160,"monthly_limit":5000}}}
JSON
export CC_USAGE_CONFIG="$T/config.json" XDG_CACHE_HOME="$T/cache"

# 분 경계에서 떨어뜨린다. 4800(=80분 정각)으로 잡으면 두 실행이 1초만 어긋나도
# 1h20m ↔ 1h19m 으로 갈려 동일성 검사가 헛돈다.
R=$(( $(date +%s) + 4830 )); R7=$(( $(date +%s) + 187000 ))
P="{\"session_id\":\"cmp\",\"model\":{\"display_name\":\"Opus 5\"},\"context_window\":{\"used_percentage\":41},\"rate_limits\":{\"five_hour\":{\"used_percentage\":72,\"resets_at\":$R},\"seven_day\":{\"used_percentage\":33,\"resets_at\":$R7}}}"

printf '%s' "$P" | $GO statusline > "$T/go.out"
printf '%s' "$P" | $RS            > "$T/rs.out"
if cmp -s "$T/go.out" "$T/rs.out"; then
	echo "출력 동일 ✓"
else
	echo "출력이 다르다 — 비교 중단"; diff "$T/go.out" "$T/rs.out" || true; exit 1
fi

bench() {
	label="$1"; shift
	i=0; while [ $i -lt 5 ]; do printf '%s' "$P" | "$@" >/dev/null 2>&1; i=$((i+1)); done
	s=$(python3 -c 'import time;print(time.time())')
	i=0; while [ $i -lt 60 ]; do printf '%s' "$P" | "$@" >/dev/null 2>&1; i=$((i+1)); done
	e=$(python3 -c 'import time;print(time.time())')
	python3 -c "print(f'  {'$label':<8} {($e-$s)/60*1000:6.2f} ms')"
}
echo "핫패스 (60회 평균, 2회전)"
bench Go $GO statusline; bench Rust $RS
bench Go $GO statusline; bench Rust $RS
echo "바이너리"
ls -l "$GO" "$RS" | awk '{printf "  %-42s %.2f MB\n", $9, $5/1048576}'
