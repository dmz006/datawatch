// F10 sprint 5 (S5.6) — git provider interface + GitHub gh-CLI shell-out
// + GitLab stub tests.

package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newFakeGH writes a tiny shell script named "gh" into a tempdir
// that:
//   * records every invocation to invocations.log
//   * routes by joined argv: output.<key> emits to stdout
//   * exits with code in exitcode.<key> if present
// The "key" is the full args, dot-joined, e.g. "auth.token" or
// "pr.create".
func newFakeGH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
echo "$@" >> "` + dir + `/invocations.log"
key=$(echo "$1.$2" | sed 's| |.|g')
if [ -f "` + dir + `/output.$key" ]; then
    cat "` + dir + `/output.$key"
fi
if [ -f "` + dir + `/exitcode.$key" ]; then
    exit "$(cat "` + dir + `/exitcode.$key")"
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	return dir
}

func TestGitHub_Kind(t *testing.T) {
	if got := NewGitHub().Kind(); got != "github" {
		t.Errorf("Kind=%q want github", got)
	}
}

func TestGitLab_Kind(t *testing.T) {
	if got := NewGitLab().Kind(); got != "gitlab" {
		t.Errorf("Kind=%q want gitlab", got)
	}
}

func TestGitLab_StubReturnsNotImplemented(t *testing.T) {
	g := NewGitLab()
	if _, err := g.MintToken(context.Background(), "x/y", time.Hour); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("MintToken err=%v want ErrNotImplemented", err)
	}
	if err := g.RevokeToken(context.Background(), "tok"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("RevokeToken err=%v want ErrNotImplemented", err)
	}
	if _, err := g.OpenPR(context.Background(), PROptions{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("OpenPR err=%v want ErrNotImplemented", err)
	}
}

func TestResolve_Routing(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{"github", "github"},
		{"gitlab", "gitlab"},
		{"unknown-forge", "unknown-forge"}, // stub echoes
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			p := Resolve(c.kind)
			if p == nil {
				t.Fatalf("Resolve(%q) returned nil", c.kind)
			}
			if got := p.Kind(); got != c.want {
				t.Errorf("Kind=%q want %q", got, c.want)
			}
		})
	}
}

// Stub provider returns ErrNotImplemented for any call.
func TestResolve_StubProviderRejects(t *testing.T) {
	p := Resolve("bogus")
	if _, err := p.MintToken(context.Background(), "x", time.Hour); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("err=%v want ErrNotImplemented", err)
	}
}

// MintToken: gh auth token outputs the token; we trim and return it.
// When repo is non-empty, a follow-up `gh api /repos/<repo>` probes
// access. Both should appear in the invocation log.
func TestGitHub_MintToken_Success(t *testing.T) {
	dir := newFakeGH(t)
	prev := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+prev)
	if err := os.WriteFile(filepath.Join(dir, "output.auth.token"),
		[]byte("ghp_FAKE_TOKEN_123\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "output.api.-X"),
		[]byte("test-repo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	g := NewGitHub()
	tok, err := g.MintToken(context.Background(), "owner/repo", 30*time.Minute)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	if tok.Token != "ghp_FAKE_TOKEN_123" {
		t.Errorf("Token=%q want ghp_FAKE_TOKEN_123", tok.Token)
	}
	if tok.ExpiresAt.Before(time.Now()) {
		t.Errorf("ExpiresAt is in the past: %v", tok.ExpiresAt)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
	if !strings.Contains(string(log), "auth token") {
		t.Errorf("auth token not invoked:\n%s", log)
	}
	if !strings.Contains(string(log), "/repos/owner/repo") {
		t.Errorf("api probe not run:\n%s", log)
	}
}

// MintToken with empty repo skips the access probe.
func TestGitHub_MintToken_NoRepoSkipsProbe(t *testing.T) {
	dir := newFakeGH(t)
	prev := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+prev)
	_ = os.WriteFile(filepath.Join(dir, "output.auth.token"), []byte("tok\n"), 0644)

	g := NewGitHub()
	if _, err := g.MintToken(context.Background(), "", time.Hour); err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
	if strings.Contains(string(log), "/repos/") {
		t.Errorf("api probe should be skipped for empty repo:\n%s", log)
	}
}

// `gh auth token` exiting non-zero (no auth) surfaces a clear error
// with the operator-actionable hint.
func TestGitHub_MintToken_AuthMissing(t *testing.T) {
	dir := newFakeGH(t)
	prev := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+prev)
	_ = os.WriteFile(filepath.Join(dir, "exitcode.auth.token"), []byte("1"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "output.auth.token"), []byte("not authenticated\n"), 0644)

	g := NewGitHub()
	_, err := g.MintToken(context.Background(), "x/y", time.Hour)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "gh auth login") {
		t.Errorf("error missing actionable hint: %v", err)
	}
}

// Empty `gh auth token` output (gh succeeds but prints nothing) is
// still a failure.
func TestGitHub_MintToken_EmptyToken(t *testing.T) {
	dir := newFakeGH(t)
	prev := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+prev)
	_ = os.WriteFile(filepath.Join(dir, "output.auth.token"), []byte(""), 0644)

	g := NewGitHub()
	_, err := g.MintToken(context.Background(), "", time.Hour)
	if err == nil {
		t.Error("expected error on empty token")
	}
}

// RevokeToken is a v1 no-op — it must succeed regardless of input.
func TestGitHub_RevokeToken_NoOp(t *testing.T) {
	g := NewGitHub()
	if err := g.RevokeToken(context.Background(), "anything"); err != nil {
		t.Errorf("RevokeToken should be a no-op: %v", err)
	}
}

// OpenPR happy path — args wired correctly + URL extracted from gh
// stdout.
func TestGitHub_OpenPR_Success(t *testing.T) {
	dir := newFakeGH(t)
	prev := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+prev)
	_ = os.WriteFile(filepath.Join(dir, "output.pr.create"),
		[]byte("Creating pull request for...\nhttps://github.com/owner/repo/pull/42\n"), 0644)

	g := NewGitHub()
	url, err := g.OpenPR(context.Background(), PROptions{
		Repo:       "owner/repo",
		HeadBranch: "feat/x",
		BaseBranch: "main",
		Title:      "Add x",
		Body:       "fixes nothing",
	})
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	if url != "https://github.com/owner/repo/pull/42" {
		t.Errorf("url=%q", url)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
	for _, want := range []string{"pr create", "--repo owner/repo", "--head feat/x",
		"--base main", "--title Add x"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("log missing %q:\n%s", want, log)
		}
	}
}

// OpenPR validates required args before shelling out.
func TestGitHub_OpenPR_RequiredArgs(t *testing.T) {
	g := NewGitHub()
	cases := []struct {
		name string
		opts PROptions
	}{
		{"missing repo", PROptions{HeadBranch: "x"}},
		{"missing head", PROptions{Repo: "o/r"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := g.OpenPR(context.Background(), c.opts); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

// OpenPR surfaces gh's stdout when the call fails.
func TestGitHub_OpenPR_GhFailure(t *testing.T) {
	dir := newFakeGH(t)
	prev := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+prev)
	_ = os.WriteFile(filepath.Join(dir, "exitcode.pr.create"), []byte("1"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "output.pr.create"),
		[]byte("error: a pull request for branch \"feat/x\" already exists"), 0644)

	g := NewGitHub()
	_, err := g.OpenPR(context.Background(), PROptions{
		Repo: "o/r", HeadBranch: "feat/x", Title: "x", Body: "y",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error missing gh stderr: %v", err)
	}
}
