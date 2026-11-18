// Tests for `datawatch plugins service` and `datawatch plugins widgets`.
// The CLI is a thin REST proxy, so these tests point it at a fake daemon
// (serverURL override, same pattern as the cost-report parity tests) and
// pin: the exact method/path/body of each subcommand, the restart
// ordering (stop leg before start leg), and the attach-only refusal
// surfaced on start/stop/restart (the daemon's 400 is rewritten into the
// operator-actionable message, and restart's start leg is never
// attempted when the stop leg is refused).

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

// pluginCalls records every request the fake daemon saw, in order.
type pluginCalls struct {
	mu      sync.Mutex
	methods []string
	paths   []string
	bodies  []any
}

type pluginCall struct {
	method string
	path   string
	body   any
}

func (c *pluginCalls) add(call pluginCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.methods = append(c.methods, call.method)
	c.paths = append(c.paths, call.path)
	c.bodies = append(c.bodies, call.body)
}

func (c *pluginCalls) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.methods)
}

func (c *pluginCalls) at(i int) pluginCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return pluginCall{c.methods[i], c.paths[i], c.bodies[i]}
}

func pluginDecodeBody(t *testing.T, body any) map[string]any {
	t.Helper()
	b, ok := body.(map[string]any)
	if !ok {
		t.Fatalf("body did not decode to an object: %#v", body)
	}
	return b
}

// newPluginFakeServer serves every request with code + respBody and
// records the calls in rec (decoding JSON bodies when present).
func newPluginFakeServer(t *testing.T, rec *pluginCalls, code int, respBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body any
		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &body)
			}
		}
		rec.add(pluginCall{req.Method, req.URL.Path, body})
		w.WriteHeader(code)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runPluginCLI runs one subcommand against the fake daemon, capturing
// stdout, and returns (stdout, error). The error is the CLI subcommand's
// own result (withServerURL propagates it), so expected failures like the
// attach-only refusal are returned for assertion, not fatal.
func runPluginCLI(t *testing.T, srv *httptest.Server, c *cobra.Command, args []string) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	reader, writer, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("pipe: %v", pipeErr)
	}
	var runErr error
	_ = withServerURL(t, srv, func() error {
		os.Stdout = writer
		defer func() { os.Stdout = oldStdout }()
		runErr = c.RunE(c, args)
		return runErr
	})
	if closeErr := writer.Close(); closeErr != nil {
		t.Errorf("writer.Close: %v", closeErr)
	}
	out, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read: %v", readErr)
	}
	return string(out), runErr
}

// runPluginCLIOut is runPluginCLI when all the test cares about is the
// daemon interaction (output discarded).
func runPluginCLIOut(t *testing.T, srv *httptest.Server, c *cobra.Command, args []string) error {
	t.Helper()
	_, runErr := runPluginCLI(t, srv, c, args)
	return runErr
}

func TestPluginService_SubcommandsPresent(t *testing.T) {
	plugins := newPluginsCmd()
	var serviceCmd, widgetsCmd *cobra.Command
	for _, sub := range plugins.Commands() {
		switch sub.Name() {
		case "service":
			serviceCmd = sub
		case "widgets":
			widgetsCmd = sub
		}
	}
	if serviceCmd == nil {
		t.Fatal("plugins has no `service` subcommand")
	}
	if widgetsCmd == nil {
		t.Fatal("plugins has no `widgets` subcommand")
	}
	wantService := map[string]bool{"status": false, "start": false, "stop": false, "restart": false}
	for _, sub := range serviceCmd.Commands() {
		if _, ok := wantService[sub.Name()]; ok {
			wantService[sub.Name()] = true
		}
	}
	for name, ok := range wantService {
		if !ok {
			t.Errorf("plugins service missing subcommand %q", name)
		}
	}
	wantWidgets := map[string]bool{"list": false, "get": false}
	for _, sub := range widgetsCmd.Commands() {
		if _, ok := wantWidgets[sub.Name()]; ok {
			wantWidgets[sub.Name()] = true
		}
	}
	for name, ok := range wantWidgets {
		if !ok {
			t.Errorf("plugins widgets missing subcommand %q", name)
		}
	}
}

// findSub returns the named subcommand of parent, failing the test when
// a required command is missing.
func findSub(t *testing.T, parent *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	t.Fatalf("%s has no %q subcommand", parent.Name(), name)
	return nil
}

func TestPluginService_Status(t *testing.T) {
	var rec pluginCalls
	srv := newPluginFakeServer(t, &rec, http.StatusOK, `{
  "name": "translator",
  "kind": "service",
  "service": {
    "mode": "managed",
    "state": "healthy",
    "address": "http://127.0.0.1:9321",
    "version": "1.4.2",
    "uptime_seconds": 3600,
    "last_health_check": "2026-11-01T09:00:00Z",
    "last_health_status": "up"
  }
}`)

	status := findSub(t, newPluginServiceCmd(), "status")
	out, runErr := runPluginCLI(t, srv, status, []string{"translator"})
	if runErr != nil {
		t.Fatalf("status: %v", runErr)
	}
	call := rec.at(rec.count() - 1)
	if call.method != http.MethodGet {
		t.Errorf("method = %q want GET", call.method)
	}
	if call.path != "/api/plugins/translator" {
		t.Errorf("path = %q want /api/plugins/translator", call.path)
	}
	for _, frag := range []string{"managed", "healthy", "http://127.0.0.1:9321", "1.4.2", "3600", "up"} {
		if !strings.Contains(out, frag) {
			t.Errorf("status output misses %q:\n%s", frag, out)
		}
	}
}

func TestPluginService_StartPostsManagedTrue(t *testing.T) {
	var rec pluginCalls
	srv := newPluginFakeServer(t, &rec, http.StatusOK, `{"status":"ok","name":"translator"}`)

	start := findSub(t, newPluginServiceCmd(), "start")
	runErr := runPluginCLIOut(t, srv, start, []string{"translator"})
	if runErr != nil {
		t.Fatalf("start: %v", runErr)
	}
	call := rec.at(rec.count() - 1)
	if call.method != http.MethodPost {
		t.Errorf("method = %q want POST", call.method)
	}
	if call.path != "/api/plugins/translator/enable" {
		t.Errorf("path = %q want /api/plugins/translator/enable", call.path)
	}
	if got := pluginDecodeBody(t, call.body); !reflect.DeepEqual(got, map[string]any{"managed": true}) {
		t.Errorf("body = %v want {managed:true}", got)
	}
}

func TestPluginService_StopPostsManagedFalse(t *testing.T) {
	var rec pluginCalls
	srv := newPluginFakeServer(t, &rec, http.StatusOK, `{"status":"ok","name":"translator"}`)

	stop := findSub(t, newPluginServiceCmd(), "stop")
	runErr := runPluginCLIOut(t, srv, stop, []string{"translator"})
	if runErr != nil {
		t.Fatalf("stop: %v", runErr)
	}
	call := rec.at(rec.count() - 1)
	if call.method != http.MethodPost {
		t.Errorf("method = %q want POST", call.method)
	}
	if call.path != "/api/plugins/translator/enable" {
		t.Errorf("path = %q want /api/plugins/translator/enable", call.path)
	}
	if got := pluginDecodeBody(t, call.body); !reflect.DeepEqual(got, map[string]any{"managed": false}) {
		t.Errorf("body = %v want {managed:false}", got)
	}
}

func TestPluginService_RestartPostsStopThenStart(t *testing.T) {
	var rec pluginCalls
	srv := newPluginFakeServer(t, &rec, http.StatusOK, `{"status":"ok","name":"translator"}`)

	restart := findSub(t, newPluginServiceCmd(), "restart")
	out, runErr := runPluginCLI(t, srv, restart, []string{"translator"})
	if runErr != nil {
		t.Fatalf("restart: %v", runErr)
	}
	if n := rec.count(); n != 2 {
		t.Fatalf("restart made %d daemon calls, want 2", n)
	}
	first, second := rec.at(0), rec.at(1)
	for i, call := range []pluginCall{first, second} {
		if call.method != http.MethodPost || call.path != "/api/plugins/translator/enable" {
			t.Errorf("leg %d = %s %s want POST /api/plugins/translator/enable", i+1, call.method, call.path)
		}
	}
	if got := pluginDecodeBody(t, first.body); !reflect.DeepEqual(got, map[string]any{"managed": false}) {
		t.Errorf("leg 1 body = %v want {managed:false} (stop first)", got)
	}
	if got := pluginDecodeBody(t, second.body); !reflect.DeepEqual(got, map[string]any{"managed": true}) {
		t.Errorf("leg 2 body = %v want {managed:true} (start second)", got)
	}
	for _, frag := range []string{`"stop"`, `"start"`, "translator"} {
		if !strings.Contains(out, frag) {
			t.Errorf("restart output misses %q:\n%s", frag, out)
		}
	}
}

// TestPluginService_StartAttachOnlyRefused pins the refusal path: the
// daemon answers 400 "does not declare managed lifecycle support" for an
// attach-only plugin, and the CLI surfaces that as the attach-only
// refusal (same wording family as the MCP plugin_service_* tools) instead
// of a bare HTTP error.
func newAttachOnlyServer(t *testing.T, rec *pluginCalls) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		rec.add(pluginCall{req.Method, req.URL.Path, string(raw)})
		// The daemon's own refusal for a plugin whose manifest does not
		// declare the managed lifecycle mode (applyPluginManaged, 400).
		http.Error(w, `plugin "inbox-integrator" does not declare managed lifecycle support`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPluginService_StartAttachOnlyRefused(t *testing.T) {
	var rec pluginCalls
	srv := newAttachOnlyServer(t, &rec)

	start := findSub(t, newPluginServiceCmd(), "start")
	runErr := runPluginCLIOut(t, srv, start, []string{"inbox-integrator"})
	if runErr == nil {
		t.Fatal("start of an attach-only plugin must fail")
	}
	errMsg := runErr.Error()
	for _, frag := range []string{"attach-only", `plugin "inbox-integrator"`, "cannot start, stop or restart"} {
		if !strings.Contains(errMsg, frag) {
			t.Errorf("refusal misses %q, got: %s", frag, errMsg)
		}
	}
	// One attempt only, on the enable route.
	n := rec.count()
	if n != 1 {
		t.Fatalf("made %d daemon calls, want 1", n)
	}
	call := rec.at(0)
	if call.method != http.MethodPost || call.path != "/api/plugins/inbox-integrator/enable" {
		t.Errorf("refused request = %s %s want POST /api/plugins/inbox-integrator/enable", call.method, call.path)
	}
}

func TestPluginService_StopAttachOnlyRefused(t *testing.T) {
	var rec pluginCalls
	srv := newAttachOnlyServer(t, &rec)

	stop := findSub(t, newPluginServiceCmd(), "stop")
	runErr := runPluginCLIOut(t, srv, stop, []string{"inbox-integrator"})
	if runErr == nil {
		t.Fatal("stop of an attach-only plugin must fail")
	}
	if !strings.Contains(runErr.Error(), "attach-only") {
		t.Errorf("refusal must name the attach-only condition, got: %s", runErr)
	}
}

func TestPluginService_RestartAttachOnlyRefusedSkipsStart(t *testing.T) {
	var rec pluginCalls
	srv := newAttachOnlyServer(t, &rec)

	restart := findSub(t, newPluginServiceCmd(), "restart")
	runErr := runPluginCLIOut(t, srv, restart, []string{"inbox-integrator"})
	if runErr == nil {
		t.Fatal("restart of an attach-only plugin must fail")
	}
	if !strings.Contains(runErr.Error(), "attach-only") {
		t.Errorf("refusal must name the attach-only condition, got: %s", runErr)
	}
	if n := rec.count(); n != 1 {
		t.Errorf("restart made %d daemon calls, want 1 (the start leg must never be attempted)", n)
	}
}

func TestPluginWidgets_List(t *testing.T) {
	var rec pluginCalls
	srv := newPluginFakeServer(t, &rec, http.StatusOK, `{
  "plugin": "translator",
  "widgets": [
    {"id": "rate", "data_mode": "pull", "value": null},
    {"id": "last-translation", "data_mode": "push",
     "value": {"updated_at": "2026-11-01T08:00:00Z", "data": {"lang": "de"}}}
  ]
}`)

	list := findSub(t, newPluginWidgetsCmd(), "list")
	out, runErr := runPluginCLI(t, srv, list, []string{"translator"})
	if runErr != nil {
		t.Fatalf("widgets list: %v", runErr)
	}
	call := rec.at(rec.count() - 1)
	if call.method != http.MethodGet {
		t.Errorf("method = %q want GET", call.method)
	}
	if call.path != "/api/plugins/translator/widgets" {
		t.Errorf("path = %q want /api/plugins/translator/widgets", call.path)
	}
	for _, frag := range []string{"rate", "last-translation", "de"} {
		if !strings.Contains(out, frag) {
			t.Errorf("widgets list output misses %q:\n%s", frag, out)
		}
	}
}

func TestPluginWidgets_Get(t *testing.T) {
	var rec pluginCalls
	srv := newPluginFakeServer(t, &rec, http.StatusOK, `{
  "id": "last-translation",
  "data_mode": "push",
  "value": {"updated_at": "2026-11-01T08:00:00Z", "data": {"lang": "de"}}
}`)

	get := findSub(t, newPluginWidgetsCmd(), "get")
	out, runErr := runPluginCLI(t, srv, get, []string{"translator", "last-translation"})
	if runErr != nil {
		t.Fatalf("widgets get: %v", runErr)
	}
	call := rec.at(rec.count() - 1)
	if call.method != http.MethodGet {
		t.Errorf("method = %q want GET", call.method)
	}
	if call.path != "/api/plugins/translator/widgets/last-translation" {
		t.Errorf("path = %q want /api/plugins/translator/widgets/last-translation", call.path)
	}
	for _, frag := range []string{"last-translation", "push", "de"} {
		if !strings.Contains(out, frag) {
			t.Errorf("widgets get output misses %q:\n%s", frag, out)
		}
	}
}

func TestPluginWidgets_Get404Surfaces(t *testing.T) {
	var rec pluginCalls
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rec.add(pluginCall{req.Method, req.URL.Path, nil})
		http.Error(w, `widget "nope" is not declared by plugin "translator"`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	get := findSub(t, newPluginWidgetsCmd(), "get")
	runErr := runPluginCLIOut(t, srv, get, []string{"translator", "nope"})
	if runErr == nil {
		t.Fatal("widgets get for an undeclared widget must fail")
	}
	if !strings.Contains(runErr.Error(), `is not declared by plugin "translator"`) {
		t.Errorf("daemon error must surface, got: %s", runErr)
	}
}
