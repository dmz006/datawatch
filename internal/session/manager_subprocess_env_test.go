package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// envMap turns a KEY=VALUE list into a map (first occurrence wins per key,
// matching how exec.Cmd de-duplicates).
func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok {
			if _, exists := m[k]; !exists {
				m[k] = kv[strings.Index(kv, "=")+1:]
			}
		}
	}
	return m
}

// TestSubprocessResultEnvHasIdentityBaseURLAndToken (GH#204 Phase 1) —
// a subprocess session's child environment must carry the session's
// identity, the daemon base URL, and a freshly minted scoped credential
// that resolves back to exactly this session with exactly
// sessions:result_write — nothing wider.
func TestSubprocessResultEnvHasIdentityBaseURLAndToken(t *testing.T) {
	mgr, err := NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(mgr)
	tokenStore, err := auth.NewSessionTokenStore("") // "" = in-memory
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetSessionTokenMinter(tokenStore)
	mgr.SetSubprocessBaseURL("http://127.0.0.1:18174/") // trailing slash must not leak through

	sess := &Session{FullID: "testhost-ab12cd"}
	env := envMap(mgr.subprocessEnv(sess))

	if env["DATAWATCH_SESSION_ID"] != "testhost-ab12cd" {
		t.Errorf("DATAWATCH_SESSION_ID = %q, want testhost-ab12cd", env["DATAWATCH_SESSION_ID"])
	}
	if env["DATAWATCH_BASE_URL"] != "http://127.0.0.1:18174" {
		t.Errorf("DATAWATCH_BASE_URL = %q, want http://127.0.0.1:18174", env["DATAWATCH_BASE_URL"])
	}
	tok := env["DATAWATCH_RESULT_TOKEN"]
	if tok == "" {
		t.Fatal("DATAWATCH_RESULT_TOKEN missing — a minter is wired, expected a minted scoped credential")
	}
	if got := tokenStore.SessionIDForToken(tok); got != "testhost-ab12cd" {
		t.Errorf("token resolves to session %q, want testhost-ab12cd", got)
	}
	caps, ok := tokenStore.CapsForToken(tok)
	if !ok {
		t.Fatal("DATAWATCH_RESULT_TOKEN not resolvable in the token store")
	}
	if len(caps) != 1 || caps[0] != federation.CapSessionsResultWrite {
		t.Errorf("token caps = %v, want exactly [%s]", caps, federation.CapSessionsResultWrite)
	}
	// The child still inherits the daemon's own environment.
	if want := os.Getenv("PATH"); want != "" && env["PATH"] != want {
		t.Errorf("child environment must inherit the daemon's PATH; got %q, want %q", env["PATH"], want)
	}
}

// TestSubprocessResultEnvOmitsTokenWithoutStore (GH#204 Phase 1) — nil-safe
// wiring: without a minter set (tests, or a pre-upgrade daemon image) the
// child gets identity and base URL but NO token variable at all — an unset
// minter can't mint, so there is no silent fallback to an admin token.
func TestSubprocessResultEnvOmitsTokenWithoutStore(t *testing.T) {
	mgr, err := NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(mgr)
	mgr.SetSubprocessBaseURL("http://127.0.0.1:18175")

	sess := &Session{FullID: "testhost-ab12cd"}
	env := envMap(mgr.subprocessEnv(sess))

	if env["DATAWATCH_SESSION_ID"] != "testhost-ab12cd" {
		t.Errorf("DATAWATCH_SESSION_ID = %q, want testhost-ab12cd", env["DATAWATCH_SESSION_ID"])
	}
	if env["DATAWATCH_BASE_URL"] != "http://127.0.0.1:18175" {
		t.Errorf("DATAWATCH_BASE_URL = %q, want http://127.0.0.1:18175", env["DATAWATCH_BASE_URL"])
	}
	if tok, present := envMap(mgr.subprocessEnv(sess))["DATAWATCH_RESULT_TOKEN"]; present {
		t.Errorf("no minter wired: DATAWATCH_RESULT_TOKEN must be absent, got %q", tok)
	}
}

// TestSubprocessResultChildActuallyReceivesToken (GH#204 Phase 1) —
// end-to-end: a real subprocess session starts, the child's visible
// environment carries all three variables, and the token the child can see
// is the one the daemon minted for exactly that session.
func TestSubprocessResultChildActuallyReceivesToken(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash not available: %v", err)
	}
	dataDir := t.TempDir()
	mgr, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(mgr)
	mgr.SetConfig(config.DefaultConfig())
	tokenStore, err := auth.NewSessionTokenStore("")
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetSessionTokenMinter(tokenStore)
	mgr.SetSubprocessBaseURL("http://127.0.0.1:18176")

	out := filepath.Join(dataDir, "child-env.txt")
	// A subprocess task is one program, so the pipe and redirect live in an
	// explicitly named bash.
	task := fmt.Sprintf(`bash -c 'env | grep -E "^DATAWATCH_(SESSION_ID|BASE_URL|RESULT_TOKEN)=" > %s'`, out)

	projectDir := t.TempDir()
	sess, err := mgr.Start(context.Background(), task, "", projectDir, &StartOptions{Subprocess: true, OneShot: true})
	if err != nil {
		t.Fatalf("Start(subprocess): %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	var cur *Session
	for time.Now().Before(deadline) {
		if s, ok := mgr.store.Get(sess.FullID); ok && (s.State == StateComplete || s.State == StateFailed) {
			cur = s
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if cur == nil || cur.State != StateComplete {
		if cur != nil {
			t.Fatalf("subprocess session did not complete cleanly: state=%s", cur.State)
		}
		t.Fatal("timed out waiting for subprocess session to finish")
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("child did not write its environment: %v", err)
	}
	env := envMap(strings.Split(strings.TrimSpace(string(raw)), "\n"))

	if env["DATAWATCH_SESSION_ID"] != sess.FullID {
		t.Errorf("child DATAWATCH_SESSION_ID = %q, want %q", env["DATAWATCH_SESSION_ID"], sess.FullID)
	}
	if env["DATAWATCH_BASE_URL"] != "http://127.0.0.1:18176" {
		t.Errorf("child DATAWATCH_BASE_URL = %q, want http://127.0.0.1:18176", env["DATAWATCH_BASE_URL"])
	}
	tok := env["DATAWATCH_RESULT_TOKEN"]
	if tok == "" {
		t.Fatal("child environment has no DATAWATCH_RESULT_TOKEN")
	}
	if got := tokenStore.SessionIDForToken(tok); got != sess.FullID {
		t.Errorf("child's token resolves to session %q, want %q", got, sess.FullID)
	}
	caps, ok := tokenStore.CapsForToken(tok)
	if !ok || len(caps) != 1 || caps[0] != federation.CapSessionsResultWrite {
		t.Errorf("child's token caps = %v (ok=%v), want exactly [%s]", caps, ok, federation.CapSessionsResultWrite)
	}
}
