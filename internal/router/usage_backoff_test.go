// BL428 Phase 4 — comm-channel usage_backoff verbs.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestParse_UsageBackoff_Status(t *testing.T) {
	for _, in := range []string{"usage_backoff", "usage_backoff status", "usage-backoff", "USAGE_BACKOFF Status"} {
		c := Parse(in)
		if c.Type != CmdUsageBackoff || c.UsageBackoffVerb != "status" {
			t.Errorf("Parse(%q) = %+v", in, c)
		}
	}
}

func TestParse_UsageBackoff_Set(t *testing.T) {
	c := Parse("usage_backoff set claude-code 45 weekly cap hit")
	if c.Type != CmdUsageBackoff || c.UsageBackoffVerb != "set" {
		t.Fatalf("got %+v", c)
	}
	if c.UsageBackoffBackend != "claude-code" || c.UsageBackoffMinutes != 45 || c.UsageBackoffUntil != "" {
		t.Errorf("fields = %+v", c)
	}
	if c.UsageBackoffReason != "weekly cap hit" {
		t.Errorf("reason = %q", c.UsageBackoffReason)
	}

	u := Parse("usage_backoff set pi 2026-10-23T06:00:00Z")
	if u.UsageBackoffUntil != "2026-10-23T06:00:00Z" || u.UsageBackoffMinutes != 0 {
		t.Errorf("until form = %+v", u)
	}

	// Missing/unusable deadline leaves the deadline zero so the handler
	// answers with usage instead of guessing.
	for _, in := range []string{"usage_backoff set", "usage_backoff set pi", "usage_backoff set pi soon"} {
		c := Parse(in)
		if c.Type != CmdUsageBackoff || c.UsageBackoffVerb != "set" || c.UsageBackoffMinutes != 0 || c.UsageBackoffUntil != "" {
			t.Errorf("Parse(%q) = %+v", in, c)
		}
	}
}

func TestParse_UsageBackoff_Clear(t *testing.T) {
	cases := map[string]string{
		"usage_backoff clear":             "",
		"usage_backoff clear all":         "",
		"usage_backoff clear ALL":         "",
		"usage_backoff clear claude-code": "claude-code",
	}
	for in, want := range cases {
		c := Parse(in)
		if c.Type != CmdUsageBackoff || c.UsageBackoffVerb != "clear" || c.UsageBackoffBackend != want {
			t.Errorf("Parse(%q) = %+v, want backend %q", in, c, want)
		}
	}
}

func TestParse_UsageBackoff_UnknownVerb(t *testing.T) {
	c := Parse("usage_backoff frobnicate")
	if c.Type != CmdUsageBackoff || c.UsageBackoffVerb != "invalid" {
		t.Errorf("got %+v", c)
	}
}

func TestParse_UsageBackoff_DoesNotCaptureOtherCommands(t *testing.T) {
	if c := Parse("usage_backoffs"); c.Type == CmdUsageBackoff {
		t.Errorf("a longer word must not parse as usage_backoff: %+v", c)
	}
}

// fakeUsageBackoffREST is a stateful stand-in for /api/usage-backoff that
// records the requests the comm verbs make.
type fakeUsageBackoffREST struct {
	mu      sync.Mutex
	active  map[string]map[string]any
	methods []string
	bodies  []map[string]any
}

func (f *fakeUsageBackoffREST) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	f.methods = append(f.methods, r.Method)
	f.bodies = append(f.bodies, body)
	switch r.Method {
	case http.MethodPost:
		b, _ := body["backend"].(string)
		f.active[b] = body
	case http.MethodDelete:
		if b, _ := body["backend"].(string); b != "" {
			delete(f.active, b)
		} else {
			f.active = map[string]map[string]any{}
		}
	}
	list := []map[string]any{}
	for _, v := range f.active {
		list = append(list, v)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"active": list})
}

func TestUsageBackoffComm_RoundTrip(t *testing.T) {
	fake := &fakeUsageBackoffREST{active: map[string]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/usage-backoff", fake.handler)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r := loopbackRouter(t, srv)

	send := func(msg string) string { return strings.Join(r.HandleTestMessage(msg), "\n") }

	out := send("usage_backoff set claude-code 45 weekly cap")
	if !strings.Contains(out, "usage_backoff set") || !strings.Contains(out, "claude-code") {
		t.Errorf("set reply = %q", out)
	}
	if len(fake.bodies) != 1 || fake.methods[0] != http.MethodPost {
		t.Fatalf("set request = %v %v", fake.methods, fake.bodies)
	}
	got := fake.bodies[0]
	if got["backend"] != "claude-code" || got["minutes"] != float64(45) || got["reason"] != "weekly cap" {
		t.Errorf("set body = %v", got)
	}
	if _, ok := got["until"]; ok {
		t.Errorf("set body carried until: %v", got)
	}

	out = send("usage_backoff status")
	if !strings.Contains(out, "usage_backoff status") || !strings.Contains(out, "claude-code") {
		t.Errorf("status reply = %q", out)
	}

	send("usage_backoff set pi 2026-10-23T06:00:00Z")
	if b := fake.bodies[len(fake.bodies)-1]; b["until"] != "2026-10-23T06:00:00Z" || b["minutes"] != nil {
		t.Errorf("until body = %v", b)
	}

	send("usage_backoff clear claude-code")
	if m, b := fake.methods[len(fake.methods)-1], fake.bodies[len(fake.bodies)-1]; m != http.MethodDelete || b["backend"] != "claude-code" {
		t.Errorf("clear one = %s %v", m, b)
	}
	if _, still := fake.active["claude-code"]; still || len(fake.active) != 1 {
		t.Errorf("active after clear one = %v", fake.active)
	}

	out = send("usage_backoff clear")
	if m, b := fake.methods[len(fake.methods)-1], fake.bodies[len(fake.bodies)-1]; m != http.MethodDelete || b["backend"] != nil {
		t.Errorf("clear all = %s %v", m, b)
	}
	if !strings.Contains(out, "usage_backoff clear") || len(fake.active) != 0 {
		t.Errorf("clear all reply=%q active=%v", out, fake.active)
	}
}

func TestUsageBackoffComm_UsageAndErrors(t *testing.T) {
	// No loopback: set with a bad deadline must answer usage without
	// reaching REST; a valid set must report the missing loopback.
	r := &Router{hostname: "h"}
	out := strings.Join(r.HandleTestMessage("usage_backoff set pi"), "\n")
	if !strings.Contains(out, "usage_backoff set failed") || !strings.Contains(out, "usage:") {
		t.Errorf("bad set reply = %q", out)
	}
	out = strings.Join(r.HandleTestMessage("usage_backoff set pi 5"), "\n")
	if !strings.Contains(out, "usage_backoff set failed") || !strings.Contains(out, "loopback") {
		t.Errorf("no-loopback reply = %q", out)
	}
	out = strings.Join(r.HandleTestMessage("usage_backoff nope"), "\n")
	if !strings.Contains(out, "usage_backoff failed") || !strings.Contains(out, "usage:") {
		t.Errorf("unknown verb reply = %q", out)
	}
	for _, in := range []string{"usage_backoff status", "usage_backoff clear"} {
		if out := strings.Join(r.HandleTestMessage(in), "\n"); !strings.Contains(out, "failed") {
			t.Errorf("%q without loopback = %q, want failure", in, out)
		}
	}
}

func TestUsageBackoffComm_HelpText(t *testing.T) {
	help := HelpText("h")
	for _, want := range []string{"usage_backoff set <backend> <minutes>", "usage_backoff clear [backend|all]", "usage_backoff recheck"} {
		if !strings.Contains(help, want) {
			t.Errorf("help text missing %q", want)
		}
	}
}

func TestConfigureHelpListsUsageBackoffKeys(t *testing.T) {
	r := &Router{hostname: "h"}
	r.SetConfigureFunc(func(string, string) error { return nil })
	for _, in := range []string{"configure help", "configure list"} {
		out := strings.Join(r.HandleTestMessage(in), "\n")
		for _, key := range []string{"autonomous.usage_backoff", "autonomous.backoff_max_wait_minutes"} {
			if !strings.Contains(out, key) {
				t.Errorf("%q help missing %s:\n%s", in, key, out)
			}
		}
	}
}

func TestParse_UsageBackoff_Recheck(t *testing.T) {
	for _, in := range []string{"usage_backoff recheck", "usage-backoff RECHECK"} {
		c := Parse(in)
		if c.Type != CmdUsageBackoff || c.UsageBackoffVerb != "recheck" {
			t.Errorf("Parse(%q) = %+v", in, c)
		}
	}
}

func TestUsageBackoffComm_Recheck(t *testing.T) {
	var method, path string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/usage-backoff/recheck", func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":[],"patterns_version":"abc123def456","patterns_stale":false,"claude_cli_version":"1.2.3-test"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r := loopbackRouter(t, srv)

	out := strings.Join(r.HandleTestMessage("usage_backoff recheck"), "\n")
	if method != http.MethodPost || path != "/api/usage-backoff/recheck" {
		t.Errorf("recheck request = %s %s", method, path)
	}
	for _, want := range []string{"usage_backoff recheck", "patterns_version", "patterns_stale", "claude_cli_version"} {
		if !strings.Contains(out, want) {
			t.Errorf("recheck reply lacks %q: %q", want, out)
		}
	}

	bare := &Router{hostname: "h"}
	if out := strings.Join(bare.HandleTestMessage("usage_backoff recheck"), "\n"); !strings.Contains(out, "usage_backoff recheck failed") {
		t.Errorf("recheck without loopback = %q, want failure", out)
	}
}

func TestUsageBackoffComm_StatusShowsPatternsFields(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/usage-backoff", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":[],"patterns_version":"abc123def456","patterns_stale":true,"claude_cli_version":"1.2.3-test"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	out := strings.Join(loopbackRouter(t, srv).HandleTestMessage("usage_backoff status"), "\n")
	for _, want := range []string{"patterns_version", "patterns_stale", "claude_cli_version"} {
		if !strings.Contains(out, want) {
			t.Errorf("status reply lacks %q: %q", want, out)
		}
	}
}
