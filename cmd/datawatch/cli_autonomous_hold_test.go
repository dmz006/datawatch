package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// holdCLICall is one request the fake daemon saw.
type holdCLICall struct {
	method, path string
	body         map[string]any
	rawBody      string
}

// runHoldCLI runs `autonomous <args...>` against a fake daemon that answers
// with status and reply, and returns the one request it saw.
func runHoldCLI(t *testing.T, status int, reply string, args ...string) (holdCLICall, error) {
	t.Helper()
	var call holdCLICall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		call = holdCLICall{method: r.Method, path: r.URL.Path, rawBody: string(b)}
		if len(b) > 0 {
			if err := json.Unmarshal(b, &call.body); err != nil {
				t.Errorf("request body is not JSON: %q", b)
			}
		}
		if status != http.StatusOK {
			http.Error(w, reply, status)
			return
		}
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()
	old := serverURL
	serverURL = srv.URL
	defer func() { serverURL = old }()

	cmd := newAutonomousCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return call, cmd.Execute()
}

func TestHoldCLICommandsBuildRequests(t *testing.T) {
	t.Run("prd-hold with every flag", func(t *testing.T) {
		call, err := runHoldCLI(t, http.StatusOK, `{"status":"held"}`,
			"prd-hold", "aaaa0001", "--reason", "  wait for the release ", "--until", "72h",
			"--condition", "until after v10", "--actor", "alex")
		if err != nil {
			t.Fatal(err)
		}
		if call.method != http.MethodPost || call.path != "/api/autonomous/prds/aaaa0001/hold" {
			t.Fatalf("sent %s %s", call.method, call.path)
		}
		want := map[string]any{"reason": "wait for the release", "condition": "until after v10", "until": "72h", "actor": "alex"}
		if !reflect.DeepEqual(call.body, want) {
			t.Errorf("body = %v, want %v", call.body, want)
		}
	})

	t.Run("prd-hold defaults the actor and leaves optional fields empty", func(t *testing.T) {
		call, err := runHoldCLI(t, http.StatusOK, `{}`, "prd-hold", "aaaa0001", "--reason", "parked")
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"reason": "parked", "condition": "", "until": "", "actor": "operator"}
		if !reflect.DeepEqual(call.body, want) {
			t.Errorf("body = %v, want %v", call.body, want)
		}
	})

	t.Run("prd-hold passes a daemon refusal back as an error", func(t *testing.T) {
		_, err := runHoldCLI(t, http.StatusBadRequest, "a hold needs a reason; say why the Automaton is waiting",
			"prd-hold", "aaaa0001")
		if err == nil || !strings.Contains(err.Error(), "needs a reason") {
			t.Fatalf("err = %v; want the daemon's refusal", err)
		}
	})

	t.Run("prd-hold needs exactly one id", func(t *testing.T) {
		if _, err := runHoldCLI(t, http.StatusOK, `{}`, "prd-hold", "--reason", "r"); err == nil {
			t.Error("no id must be refused")
		}
	})

	t.Run("prd-release-hold", func(t *testing.T) {
		call, err := runHoldCLI(t, http.StatusOK, `{"status":"blocked"}`,
			"prd-release-hold", "aaaa0001", "--note", "release shipped")
		if err != nil {
			t.Fatal(err)
		}
		if call.method != http.MethodPost || call.path != "/api/autonomous/prds/aaaa0001/release_hold" {
			t.Fatalf("sent %s %s", call.method, call.path)
		}
		want := map[string]any{"note": "release shipped", "actor": "operator"}
		if !reflect.DeepEqual(call.body, want) {
			t.Errorf("body = %v, want %v", call.body, want)
		}
	})

	t.Run("held reads the group", func(t *testing.T) {
		call, err := runHoldCLI(t, http.StatusOK, `{"count":0,"items":[]}`, "held")
		if err != nil {
			t.Fatal(err)
		}
		if call.method != http.MethodGet || call.path != "/api/autonomous/held" || call.rawBody != "" {
			t.Fatalf("sent %s %s body=%q", call.method, call.path, call.rawBody)
		}
		if _, err := runHoldCLI(t, http.StatusOK, `{}`, "held", "extra"); err == nil {
			t.Error("held takes no arguments")
		}
	})

	t.Run("held-migrate real run with repeated ids", func(t *testing.T) {
		call, err := runHoldCLI(t, http.StatusOK, `{"held":2}`,
			"held-migrate", "--id", "aaaa0001", "--id", "bbbb0002", "--reason", "parked",
			"--until", "2026-12-01", "--condition", "after the release")
		if err != nil {
			t.Fatal(err)
		}
		if call.method != http.MethodPost || call.path != "/api/autonomous/held/migrate" {
			t.Fatalf("sent %s %s", call.method, call.path)
		}
		want := map[string]any{
			"ids": []any{"aaaa0001", "bbbb0002"}, "reason": "parked", "until": "2026-12-01",
			"condition": "after the release", "actor": "operator",
		}
		if !reflect.DeepEqual(call.body, want) {
			t.Errorf("body = %v, want %v", call.body, want)
		}
	})

	t.Run("held-migrate dry run and the default", func(t *testing.T) {
		call, err := runHoldCLI(t, http.StatusOK, `{"dry_run":true}`, "held-migrate", "--dry-run")
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"dry_run": true, "actor": "operator"}
		if !reflect.DeepEqual(call.body, want) {
			t.Errorf("dry run body = %v, want %v", call.body, want)
		}
		// No flag at all: dry_run is not sent, so the daemon's default applies.
		call, err = runHoldCLI(t, http.StatusOK, `{}`, "held-migrate")
		if err != nil {
			t.Fatal(err)
		}
		if _, sent := call.body["dry_run"]; sent {
			t.Errorf("dry_run must be left out when the flag is not set: %v", call.body)
		}
		// An explicit false is sent as false.
		call, err = runHoldCLI(t, http.StatusOK, `{}`, "held-migrate", "--dry-run=false", "--id", "x", "--reason", "r")
		if err != nil {
			t.Fatal(err)
		}
		if v, sent := call.body["dry_run"]; !sent || v != false {
			t.Errorf("--dry-run=false must send false: %v", call.body)
		}
	})

	t.Run("held-migrate turns a 409 into the daemon's plain message", func(t *testing.T) {
		_, err := runHoldCLI(t, http.StatusConflict, "held migration: a final gate is running",
			"held-migrate", "--id", "x", "--reason", "r")
		if err == nil {
			t.Fatal("a 409 must make the command fail")
		}
		if strings.Contains(err.Error(), "HTTP 409") || !strings.Contains(err.Error(), "a final gate is running") {
			t.Errorf("err = %q; want the daemon's plain message", err)
		}
		_, err = runHoldCLI(t, http.StatusBadRequest, "a real run needs a reason from the operator", "held-migrate", "--id", "x")
		if err == nil || !strings.Contains(err.Error(), "needs a reason") {
			t.Errorf("err = %v; want the daemon's refusal", err)
		}
	})

	t.Run("commands are registered under autonomous", func(t *testing.T) {
		have := map[string]bool{}
		for _, c := range newAutonomousCmd().Commands() {
			have[c.Name()] = true
		}
		for _, n := range []string{"prd-hold", "prd-release-hold", "held", "held-migrate"} {
			if !have[n] {
				t.Errorf("%s is not registered under autonomous", n)
			}
		}
	})
}
