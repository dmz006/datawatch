package pinkeeper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/capacity"
)

const (
	testNode  = "gpu1"
	testModel = "qwen3.8-64k:27b"
	testOther = "llama3:70b"
	// testInterval keeps the tests fast. The 300s default is never used here.
	testInterval = time.Second
)

// fakeOllama is a real HTTP server standing in for one Ollama node. It
// keeps one resident model per load, like a node with no spare memory,
// so a load of one model evicts the rest.
type fakeOllama struct {
	srv *httptest.Server

	mu        sync.Mutex
	loaded    map[string]time.Time // model -> expires_at; zero means unlimited
	psCalls   int
	generates []map[string]any // bodies of every POST /api/generate
}

func newFakeOllama(t *testing.T) *fakeOllama {
	t.Helper()
	f := &fakeOllama{loaded: map[string]time.Time{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/ps", f.handlePS)
	mux.HandleFunc("POST /api/generate", f.handleGenerate)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOllama) handlePS(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.psCalls++
	models := []map[string]any{}
	for name, expires := range f.loaded {
		models = append(models, map[string]any{"name": name, "model": name, "expires_at": expires})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"models": models})
}

func (f *fakeOllama) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	model, _ := body["model"].(string)
	f.mu.Lock()
	f.generates = append(f.generates, body)
	if keep, ok := body["keep_alive"].(float64); ok && keep < 0 {
		// Unlimited keep_alive: the model stays, and the others are evicted.
		f.loaded = map[string]time.Time{model: {}}
	} else {
		f.loaded[model] = time.Now().Add(5 * time.Minute)
	}
	f.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{"model": model, "done": true})
}

func (f *fakeOllama) setLoaded(model string, expires time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loaded[model] = expires
}

func (f *fakeOllama) evict(model string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.loaded, model)
}

// pinRequests counts the generate calls that load a model with keep_alive -1.
func (f *fakeOllama) pinRequests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, g := range f.generates {
		if keep, ok := g["keep_alive"].(float64); ok && keep == -1 {
			n++
		}
	}
	return n
}

func (f *fakeOllama) allGenerates() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.generates)
}

func (f *fakeOllama) psCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.psCalls
}

// lastPinBody returns the most recent keep_alive -1 request body.
func (f *fakeOllama) lastPinBody(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.generates) - 1; i >= 0; i-- {
		if keep, ok := f.generates[i]["keep_alive"].(float64); ok && keep == -1 {
			return f.generates[i]
		}
	}
	t.Fatal("no keep_alive -1 request was received")
	return nil
}

// logRecorder collects the keeper's decision lines.
type logRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (l *logRecorder) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logRecorder) saw(substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.lines {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

func newAlertStore(t *testing.T) *alerts.Store {
	t.Helper()
	s, err := alerts.NewStore(filepath.Join(t.TempDir(), "alerts.json"))
	if err != nil {
		t.Fatalf("alerts.NewStore: %v", err)
	}
	return s
}

// waitFor polls cond until it holds or the timeout passes.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// newKeeper builds a keeper against fake with a 1s interval. busy may be nil.
func newKeeper(fake *fakeOllama, store *alerts.Store, rec *logRecorder, busy func(string) bool) *PinKeeper {
	return New(Options{
		Node:     testNode,
		Address:  fake.srv.URL,
		Model:    testModel,
		Interval: testInterval,
		Busy:     busy,
		Alerts:   store,
		Logf:     rec.logf,
	})
}

func TestPinnedModelHealthyNoAction(t *testing.T) {
	fake := newFakeOllama(t)
	fake.setLoaded(testModel, time.Time{}) // loaded with unlimited keep_alive
	store := newAlertStore(t)
	rec := &logRecorder{}
	k := newKeeper(fake, store, rec, nil)
	k.Start(context.Background())
	t.Cleanup(k.Stop)

	waitFor(t, 5*time.Second, "at least two checks", func() bool { return fake.psCount() >= 2 })
	k.Stop()

	if n := fake.allGenerates(); n != 0 {
		t.Fatalf("healthy node received %d generate requests, want 0", n)
	}
	if n := len(store.List()); n != 0 {
		t.Fatalf("healthy node raised %d alerts, want 0", n)
	}
}

func TestEvictedPinnedModelRepinnedAndAlerted(t *testing.T) {
	fake := newFakeOllama(t)
	store := newAlertStore(t)
	rec := &logRecorder{}
	k := newKeeper(fake, store, rec, func(string) bool { return false })
	k.Start(context.Background())
	t.Cleanup(k.Stop)

	waitFor(t, 5*time.Second, "the re-pin request", func() bool { return fake.pinRequests() >= 1 })
	// Let several more checks run. The model is resident now, so none may re-pin.
	waitFor(t, 5*time.Second, "healthy checks after the re-pin", func() bool { return fake.psCount() >= 3 })
	k.Stop()

	if n := fake.pinRequests(); n != 1 {
		t.Fatalf("got %d keep_alive -1 requests, want exactly 1", n)
	}
	body := fake.lastPinBody(t)
	if body["model"] != testModel {
		t.Fatalf("pin request model = %v, want %s", body["model"], testModel)
	}
	if keep := body["keep_alive"]; keep != float64(-1) {
		t.Fatalf("pin request keep_alive = %v, want -1", keep)
	}
	got := store.List()
	if len(got) != 1 {
		t.Fatalf("got %d alerts, want exactly 1", len(got))
	}
	wantTitle := "Repinned qwen3.8-64k:27b on gpu1 — it had been evicted"
	if got[0].Title != wantTitle {
		t.Fatalf("alert title = %q, want %q", got[0].Title, wantTitle)
	}
	if got[0].Level != alerts.LevelWarn || got[0].Source != "system" {
		t.Fatalf("alert level/source = %s/%s, want warn/system", got[0].Level, got[0].Source)
	}
}

func TestReplacedPinnedModelWhileBusyDoesNothing(t *testing.T) {
	fake := newFakeOllama(t)
	fake.setLoaded(testOther, time.Time{}) // a different large model took the node
	store := newAlertStore(t)
	rec := &logRecorder{}
	var busyCalls atomic.Int32
	busy := func(node string) bool {
		busyCalls.Add(1)
		return node == testNode // a session is mid-load on the node
	}
	k := newKeeper(fake, store, rec, busy)
	k.Start(context.Background())
	t.Cleanup(k.Stop)

	waitFor(t, 5*time.Second, "busy-skipped decisions", func() bool { return rec.saw("busy-skipped") })
	waitFor(t, 5*time.Second, "at least two checks", func() bool { return busyCalls.Load() >= 2 })
	k.Stop()

	if n := fake.allGenerates(); n != 0 {
		t.Fatalf("busy node received %d generate requests, want 0", n)
	}
	if n := len(store.List()); n != 0 {
		t.Fatalf("busy node raised %d alerts, want 0", n)
	}
}

func TestZeroIntervalDisablesKeeper(t *testing.T) {
	fake := newFakeOllama(t) // pinned model absent: any check would act
	store := newAlertStore(t)
	k := New(Options{
		Node:     testNode,
		Address:  fake.srv.URL,
		Model:    testModel,
		Interval: 0, // pin_check_seconds = 0
		Alerts:   store,
		Logf:     (&logRecorder{}).logf,
	})

	start := time.Now()
	k.Start(context.Background())
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Start took %v with a zero interval, want an immediate return", elapsed)
	}
	time.Sleep(1500 * time.Millisecond)
	k.Stop() // must return promptly after a no-op Start

	if n := fake.psCount(); n != 0 {
		t.Fatalf("disabled keeper sent %d /api/ps requests, want 0", n)
	}
	if n := fake.allGenerates(); n != 0 {
		t.Fatalf("disabled keeper sent %d generate requests, want 0", n)
	}
	if n := len(store.List()); n != 0 {
		t.Fatalf("disabled keeper raised %d alerts, want 0", n)
	}
}

func TestRepinFiresAgainAfterSecondEviction(t *testing.T) {
	fake := newFakeOllama(t)
	store := newAlertStore(t)
	rec := &logRecorder{}
	k := newKeeper(fake, store, rec, nil)
	k.Start(context.Background())
	t.Cleanup(k.Stop)

	waitFor(t, 5*time.Second, "first re-pin", func() bool { return fake.pinRequests() == 1 })
	fake.evict(testModel) // second eviction
	waitFor(t, 5*time.Second, "second re-pin", func() bool { return fake.pinRequests() == 2 })
	// Once re-pinned, the node stays healthy: no third request.
	time.Sleep(1500 * time.Millisecond)
	k.Stop()

	if n := fake.pinRequests(); n != 2 {
		t.Fatalf("got %d keep_alive -1 requests, want exactly 2", n)
	}
	if n := len(store.List()); n != 2 {
		t.Fatalf("got %d alerts, want one per eviction (2)", n)
	}
}

func TestFiniteExpiryUpgradedWithoutAlert(t *testing.T) {
	fake := newFakeOllama(t)
	// Loaded by an ordinary request, so it will unload when idle.
	fake.setLoaded(testModel, time.Now().Add(5*time.Minute))
	store := newAlertStore(t)
	rec := &logRecorder{}
	k := newKeeper(fake, store, rec, nil)
	k.Start(context.Background())
	t.Cleanup(k.Stop)

	waitFor(t, 5*time.Second, "keep_alive upgrade", func() bool { return fake.pinRequests() >= 1 })
	time.Sleep(1500 * time.Millisecond)
	k.Stop()

	if n := fake.pinRequests(); n != 1 {
		t.Fatalf("got %d keep_alive -1 requests, want exactly 1", n)
	}
	if n := len(store.List()); n != 0 {
		t.Fatalf("finite-expiry upgrade raised %d alerts, want 0 (no eviction happened)", n)
	}
}

func TestStopBeforeStartAndTwiceIsSafe(t *testing.T) {
	fake := newFakeOllama(t)
	k := newKeeper(fake, newAlertStore(t), &logRecorder{}, nil)
	k.Stop() // never started
	k.Start(context.Background())
	k.Start(context.Background()) // second Start on a running keeper does nothing
	k.Stop()
	k.Stop() // second Stop is a no-op
}

func TestEmptyModelDoesNotStart(t *testing.T) {
	fake := newFakeOllama(t)
	k := New(Options{Node: testNode, Address: fake.srv.URL, Model: "", Interval: testInterval})
	k.Start(context.Background())
	time.Sleep(1200 * time.Millisecond)
	k.Stop()
	if n := fake.psCount(); n != 0 {
		t.Fatalf("keeper with no model sent %d /api/ps requests, want 0", n)
	}
}

func TestClassifyStates(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		models []psModel
		want   state
	}{
		{"absent", nil, stateAbsent},
		{"zero expiry is unlimited", []psModel{{Name: testModel, ExpiresAt: time.Time{}}}, stateHealthy},
		{"far-future expiry is unlimited", []psModel{{Name: testModel, ExpiresAt: now.Add(200 * 365 * 24 * time.Hour)}}, stateHealthy},
		{"near expiry is expiring", []psModel{{Name: testModel, ExpiresAt: now.Add(5 * time.Minute)}}, stateExpiring},
		{"untagged name matches latest tag", []psModel{{Name: "qwen3.8-64k", ExpiresAt: time.Time{}}}, stateAbsent},
		{"other model only is absent", []psModel{{Name: testOther}}, stateAbsent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classify(tc.models, testModel, now)
			if got.state != tc.want {
				t.Fatalf("classify state = %v, want %v", got.state, tc.want)
			}
		})
	}
}

func TestLedgerBusyReflectsNodeLeases(t *testing.T) {
	led := capacity.New(capacity.Options{})
	busy := LedgerBusy(led)
	if busy(testNode) {
		t.Fatal("empty ledger reports the node busy")
	}
	led.Adopt(capacity.Request{Holder: "task-1", PRDID: "prd-1", Node: testNode, Pools: []string{"node:" + testNode}}, "sess-1")
	if !busy(testNode) {
		t.Fatal("node with an active lease reports idle")
	}
	if busy("gpu2") {
		t.Fatal("lease on gpu1 marks gpu2 busy")
	}
	led.Release("task-1")
	if busy(testNode) {
		t.Fatal("node reports busy after its lease was released")
	}
	if LedgerBusy(nil)(testNode) {
		t.Fatal("nil ledger reports busy")
	}
}
