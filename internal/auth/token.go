// Package auth reads the Claude Code OAuth access token. It never writes or refreshes credentials.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"cc-usage/internal/config"
)

// ErrExpired means the stored token is past expiresAt. Claude Code refreshes it on its next request.
var ErrExpired = errors.New("oauth token expired (Claude Code를 한 번 사용하면 갱신됩니다)")

type Token struct {
	AccessToken string
	ExpiresAt   time.Time // zero if unknown
	Source      string    // env | keychain | file
}

type credentials struct {
	ClaudeAiOauth *struct {
		AccessToken string `json:"accessToken"`
		ExpiresAt   int64  `json:"expiresAt"` // epoch ms
	} `json:"claudeAiOauth"`
}

// Load resolves a token: token_env > macOS keychain > credentials file.
func Load(ctx context.Context, p *config.Config) (*Token, error) {
	if p.TokenEnv != "" {
		if v := strings.TrimSpace(os.Getenv(p.TokenEnv)); v != "" {
			return &Token{AccessToken: v, Source: "env:" + p.TokenEnv}, nil
		}
	}
	var errs []string
	if runtime.GOOS == "darwin" {
		t, err := fromKeychain(ctx, p.KeychainService)
		if err == nil {
			return check(t)
		}
		errs = append(errs, "keychain: "+err.Error())
	}
	b, err := os.ReadFile(p.CredentialsFile)
	if err == nil {
		t, perr := parse(b, "file")
		if perr == nil {
			return check(t)
		}
		errs = append(errs, "file: "+perr.Error())
	} else {
		errs = append(errs, "file: "+err.Error())
	}
	return nil, fmt.Errorf("token not found (%s)", strings.Join(errs, "; "))
}

func fromKeychain(ctx context.Context, service string) (*Token, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", service, "-w").Output()
	if err != nil {
		return nil, fmt.Errorf("service %q: %w", service, err)
	}
	return parse(out, "keychain")
}

func parse(b []byte, source string) (*Token, error) {
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) == 0 {
		return nil, errors.New("empty")
	}
	if b[0] != '{' { // bare token
		return &Token{AccessToken: string(b), Source: source}, nil
	}
	var c credentials
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.ClaudeAiOauth == nil || c.ClaudeAiOauth.AccessToken == "" {
		return nil, errors.New("claudeAiOauth.accessToken missing")
	}
	t := &Token{AccessToken: c.ClaudeAiOauth.AccessToken, Source: source}
	if c.ClaudeAiOauth.ExpiresAt > 0 {
		t.ExpiresAt = time.UnixMilli(c.ClaudeAiOauth.ExpiresAt)
	}
	return t, nil
}

func check(t *Token) (*Token, error) {
	if !t.ExpiresAt.IsZero() && time.Now().After(t.ExpiresAt) {
		return t, ErrExpired
	}
	return t, nil
}

// KeychainServices lists generic-password service names that look like Claude Code credentials.
// It uses `security dump-keychain` without -d, so no secrets are printed.
func KeychainServices(ctx context.Context) ([]string, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("macOS only")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/security", "dump-keychain").Output()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var res []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		const prefix = `"svce"<blob>="`
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(line, prefix), `"`)
		if strings.HasPrefix(name, "Claude Code") && !seen[name] {
			seen[name] = true
			res = append(res, name)
		}
	}
	return res, nil
}
