package router

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// requestWatchBody builds a request-watch status body for the comm tests.
func requestWatchBody(enabled bool, observations ...string) string {
	on := "false"
	if enabled {
		on = "true"
	}
	return `{"enabled":` + on + `,"poll_seconds":30,"no_progress_minutes":30,"observations":[` +
		strings.Join(observations, ",") + `]}`
}

const (
	requestWatchGenerating = `{"session_id":"s1","automaton_id":"a1","task_id":"t1","role":"worker",` +
		`"node":"gpu-a","model":"m1","state":"generating","last_change":"2026-11-01T12:00:00Z",` +
		`"no_progress_seconds":0,"last_poll":"2026-11-01T12:00:30Z","over_threshold":false,` +
		`"summary":"waiting on a model request: generating"}`
	requestWatchIdle = `{"session_id":"s2","automaton_id":"a2","task_id":"t2","role":"verifier",` +
		`"node":"gpu-b","model":"m2","state":"idle","last_change":"2026-11-01T11:48:00Z",` +
		`"no_progress_seconds":720,"last_poll":"2026-11-01T12:00:30Z","over_threshold":false,` +
		`"summary":"waiting on a model request: no progress for 12 minutes"}`
	requestWatchUnreachable = `{"session_id":"s3","automaton_id":"a2","task_id":"t3","role":"worker",` +
		`"node":"gpu-c","model":"m3","state":"unreachable","last_change":"2026-11-01T11:30:00Z",` +
		`"no_progress_seconds":2400,"last_poll":"2026-11-01T12:00:30Z","over_threshold":true,` +
		`"error":"connection refused",` +
		`"summary":"waiting on a model request: node unreachable, no progress for 40 minutes"}`
)

func TestRequestWatchCommVerbParity(t *testing.T) {
	if !slices.Contains(RequestWatchCommVerbs(), "request-watch") {
		t.Fatalf("RequestWatchCommVerbs() = %v, must contain request-watch", RequestWatchCommVerbs())
	}
	for _, verb := range []string{"request-watch", "request_watch"} {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	help := strings.Join(r.HandleTestMessage("autonomous"), " ")
	if !strings.Contains(help, "request-watch [session <id>|automaton <id>]") {
		t.Errorf("autonomous help lacks the request-watch verb: %q", help)
	}
}

func TestRequestWatchGeneratingLine(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, requestWatchBody(true, requestWatchGenerating))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous request-watch"), " ")
	if log.path != "/api/autonomous/request-watch" || log.query != "" {
		t.Errorf("path=%s query=%q; want the request-watch route with no filter", log.path, log.query)
	}
	if log.calls != 1 {
		t.Errorf("the read is one call, got %d", log.calls)
	}
	for _, want := range []string{"s1", "worker", "task t1", "gpu-a", "waiting on a model request: generating"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestRequestWatchIdleNoProgressLine(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, requestWatchBody(true, requestWatchIdle))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous request-watch"), " ")
	for _, want := range []string{"s2", "verifier", "task t2", "gpu-b", "waiting on a model request: no progress for 12 minutes"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestRequestWatchUnreachableLine(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, requestWatchBody(true, requestWatchUnreachable))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous request-watch"), " ")
	for _, want := range []string{"s3", "worker", "task t3", "gpu-c", "waiting on a model request: node unreachable, no progress for 40 minutes"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestRequestWatchOneLinePerObservation(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK,
		requestWatchBody(true, requestWatchGenerating, requestWatchIdle, requestWatchUnreachable))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous request-watch"), "\n")
	n := 0
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "waiting on a model request") {
			n++
		}
	}
	if n != 3 {
		t.Errorf("want one line per observation (3), got %d: %q", n, got)
	}
}

func TestRequestWatchEmptyAnswer(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, requestWatchBody(true))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous request-watch"), "\n")
	if !strings.Contains(got, "No model requests are being watched") {
		t.Errorf("empty read must say nothing is watched: %q", got)
	}
	body := got[strings.Index(got, "No model requests"):]
	if strings.Contains(strings.TrimSpace(body), "\n") {
		t.Errorf("empty read must be one sentence, got: %q", body)
	}
}

func TestRequestWatchFeatureOffAnswer(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, requestWatchBody(false))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous request-watch"), "\n")
	if !strings.Contains(got, "watching is off") || !strings.Contains(got, "autonomous.request_watch.enabled") {
		t.Errorf("feature-off read must say the feature is off: %q", got)
	}
	if strings.Contains(got, "No model requests are being watched") {
		t.Errorf("feature-off read must not claim nothing is watched: %q", got)
	}
}

func TestRequestWatchSessionAndAutomatonFilters(t *testing.T) {
	for _, tc := range []struct{ msg, query string }{
		{"autonomous request-watch session s2", "session=s2"},
		{"autonomous request-watch automaton a2", "automaton=a2"},
		{"autonomous request_watch automaton a1", "automaton=a1"},
	} {
		log, port := laneFillCommDaemon(t, http.StatusOK, requestWatchBody(true, requestWatchIdle))
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage(tc.msg), " ")
		if log.path != "/api/autonomous/request-watch" || log.query != tc.query {
			t.Errorf("%q: path=%s query=%q, want the request-watch route with %s", tc.msg, log.path, log.query, tc.query)
		}
		if !strings.Contains(got, "s2") {
			t.Errorf("%q: reply lacks the observation: %q", tc.msg, got)
		}
	}
}

func TestRequestWatchBadArgumentsShowUsageAndSendNothing(t *testing.T) {
	for _, msg := range []string{
		"autonomous request-watch session",
		"autonomous request-watch node n1",
		"autonomous request-watch session s1 extra",
	} {
		log, port := laneFillCommDaemon(t, http.StatusOK, requestWatchBody(true))
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if log.calls != 0 {
			t.Errorf("%q: a bad argument must send nothing, got %d calls", msg, log.calls)
		}
		if !strings.Contains(got, "usage: autonomous request-watch") {
			t.Errorf("%q: reply lacks the usage line: %q", msg, got)
		}
	}
}

func TestRequestWatchDaemonErrorIsReported(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusServiceUnavailable, "request watch unavailable")
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous request-watch"), " ")
	if !strings.Contains(got, "autonomous request-watch failed") {
		t.Errorf("a daemon error must be reported as a failure: %q", got)
	}
}

// TestRequestWatchConfigureRoundTrip proves the existing configure path sends
// every autonomous.request_watch key to the daemon unchanged: a boolean, a
// number and the list, then the rest.
func TestRequestWatchConfigureRoundTrip(t *testing.T) {
	r, bodies := infraRetryConfigDaemon(t)
	for key, value := range map[string]string{
		"autonomous.request_watch.enabled":             "false",
		"autonomous.request_watch.poll_seconds":        "45",
		"autonomous.request_watch.remediation_options": "keep_waiting,end_worker",
		"autonomous.request_watch.no_progress_minutes": "20",
		"autonomous.request_watch.observe_workers":     "false",
		"autonomous.request_watch.observe_verifiers":   "true",
		"autonomous.request_watch.use_gpu_signal":      "false",
		"autonomous.request_watch.gpu_busy_percent":    "25",
		"autonomous.request_watch.unreachable_counts":  "false",
		"autonomous.request_watch.raise_alert":         "false",
		"autonomous.request_watch.raise_question":      "false",
	} {
		before := len(*bodies)
		reply := strings.Join(r.HandleTestMessage("configure "+key+"="+value), " ")
		if !strings.Contains(reply, "Set "+key+" = "+value) {
			t.Errorf("%s: expected a confirmation, got %q", key, reply)
		}
		if len(*bodies) != before+1 || (*bodies)[before][key] != value || len((*bodies)[before]) != 1 {
			t.Errorf("%s: PUT /api/config bodies = %v, want one body {%s: %s}", key, *bodies, key, value)
		}
	}
}

func TestRequestWatchConfigureHelpListsKeys(t *testing.T) {
	r := &Router{}
	r.SetConfigureFunc(func(string, string) error { return nil })
	for _, text := range []string{"help", "list"} {
		out := strings.Join(r.HandleTestMessage("configure "+text), " ")
		for _, key := range []string{
			"enabled", "poll_seconds", "no_progress_minutes", "observe_workers", "observe_verifiers",
			"use_gpu_signal", "gpu_busy_percent", "unreachable_counts", "raise_alert", "raise_question",
			"remediation_options",
		} {
			if !strings.Contains(out, "autonomous.request_watch."+key) {
				t.Errorf("configure %s lacks autonomous.request_watch.%s", text, key)
			}
		}
	}
}
