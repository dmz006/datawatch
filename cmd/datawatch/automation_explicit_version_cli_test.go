// CLI coverage for explicit --expected-version on Automation controls: an
// explicit value (even a stale one, even 0) is posted exactly as given
// without a preceding GET and without any retry, and the daemon's 409
// surfaces verbatim as the command's error. Serves the daemon side with its
// own httptest fake (same pattern as withCaptureServer in
// cli_autonomous_subscriptions_test.go).
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const explicitVersionConflictBody = "409: version mismatch: expected version 3, current version is 4"

// explicitVersionFake counts GETs and POSTs and records each POST's path
// and body. GET /api/automations/ab12 reports version 4; every POST is
// refused with the daemon's optimistic-lock 409.
type explicitVersionFake struct {
	gets       int
	posts      int
	postPaths  []string
	postBodies []map[string]any
}

func (f *explicitVersionFake) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/automations/ab12":
			f.gets++
			_, _ = w.Write([]byte(`{"id":"ab12","name":"stale","status":"running","version":4}`))
		case r.Method == http.MethodGet:
			f.gets++
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("unexpected GET " + r.URL.Path))
		case r.Method == http.MethodPost:
			f.posts++
			f.postPaths = append(f.postPaths, r.URL.Path)
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			f.postBodies = append(f.postBodies, m)
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(explicitVersionConflictBody))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func (f *explicitVersionFake) reset() {
	f.gets = 0
	f.posts = 0
	f.postPaths = nil
	f.postBodies = nil
}

// withExplicitVersionFake points the CLI at the fake daemon, with the
// serverURL restored on cleanup.
func withExplicitVersionFake(t *testing.T, f *explicitVersionFake) {
	t.Helper()
	old := serverURL
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
}

func TestAutomationCLIExplicitStaleVersionStillRefused(t *testing.T) {
	f := &explicitVersionFake{}
	withExplicitVersionFake(t, f)

	verbs := map[string][]string{
		"insert": {"insert", "ab12", "prd-1", "--reason", "r", "--expected-version", "3"},
		"pause":  {"pause", "ab12", "--reason", "r", "--expected-version", "3"},
	}
	for verb, args := range verbs {
		t.Run(verb, func(t *testing.T) {
			f.reset()
			err := runCmd(t, newAutomationCmd(), args...)
			if err == nil {
				t.Fatal("command succeeded, want the daemon's 409 surfaced as an error")
			}
			msg := err.Error()
			if !strings.Contains(msg, "409") {
				t.Errorf("err = %q, want one containing 409", msg)
			}
			if !strings.Contains(msg, "version mismatch") {
				t.Errorf("err = %q, want one mentioning version mismatch", msg)
			}
			if f.posts != 1 {
				t.Fatalf("POST count = %d, want exactly 1 (no retry)", f.posts)
			}
			if f.gets != 0 {
				t.Fatalf("GET count = %d, want 0 (an explicit version forbids a read)", f.gets)
			}
			got := f.postBodies[0]
			ev, ok := got["expected_version"]
			if !ok {
				t.Fatalf("body %v lacks expected_version", got)
			}
			if gotEv, isNum := ev.(float64); !isNum || gotEv != 3 {
				t.Errorf("expected_version = %v, want 3 (body %v)", ev, got)
			}
		})
	}
}

func TestAutomationCLIExplicitVersionIsSentUnchanged(t *testing.T) {
	f := &explicitVersionFake{}
	withExplicitVersionFake(t, f)

	err := runCmd(t, newAutomationCmd(), "pause", "ab12", "--expected-version", "0")
	if err == nil {
		t.Fatal("daemon refused with 409, but the command reported success")
	}
	msg := err.Error()
	if !strings.Contains(msg, "409") || !strings.Contains(msg, "version mismatch") {
		t.Errorf("err = %q, want the daemon's 409 version-mismatch refusal", msg)
	}
	if f.posts != 1 {
		t.Fatalf("POST count = %d, want exactly 1", f.posts)
	}
	if f.gets != 0 {
		t.Fatalf("GET count = %d, want 0 (an explicit version forbids a read)", f.gets)
	}
	got := f.postBodies[0]
	ev, ok := got["expected_version"]
	if !ok {
		t.Fatalf("body %v must carry expected_version even at 0", got)
	}
	if gotEv, isNum := ev.(float64); !isNum || gotEv != 0 {
		t.Errorf("expected_version = %v, want the given value 0 unchanged (body %v)", ev, got)
	}
}
