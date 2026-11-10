package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// wpFake records the PUT /api/config bodies a worker profile set sends.
type wpFake struct {
	mu   sync.Mutex
	puts []map[string]any
}

// withWPFake points the CLI at a fake daemon through serverURL, restoring it
// after the test, and returns the recorder.
func withWPFake(t *testing.T) *wpFake {
	t.Helper()
	f := &wpFake{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err == nil && r.Method == http.MethodPut {
			f.mu.Lock()
			f.puts = append(f.puts, m)
			f.mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	return f
}

func (f *wpFake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.puts)
}

func (f *wpFake) last(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.puts) == 0 {
		t.Fatalf("no PUT /api/config request was sent")
	}
	return f.puts[len(f.puts)-1]
}

const wpValidProfiles = `{"mine":{"context":"minimal","mcp":{"mode":"none"},"tools":{"mode":"all"}}}`

// A valid set reaches the daemon as one PUT carrying the matching autonomous
// key, and the command returns nil (exit zero).
func TestWorkerProfileConfigSet_ValidSet(t *testing.T) {
	t.Run("global profile name", func(t *testing.T) {
		f := withWPFake(t)
		if err := runConfigSet(config.DefaultConfig(), "worker_profile", "minimal"); err != nil {
			t.Fatalf("valid set refused: %v", err)
		}
		if f.count() != 1 {
			t.Fatalf("want 1 PUT, got %d", f.count())
		}
		if got := f.last(t)["autonomous.worker_profile"]; got != "minimal" {
			t.Fatalf("autonomous.worker_profile = %#v, want \"minimal\"", got)
		}
	})

	t.Run("user profile table", func(t *testing.T) {
		f := withWPFake(t)
		if err := runConfigSet(config.DefaultConfig(), "worker_profiles", wpValidProfiles); err != nil {
			t.Fatalf("valid set refused: %v", err)
		}
		tbl, ok := f.last(t)["autonomous.worker_profiles"].(map[string]any)
		if !ok {
			t.Fatalf("autonomous.worker_profiles is not an object: %v", f.last(t))
		}
		if _, ok := tbl["mine"]; !ok {
			t.Fatalf("profile table lacks \"mine\": %v", tbl)
		}
	})

	t.Run("role defaults keep the kind map", func(t *testing.T) {
		f := withWPFake(t)
		cfg := config.DefaultConfig()
		cfg.Autonomous.WorkerProfileDefaults.Kind = map[string]string{"docs": "minimal"}
		if err := runConfigSet(cfg, "worker_profile_defaults.role", `{"worker":"full"}`); err != nil {
			t.Fatalf("valid set refused: %v", err)
		}
		d, ok := f.last(t)["autonomous.worker_profile_defaults"].(map[string]any)
		if !ok {
			t.Fatalf("autonomous.worker_profile_defaults is not an object: %v", f.last(t))
		}
		role, _ := d["role"].(map[string]any)
		if role["worker"] != "full" {
			t.Fatalf("role.worker = %v, want full", role["worker"])
		}
		kind, _ := d["kind"].(map[string]any)
		if kind["docs"] != "minimal" {
			t.Fatalf("kind map not carried over: %v", d["kind"])
		}
	})
}

// An invalid value is refused before anything is sent: the call returns the
// refusal reason (which makes the command exit non-zero) and no PUT is made.
func TestWorkerProfileConfigSet_InvalidRefused(t *testing.T) {
	cases := []struct {
		name, key, value, wantInErr string
	}{
		{"unknown profile name", "worker_profile", "nope", "nope"},
		{"built-in name reused as a user profile", "worker_profiles", `{"full":{"context":"minimal","mcp":{"mode":"none"},"tools":{"mode":"all"}}}`, "built-in"},
		{"profile fails validation", "worker_profiles", `{"bad":{"context":"huge","mcp":{"mode":"none"},"tools":{"mode":"all"}}}`, "context"},
		{"not a JSON object", "worker_profiles", "nope", "JSON object"},
		{"unknown role key", "worker_profile_defaults.role", `{"janitor":"minimal"}`, "janitor"},
		{"defaults point at an unknown profile", "worker_profile_defaults.kind", `{"docs":"ghost"}`, "ghost"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := withWPFake(t)
			err := runConfigSet(config.DefaultConfig(), c.key, c.value)
			if err == nil {
				t.Fatalf("invalid set accepted; want a non-zero exit")
			}
			if !strings.Contains(err.Error(), c.wantInErr) {
				t.Fatalf("refusal %q does not name %q", err.Error(), c.wantInErr)
			}
			if f.count() != 0 {
				t.Fatalf("refused value was still sent (%d PUT)", f.count())
			}
		})
	}
}

// An unknown key that belongs to the worker profile family is refused with a
// non-zero result, and nothing is sent.
func TestWorkerProfileConfigSet_UnknownKey(t *testing.T) {
	f := withWPFake(t)
	err := runConfigSet(config.DefaultConfig(), "worker_profile_bogus", "minimal")
	if err == nil {
		t.Fatalf("unknown key accepted; want a non-zero exit")
	}
	if !strings.Contains(err.Error(), "unknown worker profile key") {
		t.Fatalf("error %q does not say the key is unknown", err.Error())
	}
	if f.count() != 0 {
		t.Fatalf("unknown key was still sent (%d PUT)", f.count())
	}
}

// config get reads the same keys from the autonomous block of GET /api/config.
func TestWorkerProfileGetKey(t *testing.T) {
	cases := map[string]string{
		"worker_profile":               "autonomous.worker_profile",
		"worker_profiles":              "autonomous.worker_profiles",
		"worker_profile_defaults.role": "autonomous.worker_profile_defaults.role",
		"worker_profile_defaults.kind": "autonomous.worker_profile_defaults.kind",
		"worker_profile_bogus":         "worker_profile_bogus",
		"session.llm_backend":          "session.llm_backend",
	}
	for in, want := range cases {
		if got := workerProfileGetKey(in); got != want {
			t.Errorf("workerProfileGetKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// The four accepted worker profile keys print the profile in force on read;
// nothing else does, and an unknown worker_profile key does not.
func TestWorkerProfileInForceKey(t *testing.T) {
	for _, k := range []string{"worker_profile", "worker_profiles", "worker_profile_defaults.role", "worker_profile_defaults.kind"} {
		if !isWorkerProfileInForceKey(k) {
			t.Errorf("isWorkerProfileInForceKey(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"worker_profile_other", "session.max_sessions", "autonomous.worker_profile"} {
		if isWorkerProfileInForceKey(k) {
			t.Errorf("isWorkerProfileInForceKey(%q) = true, want false", k)
		}
	}
}

// workerProfileInForceLines prints a header then one indented line per role and
// task kind, matching workerprofile.InForce over the same config body.
func TestWorkerProfileInForceLines(t *testing.T) {
	body := []byte(`{"autonomous":{"worker_profile":"","worker_profiles":{},` +
		`"worker_profile_defaults":{"role":{"worker":"full"},"kind":{"note":"digest"}}}}`)
	lines, err := workerProfileInForceLines(body)
	if err != nil {
		t.Fatalf("workerProfileInForceLines: %v", err)
	}
	if len(lines) != 1+3+4 {
		t.Fatalf("got %d lines, want 8 (header, 3 roles, 4 kinds): %q", len(lines), lines)
	}
	if lines[0] != "profile in force:" {
		t.Errorf("header = %q", lines[0])
	}
	want := map[string]bool{
		"  role worker profile=full tier=role": false,
		"  kind note profile=digest tier=kind": false,
	}
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, "  role ") && !strings.HasPrefix(l, "  kind ") {
			t.Errorf("line %q is not indented scope line", l)
		}
		if _, ok := want[l]; ok {
			want[l] = true
		}
	}
	for l, seen := range want {
		if !seen {
			t.Errorf("missing line %q in %q", l, lines)
		}
	}
}

// A body that is not JSON is an error, so the read fails instead of printing a
// block built from nothing.
func TestWorkerProfileInForceLines_BadBody(t *testing.T) {
	if _, err := workerProfileInForceLines([]byte("not json")); err == nil {
		t.Fatal("want an error for a non-JSON config body")
	}
}

// workerProfileInForceReader reads GET /api/config from the daemon with the
// bearer token and builds the in-force entries from the live values.
func TestWorkerProfileInForceReader(t *testing.T) {
	var gotAuth, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotMethod = r.Header.Get("Authorization"), r.Method
		w.Write([]byte(`{"autonomous":{"worker_profile":"minimal","worker_profiles":{},` + //nolint:errcheck
			`"worker_profile_defaults":{"role":{},"kind":{}}}}`))
	}))
	defer srv.Close()
	cfg := &config.Config{}
	cfg.Server.Token = "tok"
	pointServerAt(t, srv.URL)
	entries, err := workerProfileInForceReader(cfg)()
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	if gotMethod != http.MethodGet || gotAuth != "Bearer tok" {
		t.Errorf("request method=%q auth=%q, want GET with bearer token", gotMethod, gotAuth)
	}
	if len(entries) == 0 || entries[0].Profile != "minimal" || entries[0].Tier != "daemon" {
		t.Errorf("entries[0] = %+v, want the global profile minimal at tier daemon", entries[0])
	}
}

// A daemon that refuses the read makes the reader fail with the HTTP status.
func TestWorkerProfileInForceReader_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	pointServerAt(t, srv.URL)
	cfg := &config.Config{}
	if _, err := workerProfileInForceReader(cfg)(); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v, want HTTP 500", err)
	}
}

// pointServerAt aims the CLI at a test server for the rest of the test.
func pointServerAt(t *testing.T, url string) {
	t.Helper()
	old := serverURL
	serverURL = url
	t.Cleanup(func() { serverURL = old })
}
