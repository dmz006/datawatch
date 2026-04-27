// v5.26.22 — git credential abstraction tests.

package server

import (
	"strings"
	"testing"
)

func TestInjectGitToken_HTTPSPublic(t *testing.T) {
	got := injectGitToken("https://github.com/foo/bar", "ghp_abc")
	want := "https://x-access-token:ghp_abc@github.com/foo/bar"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestInjectGitToken_HTTPSWithExistingAuth(t *testing.T) {
	// Operator already embedded auth — don't overwrite.
	in := "https://my-user:my-pass@gitea.example.com/foo/bar"
	got := injectGitToken(in, "ghp_abc")
	if got != in {
		t.Errorf("preserved-auth URL got rewritten: %q", got)
	}
}

func TestInjectGitToken_SSH(t *testing.T) {
	in := "git@github.com:foo/bar.git"
	got := injectGitToken(in, "ghp_abc")
	if got != in {
		t.Errorf("SSH URL got rewritten: %q", got)
	}
}

func TestInjectGitToken_EmptyToken(t *testing.T) {
	in := "https://github.com/foo/bar"
	if got := injectGitToken(in, ""); got != in {
		t.Errorf("empty token rewrote URL: %q", got)
	}
}

func TestInjectGitToken_BadURL(t *testing.T) {
	in := "://nonsense"
	got := injectGitToken(in, "tok")
	if got != in {
		t.Errorf("bad URL got rewritten: %q", got)
	}
}

func TestInjectGitToken_HTTPS_GitLabStyle(t *testing.T) {
	got := injectGitToken("https://gitlab.com/group/proj.git", "glpat_xyz")
	if !strings.Contains(got, "x-access-token:glpat_xyz") {
		t.Errorf("missing token in URL: %q", got)
	}
	if !strings.Contains(got, "@gitlab.com/group/proj.git") {
		t.Errorf("URL host/path mangled: %q", got)
	}
}

func TestRedactGitToken_RemovesInjected(t *testing.T) {
	blob := "fatal: could not read from https://x-access-token:ghp_secret@github.com/foo/bar"
	got := redactGitToken(blob, "https://github.com/foo/bar")
	if strings.Contains(got, "ghp_secret") {
		t.Errorf("token leaked through redact: %q", got)
	}
	if !strings.Contains(got, "x-access-token:***@") {
		t.Errorf("redacted form missing: %q", got)
	}
}

func TestRedactGitToken_RemovesEmbedded(t *testing.T) {
	// Operator embedded a non-x-access-token user in the URL.
	blob := "fatal: bad creds for https://alice:supersecret@gitea.example.com/foo/bar"
	got := redactGitToken(blob, "https://alice:supersecret@gitea.example.com/foo/bar")
	if strings.Contains(got, "supersecret") {
		t.Errorf("embedded token leaked: %q", got)
	}
	if !strings.Contains(got, "alice:***@") {
		t.Errorf("redacted form missing: %q", got)
	}
}

func TestRedactGitToken_BlobWithoutToken(t *testing.T) {
	blob := "no auth in this blob"
	got := redactGitToken(blob, "https://github.com/foo/bar")
	if got != blob {
		t.Errorf("redact mutated unrelated blob: %q", got)
	}
}
