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
	"strings"
	"syscall"
	"time"

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

const usage = `cc-usage — Claude Code statusline / 크레딧 guard

사용법:
  cc-usage statusline [--profile NAME]   statusLine command (stdin JSON → stdout)
  cc-usage guard      [--profile NAME]   UserPromptSubmit hook (한도 소진 시 exit 2)
  cc-usage allow      [DURATION|off] [--profile NAME]   guard 일시 해제 (기본 30m)
  cc-usage refresh    [--profile NAME]   usage API 1회 조회 (statusline이 자동 호출)
  cc-usage probe      [--profile NAME]   usage API 원본 응답 출력 (필드 확인용)
  cc-usage doctor     [--profile NAME]   profile/token/cache/keychain 진단
  cc-usage version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
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
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cc-usage:", err)
		os.Exit(1)
	}
}

// profileFlags parses --profile and returns the resolved profile plus remaining args.
func profileFlags(name string, args []string) (*config.Profile, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	prof := fs.String("profile", "", "profile name")
	// allow positional args before flags (e.g. `allow 1h --profile work`)
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	p, err := cfg.Resolve(*prof)
	return p, positional, err
}

func runStatusline(args []string) error {
	p, _, err := profileFlags("statusline", args)
	if err != nil {
		fmt.Println("[cc-usage] " + err.Error()) // statusline must still print something
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
	in, _ := core.ParseInput(raw)
	now := time.Now()

	var st store.StateFile
	var uf store.UsageFile
	_ = store.Read(store.StatePath(p), &st)
	_ = store.Read(store.UsagePath(p), &uf)

	five, seven := in.StdinLimits()
	stdinPresent := five != nil || seven != nil
	useStdin := core.UseStdin(p, &st, stdinPresent, now)
	lim := core.Merge(useStdin, five, seven, &st, &uf, now)

	dirty := false
	if stdinPresent {
		st.ObservedAt, st.StdinLimitsSeen, st.FiveHour, st.SevenDay = now, now, five, seven
		dirty = true
	}
	if core.NeedRefresh(p, useStdin, lim, &st, &uf, now) && spawnRefresh(p) == nil {
		st.SpawnedAt = now
		dirty = true
	}
	alert, alertDirty := core.Alerts(p, lim, &st, now)
	dirty = dirty || alertDirty
	if alert.Fired {
		spawnNotify(p, alert) // 엣지에서 한 번. 실패해도 statusline은 그대로 간다.
	}
	if dirty {
		_ = store.Write(store.StatePath(p), &st)
	}

	dir := in.Workspace.CurrentDir
	var gs *git.Status
	if st, ok := git.Read(context.Background(), dir); ok {
		gs = &st
	}

	lines := render.Lines(render.View{
		Profile:    p,
		Dir:        dir,
		Git:        gs,
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
func spawnRefresh(p *config.Profile) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "refresh", "--profile", p.Name)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// spawnNotify fires the profile's notify command detached, once per 단계.
// 어떤 알림 수단인지는 설정에만 있다 — 코드는 argv와 치환만 안다.
func spawnNotify(p *config.Profile, a core.Alert) {
	if !p.Notify.On() {
		return
	}
	argv, ok := extra.Expand(p.Notify.Command, map[string]string{
		"{{level}}":   a.LevelName(),
		"{{window}}":  a.Window,
		"{{percent}}": fmt.Sprintf("%.0f", a.Percent),
		"{{message}}": a.Message(),
	})
	if !ok {
		return
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

func runRefresh(args []string) error {
	p, _, err := profileFlags("refresh", args)
	if err != nil {
		return err
	}
	unlock, ok, err := store.TryLock(store.LockPath(p))
	if err != nil || !ok {
		return err // another refresh is running
	}
	defer unlock()

	now := time.Now()
	var uf store.UsageFile
	var st store.StateFile
	_ = store.Read(store.UsagePath(p), &uf)
	_ = store.Read(store.StatePath(p), &st)
	if now.Before(uf.BackoffUntil) {
		return nil
	}

	ctx := context.Background()
	tok, err := auth.Load(ctx, p)
	if err != nil {
		core.ApplyFailure(&uf, err, 0, now)
		return errors.Join(err, store.Write(store.UsagePath(p), &uf))
	}
	u, err := api.Fetch(ctx, tok.AccessToken)
	if err != nil {
		var rl *api.RateLimitedError
		var ra time.Duration
		if errors.As(err, &rl) {
			ra = rl.RetryAfter
		}
		core.ApplyFailure(&uf, err, ra, now)
		return errors.Join(err, store.Write(store.UsagePath(p), &uf))
	}

	useStdin := core.UseStdin(p, &st, false, now)
	tmp := uf
	tmp.Usage = u
	lim := core.Merge(useStdin, nil, nil, &st, &tmp, now)
	hit, key := lim.Exhausted()
	core.ApplyFetch(&uf, u, hit, key, now)
	return store.Write(store.UsagePath(p), &uf)
}

func runGuard(args []string) error {
	_, _ = io.Copy(io.Discard, io.LimitReader(os.Stdin, 4<<20))
	p, _, err := profileFlags("guard", args)
	if err != nil {
		return nil // never block on misconfiguration
	}
	now := time.Now()
	var st store.StateFile
	var uf store.UsageFile
	var allow store.AllowFile
	_ = store.Read(store.StatePath(p), &st)
	_ = store.Read(store.UsagePath(p), &uf)
	_ = store.Read(store.AllowPath(p), &allow)

	useStdin := core.UseStdin(p, &st, false, now)
	lim := core.Merge(useStdin, nil, nil, &st, &uf, now)
	d := core.Guard(p, lim, &uf, &allow, now)
	if !d.Block {
		return nil
	}
	fmt.Fprintf(os.Stderr, "[cc-usage:%s] %s.\n계속하려면 터미널에서: cc-usage allow 30m --profile %s\n", p.Display(), d.Reason, p.Name)
	os.Exit(2)
	return nil
}

func runAllow(args []string) error {
	p, pos, err := profileFlags("allow", args)
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
		fmt.Printf("[%s] guard 다시 활성화\n", p.Display())
	} else {
		d, err := time.ParseDuration(arg)
		if err != nil || d <= 0 {
			return fmt.Errorf("invalid duration %q (예: 30m, 2h)", arg)
		}
		a.AllowUntil = time.Now().Add(d)
		fmt.Printf("[%s] %s까지 크레딧 사용 허용\n", p.Display(), a.AllowUntil.Format("15:04"))
	}
	return store.Write(store.AllowPath(p), &a)
}

func runProbe(args []string) error {
	p, _, err := profileFlags("probe", args)
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

// notifyState explains why notifications will or will not fire.
func notifyState(p *config.Profile) string {
	switch {
	case p.Notify == nil:
		return "설정 없음"
	case len(p.Notify.Command) == 0:
		return "command 없음"
	case !p.Notify.On():
		return "enabled=false (명령은 보존됨)"
	}
	return "on — " + strings.Join(p.Notify.Command, " ")
}

func runDoctor(args []string) error {
	p, _, err := profileFlags("doctor", args)
	if err != nil {
		return err
	}
	if cfg, err := config.Load(); err == nil {
		for _, o := range cfg.Profiles {
			if o.Name != p.Name && o.ConfigDir != p.ConfigDir && o.TokenEnv == "" && p.TokenEnv == "" &&
				o.KeychainService == p.KeychainService {
				fmt.Printf("⚠︎ profile %q와 keychain_service가 같습니다 (%s). 계정이 섞일 수 있으니 아래 목록에서 맞는 항목을 지정하세요.\n", o.Name, p.KeychainService)
			}
		}
	}
	if svcs, err := auth.KeychainServices(context.Background()); err == nil {
		fmt.Printf("keychain 후보:  %s\n", strings.Join(svcs, ", "))
	}
	fmt.Printf("config:        %s\n", config.Path())
	fmt.Printf("profile:       %s (label=%s, source=%s)\n", p.Name, p.StatusLabel(), p.Source)
	fmt.Printf("config_dir:    %s\n", p.ConfigDir)
	fmt.Printf("keychain:      %s\n", p.KeychainService)
	fmt.Printf("creds file:    %s\n", p.CredentialsFile)
	fmt.Printf("cache dir:     %s\n", store.Dir(p))
	fmt.Printf("guard:         %v\n", p.Guard)
	fmt.Printf("alert:         임박 %.0f%% (0이면 소진만)\n", p.AlertPercent)
	fmt.Printf("notify:        %v\n", notifyState(p))

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

	var st store.StateFile
	var uf store.UsageFile
	_ = store.Read(store.StatePath(p), &st)
	_ = store.Read(store.UsagePath(p), &uf)
	b, _ := json.MarshalIndent(struct {
		State store.StateFile `json:"state"`
		Usage store.UsageFile `json:"usage"`
	}{st, uf}, "", "  ")
	fmt.Println(string(b))
	return nil
}
