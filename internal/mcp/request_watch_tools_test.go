package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

const requestWatchSampleBody = `{"enabled":true,"poll_seconds":30,"no_progress_minutes":30,"observations":[` +
	`{"session_id":"s1","automaton_id":"abcd1234","task_id":"t1","role":"worker","node":"node-a","model":"m1",` +
	`"state":"idle","last_change":"2026-11-01T09:00:00Z","no_progress_seconds":1900,"last_poll":"2026-11-01T09:31:40Z",` +
	`"over_threshold":true,"summary":"waiting on a model request: no progress for 31 minutes"}]}`

func TestRequestWatchToolRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range mergeTestToolNames(t) {
		have[n] = true
	}
	for _, want := range RequestWatchToolNames() {
		if !have[want] {
			t.Errorf("tool %q is not registered", want)
		}
	}
	tool := (&Server{}).toolAutonomousRequestWatch()
	if tool.Name != "autonomous_request_watch" {
		t.Errorf("tool name = %q", tool.Name)
	}
	if hint := tool.Annotations.ReadOnlyHint; hint == nil || !*hint {
		t.Error("autonomous_request_watch must be marked read-only")
	}
	for _, p := range []string{"session", "automaton"} {
		if _, ok := tool.InputSchema.Properties[p]; !ok {
			t.Errorf("tool lacks the %s argument", p)
		}
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("both arguments must be optional, got required %v", tool.InputSchema.Required)
	}
	if strings.Contains(tool.Description, "PRD") {
		t.Errorf("description must say Automaton, not PRD: %s", tool.Description)
	}
	for _, field := range []string{"observations", "session_id", "automaton_id", "task_id", "role", "node",
		"model", "state", "last_change", "no_progress_seconds", "last_poll", "over_threshold", "summary"} {
		if !strings.Contains(tool.Description, `"`+field+`"`) {
			t.Errorf("description does not name %q", field)
		}
	}
}

func TestRequestWatchToolFederationCapability(t *testing.T) {
	got, ok := federation.RequiredCapForMCPTool("autonomous_request_watch")
	if !ok || got != federation.CapAutonomousRead {
		t.Errorf("autonomous_request_watch capability = %q (known %v), want the autonomous read capability", got, ok)
	}
}

func TestRequestWatchToolAnswersRESTShape(t *testing.T) {
	var path, method, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, rawQuery = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(requestWatchSampleBody))
	})
	res, err := s.handleAutonomousRequestWatch(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/request-watch" || rawQuery != "" {
		t.Errorf("proxied %s %s?%s; want the route with no filter", method, path, rawQuery)
	}
	if text := automationText(t, res); text != requestWatchSampleBody {
		t.Errorf("the tool must return the REST body unchanged\ngot:  %s\nwant: %s", text, requestWatchSampleBody)
	}
}

func TestRequestWatchToolForwardsBothFilters(t *testing.T) {
	var rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"observations":[]}`))
	})
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"session": "s1"}, "session=s1"},
		{map[string]any{"automaton": "abcd1234"}, "automaton=abcd1234"},
		{map[string]any{"session": "s1", "automaton": "abcd1234"}, "automaton=abcd1234&session=s1"},
	}
	for _, c := range cases {
		res, err := s.handleAutonomousRequestWatch(context.Background(), callReq(c.args))
		if err != nil || res == nil || res.IsError {
			t.Fatalf("%v: err=%v res=%+v", c.args, err, res)
		}
		if rawQuery != c.want {
			t.Errorf("%v: query = %q, want %q", c.args, rawQuery, c.want)
		}
	}
}

func TestRequestWatchToolUnreachableDaemonIsClearError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	srv.Close()
	s := &Server{webPort: port}
	res, err := s.handleAutonomousRequestWatch(context.Background(), callReq(nil))
	if err != nil {
		t.Fatalf("an unreachable daemon must be a tool error, not a protocol error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("want an error result, got %+v", res)
	}
	if text := automationText(t, res); !strings.HasPrefix(text, "autonomous_request_watch:") {
		t.Errorf("the error does not name the tool: %s", text)
	}
}

func TestRequestWatchToolDaemonRefusalIsError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "request watch unavailable", http.StatusServiceUnavailable)
	})
	res, err := s.handleAutonomousRequestWatch(context.Background(), callReq(nil))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("err=%v res=%+v; want an error result", err, res)
	}
	if text := automationText(t, res); !strings.Contains(text, "request watch unavailable") {
		t.Errorf("the daemon's reason was lost: %s", text)
	}
}

func TestRequestWatchConfigSetHasTheArguments(t *testing.T) {
	tool := (&Server{}).toolAutonomousConfigSet()
	for _, k := range requestWatchConfigKeys {
		prop, ok := tool.InputSchema.Properties["request_watch_"+k].(map[string]any)
		if !ok {
			t.Errorf("autonomous_config_set lacks the request_watch_%s argument", k)
			continue
		}
		d, _ := prop["description"].(string)
		if !strings.Contains(d, "autonomous.request_watch."+k) {
			t.Errorf("request_watch_%s description does not name its config key: %s", k, d)
		}
		if strings.Contains(d, "PRD") {
			t.Errorf("request_watch_%s description must say Automaton, not PRD: %s", k, d)
		}
	}
	if len(requestWatchConfigKeys) != 11 {
		t.Errorf("want eleven request_watch keys, have %d", len(requestWatchConfigKeys))
	}
}

func TestRequestWatchConfigSetForwardsEveryKey(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"request_watch_enabled":             false,
		"request_watch_poll_seconds":        float64(15),
		"request_watch_no_progress_minutes": float64(45),
		"request_watch_observe_workers":     false,
		"request_watch_observe_verifiers":   false,
		"request_watch_use_gpu_signal":      false,
		"request_watch_gpu_busy_percent":    float64(25),
		"request_watch_unreachable_counts":  false,
		"request_watch_raise_alert":         false,
		"request_watch_raise_question":      false,
		"request_watch_remediation_options": []any{"end_worker", "unload_reload_model"},
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if rec.method != http.MethodPut || rec.path != "/api/config" {
		t.Fatalf("proxied %s %s, want PUT /api/config", rec.method, rec.path)
	}
	if len(rec.body) != 11 {
		t.Fatalf("body = %v, want exactly the eleven keys given", rec.body)
	}
	for _, k := range []string{"enabled", "observe_workers", "observe_verifiers", "use_gpu_signal",
		"unreachable_counts", "raise_alert", "raise_question"} {
		if v, ok := rec.body["autonomous.request_watch."+k]; !ok || v != false {
			t.Errorf("%s: explicit false was not sent: %v", k, rec.body)
		}
	}
	for k, want := range map[string]float64{"poll_seconds": 15, "no_progress_minutes": 45, "gpu_busy_percent": 25} {
		if rec.body["autonomous.request_watch."+k] != want {
			t.Errorf("%s: want %v, got %v", k, want, rec.body["autonomous.request_watch."+k])
		}
	}
	list, _ := rec.body["autonomous.request_watch.remediation_options"].([]any)
	if len(list) != 2 || list[0] != "end_worker" || list[1] != "unload_reload_model" {
		t.Errorf("remediation_options lost: %v", rec.body)
	}
}

func TestRequestWatchConfigSetAcceptsCommaSeparatedOptions(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"request_watch_remediation_options": "end_worker, retry_other_backend",
	})); err != nil {
		t.Fatal(err)
	}
	list, _ := rec.body["autonomous.request_watch.remediation_options"].([]any)
	if len(list) != 2 || list[0] != "end_worker" || list[1] != "retry_other_backend" {
		t.Errorf("comma list not split into a list: %v", rec.body)
	}
}

func TestRequestWatchConfigSetKeepsExplicitEmptyOptions(t *testing.T) {
	for _, empty := range []any{[]any{}, ""} {
		s, rec := activateServer(t, http.StatusOK, `{}`)
		if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
			"request_watch_remediation_options": empty,
		})); err != nil {
			t.Fatal(err)
		}
		list, ok := rec.body["autonomous.request_watch.remediation_options"].([]any)
		if !ok || len(list) != 0 {
			t.Errorf("%#v: an explicit empty list was not sent as an empty list: %v", empty, rec.body)
		}
	}
}

func TestRequestWatchConfigSetSendsNothingWhenAbsent(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"poll_interval_seconds": float64(45),
	})); err != nil {
		t.Fatal(err)
	}
	if rec.path != "/api/autonomous/config" {
		t.Fatalf("proxied %s, want the Automata config only", rec.path)
	}
	for k := range rec.body {
		if strings.Contains(k, "request_watch") {
			t.Errorf("body carries %s though no request_watch argument was given", k)
		}
	}
}

func TestRequestWatchConfigSetRefusalIsPassedBack(t *testing.T) {
	s, _ := activateServer(t, http.StatusBadRequest, "autonomous.request_watch.gpu_busy_percent: 101 is invalid; use a whole number from 1 to 100")
	_, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"request_watch_gpu_busy_percent": float64(101),
	}))
	if err == nil || !strings.Contains(err.Error(), "is invalid") {
		t.Fatalf("a refused value must come back as an error naming the key, got %v", err)
	}
}

func TestRequestWatchConfigGetShowsTheBlock(t *testing.T) {
	var path, method string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_, _ = w.Write([]byte(`{"enabled":true,"request_watch":{"enabled":true,"poll_seconds":30}}`))
	})
	res, err := s.handleAutonomousConfigGet(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/config" {
		t.Errorf("proxied %s %s", method, path)
	}
	if text := automationText(t, res); !strings.Contains(text, `"request_watch":{"enabled":true,"poll_seconds":30}`) {
		t.Errorf("autonomous_config_get dropped the request_watch block: %s", text)
	}
}
