// cc-usage: Claude Code statusline + credit guard for personal (Pro/Max) and Team accounts.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"cc-usage/internal/account"
	"cc-usage/internal/api"
	"cc-usage/internal/auth"
	"cc-usage/internal/config"
	"cc-usage/internal/core"
	"cc-usage/internal/extra"
	"cc-usage/internal/git"
	"cc-usage/internal/render"
	"cc-usage/internal/store"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, helpText())
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	// `<명령> --help` 는 flag 에러가 아니라 도움말이다. 명령마다 flagset 이 달라
	// 각자 처리하면 "flag: help requested" 로 exit 1 하는 것이 섞인다.
	if len(args) > 0 && isHelp(args[0]) {
		printHelp(cmd)
		return
	}
	var err error
	switch cmd {
	case "statusline":
		err = runStatusline(args)
	case "guard":
		err = runGuard(args)
	case "allow":
		err = runAllow(args)
	case "refresh":
		err = runRefresh(args)
	case "probe":
		err = runProbe(args)
	case "doctor":
		err = runDoctor(args)
	case "config":
		err = runConfig(args)
	case "update":
		err = runUpdate(args)
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		// `cc-usage help statusline` 도 `cc-usage statusline --help` 와 같게.
		if len(args) > 0 {
			printHelp(args[0])
		} else {
			fmt.Print(helpText())
		}
	default:
		fmt.Fprint(os.Stderr, helpText())
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cc-usage:", err)
		os.Exit(1)
	}
}

// loadConfig parses the (flagless) subcommand args and loads the config.
func loadConfig(name string, args []string) (*config.Config, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	cfg, err := config.Load()
	return cfg, fs.Args(), err
}

func runStatusline(args []string) error {
	p, _, err := loadConfig("statusline", args)
	if err != nil {
		fmt.Println("[cc-usage] " + err.Error()) // statusline must still print something
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
	in, _ := core.ParseInput(raw)
	now := time.Now()

	var st store.StateFile
	var uf store.UsageFile
	_ = store.Read(store.StatePath(), &st)
	_ = store.Read(store.UsagePath(), &uf)

	five, seven := in.StdinLimits()
	stdinPresent := five != nil || seven != nil
	useStdin := core.UseStdin(p, &st, stdinPresent, now)
	lim := core.Merge(useStdin, five, seven, &st, &uf, now)

	dirty := false
	if stdinPresent {
		st.ObservedAt, st.StdinLimitsSeen, st.FiveHour, st.SevenDay = now, now, five, seven
		dirty = true
	}
	if core.NeedRefresh(p, useStdin, lim, &st, &uf, now) && spawnRefresh() == nil {
		st.SpawnedAt = now
		dirty = true
	}
	// 로그인된 계정은 한도가 움직였을 때만 다시 읽는다 — 매 렌더 읽지 않는
	// 이유와 놓치는 경우는 core.NeedAccountCheck 에 적혀 있다.
	// Source 는 파일을 읽지 않는다 — 읽는 것은 아래 Email 뿐이고, 그것도
	// 판정이 통과했을 때만이다.
	src := account.Source(p)
	if core.NeedAccountCheck(src, lim, &st, now) {
		// 읽기에 실패하면 캐시를 건드리지 않는다. 빈 값으로 덮으면 일시적
		// 실패(원자적 재작성 창 등)가 "기본 계정" 이라는 틀린 신호로 최대 TTL
		// 동안 굳는다. 손대지 않으면 다음 렌더에 다시 시도한다.
		if email, ok := account.Email(p); ok {
			st.AccountSource, st.AccountEmail = src, email
			st.AccountAt, st.AccountCheckedAt = lim.Key(), now
			dirty = true
		}
	}
	// 캐시를 남겨 두더라도 **다른 자리의 것이면 그리지 않는다.**
	var badge *config.Badge
	if email := core.AccountCached(src, &st); email != "" {
		if b, ok := p.Badges[email]; ok {
			badge = &b
		}
	}

	alert, alertDirty := core.Alerts(p, lim, &st, now)
	dirty = dirty || alertDirty
	// 단계가 올라간 시각을 기록해야 다음 렌더가 burst 구간인지 안다.
	if dirty {
		_ = store.Write(store.StatePath(), &st)
	}

	dir := in.Workspace.CurrentDir
	var gs *git.Status
	if st, ok := git.Read(context.Background(), dir); ok {
		gs = &st
	}

	lines := render.Lines(render.View{
		Config:     p,
		Dir:        dir,
		Git:        gs,
		Badge:      badge,
		Model:      in.Model.DisplayName,
		ContextPct: in.ContextWindow.UsedPercentage,
		Limits:     lim,
		Alert:      alert,
		Usage:      &uf,
		Credits:    core.Credits(p, lim, &uf, now),
		Now:        now,
	}, render.DefaultStyle())
	// 다른 도구의 세그먼트는 cc-usage 줄 아래에 그대로 붙인다. 실패해도 조용히
	// 빠질 뿐이라 statusline은 항상 무언가를 출력한다.
	lines = append(lines, extra.Run(context.Background(), p.ExtraCommands,
		extra.Vars{SessionID: in.SessionID, Cwd: dir})...)
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

// spawnRefresh starts `cc-usage refresh` detached so it survives statusline cancellation.
func spawnRefresh() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "refresh")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func runRefresh(args []string) error {
	p, _, err := loadConfig("refresh", args)
	if err != nil {
		return err
	}
	unlock, ok, err := store.TryLock(store.LockPath())
	if err != nil || !ok {
		return err // another refresh is running
	}
	defer unlock()

	now := time.Now()
	var uf store.UsageFile
	var st store.StateFile
	_ = store.Read(store.UsagePath(), &uf)
	_ = store.Read(store.StatePath(), &st)
	if now.Before(uf.BackoffUntil) {
		return nil
	}

	ctx := context.Background()
	tok, err := auth.Load(ctx, p)
	if err != nil {
		core.ApplyFailure(&uf, err, 0, now)
		return errors.Join(err, store.Write(store.UsagePath(), &uf))
	}
	u, err := api.Fetch(ctx, tok.AccessToken)
	if err != nil {
		var rl *api.RateLimitedError
		var ra time.Duration
		if errors.As(err, &rl) {
			ra = rl.RetryAfter
		}
		core.ApplyFailure(&uf, err, ra, now)
		return errors.Join(err, store.Write(store.UsagePath(), &uf))
	}

	useStdin := core.UseStdin(p, &st, false, now)
	tmp := uf
	tmp.Usage = u
	lim := core.Merge(useStdin, nil, nil, &st, &tmp, now)
	hit, key := lim.Exhausted()
	core.ApplyFetch(&uf, u, hit, key, now)
	return store.Write(store.UsagePath(), &uf)
}

func runGuard(args []string) error {
	_, _ = io.Copy(io.Discard, io.LimitReader(os.Stdin, 4<<20))
	p, _, err := loadConfig("guard", args)
	if err != nil {
		return nil // never block on misconfiguration
	}
	now := time.Now()
	var st store.StateFile
	var uf store.UsageFile
	var allow store.AllowFile
	_ = store.Read(store.StatePath(), &st)
	_ = store.Read(store.UsagePath(), &uf)
	_ = store.Read(store.AllowPath(), &allow)

	useStdin := core.UseStdin(p, &st, false, now)
	lim := core.Merge(useStdin, nil, nil, &st, &uf, now)
	// 계정 파일은 prompt 당 한 번만 읽는다 — statusline 과 달리 이 경로는 초당
	// 두 번 돌지 않는다.
	d := core.Guard(p, lim, &uf, &allow, account.ExtraUsageEnabled(p), now)
	if !d.Block {
		return nil
	}
	fmt.Fprintf(os.Stderr, "[cc-usage] %s.\n계속하려면 터미널에서: cc-usage allow 30m\n", d.Reason)
	os.Exit(2)
	return nil
}

func runAllow(args []string) error {
	_, pos, err := loadConfig("allow", args)
	if err != nil {
		return err
	}
	arg := "30m"
	if len(pos) > 0 {
		arg = pos[0]
	}
	var a store.AllowFile
	if arg == "off" || arg == "0" {
		a.AllowUntil = time.Time{}
		fmt.Println("[cc-usage] guard 다시 활성화")
	} else {
		d, err := time.ParseDuration(arg)
		if err != nil || d <= 0 {
			return fmt.Errorf("invalid duration %q (예: 30m, 2h)", arg)
		}
		a.AllowUntil = time.Now().Add(d)
		fmt.Printf("[cc-usage] %s까지 크레딧 사용 허용\n", a.AllowUntil.Format("15:04"))
	}
	return store.Write(store.AllowPath(), &a)
}

func runProbe(args []string) error {
	p, _, err := loadConfig("probe", args)
	if err != nil {
		return err
	}
	ctx := context.Background()
	tok, err := auth.Load(ctx, p)
	if err != nil {
		return err
	}
	body, err := api.FetchRaw(ctx, tok.AccessToken)
	if err != nil {
		return err
	}
	var v any
	if json.Unmarshal(body, &v) == nil {
		out, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Println(string(body))
	return nil
}

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	sessionID := fs.String("session-id", "", "extra_commands 의 {{session_id}} 에 넣을 값")
	if help, err := parseFlags(fs, args); help || err != nil {
		return err
	}
	p, err := config.Load()
	if err != nil {
		return err
	}
	if svcs, err := auth.KeychainServices(context.Background()); err == nil {
		fmt.Printf("keychain 후보:  %s\n", strings.Join(svcs, ", "))
	}
	fmt.Printf("config:        %s\n", config.Path())
	fmt.Printf("source:        %s\n", p.Source)
	fmt.Printf("config_dir:    %s\n", p.ConfigDir)
	if p.KeychainService == "" {
		// 빈 값은 설정 누락이 아니라 의도다 — 왜 건너뛰는지 화면에 적는다.
		fmt.Println("keychain:      (건너뜀 — 비기본 config_dir, creds 파일만 봅니다)")
	} else {
		fmt.Printf("keychain:      %s\n", p.KeychainService)
	}
	fmt.Printf("creds file:    %s\n", p.CredentialsFile)
	fmt.Printf("cache dir:     %s\n", store.Dir())
	fmt.Printf("guard:         %v\n", p.Guard)

	var st store.StateFile
	var uf store.UsageFile
	_ = store.Read(store.StatePath(), &st)
	_ = store.Read(store.UsagePath(), &uf)

	// guard 가 한도 소진에서 실제로 막을지는 크레딧이 켜져 있느냐에 달렸다.
	// **Guard 와 같은 함수로 계산한다** — 여기서 계정 파일만 보면, 관측값이 그와
	// 다를 때(크레딧을 방금 켜고 끈 직후) 화면이 실제 동작과 반대를 말한다.
	src := ".claude.json"
	if uf.Usage != nil && uf.Usage.Extra != nil {
		src = "usage.json"
	}
	state, blocks := "모름", true
	switch on := core.CreditsEnabled(&uf, account.ExtraUsageEnabled(p)); {
	case on == nil:
		src = "-" // 어느 쪽도 답을 내지 못했다
	case *on:
		state = "켜짐"
	default:
		state, blocks = "꺼짐", false
	}
	// guard 가 꺼져 있으면 막고 안 막고를 말하지 않는다 — 어차피 아무것도 막지 않는다.
	switch {
	case !p.Guard:
		fmt.Printf("크레딧:        %s (%s)\n", state, src)
	case blocks:
		fmt.Printf("크레딧:        %s (%s) — 소진 시 guard 가 막습니다\n", state, src)
	default:
		fmt.Printf("크레딧:        %s (%s) — guard 가 막지 않습니다\n", state, src)
	}
	fmt.Printf("alert:         임박 %.0f%% (0이면 소진만)\n", p.Alert())

	tok, err := auth.Load(context.Background(), p)
	switch {
	case tok != nil && err == nil:
		exp := "unknown"
		if !tok.ExpiresAt.IsZero() {
			exp = tok.ExpiresAt.Format(time.RFC3339)
		}
		fmt.Printf("token:         ok (source=%s, expires=%s)\n", tok.Source, exp)
	case tok != nil:
		fmt.Printf("token:         %v (source=%s)\n", err, tok.Source)
	default:
		fmt.Printf("token:         %v\n", err)
	}

	printExtras(p.ExtraCommands, *sessionID)

	b, _ := json.MarshalIndent(struct {
		State store.StateFile `json:"state"`
		Usage store.UsageFile `json:"usage"`
	}{st, uf}, "", "  ")
	fmt.Println(string(b))
	return nil
}

// printExtras runs each extra command the way statusline would and says what
// happened. statusline 은 실패를 조용히 삼키므로 "왜 안 붙는가" 는 여기서만 보인다.
//
// doctor 에는 Claude Code 세션이 없다 — {{cwd}} 는 지금 디렉터리로 채우고,
// {{session_id}} 는 --session-id 로 받지 않으면 비워 둔다(그 명령은 statusline 과
// 똑같이 건너뛴다고 나온다).
func printExtras(cmds []config.ExtraCommand, sessionID string) {
	if len(cmds) == 0 {
		fmt.Println("extra_commands: 없음 (다른 도구 줄을 붙이는 법: cc-usage config)")
		return
	}
	cwd, _ := os.Getwd()
	sid := sessionID
	if sid == "" {
		sid = "(비어 있음 — --session-id 로 채운다)"
	}
	fmt.Printf("extra_commands: %d개  cwd=%s  session_id=%s\n", len(cmds), cwd, sid)
	res := extra.Probe(context.Background(), cmds, extra.Vars{SessionID: sessionID, Cwd: cwd})
	for i, r := range res {
		fmt.Printf("  [%d] %s\n", i+1, strings.Join(cmds[i].Command, " "))
		// 치환 결과가 원인일 때가 있다 — placeholder 가 있었으면 실제 argv 도 보인다.
		if r.Argv != nil && !slices.Equal(r.Argv, cmds[i].Command) {
			fmt.Printf("      = %s\n", strings.Join(r.Argv, " "))
		}
		fmt.Printf("      %s\n", extra.Describe(r, cmds[i].Timeout()))
	}
}
