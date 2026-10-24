package autonomous

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSessionLister struct {
	sessions []PoolSession
	faults   []PoolHostFault
	calls    atomic.Int32
}

func (f *fakeSessionLister) RunningSessions(ctx context.Context) ([]PoolSession, []PoolHostFault) {
	f.calls.Add(1)
	return f.sessions, f.faults
}

type fakeHostLister struct{ hosts []PoolHost }

func (f *fakeHostLister) PoolHosts(ctx context.Context) []PoolHost { return f.hosts }

type fakeModelFetcher struct {
	mu     sync.Mutex
	models map[string][]string
	errs   map[string]error
	calls  int
}

func (f *fakeModelFetcher) LoadedModels(ctx context.Context, endpoint string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if err := f.errs[endpoint]; err != nil {
		return nil, err
	}
	return f.models[endpoint], nil
}

func psServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestLaneFillEvidenceFetchLoadedModels(t *testing.T) {
	srv := psServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ps" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3.8:27b","model":"qwen3.8:27b"},{"model":"llama3:8b"}]}`))
	})
	got, err := NewHTTPModelFetcher(nil).LoadedModels(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "qwen3.8:27b,llama3:8b" {
		t.Fatalf("models = %v", got)
	}
}

func TestLaneFillEvidenceFetchEmptyList(t *testing.T) {
	srv := psServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"models":[]}`)) })
	got, err := NewHTTPModelFetcher(nil).LoadedModels(context.Background(), srv.URL)
	if err != nil || len(got) != 0 {
		t.Fatalf("models = %v, err = %v; want none, no error", got, err)
	}
}

func TestLaneFillEvidenceFetchHTTPError(t *testing.T) {
	srv := psServer(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) })
	_, err := NewHTTPModelFetcher(nil).LoadedModels(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v, want an HTTP 500 error", err)
	}
}

func TestLaneFillEvidenceFetchTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := psServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	f := NewHTTPModelFetcher(nil).(*httpModelFetcher)
	f.timeout = 50 * time.Millisecond
	start := time.Now()
	_, err := f.LoadedModels(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("a hanging host did not error")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("timeout not applied, took %v", time.Since(start))
	}
}

func TestLaneFillEvidenceFetchOversizedBody(t *testing.T) {
	srv := psServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"` + strings.Repeat("x", 4096) + `"}]}`))
	})
	f := NewHTTPModelFetcher(nil).(*httpModelFetcher)
	f.maxBytes = 1024
	_, err := f.LoadedModels(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v, want an oversized-body error", err)
	}
}

func TestLaneFillEvidenceFetchErrorsCarryNoCredentials(t *testing.T) {
	srv := psServer(t, func(w http.ResponseWriter, r *http.Request) {})
	srv.Close() // the connection is refused
	endpoint := strings.Replace(srv.URL, "http://", "http://user:s3cret@", 1)
	_, err := NewHTTPModelFetcher(nil).LoadedModels(context.Background(), endpoint)
	if err == nil {
		t.Fatal("a closed host did not error")
	}
	if strings.Contains(err.Error(), "s3cret") || strings.Contains(err.Error(), "http://") {
		t.Fatalf("error leaks the endpoint credentials: %v", err)
	}
}

func TestLaneFillEvidenceCountsLocalAndRemoteSessions(t *testing.T) {
	sessions := &fakeSessionLister{sessions: []PoolSession{
		{ID: "a", Backend: "pi-prompt"},                         // local, on the GPU backend
		{ID: "b", Backend: "pi-prompt", Host: "peer1"},          // remote, on the GPU backend
		{ID: "c", Backend: "opencode", Node: "gpu-box"},         // other backend talking to a pool host
		{ID: "d", Backend: "claude-code"},                       // not on the pool
		{ID: "e", Backend: "opencode", Node: "some-other-node"}, // names a node outside the pool
	}}
	hosts := &fakeHostLister{hosts: []PoolHost{{Name: "local", Endpoint: "http://l"}, {Name: "gpu-box", Endpoint: "http://g"}}}
	fetch := &fakeModelFetcher{models: map[string][]string{"http://l": {"m1"}, "http://g": {"m2", "m0"}}}
	src := NewPoolEvidenceSource(sessions, hosts, fetch)

	ev, err := src.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ev.ActiveGPUSessions != 3 {
		t.Errorf("active gpu sessions = %d, want 3", ev.ActiveGPUSessions)
	}
	want := []LoadedModel{{"gpu-box", "m0"}, {"gpu-box", "m2"}, {"local", "m1"}}
	if len(ev.LoadedModels) != len(want) {
		t.Fatalf("loaded models = %+v", ev.LoadedModels)
	}
	for i := range want {
		if ev.LoadedModels[i] != want[i] {
			t.Errorf("loaded model %d = %+v, want %+v", i, ev.LoadedModels[i], want[i])
		}
	}
	if ev.CheckedAt.IsZero() {
		t.Error("CheckedAt not set")
	}
}

func TestLaneFillEvidenceResidentModelAloneIsNotBusy(t *testing.T) {
	src := NewPoolEvidenceSource(&fakeSessionLister{},
		&fakeHostLister{hosts: []PoolHost{{Name: "local", Endpoint: "http://l"}}},
		&fakeModelFetcher{models: map[string][]string{"http://l": {"qwen3.8:27b"}}})
	ev, err := src.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ev.ActiveGPUSessions != 0 || len(ev.LoadedModels) != 1 {
		t.Fatalf("evidence = %+v, want no busy sessions and one resident model", ev)
	}
}

func TestLaneFillEvidenceUnreadableHostGivesErrorAndPartialData(t *testing.T) {
	sessions := &fakeSessionLister{
		sessions: []PoolSession{{ID: "a", Backend: "pi-prompt"}},
		faults:   []PoolHostFault{{Host: "peer2", Err: context.DeadlineExceeded}},
	}
	hosts := &fakeHostLister{hosts: []PoolHost{{Name: "local", Endpoint: "http://l"}, {Name: "gpu-box", Endpoint: "http://g"}}}
	fetch := &fakeModelFetcher{
		models: map[string][]string{"http://l": {"m1"}},
		errs:   map[string]error{"http://g": context.DeadlineExceeded},
	}
	ev, err := NewPoolEvidenceSource(sessions, hosts, fetch).Check(context.Background())
	if err == nil {
		t.Fatal("unreadable hosts gave no error")
	}
	for _, name := range []string{"peer2", "gpu-box"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
	if ev.ActiveGPUSessions != 1 || len(ev.LoadedModels) != 1 || ev.LoadedModels[0].Host != "local" {
		t.Errorf("partial evidence = %+v", ev)
	}
}

func TestLaneFillEvidenceCachesForFiveSeconds(t *testing.T) {
	sessions := &fakeSessionLister{}
	fetch := &fakeModelFetcher{}
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	src := NewPoolEvidenceSource(sessions, &fakeHostLister{hosts: []PoolHost{{Name: "local", Endpoint: "http://l"}}}, fetch,
		PoolEvidenceClock(func() time.Time { return now }))
	for i := 0; i < 3; i++ {
		if _, err := src.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if sessions.calls.Load() != 1 || fetch.calls != 1 {
		t.Fatalf("hosts read %d/%d times within the cache window, want once", sessions.calls.Load(), fetch.calls)
	}
	now = now.Add(5 * time.Second)
	if _, err := src.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sessions.calls.Load() != 2 || fetch.calls != 2 {
		t.Fatalf("hosts read %d/%d times after the window, want twice", sessions.calls.Load(), fetch.calls)
	}
}

func TestLaneFillEvidenceGPUBackendReadLive(t *testing.T) {
	backend := "pi-prompt"
	src := NewPoolEvidenceSource(
		&fakeSessionLister{sessions: []PoolSession{{ID: "a", Backend: "goose-prompt"}}},
		&fakeHostLister{}, &fakeModelFetcher{},
		PoolEvidenceGPUBackend(func() string { return backend }))
	ev, _ := src.Check(context.Background())
	if ev.ActiveGPUSessions != 0 {
		t.Fatalf("counted a session of another backend: %+v", ev)
	}
	backend = "goose-prompt"
	ev, _ = src.Check(context.Background())
	if ev.ActiveGPUSessions != 1 {
		t.Fatalf("a changed gpu_backend did not apply: %+v", ev)
	}
}

// poolHarness builds a shadow harness over a real evidence source made of fakes
// and one GPU-preferring Automaton that could start on the GPU lane.
func newLaneFillPoolHarness(t *testing.T, sessions *fakeSessionLister, hostErr error) *laneFillHarness {
	t.Helper()
	h := newLaneFillHarness(t, "shadow")
	fetch := &fakeModelFetcher{errs: map[string]error{}}
	if hostErr != nil {
		fetch.errs["http://g"] = hostErr
	}
	h.c.SetLaneFillEvidenceSource(NewPoolEvidenceSource(sessions,
		&fakeHostLister{hosts: []PoolHost{{Name: "gpu-box", Endpoint: "http://g"}}}, fetch,
		PoolEvidenceClock(func() time.Time { return h.now })))
	h.c.SetReady()
	h.addPRD("gpu work", PRDApproved, "gpu", "pi-prompt")
	return h
}

func TestLaneFillPoolLiveSessionIsNotIdleEvenWithNoRunningAutomaton(t *testing.T) {
	sessions := &fakeSessionLister{sessions: []PoolSession{{ID: "s1", Backend: "pi-prompt", Host: "peer1"}}}
	h := newLaneFillPoolHarness(t, sessions, nil)

	h.poll()

	st := h.status()
	if st.Evidence.GPUActiveSessions != 1 || st.Evidence.Error != "" {
		t.Fatalf("evidence = %+v", st.Evidence)
	}
	for _, l := range st.Lanes {
		if l.Type == LaneTypeGPU && l.State != "occupied" {
			t.Fatalf("gpu lane is %s with a live session on the pool: %+v", l.State, l)
		}
	}
	for _, d := range st.Decisions {
		if d.LaneType == LaneTypeGPU && (d.Action == LaneFillActionStartPRD || d.Action == LaneFillActionIdle) {
			t.Errorf("the pool was called idle: %+v", d)
		}
	}
}

func TestLaneFillPoolIdleWhenPoolReadsEmpty(t *testing.T) {
	h := newLaneFillPoolHarness(t, &fakeSessionLister{}, nil)
	h.poll()
	findDecision(t, h.status(), "gpu-1", LaneFillActionStartPRD)
}

func TestLaneFillPoolUnreadableHostBlocksIdleGPULanes(t *testing.T) {
	h := newLaneFillPoolHarness(t, &fakeSessionLister{}, context.DeadlineExceeded)

	h.poll()

	st := h.status()
	if !strings.Contains(st.Evidence.Error, "gpu-box") {
		t.Fatalf("evidence error = %q, want it to name the host", st.Evidence.Error)
	}
	d := findDecision(t, st, "gpu-1", LaneFillActionBlocked)
	if !strings.Contains(d.Blocker, "gpu-box") {
		t.Errorf("blocker = %q", d.Blocker)
	}
	for _, d := range st.Decisions {
		if d.LaneType == LaneTypeGPU && d.Action == LaneFillActionStartPRD {
			t.Errorf("a GPU lane was started on an unreadable pool: %+v", d)
		}
	}
}
