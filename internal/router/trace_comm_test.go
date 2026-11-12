package router

import (
	"net/http"
	"strings"
	"testing"
)

func TestTraceComm_VerbsHitRoutes(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"ok":true}`)
	r := setupPeerRouter(t, port)
	cases := []struct {
		msg, path, query string
	}{
		{"trace session s1", "/api/trace/session/s1", ""},
		{"trace session s1 tool=Read errors=true limit=5", "/api/trace/session/s1", "errors=true&limit=5&tool=Read"},
		{"trace automaton a1 kind=error", "/api/trace/automaton/a1", "kind=error"},
		{"trace rollup", "/api/trace/rollup", ""},
		{"trace rollup dimension=backend key=pi", "/api/trace/rollup", "dimension=backend&key=pi"},
		{"trace coverage backend=ollama", "/api/trace/coverage", "backend=ollama"},
		{"TRACE Coverage", "/api/trace/coverage", ""},
	}
	for _, c := range cases {
		got := strings.Join(r.HandleTestMessage(c.msg), " ")
		if log.path != c.path || log.query != c.query {
			t.Errorf("%q -> %s?%s, want %s?%s", c.msg, log.path, log.query, c.path, c.query)
		}
		if !strings.Contains(got, `"ok":true`) {
			t.Errorf("%q reply = %q", c.msg, got)
		}
	}
}

func TestTraceComm_BadInput(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{}`)
	r := setupPeerRouter(t, port)
	for _, msg := range []string{
		"trace", "trace bogus", "trace session", "trace session tool=Read",
		"trace rollup limit=3", "trace coverage tool=x", "trace rollup dimension=",
		"trace session s1 nonsense",
	} {
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, "usage: trace") {
			t.Errorf("%q: want the usage line, got %q", msg, got)
		}
	}
	if log.calls != 0 {
		t.Errorf("bad input reached the daemon %d times", log.calls)
	}
}

func TestTraceComm_DaemonErrorIsRelayed(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusNotFound, "no trace for session zz")
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("trace session zz"), " ")
	if !strings.Contains(got, "failed") || !strings.Contains(got, "no trace for session zz") {
		t.Errorf("reply = %q", got)
	}
}

func TestTraceComm_ParseDoesNotSwallowOthers(t *testing.T) {
	if c := Parse("trace session s1"); c.Type != CmdTrace || c.Text != "session s1" {
		t.Errorf("parse = %+v", c)
	}
	if c := Parse("tracer"); c.Type == CmdTrace {
		t.Error("tracer is not the trace verb")
	}
}

// The tracing.* settings must be discoverable on both operator surfaces
// (`configure help` and `configure list`), and a tracing key must reach the
// configure function unchanged.
func TestConfigureHelpAndListMentionTracingKeys(t *testing.T) {
	r := &Router{hostname: "h"}
	var gotKey, gotValue string
	r.SetConfigureFunc(func(k, v string) error { gotKey, gotValue = k, v; return nil })

	for _, text := range []string{"help", "list"} {
		out := strings.Join(r.HandleTestMessage("configure "+text), "\n")
		for _, key := range []string{"tracing.mode", "tracing.arg_cap_bytes", "tracing.retention_days", "tracing.backends."} {
			if !strings.Contains(out, key) {
				t.Errorf("configure %s lacks %s", text, key)
			}
		}
	}
	out := strings.Join(r.HandleTestMessage("configure tracing.mode=full"), "\n")
	if gotKey != "tracing.mode" || gotValue != "full" {
		t.Fatalf("configureFn got key=%q value=%q", gotKey, gotValue)
	}
	if !strings.Contains(out, "Set tracing.mode = full") {
		t.Errorf("confirmation missing:\n%s", out)
	}
}
