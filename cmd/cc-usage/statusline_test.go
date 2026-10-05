package main

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"cc-usage/internal/render"
	"cc-usage/internal/store"
)

// statusline 을 프로세스째 돌려 "어느 경로가 refresh 를 띄우고 cache 를 쓰는가" 를
// 고정한다. 주 대상인 Claude Code 경로가 다른 호스트 지원 때문에 바뀌지 않았는지가
// 핵심이다 — 렌더 문자열은 internal/render 가 이미 보므로 여기서는 부수효과를 본다.

type slHarness struct {
	dir     string
	spawned []string // spawn 이 받은 source, 호출 순서대로
}

// newHarness isolates HOME·cache·config in a temp dir. token 은 어디서도 찾을 수
// 없게 가리키고, spawn 은 기록만 한다.
func newHarness(t *testing.T, source string) *slHarness {
	t.Helper()
	h := &slHarness{dir: t.TempDir()}
	home := filepath.Join(h.dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	// 배지가 그려지는지로 "Claude 계정 파일을 읽었나" 를 본다.
	h.write(t, filepath.Join(home, ".claude.json"), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	h.write(t, filepath.Join(h.dir, "config.json"), `{"source":"`+source+`",
		"keychain_service":"cc-usage-test-absent",
		"credentials_file":"`+filepath.Join(h.dir, "absent.json")+`",
		"badges":{"a@example.com":{"emoji":"🏢"}}}`)

	for k, v := range map[string]string{
		"HOME":              home,
		"XDG_CACHE_HOME":    filepath.Join(h.dir, "cache"),
		"CC_USAGE_CONFIG":   filepath.Join(h.dir, "config.json"),
		"NO_COLOR":          "1",
		"COLUMNS":           "",
		"CC_USAGE_SOURCE":   "",
		"CLAUDE_CONFIG_DIR": "",
	} {
		t.Setenv(k, v)
	}

	orig := spawn
	spawn = func(src string) error {
		h.spawned = append(h.spawned, src)
		return nil
	}
	t.Cleanup(func() { spawn = orig })
	return h
}

func (h *slHarness) write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// seedClaudeCache leaves what a Claude Code session on the same machine would
// have written — 다른 호스트의 줄에 이것이 새면 안 된다.
func (h *slHarness) seedClaudeCache(t *testing.T) {
	t.Helper()
	now := time.Now()
	w := &store.Window{Percent: 30, ResetsAt: now.Add(time.Hour)}
	st := store.StateFile{ObservedAt: now, StdinLimitsSeen: now, FiveHour: w}
	if err := store.Write(store.StatePath(), &st); err != nil {
		t.Fatal(err)
	}
}

// cacheFiles lists the cache dir — 비어 있어야 하는 경로가 무언가 썼는지 본다.
func (h *slHarness) cacheFiles() []string {
	es, _ := os.ReadDir(store.Dir())
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

// run feeds payload to `cc-usage statusline args...` and returns stdout.
func (h *slHarness) run(t *testing.T, payload string, args ...string) string {
	t.Helper()
	in := filepath.Join(h.dir, "stdin.json")
	h.write(t, in, payload)
	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = f, w
	err = runStatusline(args)
	os.Stdin, os.Stdout = oldIn, oldOut
	w.Close()
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("runStatusline: %v", err)
	}
	return string(out)
}

// claudePayload 는 Claude Code 꼴이다 — product 가 없다.
func claudePayload(dir string, rateLimits bool) string {
	p := `{"session_id":"s","model":{"display_name":"Opus 5"},"effort":{"level":"high"},` +
		`"context_window":{"used_percentage":41},"workspace":{"current_dir":"` + dir + `"},"cwd":"` + dir + `"`
	if rateLimits {
		p += `,"rate_limits":{"five_hour":{"used_percentage":30,"resets_at":` +
			strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10) + `}}`
	}
	return p + "}"
}

func TestStatuslineClaudeStdinKeepsItsSideEffects(t *testing.T) {
	// Pro/Max: stdin 의 rate_limits 를 state.json 에 남기고, 한도 전이라 refresh 는 없다.
	h := newHarness(t, "stdin")
	out := h.run(t, claudePayload(h.dir, true))
	for _, want := range []string{"🏢", "Opus 5 high", "ctx 41%", "5h 70%"} {
		if !strings.Contains(out, want) {
			t.Errorf("출력에 %q 가 없다:\n%s", want, out)
		}
	}
	if len(h.spawned) != 0 {
		t.Errorf("한도 전인데 refresh: %v", h.spawned)
	}
	var st store.StateFile
	if err := store.Read(store.StatePath(), &st); err != nil || st.FiveHour == nil || st.FiveHour.Percent != 30 {
		t.Errorf("state.json 에 5h 가 남지 않았다: %+v err=%v", st, err)
	}
}

func TestStatuslineClaudeSpawnsRefreshWithItsSource(t *testing.T) {
	// rate_limits 가 없는 Claude 세션은 여전히 API 쪽으로 간다. 자식에게 넘기는
	// source 는 설정값이 아니라 이 실행이 정한 값이어야 한다(--source 가 이긴다).
	cases := []struct {
		name, setting, env string
		args               []string
		want               string
	}{
		{"auto 설정", "auto", "", nil, "auto"},
		{"api 설정", "api", "", nil, "api"},
		{"플래그가 설정을 이긴다", "stdin", "", []string{"--source", "api"}, "api"},
		{"플래그가 env none 을 이긴다", "auto", "none", []string{"--source=auto"}, "auto"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, c.setting)
			t.Setenv("CC_USAGE_SOURCE", c.env)
			h.run(t, claudePayload(h.dir, false), c.args...)
			if !slices.Equal(h.spawned, []string{c.want}) {
				t.Errorf("spawn %v, want [%s]", h.spawned, c.want)
			}
			if !slices.Contains(h.cacheFiles(), "state.json") {
				t.Errorf("spawn 시각이 state.json 에 남지 않았다: %v", h.cacheFiles())
			}
		})
	}
}

func TestStatuslineNoneTouchesNothing(t *testing.T) {
	// none 은 Claude 쪽을 하나도 건드리지 않는다: refresh 없음, cache 읽기·쓰기 없음,
	// 계정 배지 없음. 같은 머신 Claude 세션의 cache 가 있어도 새지 않아야 한다.
	cases := []struct {
		name string
		env  string
		args []string
	}{
		{"--source none", "", []string{"--source", "none"}},
		{"--source=none", "", []string{"--source=none"}},
		{"CC_USAGE_SOURCE=none", "none", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, "auto")
			t.Setenv("CC_USAGE_SOURCE", c.env)
			h.seedClaudeCache(t)
			before := h.cacheFiles()
			out := h.run(t, claudePayload(h.dir, true), c.args...)
			if len(h.spawned) != 0 {
				t.Errorf("none 인데 refresh: %v", h.spawned)
			}
			if after := h.cacheFiles(); !slices.Equal(before, after) {
				t.Errorf("cache 가 바뀌었다: %v → %v", before, after)
			}
			for _, banned := range []string{"5h", "7d", "usage", "💳", "🏢"} {
				if strings.Contains(out, banned) {
					t.Errorf("none 출력에 %q:\n%s", banned, out)
				}
			}
			if !strings.Contains(out, "Opus 5 high · ctx 41%") {
				t.Errorf("모델·ctx 가 빠졌다:\n%s", out)
			}
		})
	}
}

func TestStatuslineBadSourceFlagStillPrints(t *testing.T) {
	// 잘못된 플래그도 statusline 을 비우지 않는다 — 무엇이 틀렸는지 한 줄을 낸다.
	h := newHarness(t, "auto")
	out := h.run(t, claudePayload(h.dir, true), "--source", "bogus")
	if !strings.Contains(out, "[cc-usage]") || !strings.Contains(out, "bogus") {
		t.Errorf("got %q", out)
	}
	if len(h.spawned) != 0 || len(h.cacheFiles()) != 0 {
		t.Errorf("설정 오류인데 부수효과: spawn=%v cache=%v", h.spawned, h.cacheFiles())
	}
}

func TestRefreshCmdPassesSource(t *testing.T) {
	// 물려받은 $CC_USAGE_SOURCE 가 무엇이든 자식은 부모가 정한 source 로 돈다.
	// exec 는 같은 키가 여럿이면 마지막 값을 쓰므로, 뒤에 붙인 값이 이겨야 한다.
	t.Setenv("CC_USAGE_SOURCE", "none")
	cmd := refreshCmd("/bin/true", "api")
	var got []string
	for _, kv := range cmd.Environ() {
		if strings.HasPrefix(kv, "CC_USAGE_SOURCE=") {
			got = append(got, kv)
		}
	}
	if !slices.Equal(got, []string{"CC_USAGE_SOURCE=api"}) {
		t.Errorf("자식 env 의 CC_USAGE_SOURCE = %v", got)
	}
	if !slices.Equal(cmd.Args[1:], []string{"refresh"}) || cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		t.Errorf("detached refresh 가 아니다: args=%v attr=%+v", cmd.Args, cmd.SysProcAttr)
	}
}

func TestRefreshNoneIsNoop(t *testing.T) {
	// none 은 token 도 API 도 보지 않는다 — 설정이 none 이든, 부모 statusline 이 env 로
	// none 을 넘겼든. token_env 에 값을 둬서 조회까지 갔다면 usage.json 이 남게 한다.
	cases := []struct{ name, setting, env string }{
		{"설정 none", "none", ""},
		{"env none (부모에게서)", "auto", "none"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, c.setting)
			t.Setenv("CC_USAGE_SOURCE", c.env)
			t.Setenv("CC_USAGE_API_URL", "http://127.0.0.1:1/x")
			if err := runRefresh(nil); err != nil {
				t.Fatalf("runRefresh: %v", err)
			}
			if files := h.cacheFiles(); len(files) != 0 {
				t.Errorf("none 인데 refresh 가 무언가 남겼다: %v", files)
			}
		})
	}
}

// agyPayload 는 agy 1.2.14 실측 꼴에서 식별 정보를 뺀 것이다.
func agyPayload(dir string) string {
	r5 := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	r7 := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	return `{"cwd":"` + dir + `","session_id":"s","product":"antigravity","plan_tier":"Google AI Pro",` +
		`"model":{"id":"Gemini 3.8 Flash (High)","display_name":"Gemini 3.8 Flash (High)","effort":"high"},` +
		`"workspace":{"current_dir":"` + dir + `","project_dir":"` + dir + `"},` +
		`"context_window":{"used_percentage":12,"context_window_size":1048576},` +
		`"quota":{"gemini-5h":{"remaining_fraction":0.86,"reset_time":"` + r5 + `"},` +
		`"gemini-weekly":{"remaining_fraction":0.94,"reset_time":"` + r7 + `"},` +
		`"3p-5h":{"remaining_fraction":1,"reset_time":"` + r5 + `"}}}`
}

func TestStatuslineAgyNeverTouchesClaude(t *testing.T) {
	// agy 는 source 설정과 무관하게 Claude 쪽(refresh·cache·계정 배지)을 건드리지
	// 않고, 한도는 stdin 의 quota 로만 그린다. 같은 머신 Claude 세션의 5h(70% 남음)
	// 가 새면 안 된다.
	for _, source := range []string{"auto", "stdin", "api"} {
		t.Run(source, func(t *testing.T) {
			h := newHarness(t, source)
			h.seedClaudeCache(t)
			before := h.cacheFiles()
			out := h.run(t, agyPayload(h.dir))
			if len(h.spawned) != 0 {
				t.Errorf("agy 인데 refresh: %v", h.spawned)
			}
			if after := h.cacheFiles(); !slices.Equal(before, after) {
				t.Errorf("cache 가 바뀌었다: %v → %v", before, after)
			}
			if !strings.Contains(out, "Gemini 3.8 Flash (High) · ctx 12% · 5h 86%") {
				t.Errorf("agy quota 가 안 그려졌다:\n%s", out)
			}
			for _, banned := range []string{"70%", "usage", "💳", "🏢"} {
				if strings.Contains(out, banned) {
					t.Errorf("agy 출력에 Claude 쪽 %q:\n%s", banned, out)
				}
			}
		})
	}
}

func TestStatuslineAgyWithSourceNoneDrawsNoLimits(t *testing.T) {
	h := newHarness(t, "auto")
	out := h.run(t, agyPayload(h.dir), "--source", "none")
	if strings.Contains(out, "5h") || strings.Contains(out, "7d") {
		t.Errorf("none 인데 한도:\n%s", out)
	}
	if len(h.spawned) != 0 || len(h.cacheFiles()) != 0 {
		t.Errorf("부수효과: spawn=%v cache=%v", h.spawned, h.cacheFiles())
	}
}

func TestStatuslineClaudeIgnoresAgyFields(t *testing.T) {
	// product 가 antigravity 가 아니면 quota 가 같이 와도 Claude 경로다 — 한도는
	// rate_limits 에서 오고 state.json 에 남는다. agy 판별이 넓어지면 이 테스트가 깨진다.
	for _, product := range []string{"", "claude-code"} {
		t.Run("product="+product, func(t *testing.T) {
			h := newHarness(t, "stdin")
			p := strings.TrimSuffix(claudePayload(h.dir, true), "}") +
				`,"product":"` + product + `","quota":{"gemini-5h":{"remaining_fraction":0.1}}}`
			out := h.run(t, p)
			if !strings.Contains(out, "5h 70%") || strings.Contains(out, "5h 10%") || !strings.Contains(out, "🏢") {
				t.Errorf("Claude 경로가 아니다:\n%s", out)
			}
			if !slices.Contains(h.cacheFiles(), "state.json") {
				t.Errorf("state.json 이 없다: %v", h.cacheFiles())
			}
		})
	}
}

func TestClockHonorsNowEnv(t *testing.T) {
	t.Setenv(NowEnv, "2026-09-16T16:40:00+09:00")
	want := time.Date(2026, 9, 16, 7, 40, 0, 0, time.UTC)
	if got := clock(); !got.Equal(want) {
		t.Fatalf("clock() = %v, want %v", got, want)
	}
	// 읽을 수 없는 값은 실제 시계로 떨어진다 — 테스트 훅이 statusline 을 깨면 안 된다.
	t.Setenv(NowEnv, "yesterday")
	if got := clock(); time.Since(got) > time.Minute {
		t.Fatalf("clock() with bad env = %v, want ~now", got)
	}
}

func TestStatuslineResetTextFollowsNowEnv(t *testing.T) {
	// 남은 시간은 실제 시계가 아니라 CC_USAGE_NOW 로 센다 — 리셋 50시간 전으로 고정하면
	// 날짜가 달라 남은 시간 표기가 나오고, 그 값이 고정 시각 기준이어야 한다.
	h := newHarness(t, "stdin")
	reset := time.Date(2030, 1, 3, 0, 0, 0, 0, time.UTC)
	t.Setenv(NowEnv, reset.Add(-50*time.Hour).Format(time.RFC3339))
	p := `{"session_id":"s","model":{"display_name":"Opus 5"},"workspace":{"current_dir":"` + h.dir + `"},` +
		`"rate_limits":{"five_hour":{"used_percentage":30,"resets_at":` + strconv.FormatInt(reset.Unix(), 10) + `}}}`
	if out, want := h.run(t, p), "("+render.Duration(50*time.Hour)+")"; !strings.Contains(out, want) {
		t.Fatalf("output %q does not contain %q", out, want)
	}
}
