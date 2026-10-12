// BL403 — pull-mode widget scheduler tests.

package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/secrets"
)

// pathCounter is an httptest handler that counts hits per path and
// delegates the response to per-path handlers.
type pathCounter struct {
	mu       sync.Mutex
	hits     map[string]int
	handlers map[string]http.HandlerFunc
}

func newPathCounter() *pathCounter {
	return &pathCounter{hits: map[string]int{}, handlers: map[string]http.HandlerFunc{}}
}

func (c *pathCounter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.hits[r.URL.Path]++
	h := c.handlers[r.URL.Path]
	c.mu.Unlock()
	if h == nil {
		http.NotFound(w, r)
		return
	}
	h(w, r)
}

func (c *pathCounter) handle(path string, h http.HandlerFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[path] = h
}

func (c *pathCounter) count(path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits[path]
}

func (c *pathCounter) snapshot() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]int{}
	for k, v := range c.hits {
		out[k] = v
	}
	return out
}

func jsonHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

// pullWidgetYAML renders one pull-mode widget entry.
func pullWidgetYAML(id, path string) string {
	return fmt.Sprintf("  - id: %s\n    surface: dashboard\n    kind: stat_tile\n    data_mode: pull\n    pull_path: %s\n    interval_seconds: 1\n", id, path)
}

// servicePluginYAML renders a kind: service manifest pointing at baseURL.
func servicePluginYAML(name, baseURL, token string, widgets ...string) string {
	src := fmt.Sprintf("name: %s\nkind: service\nconnection:\n  base_url: %s/\n", name, baseURL)
	if token != "" {
		src += fmt.Sprintf("  auth:\n    type: bearer\n    token: %q\n", token)
	}
	return src + "widgets:\n" + strings.Join(widgets, "")
}

// newTestScheduler writes the manifests into a plugin dir, discovers them
// and returns a scheduler whose widget intervals come from intervals
// (keyed by widget id).
func newTestScheduler(t *testing.T, manifests map[string]string, intervals map[string]time.Duration, st secrets.Store) (*Registry, *WidgetStore, *WidgetScheduler) {
	t.Helper()
	root := t.TempDir()
	for dirName, src := range manifests {
		dir := filepath.Join(root, dirName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeManifest(t, dir, src)
	}
	return newTestSchedulerAt(t, root, intervals, st)
}

func newTestSchedulerAt(t *testing.T, root string, intervals map[string]time.Duration, st secrets.Store) (*Registry, *WidgetStore, *WidgetScheduler) {
	t.Helper()
	reg, err := NewRegistry(Config{Enabled: true, Dir: root, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	ws, err := NewWidgetStore("")
	if err != nil {
		t.Fatal(err)
	}
	s := NewWidgetScheduler(reg, ws, st)
	s.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	s.intervalFor = func(_ string, w Widget) time.Duration { return intervals[w.ID] }
	return reg, ws, s
}

// startScheduler runs s in the background and returns a func that stops
// it and waits for Run to return (failing if it does not exit promptly).
func startScheduler(t *testing.T, s *WidgetScheduler) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("WidgetScheduler.Run did not return after cancel")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func waitForValue(t *testing.T, ws *WidgetStore, plugin, id string, timeout time.Duration, cond func(WidgetValue) bool) WidgetValue {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		v, ok := ws.Get(plugin, id)
		if ok && cond(v) {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("widget %s/%s: condition not met within %s (last %+v, found=%v)", plugin, id, timeout, v, ok)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func hasData(v WidgetValue) bool { return len(v.Data) > 0 }

// (a) Each widget is pulled on its own interval.
func TestWidgetScheduler_IntervalRespected(t *testing.T) {
	pc := newPathCounter()
	pc.handle("/fast", jsonHandler(`{"n":1}`))
	pc.handle("/slow", jsonHandler(`{"n":2}`))
	srv := httptest.NewServer(pc)
	t.Cleanup(srv.Close)

	_, ws, s := newTestScheduler(t, map[string]string{
		"svc": servicePluginYAML("svc", srv.URL, "", pullWidgetYAML("fast", "/fast"), pullWidgetYAML("slow", "/slow")),
	}, map[string]time.Duration{"fast": 50 * time.Millisecond, "slow": 200 * time.Millisecond}, nil)

	stop := startScheduler(t, s)
	time.Sleep(600 * time.Millisecond)
	stop()

	// Expected: an immediate pull plus one per tick — about 13 for 50ms and
	// about 4 for 200ms over 600ms. The ranges allow for -race / CI jitter.
	fast, slow := pc.count("/fast"), pc.count("/slow")
	if fast < 6 || fast > 16 {
		t.Errorf("fast (50ms) hits = %d, want 6..16", fast)
	}
	if slow < 2 || slow > 6 {
		t.Errorf("slow (200ms) hits = %d, want 2..6", slow)
	}
	if fast <= slow {
		t.Errorf("fast hits (%d) should exceed slow hits (%d)", fast, slow)
	}
	if v, ok := ws.Get("svc", "fast"); !ok || string(v.Data) != `{"n":1}` || v.Source != WidgetSourcePull || v.Error != "" {
		t.Errorf("fast value = %+v, %v", v, ok)
	}
}

// (b) A hung pull of one widget never delays another widget, and ticks of
// the hung widget are skipped rather than queued.
func TestWidgetScheduler_HungPullIsolation(t *testing.T) {
	release := make(chan struct{})
	pc := newPathCounter()
	pc.handle("/fast", jsonHandler(`{"ok":true}`))
	pc.handle("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	srv := httptest.NewServer(pc)
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // runs before srv.Close

	_, ws, s := newTestScheduler(t, map[string]string{
		"svc": servicePluginYAML("svc", srv.URL, "", pullWidgetYAML("slow", "/slow"), pullWidgetYAML("fast", "/fast")),
	}, map[string]time.Duration{"fast": 50 * time.Millisecond, "slow": 10 * time.Second}, nil)
	// A pull is bounded by its widget's interval, so slow's 10s interval
	// keeps its first pull hung for the whole test.
	startScheduler(t, s)

	first := waitForValue(t, ws, "svc", "fast", time.Second, hasData)
	waitForValue(t, ws, "svc", "fast", time.Second, func(v WidgetValue) bool {
		return v.UpdatedAt.After(first.UpdatedAt)
	})
	if pc.count("/slow") != 1 {
		t.Fatalf("slow should have exactly one in-flight pull, got %d hits", pc.count("/slow"))
	}
	if v, ok := ws.Get("svc", "slow"); ok {
		t.Fatalf("hung widget should have no value yet, got %+v", v)
	}
}

// (b') While one widget's pull is hung its own ticks are dropped, not queued.
func TestWidgetScheduler_SkipsTickWhileBusy(t *testing.T) {
	release := make(chan struct{})
	var inFlight, maxInFlight atomic.Int32
	pc := newPathCounter()
	pc.handle("/busy", func(w http.ResponseWriter, r *http.Request) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	srv := httptest.NewServer(pc)
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	_, _, s := newTestScheduler(t, map[string]string{
		"svc": servicePluginYAML("svc", srv.URL, "", pullWidgetYAML("busy", "/busy")),
	}, map[string]time.Duration{"busy": 300 * time.Millisecond}, nil)
	stop := startScheduler(t, s)
	// The pull times out after one interval (300ms); ticks landing while it
	// is in flight must not start a second concurrent pull.
	time.Sleep(700 * time.Millisecond)
	stop()
	if m := maxInFlight.Load(); m > 1 {
		t.Fatalf("max concurrent pulls of one widget = %d, want 1", m)
	}
	if n := pc.count("/busy"); n < 2 || n > 4 {
		t.Fatalf("busy hits = %d, want 2..4 (timed-out pulls retried on later ticks)", n)
	}
}

// (c) Automation plugins answer status_poll through Registry.Invoke.
func TestWidgetScheduler_AutomationStatusPoll(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping subprocess plugin test on Windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "auto")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	reqFile := filepath.Join(dir, "last-request.json")
	script := fmt.Sprintf("#!/bin/sh\nread line\nprintf '%%s' \"$line\" > %q\necho '{\"fields\":{\"data\":{\"count\":3}}}'\n", reqFile)
	if err := os.WriteFile(filepath.Join(dir, "plugin.sh"), []byte(script), 0o755); err != nil { // #nosec G306 -- test plugin must be executable
		t.Fatal(err)
	}
	writeManifest(t, dir, "name: auto\nentry: ./plugin.sh\nhooks: [status_poll]\nwidgets:\n"+
		pullWidgetYAML("queue", "queue-depth"))

	_, ws, s := newTestSchedulerAt(t, root, map[string]time.Duration{"queue": 5 * time.Second}, nil)
	startScheduler(t, s)

	v := waitForValue(t, ws, "auto", "queue", 5*time.Second, hasData)
	if string(v.Data) != `{"count":3}` || v.Source != WidgetSourcePull || v.Error != "" {
		t.Fatalf("stored value = %+v (data %s)", v, v.Data)
	}
	raw, err := os.ReadFile(reqFile) // #nosec G304 -- test temp file
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("parse request %s: %v", raw, err)
	}
	if req.Hook != HookStatusPoll || req.Payload["widget_id"] != "queue" || req.Payload["pull_path"] != "queue-depth" ||
		req.Payload["surface"] != WidgetSurfaceDashboard || req.Payload["kind"] != WidgetKindStatTile {
		t.Fatalf("status_poll request = %+v", req)
	}
}

// (c') Without fields.data the whole fields object is the value; a
// response error is a failed pull.
func TestWidgetScheduler_AutomationFieldsAndError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping subprocess plugin test on Windows")
	}
	root := t.TempDir()
	for name, out := range map[string]string{
		"whole": `{"fields":{"up":true}}`,
		"err":   `{"error":"backend down"}`,
	} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		script := fmt.Sprintf("#!/bin/sh\nread line\necho '%s'\n", out)
		if err := os.WriteFile(filepath.Join(dir, "plugin.sh"), []byte(script), 0o755); err != nil { // #nosec G306 -- test plugin must be executable
			t.Fatal(err)
		}
		writeManifest(t, dir, "name: "+name+"\nentry: ./plugin.sh\nhooks: [status_poll]\nwidgets:\n"+pullWidgetYAML("w", "p"))
	}
	_, ws, s := newTestSchedulerAt(t, root, map[string]time.Duration{"w": 5 * time.Second}, nil)
	startScheduler(t, s)

	if v := waitForValue(t, ws, "whole", "w", 5*time.Second, hasData); string(v.Data) != `{"up":true}` {
		t.Fatalf("whole-fields value = %s", v.Data)
	}
	v := waitForValue(t, ws, "err", "w", 5*time.Second, func(v WidgetValue) bool { return v.Error != "" })
	if !strings.Contains(v.Error, "backend down") || len(v.Data) != 0 {
		t.Fatalf("error value = %+v", v)
	}
}

// (d) A failed pull keeps the previous Data and records Error.
func TestWidgetScheduler_FailedPullKeepsPreviousData(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"status": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
		"invalid_json": jsonHandler(`{not json`),
		"oversize":     jsonHandler(`"` + strings.Repeat("x", MaxWidgetPayloadBytes) + `"`),
	}
	for name, failing := range cases {
		t.Run(name, func(t *testing.T) {
			var fail atomic.Bool
			good := jsonHandler(`{"n":1}`)
			pc := newPathCounter()
			pc.handle("/w", func(w http.ResponseWriter, r *http.Request) {
				if fail.Load() {
					failing(w, r)
					return
				}
				good(w, r)
			})
			srv := httptest.NewServer(pc)
			t.Cleanup(srv.Close)

			_, ws, s := newTestScheduler(t, map[string]string{
				"svc": servicePluginYAML("svc", srv.URL, "", pullWidgetYAML("w", "/w")),
			}, map[string]time.Duration{"w": 30 * time.Millisecond}, nil)
			startScheduler(t, s)

			waitForValue(t, ws, "svc", "w", time.Second, hasData)
			fail.Store(true)
			v := waitForValue(t, ws, "svc", "w", time.Second, func(v WidgetValue) bool { return v.Error != "" })
			if string(v.Data) != `{"n":1}` {
				t.Fatalf("previous data should be kept, got %s (error %q)", v.Data, v.Error)
			}
			fail.Store(false)
			waitForValue(t, ws, "svc", "w", time.Second, func(v WidgetValue) bool { return v.Error == "" })
		})
	}
}

// (e) The connection bearer token is resolved and sent.
func TestWidgetScheduler_BearerAuth(t *testing.T) {
	var mu sync.Mutex
	var gotAuth string
	pc := newPathCounter()
	pc.handle("/w", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		jsonHandler(`{"ok":1}`)(w, r)
	})
	srv := httptest.NewServer(pc)
	t.Cleanup(srv.Close)

	st := &mockPluginStore{data: map[string]string{"svc-token": "s3cret"}}
	_, ws, s := newTestScheduler(t, map[string]string{
		"svc": servicePluginYAML("svc", srv.URL, "${secret:svc-token}", pullWidgetYAML("w", "/w")),
	}, map[string]time.Duration{"w": 5 * time.Second}, st)
	startScheduler(t, s)

	waitForValue(t, ws, "svc", "w", time.Second, hasData)
	mu.Lock()
	defer mu.Unlock()
	if gotAuth != "Bearer s3cret" {
		t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer s3cret")
	}
}

// (e') An unresolvable token is a failed pull, never an unauthenticated GET.
func TestWidgetScheduler_BearerAuthMissingSecret(t *testing.T) {
	pc := newPathCounter()
	pc.handle("/w", jsonHandler(`{"ok":1}`))
	srv := httptest.NewServer(pc)
	t.Cleanup(srv.Close)

	st := &mockPluginStore{data: map[string]string{}}
	_, ws, s := newTestScheduler(t, map[string]string{
		"svc": servicePluginYAML("svc", srv.URL, "${secret:missing}", pullWidgetYAML("w", "/w")),
	}, map[string]time.Duration{"w": 5 * time.Second}, st)
	startScheduler(t, s)

	v := waitForValue(t, ws, "svc", "w", time.Second, func(v WidgetValue) bool { return v.Error != "" })
	if !strings.Contains(v.Error, "connection.auth.token") {
		t.Fatalf("error = %q", v.Error)
	}
	if n := pc.count("/w"); n != 0 {
		t.Fatalf("GET sent without credentials %d times", n)
	}
}

// (f) Push-mode widgets are never polled.
func TestWidgetScheduler_PushWidgetsNeverPolled(t *testing.T) {
	pc := newPathCounter()
	pc.handle("/pulled", jsonHandler(`{"ok":1}`))
	srv := httptest.NewServer(pc)
	t.Cleanup(srv.Close)

	push := "  - id: pushed\n    surface: session\n    kind: markdown\n    data_mode: push\n"
	_, ws, s := newTestScheduler(t, map[string]string{
		"svc": servicePluginYAML("svc", srv.URL, "", push, pullWidgetYAML("pulled", "/pulled")),
	}, map[string]time.Duration{"pulled": 30 * time.Millisecond, "pushed": 30 * time.Millisecond}, nil)
	stop := startScheduler(t, s)
	waitForValue(t, ws, "svc", "pulled", time.Second, hasData)
	time.Sleep(150 * time.Millisecond)
	stop()

	for path, n := range pc.snapshot() {
		if path != "/pulled" {
			t.Errorf("unexpected request to %s (%d hits)", path, n)
		}
	}
	if v, ok := ws.Get("svc", "pushed"); ok {
		t.Fatalf("push widget should never get a pulled value: %+v", v)
	}
}

// Disabling a plugin (picked up by Reconcile) stops its widget goroutines.
func TestWidgetScheduler_ReconcileStopsDisabled(t *testing.T) {
	pc := newPathCounter()
	pc.handle("/w", jsonHandler(`{"ok":1}`))
	srv := httptest.NewServer(pc)
	t.Cleanup(srv.Close)

	reg, ws, s := newTestScheduler(t, map[string]string{
		"svc": servicePluginYAML("svc", srv.URL, "", pullWidgetYAML("w", "/w")),
	}, map[string]time.Duration{"w": 20 * time.Millisecond}, nil)
	startScheduler(t, s)
	waitForValue(t, ws, "svc", "w", time.Second, hasData)

	reg.SetEnabled("svc", false)
	s.Reconcile()
	time.Sleep(50 * time.Millisecond) // let any in-flight pull finish
	before := pc.count("/w")
	time.Sleep(150 * time.Millisecond)
	if after := pc.count("/w"); after != before {
		t.Fatalf("disabled plugin still polled: %d -> %d", before, after)
	}

	reg.SetEnabled("svc", true)
	s.Reconcile()
	waitForValue(t, ws, "svc", "w", time.Second, func(WidgetValue) bool { return pc.count("/w") > before })
}
