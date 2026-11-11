// Observer pull protocol — CLI surface tests (flag parsing + table
// output). Each test serves the daemon side with an httptest server
// overriding the package-level serverURL (same pattern as
// cli_autonomous_subscriptions_test.go).
//
// Cobra executes through the root (child.Execute() delegates to
// Root().ExecuteC()), so command-path args are always set on the tree
// top (observer/peer/pull/federation).

package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runRoot executes sub (the command path under root) and returns the
// captured out/err. Run errors fail the test with the captured output.
func runRoot(t *testing.T, root *cobra.Command, sub ...string) *bytes.Buffer {
	t.Helper()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{}, sub...))
	if err := root.Execute(); err != nil {
		t.Fatalf("execute %v: %v (output: %s)", sub, err, out)
	}
	return out
}

const observerPeersFixture = `{
  "peers": [
    {
      "name": "push-a",
      "shape": "B",
      "version": "4.0.0",
      "last_push_at": "2026-05-01T00:00:00Z",
      "source": "push",
      "shadow": false,
      "pull": {"enabled": true, "stale": false, "stale_reason": "none", "interval_s": 60, "last_success_at": "2026-05-01T00:00:00Z"}
    },
    {
      "name": "pull-b",
      "source": "pull",
      "shadow": false,
      "last_push_at": "2026-05-01T00:10:00Z",
      "pull": {"enabled": true, "stale": true, "stale_reason": "timeout", "interval_s": 60, "last_attempt_at": "2026-05-01T00:20:00Z"}
    },
    {
      "name": "shadow-c",
      "source": "pull",
      "shadow": true,
      "pull": {"enabled": false, "stale": false, "stale_reason": "none", "interval_s": 0}
    }
  ]
}`

const observerPullFixture = `{
  "peers": [
    {
      "name": "pull-b",
      "source": "pull",
      "shadow": false,
      "stale": true,
      "stale_reason": "timeout",
      "last_success_at": "2026-05-01T00:10:00Z",
      "interval_s": 60,
      "timeout_s": 5
    },
    {
      "name": "pull-c",
      "source": "pull",
      "shadow": true,
      "stale": false,
      "stale_reason": "none",
      "last_success_at": null,
      "interval_s": 120,
      "timeout_s": 10
    }
  ]
}`

// TestObserverPullCLI_PeerListTable covers `observer peer list` table
// rendering: the new SOURCE column (push / pull / shadow) and the STALE
// column (stale / ok when the row's pull is enabled, "-" otherwise).
func TestObserverPullCLI_PeerListTable(t *testing.T) {
	c := &captureDaemon{replyBody: observerPeersFixture}
	withCaptureServer(t, c)

	got := runRoot(t, newObserverPeerCmd(), "list").String()

	if c.gotMethod != http.MethodGet || c.gotPath != "/api/observer/peers" {
		t.Fatalf("route = %s %s, want GET /api/observer/peers", c.gotMethod, c.gotPath)
	}
	for _, want := range []string{"NAME", "SHAPE", "VERSION", "SOURCE", "STALE", "LAST_PUSH"} {
		if !strings.Contains(got, want) {
			t.Errorf("header missing %q in:\n%s", want, got)
		}
	}
	// push-a — push source, pull enabled and fresh ("ok" state).
	for _, want := range []string{"B", "4.0.0", "push", "ok"} {
		if !rowContains(got, "push-a", want) {
			t.Errorf("push-a row missing %q:\n%s", want, got)
		}
	}
	// pull-b — pull source, stale (timeout).
	if !rowContains(got, "pull-b", "pull") || !rowContains(got, "pull-b", "stale") {
		t.Errorf("pull-b row should read source=pull stale=stale:\n%s", got)
	}
	// shadow-c — shadow source column.
	if !rowContains(got, "shadow-c", "shadow") {
		t.Errorf("shadow-c row should read source=shadow:\n%s", got)
	}
}

// TestObserverPullCLI_PullStatusTable covers `observer pull status` —
// the new subcommand rendering GET /api/observer/pull as a table with
// STALE_REASON, SHADOW and the effective interval/timeout columns.
func TestObserverPullCLI_PullStatusTable(t *testing.T) {
	c := &captureDaemon{replyBody: observerPullFixture}
	withCaptureServer(t, c)

	got := runRoot(t, newObserverPullCmd(), "status").String()

	if c.gotMethod != http.MethodGet || c.gotPath != "/api/observer/pull" {
		t.Fatalf("route = %s %s, want GET /api/observer/pull", c.gotMethod, c.gotPath)
	}
	for _, want := range []string{"NAME", "SOURCE", "SHADOW", "STALE", "STALE_REASON", "LAST_SUCCESS", "INTERVAL_S", "TIMEOUT_S"} {
		if !strings.Contains(got, want) {
			t.Errorf("header missing %q in:\n%s", want, got)
		}
	}
	// pull-b — stale with reason timeout, effective 60s/5s.
	for _, want := range []string{"timeout", "60", "5", "stale"} {
		if !rowContains(got, "pull-b", want) {
			t.Errorf("pull-b row missing %q:\n%s", want, got)
		}
	}
	// pull-c — shadow=true, never succeeded.
	if !rowContains(got, "pull-c", "true") {
		t.Errorf("pull-c row should read shadow=true:\n%s", got)
	}
	if !rowContains(got, "pull-c", "ok") {
		t.Errorf("pull-c row should read stale=ok:\n%s", got)
	}
}

// TestObserverPullCLI_PullStatusJSONFlag covers the --json flag path of
// `observer pull status`: same GET endpoint, raw body, no table.
func TestObserverPullCLI_PullStatusJSONFlag(t *testing.T) {
	c := &captureDaemon{replyBody: observerPullFixture}
	withCaptureServer(t, c)

	runRoot(t, newObserverPullCmd(), "status", "--json")

	if c.gotMethod != http.MethodGet || c.gotPath != "/api/observer/pull" {
		t.Fatalf("route = %s %s, want GET /api/observer/pull", c.gotMethod, c.gotPath)
	}
}

// TestObserverPullCLI_PeerListJSONFlag covers the --json flag path of
// `observer peer list`.
func TestObserverPullCLI_PeerListJSONFlag(t *testing.T) {
	c := &captureDaemon{replyBody: observerPeersFixture}
	withCaptureServer(t, c)

	runRoot(t, newObserverPeerCmd(), "list", "--json")

	if c.gotMethod != http.MethodGet || c.gotPath != "/api/observer/peers" {
		t.Fatalf("route = %s %s, want GET /api/observer/peers", c.gotMethod, c.gotPath)
	}
}

// TestObserverPullCLI_FederationAddObserverPullFlags covers the new
// --observer-pull / --observer-pull-interval / --observer-pull-timeout
// flags on `federation peer add`: present → sent under the documented
// JSON field names.
func TestObserverPullCLI_FederationAddObserverPullFlags(t *testing.T) {
	c := &captureDaemon{replyBody: `{"name":"pull-b"}`}
	withCaptureServer(t, c)

	runRoot(t, newFederationCmd(), "peer", "add",
		"--name", "pull-b",
		"--url", "http://pull-b:8080",
		"--observer-pull",
		"--observer-pull-interval", "60",
		"--observer-pull-timeout", "5",
	)

	if c.gotMethod != http.MethodPost || c.gotPath != "/api/federation/peers" {
		t.Fatalf("route = %s %s, want POST /api/federation/peers", c.gotMethod, c.gotPath)
	}
	body := c.body(t)
	if v, ok := body["observer_pull"].(bool); !ok || !v {
		t.Errorf("observer_pull = %v, want true (body: %s)", body["observer_pull"], c.gotRaw)
	}
	if v := body["observer_pull_interval_s"]; asFloat(v) != 60 {
		t.Errorf("observer_pull_interval_s = %v, want 60", v)
	}
	if v := body["observer_pull_timeout_s"]; asFloat(v) != 5 {
		t.Errorf("observer_pull_timeout_s = %v, want 5", v)
	}
}

// TestObserverPullCLI_FederationAddDefaultOff covers the default: with
// no --observer-pull* flags the body must not mention them at all, so
// the server-side defaults (off) apply.
func TestObserverPullCLI_FederationAddDefaultOff(t *testing.T) {
	c := &captureDaemon{replyBody: `{"name":"push-a"}`}
	withCaptureServer(t, c)

	runRoot(t, newFederationCmd(), "peer", "add",
		"--name", "push-a",
		"--url", "http://push-a:8080",
	)

	body := c.body(t)
	for _, k := range []string{"observer_pull", "observer_pull_interval_s", "observer_pull_timeout_s"} {
		if _, present := body[k]; present {
			t.Errorf("body should not contain %q when the flag is unset (body: %s)", k, c.gotRaw)
		}
	}
}

// TestObserverPullCLI_FederationUpdateObserverPullFlags covers the
// Changed()-gated flags on `federation peer update`: explicitly passed
// values are sent (--observer-pull=false turns pull off; interval 0
// resets to the global default), while a never-passed flag is absent so
// the server-side partial PUT leaves it unchanged.
func TestObserverPullCLI_FederationUpdateObserverPullFlags(t *testing.T) {
	c := &captureDaemon{replyBody: `{}`}
	withCaptureServer(t, c)

	runRoot(t, newFederationCmd(), "peer", "update", "pull-b",
		"--observer-pull=false",
		"--observer-pull-interval", "0",
	)

	if c.gotMethod != http.MethodPut || c.gotPath != "/api/federation/peers/pull-b" {
		t.Fatalf("route = %s %s, want PUT /api/federation/peers/pull-b", c.gotMethod, c.gotPath)
	}
	body := c.body(t)
	if v, ok := body["observer_pull"].(bool); !ok || v {
		t.Errorf("observer_pull = %v, want false (turning pull off)", body["observer_pull"])
	}
	if v := body["observer_pull_interval_s"]; asFloat(v) != 0 {
		t.Errorf("observer_pull_interval_s = %v, want 0 (reset to global default)", v)
	}
	if _, present := body["observer_pull_timeout_s"]; present {
		t.Errorf("--observer-pull-timeout was not passed; key must be absent: %s", c.gotRaw)
	}
}

// TestObserverPullCLI_PullBareShowsListHelp covers running `observer
// pull` with no subcommand: it resolves to help listing the status
// subcommand rather than erroring.
func TestObserverPullCLI_PullBareShowsListHelp(t *testing.T) {
	out := runRoot(t, newObserverPullCmd())
	if !strings.Contains(out.String(), "status") {
		t.Errorf("bare `pull` help should mention the status subcommand:\n%s", out)
	}
}

// ── table assertion helpers ────────────────────────────────────────────────

// rowContains reports whether the table line containing name also
// contains want.
func rowContains(out, name, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, name) && strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// asFloat normalises a JSON number cell to float64.
func asFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}
