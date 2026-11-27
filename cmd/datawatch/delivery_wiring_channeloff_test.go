package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	authpkg "github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/llm/backends/pi"
	"github.com/dmz006/datawatch/internal/session"
)

// With Claude's channel off, an ended pi session loses what its launch gave it:
// its token stops working and its credential file is removed. Another
// session keeps both.
func TestReqWireChannelOffEndCleansUpPi(t *testing.T) {
	cfg := deliveryTestConfig(t)
	store, err := authpkg.NewSessionTokenStore(filepath.Join(cfg.DataDir, "session_tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	pre := piPreLaunch{cfg: cfg, wire: wirePiDelivery,
		mint:   func(id string) (string, error) { return store.Mint(id, []string{"queue:write"}) },
		revoke: store.Revoke,
		setEnv: func(tmux, key, value string) error {
			if key == pi.DeliveryEnv {
				env[tmux] = value
			}
			return nil
		}}
	hooks := &fakeHooks{}
	deliveryTestWiring().hooksWithoutChannel(hooks, pre)
	ended := launchMCPSession("aa01", "pi", t.TempDir(), false)
	other := launchMCPSession("aa02", "pi", t.TempDir(), false)
	hooks.pre(ended)
	hooks.pre(other)
	tokenOf := func(s *session.Session) string {
		t.Helper()
		data, err := os.ReadFile(env[s.TmuxSession])
		if err != nil {
			t.Fatalf("no credential: %v", err)
		}
		_, rest, _ := strings.Cut(string(data), `"token":`)
		_, rest, _ = strings.Cut(rest, `"`)
		tok, _, _ := strings.Cut(rest, `"`)
		if store.SessionIDForToken(tok) != s.FullID {
			t.Fatalf("%s: its token is not its own", s.FullID)
		}
		return tok
	}
	endedToken, otherToken := tokenOf(ended), tokenOf(other)

	hooks.end(ended)

	if owner := store.SessionIDForToken(endedToken); owner != "" {
		t.Errorf("the ended session's token still works for %q", owner)
	}
	if _, err := os.Stat(env[ended.TmuxSession]); !os.IsNotExist(err) {
		t.Errorf("the ended session keeps its credential file: %v", err)
	}
	if store.SessionIDForToken(otherToken) != other.FullID {
		t.Error("another session's token was revoked")
	}
	if _, err := os.Stat(env[other.TmuxSession]); err != nil {
		t.Errorf("another session's credential file was removed: %v", err)
	}

	// A session of another family is left as it was: this hook gave it nothing.
	shell, err := store.Mint("host1-shell", []string{"sessions:result_write"})
	if err != nil {
		t.Fatal(err)
	}
	hooks.end(&session.Session{FullID: "host1-shell", BackendFamily: "shell"})
	if store.SessionIDForToken(shell) != "host1-shell" {
		t.Error("a session this hook did not wire lost its token")
	}
}

// The daemon hands the hook the means to revoke.
func TestReqWireDaemonLetsPiRevoke(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "piLaunch.revoke = sessionTokenStore.Revoke") {
		t.Error("main.go gives piLaunch no revoke")
	}
}
