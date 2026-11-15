package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/tooling"
)

// instance_mcp_wiring_test.go — unit tests for the daemon-side wiring of
// the scoped-instance-token lifecycle. No real daemon, no tmux, no LLM:
// the token minter is a fake and the .mcp.json file round-trips through
// t.TempDir() with a fake node binary (DATAWATCH_NODE_BIN test seam), so
// the tests run in any environment.

// fakeInstanceTokenMinter records every mint/revoke for assertions and
// hands back deterministic scoped tokens. It implements
// channel.InstanceTokenMinter (the interface the daemon wires to the real
// auth.SessionTokenStore in production).
type fakeInstanceTokenMinter struct {
	mintCalls  []string // owner ids, in call order
	capsArg    []string // caps of the last mint
	revokeList []string // owner ids, in call order
}

func (f *fakeInstanceTokenMinter) MintScoped(ownerID string, caps []string, _ time.Duration) (string, error) {
	f.mintCalls = append(f.mintCalls, ownerID)
	f.capsArg = caps
	return "scoped-" + ownerID, nil
}

func (f *fakeInstanceTokenMinter) Revoke(ownerID string) error {
	f.revokeList = append(f.revokeList, ownerID)
	return nil
}

// fakeInstanceLifecycle records Release() invocations.
type fakeInstanceLifecycle struct {
	releases   int
	releaseErr error
}

func (f *fakeInstanceLifecycle) Refresh() (bool, error) { return false, nil }
func (f *fakeInstanceLifecycle) Release() error {
	f.releases++
	return f.releaseErr
}

func sessWith(fullID string, st session.State) *session.Session {
	return &session.Session{FullID: fullID, State: st}
}

func TestInstanceMCPLiveSessionsCounting(t *testing.T) {
	cases := []struct {
		name     string
		sessions []*session.Session
		exclude  string
		want     int
	}{
		{"empty", nil, "", 0},
		{
			"only-ends-terminal",
			[]*session.Session{sessWith("s1", session.StateComplete)},
			"s1", 0,
		},
		{
			"ending-running-counts-as-live",
			[]*session.Session{sessWith("s1", session.StateRunning)},
			"s1", 0, // excluded, even though it is live
		},
		{
			"other-running-kept",
			[]*session.Session{
				sessWith("s1", session.StateComplete),
				sessWith("s2", session.StateRunning),
			},
			"s1", 1,
		},
		{
			"waiting-input-and-stalled-are-live",
			[]*session.Session{
				sessWith("s1", session.StateComplete),
				sessWith("s2", session.StateWaitingInput),
				sessWith("s3", session.StateStalled),
			},
			"s1", 2,
		},
		{
			"rate-limited-is-live",
			[]*session.Session{
				sessWith("s1", session.StateComplete),
				sessWith("s2", session.StateRateLimited),
			},
			"s1", 1,
		},
		{
			"terminal-non-ending-ignored",
			[]*session.Session{
				sessWith("s1", session.StateComplete),
				sessWith("s2", session.StateFailed),
				sessWith("s3", session.StateKilled),
			},
			"s1", 0,
		},
		{"shutdown-no-exclude-counts-everyone",
			[]*session.Session{
				sessWith("s1", session.StateRunning),
				sessWith("s2", session.StateComplete),
			}, "", 1},
		{"nil-entry-skipped",
			[]*session.Session{nil, sessWith("s1", session.StateRunning)},
			"s1", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := instanceMCPLiveSessions(tc.sessions, tc.exclude); got != tc.want {
				t.Errorf("instanceMCPLiveSessions = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestReleaseInstanceMCPIfIdle(t *testing.T) {
	t.Run("releases-when-no-other-live-session", func(t *testing.T) {
		l := &fakeInstanceLifecycle{}
		sessions := []*session.Session{
			sessWith("ending", session.StateComplete),
			sessWith("other", session.StateFailed),
		}
		if !releaseInstanceMCPIfIdle(l, sessions, "ending") {
			t.Fatal("want release to fire")
		}
		if l.releases != 1 {
			t.Errorf("Release calls = %d, want 1", l.releases)
		}
	})

	t.Run("keeps-token-while-another-session-is-live", func(t *testing.T) {
		l := &fakeInstanceLifecycle{}
		sessions := []*session.Session{
			sessWith("ending", session.StateComplete),
			sessWith("other", session.StateRunning),
		}
		if releaseInstanceMCPIfIdle(l, sessions, "ending") {
			t.Fatal("must not release while another session is live")
		}
		if l.releases != 0 {
			t.Errorf("Release calls = %d, want 0", l.releases)
		}
	})

	t.Run("keeps-token-while-another-waiting-input-session", func(t *testing.T) {
		l := &fakeInstanceLifecycle{}
		sessions := []*session.Session{
			sessWith("ending", session.StateFailed),
			sessWith("other", session.StateWaitingInput),
		}
		if releaseInstanceMCPIfIdle(l, sessions, "ending") {
			t.Fatal("waiting-input is a live session; token must stay")
		}
		if l.releases != 0 {
			t.Errorf("Release calls = %d, want 0", l.releases)
		}
	})

	t.Run("nil-lifecycle-is-a-noop", func(t *testing.T) {
		if releaseInstanceMCPIfIdle(nil, []*session.Session{sessWith("s", session.StateComplete)}, "s") {
			t.Fatal("nil lifecycle must not fire")
		}
	})

	t.Run("release-error-still-counts-as-called", func(t *testing.T) {
		l := &fakeInstanceLifecycle{releaseErr: os.ErrNotExist}
		if !releaseInstanceMCPIfIdle(l, nil, "") {
			t.Fatal("Release was expected to be attempted")
		}
		if l.releases != 1 {
			t.Errorf("Release calls = %d, want 1", l.releases)
		}
	})
}

func TestInstanceMCPEnvTokenIsScopedAndNeverServerToken(t *testing.T) {
	// Deterministic node path (NodePath's documented test seam) so
	// WriteInstanceMCPConfig builds its entry without a real `node`.
	t.Setenv("DATAWATCH_NODE_BIN", "/usr/bin/fake-node-for-test")

	const adminServerToken = "SUPER-SECRET-DAEMON-SERVER-TOKEN-must-never-land"
	const apiURL = "http://127.0.0.1:4444"

	// The env the daemon builds for the instance .mcp.json (mirrors the
	// wiring block in main.go): API URL + optional CA cert, and NO
	// DATAWATCH_TOKEN — the token only ever comes from the minter.
	baseEnv := map[string]string{"DATAWATCH_API_URL": apiURL, "DATAWATCH_CA_CERT": "/etc/datawatch/ca.pem"}
	caps := instanceSessionGrantCaps(nil, true)

	for family := range tooling.BackendArtifacts {
		t.Run(family, func(t *testing.T) {
			// A previous daemon version left the admin token on disk.
			legacyDir := t.TempDir()
			legacyEnv := map[string]string{
				"DATAWATCH_API_URL": apiURL,
				"DATAWATCH_TOKEN":   adminServerToken,
			}
			if _, err := channel.WriteInstanceMCPConfig(legacyDir, "channel.js", legacyEnv); err != nil {
				t.Fatalf("seed legacy .mcp.json: %v", err)
			}
			legacyRaw, err := os.ReadFile(filepath.Join(legacyDir, ".mcp.json"))
			if err != nil {
				t.Fatalf("read legacy .mcp.json: %v", err)
			}
			if !strings.Contains(string(legacyRaw), adminServerToken) {
				t.Fatalf("precondition: legacy file should carry the admin token")
			}

			// The daemon now points at a fresh data dir and refreshes the
			// instance config through the scoped-token lifecycle.
			dir := t.TempDir()
			minter := &fakeInstanceTokenMinter{}
			cfg := &channel.InstanceMCPConfig{
				DataDir:       dir,
				ChannelJSPath: "/home/dmz/.datawatch/channel/channel.js",
				BaseEnv:       baseEnv,
				Caps:          caps,
				Minter:        minter,
			}
			updated, err := cfg.Refresh()
			if err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			if !updated {
				t.Fatal("Refresh: want the file rewritten")
			}
			raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
			if err != nil {
				t.Fatalf("read .mcp.json: %v", err)
			}
			if strings.Contains(string(raw), adminServerToken) {
				t.Error("instance .mcp.json leaks the daemon's server token")
			}
			var got channel.MCPProjectConfig
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal .mcp.json: %v", err)
			}
			dw, ok := got.MCPServers[channel.ProjectMCPEntryName]
			if !ok {
				t.Fatalf("missing %q entry: %+v", channel.ProjectMCPEntryName, got.MCPServers)
			}
			if tok := dw.Env["DATAWATCH_TOKEN"]; tok != "scoped-"+channel.InstanceTokenOwnerID {
				t.Errorf("DATAWATCH_TOKEN = %q, want the minted scoped token", tok)
			}
			if dw.Env["DATAWATCH_API_URL"] != apiURL {
				t.Errorf("DATAWATCH_API_URL = %q, want %q", dw.Env["DATAWATCH_API_URL"], apiURL)
			}
			if dw.Env["DATAWATCH_CA_CERT"] != "/etc/datawatch/ca.pem" {
				t.Errorf("DATAWATCH_CA_CERT = %q, want the base env value", dw.Env["DATAWATCH_CA_CERT"])
			}

			// The daemon never passed the admin token through baseEnv: the
			// minted caps are the session grant, and the owner is the
			// reserved instance id.
			if len(minter.mintCalls) == 0 || minter.mintCalls[0] != channel.InstanceTokenOwnerID {
				t.Errorf("mint owner = %v, want %q", minter.mintCalls, channel.InstanceTokenOwnerID)
			}
			if sortedJoin(minter.capsArg) != sortedJoin(caps) {
				t.Errorf("mint caps = %v, want %v", minter.capsArg, caps)
			}

			// Release (no live sessions left, or daemon shutdown): the scoped
			// token is revoked and the entry removed — nothing to leak from
			// our data dir afterwards, and the prior admin token is gone too.
			if err := cfg.Release(); err != nil {
				t.Fatalf("Release: %v", err)
			}
			if len(minter.revokeList) == 0 || minter.revokeList[len(minter.revokeList)-1] != channel.InstanceTokenOwnerID {
				t.Errorf("revoke list = %v, want it to end with %q", minter.revokeList, channel.InstanceTokenOwnerID)
			}
			if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); err == nil {
				rawAfter, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
				if strings.Contains(string(rawAfter), adminServerToken) || strings.Contains(string(rawAfter), "scoped-"+channel.InstanceTokenOwnerID) {
					t.Error("credentials still present in .mcp.json after Release")
				}
			}
		})
	}
}

func joinCaps(caps []string) string {
	out := ""
	for _, c := range caps {
		out += c + "\x00"
	}
	return out
}

// sortedJoin is like joinCaps but order-insensitive: federation.Resolve
// builds its slice from a map iteration, so order is not stable.
func sortedJoin(caps []string) string {
	out := capsetJoin(capset(caps))
	return out
}

func capset(caps []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

func capsetJoin(caps []string) string { return joinCaps(caps) }

func filterOut(keep, exclude []string) []string {
	out := make([]string, 0, len(keep))
	for _, k := range keep {
		if !has(exclude, k) {
			out = append(out, k)
		}
	}
	return out
}

func TestInstanceSessionGrantCaps(t *testing.T) {
	sessionDefault := federation.Resolve(federation.SessionDefaultGrantCaps(false), nil)
	selfCfg := federation.Resolve([]string{federation.SelfConfigGroup}, nil)
	// The self-config group overlaps session-default on some caps (e.g.
	// usage:write is granted by both); only the caps OUTSIDE
	// session-default can be used to tell the alias toggle apart.
	selfCfgOnly := filterOut(selfCfg, sessionDefault)

	t.Run("explicit-grant-used-as-written", func(t *testing.T) {
		explicit := []string{federation.CapSessionsList}
		got := instanceSessionGrantCaps(explicit, true)
		want := federation.Resolve(explicit, nil)
		if sortedJoin(got) != sortedJoin(want) {
			t.Errorf("explicit grant: got %v, want %v", got, want)
		}
		for _, c := range selfCfgOnly {
			if has(got, c) {
				t.Errorf("explicit grant unexpectedly carries self-config-only cap %q", c)
			}
		}
	})

	t.Run("default-grant-with-self-config-enabled", func(t *testing.T) {
		got := instanceSessionGrantCaps(nil, true)
		for _, c := range sessionDefault {
			if !has(got, c) {
				t.Errorf("default grant missing cap %q", c)
			}
		}
		for _, c := range selfCfg {
			if !has(got, c) {
				t.Errorf("self-config-enabled default grant missing cap %q", c)
			}
		}
	})

	t.Run("default-grant-without-self-config", func(t *testing.T) {
		got := instanceSessionGrantCaps(nil, false)
		if sortedJoin(got) != sortedJoin(sessionDefault) {
			t.Errorf("default grant = %v, want %v", got, sessionDefault)
		}
		for _, c := range selfCfgOnly {
			if has(got, c) {
				t.Errorf("default grant unexpectedly carries self-config-only cap %q", c)
			}
		}
	})
}

func has(caps []string, want string) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}
