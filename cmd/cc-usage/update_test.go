package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRepo makes a throwaway git repo with one commit.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@e"},
		{"config", "user.name", "t"},
	} {
		if out, err := gitIn(dir, args...); err != nil {
			t.Fatalf("git %v: %v%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := gitIn(dir, "add", "-A"); err != nil {
		t.Fatalf("add: %v%s", err, out)
	}
	if out, err := gitIn(dir, "commit", "-qm", "init"); err != nil {
		t.Fatalf("commit: %v%s", err, out)
	}
	return dir
}

func TestCountsNeedsUpstream(t *testing.T) {
	// upstream 이 없으면 비교 자체가 안 된다. 0/0 으로 뭉개면 호출자가 그것을
	// "최신" 으로 읽는다 — 모르는 것과 같은 것은 다르다.
	if _, _, ok := counts(newRepo(t)); ok {
		t.Error("upstream 없는 저장소에서 ok=true 가 나오면 안 된다")
	}
	if _, _, ok := counts(t.TempDir()); ok {
		t.Error("저장소가 아닌 경로에서 ok=true 가 나오면 안 된다")
	}
}

func TestCountsReadsBothDirections(t *testing.T) {
	origin := newRepo(t)
	if out, err := gitIn(origin, "config", "receive.denyCurrentBranch", "ignore"); err != nil {
		t.Fatalf("config: %v%s", err, out)
	}
	clone := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", origin, clone).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v%s", err, out)
	}
	if _, _, ok := counts(clone); !ok {
		t.Fatal("clone 직후에는 비교가 돼야 한다")
	}
	if b, a, _ := counts(clone); b != 0 || a != 0 {
		t.Errorf("clone 직후: behind=%d ahead=%d (0/0 이어야)", b, a)
	}

	// origin 에 커밋을 하나 더 얹으면 clone 은 그만큼 뒤처진다.
	if err := os.WriteFile(filepath.Join(origin, "g"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := gitIn(origin, "add", "-A"); err != nil {
		t.Fatalf("add: %v%s", err, out)
	}
	if out, err := gitIn(origin, "commit", "-qm", "second"); err != nil {
		t.Fatalf("commit: %v%s", err, out)
	}
	if out, err := gitIn(clone, "fetch", "--quiet", "origin"); err != nil {
		t.Fatalf("fetch: %v%s", err, out)
	}
	b, a, ok := counts(clone)
	if !ok || b != 1 || a != 0 {
		t.Errorf("behind=%d ahead=%d ok=%v (1/0 이어야)", b, a, ok)
	}
}

func TestIndent(t *testing.T) {
	if got := indent(""); got != "" {
		t.Errorf("빈 출력은 아무것도 붙이지 않는다: %q", got)
	}
	if got := indent("a\nb\n"); got != "\n  a\n  b" {
		t.Errorf("%q", got)
	}
}

func TestRepoCheckAcceptsWorktree(t *testing.T) {
	// linked worktree 에서는 .git 이 디렉터리가 아니라 **파일**이다. 디렉터리
	// 여부로 판단하면 멀쩡한 소스를 "없다" 고 거부한다.
	origin := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if out, err := gitIn(origin, "worktree", "add", "-q", "--detach", wt); err != nil {
		t.Skipf("worktree 생성 불가: %v%s", err, out)
	}
	t.Cleanup(func() { _, _ = gitIn(origin, "worktree", "remove", "--force", wt) })

	if fi, err := os.Stat(filepath.Join(wt, ".git")); err != nil || fi.IsDir() {
		t.Fatalf("이 테스트의 전제(.git 이 파일)가 깨졌다: %v", err)
	}

	old := repoPath
	repoPath = wt
	t.Cleanup(func() { repoPath = old })

	err := runUpdate([]string{"--check"})
	// upstream 이 없어 어차피 에러지만, "저장소가 아니다" 로 막히면 안 된다.
	if err != nil && strings.Contains(err.Error(), "git 저장소가 아닙니다") {
		t.Errorf("worktree 를 저장소가 아니라고 거부했다: %v", err)
	}
}
