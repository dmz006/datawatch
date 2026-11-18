package autonomous

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func lintGateRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "init")
	return dir
}

func writeLintFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exitError(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit 1").Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected exit error, got %v", err)
	}
	return err
}

func TestLintGateFindingsFail(t *testing.T) {
	dir := lintGateRepo(t)
	writeLintFile(t, dir, "pkg/a/verify_lint_gate.go", "package a\n\nfunc helper() {}\n")
	finding := "verify_lint_gate.go:3:6: func helper is unused (unused)"
	exitErr := exitError(t)
	var gotPkgs []string
	res, err := LintGateCheck(context.Background(), dir, func(_ context.Context, d string, pkgs []string) (string, error) {
		gotPkgs = pkgs
		return "\n" + finding + "\n", exitErr
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected Passed false")
	}
	if !strings.Contains(res.Findings, finding) {
		t.Fatalf("findings missing text: %q", res.Findings)
	}
	if len(gotPkgs) != 1 || gotPkgs[0] != "./pkg/a" {
		t.Fatalf("packages = %v", gotPkgs)
	}
}

func TestLintGateCleanPasses(t *testing.T) {
	dir := lintGateRepo(t)
	writeLintFile(t, dir, "pkg/a/a.go", "package a\n")
	writeLintFile(t, dir, "pkg/a/b.go", "package a\n")
	writeLintFile(t, dir, "root.go", "package main\n")
	res, err := LintGateCheck(context.Background(), dir, func(context.Context, string, []string) (string, error) {
		return "", nil
	})
	if err != nil || !res.Passed {
		t.Fatalf("expected pass, got %+v err=%v", res, err)
	}
	if strings.Join(res.Packages, ",") != ".,./pkg/a" {
		t.Fatalf("packages not deduplicated: %v", res.Packages)
	}
}

func TestLintGateMarkdownOnlySkipsRunner(t *testing.T) {
	dir := lintGateRepo(t)
	writeLintFile(t, dir, "docs/x.md", "# x\n")
	called := false
	res, err := LintGateCheck(context.Background(), dir, func(context.Context, string, []string) (string, error) {
		called = true
		return "", nil
	})
	if err != nil || !res.Passed || called {
		t.Fatalf("res=%+v err=%v called=%v", res, err, called)
	}
}

func TestLintGateRunnerStartFailureErrors(t *testing.T) {
	dir := lintGateRepo(t)
	writeLintFile(t, dir, "a.go", "package a\n")
	res, err := LintGateCheck(context.Background(), dir, func(context.Context, string, []string) (string, error) {
		return "", exec.ErrNotFound
	})
	if err == nil || res.Passed {
		t.Fatalf("expected error and no pass, got %+v err=%v", res, err)
	}
}

func TestLintGateFindingsCapped(t *testing.T) {
	if got := capLintFindings("  " + strings.Repeat("x", 9000) + "  "); len(got) != lintFindingsCap {
		t.Fatalf("len = %d", len(got))
	}
}
