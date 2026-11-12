package compute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The injected clock for every Measure test is the package-shared
// fixedNow (declared in headroom_test.go); the code path never touches
// wall time.

// fakeProbe implements HeadroomProbe with canned values and records
// every call so tests can assert which requests were (not) sent.
type fakeProbe struct {
	freeMB          int
	perRequestMB    int
	residentModel   string
	readFreeErr     error
	requestMemErr   error
	readFreeCalls   int
	requestMemCalls int
	lastProbeModel  string
}

func (f *fakeProbe) ReadFreeMB(_ context.Context, _ Node) (int, string, error) {
	f.readFreeCalls++
	return f.freeMB, f.residentModel, f.readFreeErr
}

func (f *fakeProbe) RequestMemoryMB(_ context.Context, _ Node, model string) (int, error) {
	f.requestMemCalls++
	f.lastProbeModel = model
	return f.perRequestMB, f.requestMemErr
}

func measureTestNode() Node { return Node{Name: "gpu-a", Kind: KindOllama} }

func baseDeps(p HeadroomProbe) MeasureDeps {
	return MeasureDeps{
		Probe:         p,
		Now:           func() time.Time { return fixedNow },
		Held:          0,
		AllowedModels: []string{"llama3.1:8b", "qwen3:32b"}, // smallest first
		Node:          measureTestNode(),
	}
}

func TestMeasure_NodeBusy_RefusesAndNeverCallsProbe(t *testing.T) {
	p := &fakeProbe{freeMB: 4096, residentModel: "llama3.1:8b", perRequestMB: 300}
	deps := baseDeps(p)
	deps.Held = 3

	m, err := Measure(context.Background(), deps)
	if !errors.Is(err, ErrNodeBusy) {
		t.Fatalf("err = %v, want ErrNodeBusy", err)
	}
	if m != (HeadroomMeasurement{}) {
		t.Fatalf("measurement = %+v, want zero value on refusal", m)
	}
	if p.readFreeCalls != 0 || p.requestMemCalls != 0 {
		t.Fatalf("probe was called (read=%d request=%d), want no calls on a busy node", p.readFreeCalls, p.requestMemCalls)
	}
}

func TestMeasure_EmptyAllowList_Refused(t *testing.T) {
	p := &fakeProbe{freeMB: 4096, residentModel: "llama3.1:8b", perRequestMB: 300}
	deps := baseDeps(p)
	deps.AllowedModels = nil

	m, err := Measure(context.Background(), deps)
	if !errors.Is(err, ErrModelNotAllowed) {
		t.Fatalf("err = %v, want ErrModelNotAllowed", err)
	}
	if m != (HeadroomMeasurement{}) {
		t.Fatalf("measurement = %+v, want zero value on refusal", m)
	}
	if p.readFreeCalls != 0 || p.requestMemCalls != 0 {
		t.Fatalf("probe was called (read=%d request=%d), want no calls with an empty allow-list", p.readFreeCalls, p.requestMemCalls)
	}
}

func TestMeasure_ResidentModelOutsideAllowList_Refused(t *testing.T) {
	p := &fakeProbe{freeMB: 4096, residentModel: "deepseek-r1:70b", perRequestMB: 300}
	// "deepseek-r1:70b" is not in baseDeps' allow-list.
	deps := baseDeps(p)

	m, err := Measure(context.Background(), deps)
	if !errors.Is(err, ErrModelNotAllowed) {
		t.Fatalf("err = %v, want ErrModelNotAllowed", err)
	}
	if m != (HeadroomMeasurement{}) {
		t.Fatalf("measurement = %+v, want zero value on refusal", m)
	}
	if p.readFreeCalls != 1 {
		t.Fatalf("readFree calls = %d, want 1 (resident must be read to be compared)", p.readFreeCalls)
	}
	if p.requestMemCalls != 0 {
		t.Fatalf("requestMem calls = %d, want 0 for a refused resident model", p.requestMemCalls)
	}
}

func TestMeasure_Success_CarriesProbeValuesAndFixedTimestamp(t *testing.T) {
	p := &fakeProbe{freeMB: 5119, residentModel: "llama3.1:8b", perRequestMB: 733}
	deps := baseDeps(p)

	m, err := Measure(context.Background(), deps)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	want := HeadroomMeasurement{
		Node:          "gpu-a",
		FreeMB:        5119,
		PerRequestMB:  733,
		ResidentModel: "llama3.1:8b",
		MeasuredAt:    fixedNow,
	}
	if m != want {
		t.Fatalf("measurement = %+v, want %+v", m, want)
	}
	if p.readFreeCalls != 1 || p.requestMemCalls != 1 {
		t.Fatalf("calls = read %d, request %d; want 1, 1", p.readFreeCalls, p.requestMemCalls)
	}
	// The probe request must use the smallest allowed model (the
	// caller's sort is the contract Measure relies on).
	if p.lastProbeModel != "llama3.1:8b" {
		t.Fatalf("probe model = %q, want smallest allowed %q", p.lastProbeModel, "llama3.1:8b")
	}
}

func TestMeasure_PerProcessMB_PreferObserverValueAndSkipProbeRequest(t *testing.T) {
	p := &fakeProbe{freeMB: 2048, residentModel: "qwen3:32b", perRequestMB: 0}
	deps := baseDeps(p)
	deps.PerProcessMB = 450

	m, err := Measure(context.Background(), deps)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if m.PerRequestMB != 450 {
		t.Fatalf("PerRequestMB = %d, want PerProcessMB 450", m.PerRequestMB)
	}
	if m.FreeMB != 2048 || m.ResidentModel != "qwen3:32b" {
		t.Fatalf("measurement = %+v, want free 2048 resident qwen3:32b", m)
	}
	if m.MeasuredAt != fixedNow {
		t.Fatalf("MeasuredAt = %v, want injected clock %v", m.MeasuredAt, fixedNow)
	}
	if p.requestMemCalls != 0 {
		t.Fatalf("requestMem calls = %d, want 0 when PerProcessMB is set", p.requestMemCalls)
	}
}

func TestMeasure_ProbeReadError_IsReturned(t *testing.T) {
	p := &fakeProbe{readFreeErr: fmt.Errorf("connection refused")}
	deps := baseDeps(p)

	_, err := Measure(context.Background(), deps)
	if err == nil {
		t.Fatal("Measure: want error from failing ReadFreeMB, got nil")
	}
	if p.requestMemCalls != 0 {
		t.Fatalf("requestMem calls = %d, want 0 after a failed read", p.requestMemCalls)
	}
}

func TestMeasure_ProbeRequestError_IsReturned(t *testing.T) {
	p := &fakeProbe{freeMB: 4096, residentModel: "llama3.1:8b", requestMemErr: fmt.Errorf("probe request timed out")}
	deps := baseDeps(p)

	_, err := Measure(context.Background(), deps)
	if err == nil {
		t.Fatal("Measure: want error from failing RequestMemoryMB, got nil")
	}
}

// ---- Concrete Ollama probe, driven through a real httptest server. ----

const (
	probeGB = uint64(1) << 30
	probeMB = uint64(1) << 20
)

// ollamaFake is an httptest Ollama. It counts every hit, records the model
// of each /api/generate request, and grows the observer's loaded memory by
// growBytes per successful generate, the way a real node would.
type ollamaFake struct {
	srv *httptest.Server

	hits         atomic.Int64
	generateHits atomic.Int64

	mu        sync.Mutex
	lastModel string
	status    int    // generate HTTP status; 0 means 200
	growBytes uint64 // added to the observer's loaded memory per generate
	resident  string // what /api/ps reports as loaded

	obsMu sync.Mutex
	obs   NodeObservation
}

func newOllamaFake(t *testing.T, obs NodeObservation) *ollamaFake {
	t.Helper()
	f := &ollamaFake{obs: obs, resident: "qwen3:4b"}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/generate", func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.generateHits.Add(1)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.lastModel = req.Model
		status, grow := f.status, f.growBytes
		f.mu.Unlock()
		if status != 0 && status != http.StatusOK {
			http.Error(w, "boom", status)
			return
		}
		f.obsMu.Lock()
		if len(f.obs.LoadedModelBytes) > 0 {
			f.obs.LoadedModelBytes[0] += grow
		}
		f.obsMu.Unlock()
		_, _ = w.Write([]byte(`{"done":true}`))
	})
	mux.HandleFunc("/api/ps", func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.mu.Lock()
		name := f.resident
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"models":[{"name":"` + name + `"}]}`))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// observe is the injected observer reader. It returns a copy so the probe
// never aliases the fake's mutable state.
func (f *ollamaFake) observe(Node) NodeObservation {
	f.obsMu.Lock()
	defer f.obsMu.Unlock()
	out := f.obs
	out.LoadedModelBytes = append([]uint64(nil), f.obs.LoadedModelBytes...)
	out.CPUModelBytes = append([]uint64(nil), f.obs.CPUModelBytes...)
	out.GPUs = append([]GPUMemory(nil), f.obs.GPUs...)
	return out
}

func (f *ollamaFake) setStatus(code int)      { f.mu.Lock(); f.status = code; f.mu.Unlock() }
func (f *ollamaFake) setGrow(b uint64)        { f.mu.Lock(); f.growBytes = b; f.mu.Unlock() }
func (f *ollamaFake) setResident(name string) { f.mu.Lock(); f.resident = name; f.mu.Unlock() }
func (f *ollamaFake) model() string           { f.mu.Lock(); defer f.mu.Unlock(); return f.lastModel }

// baseObservation: a 24 GB GPU with 4 GB used (20480 MB free), a 4 GB model
// loaded, and qwen3:4b reported as resident.
func baseObservation() NodeObservation {
	return NodeObservation{
		GPUs:             []GPUMemory{{TotalBytes: 24 * probeGB, UsedBytes: 4 * probeGB}},
		LoadedModelBytes: []uint64{4 * probeGB},
		ResidentModel:    "qwen3:4b",
	}
}

func fakeNode(srv *httptest.Server) Node {
	return Node{Name: "gpu-a", Kind: KindOllama, Address: srv.URL}
}

func measureWith(t *testing.T, f *ollamaFake, allowed []string, held int) (HeadroomMeasurement, error) {
	t.Helper()
	probe := NewOllamaHeadroomProbe(f.observe, f.srv.Client())
	return Measure(context.Background(), MeasureDeps{
		Probe:         probe,
		Now:           func() time.Time { return fixedNow },
		Held:          held,
		AllowedModels: allowed,
		Node:          fakeNode(f.srv),
	})
}

func TestOllamaProbe_Success_MeasuresViaProbeRequest(t *testing.T) {
	f := newOllamaFake(t, baseObservation())
	f.setGrow(300 * probeMB)

	m, err := measureWith(t, f, []string{"qwen3:4b", "qwen3:35b"}, 0)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	want := HeadroomMeasurement{
		Node:          "gpu-a",
		FreeMB:        20480,
		PerRequestMB:  300,
		ResidentModel: "qwen3:4b",
		MeasuredAt:    fixedNow,
	}
	if m != want {
		t.Fatalf("measurement = %+v, want %+v", m, want)
	}
	if n := f.generateHits.Load(); n != 1 {
		t.Fatalf("generate hits = %d, want exactly 1 probe request", n)
	}
}

func TestOllamaProbe_BusyNode_NoHTTPOrObserverRead(t *testing.T) {
	f := newOllamaFake(t, baseObservation())
	reads := 0
	probe := NewOllamaHeadroomProbe(func(n Node) NodeObservation { reads++; return f.observe(n) }, f.srv.Client())

	_, err := Measure(context.Background(), MeasureDeps{
		Probe: probe, Now: func() time.Time { return fixedNow },
		Held: 2, AllowedModels: []string{"qwen3:4b"}, Node: fakeNode(f.srv),
	})
	if !errors.Is(err, ErrNodeBusy) {
		t.Fatalf("err = %v, want ErrNodeBusy", err)
	}
	if f.hits.Load() != 0 || reads != 0 {
		t.Fatalf("busy node was touched: hits=%d observer reads=%d, want 0/0", f.hits.Load(), reads)
	}
}

func TestOllamaProbe_ResidentModelNotAllowed_Refused(t *testing.T) {
	obs := baseObservation()
	obs.ResidentModel = "llama3:70b"
	f := newOllamaFake(t, obs)

	m, err := measureWith(t, f, []string{"qwen3:4b", "qwen3:35b"}, 0)
	if !errors.Is(err, ErrModelNotAllowed) {
		t.Fatalf("err = %v, want ErrModelNotAllowed", err)
	}
	if m != (HeadroomMeasurement{}) {
		t.Fatalf("measurement = %+v, want zero on refusal", m)
	}
	if f.generateHits.Load() != 0 {
		t.Fatalf("probe request sent for a refused model set")
	}
}

func TestOllamaProbe_EmptyAllowList_Refused(t *testing.T) {
	f := newOllamaFake(t, baseObservation())
	_, err := measureWith(t, f, nil, 0)
	if !errors.Is(err, ErrModelNotAllowed) {
		t.Fatalf("err = %v, want ErrModelNotAllowed", err)
	}
	if f.hits.Load() != 0 {
		t.Fatalf("hits = %d, want 0", f.hits.Load())
	}
}

func TestOllamaProbe_ObserverPerProcess_SkipsHTTPRequest(t *testing.T) {
	obs := baseObservation()
	obs.PerRequestBytes = 512 * probeMB
	f := newOllamaFake(t, obs)

	m, err := measureWith(t, f, []string{"qwen3:4b", "qwen3:35b"}, 0)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if m.PerRequestMB != 512 {
		t.Fatalf("PerRequestMB = %d, want 512 from observer per-process memory", m.PerRequestMB)
	}
	if n := f.hits.Load(); n != 0 {
		t.Fatalf("server hits = %d on the observer per-process path, want 0", n)
	}
}

func TestOllamaProbe_ProbeRequestError_IsReturned(t *testing.T) {
	f := newOllamaFake(t, baseObservation())
	f.setStatus(http.StatusInternalServerError)

	m, err := measureWith(t, f, []string{"qwen3:4b"}, 0)
	if err == nil {
		t.Fatal("err = nil, want probe request error")
	}
	if errors.Is(err, ErrNodeBusy) || errors.Is(err, ErrModelNotAllowed) {
		t.Fatalf("err = %v, want a plain probe error", err)
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v, want it to name HTTP 500", err)
	}
	if m != (HeadroomMeasurement{}) {
		t.Fatalf("measurement = %+v, want zero on error", m)
	}
}

func TestOllamaProbe_ProbeModelIs4bWhen4bAnd35bAllowed(t *testing.T) {
	f := newOllamaFake(t, baseObservation())
	f.setGrow(300 * probeMB)

	smallest, ok := SmallestAllowedModel([]ModelInfo{{Name: "qwen3:35b"}, {Name: "qwen3:4b"}})
	if !ok || smallest.Name != "qwen3:4b" {
		t.Fatalf("smallest = %+v, want qwen3:4b", smallest)
	}
	allowed := []string{smallest.Name, "qwen3:35b"}

	if _, err := measureWith(t, f, allowed, 0); err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if got := f.model(); got != "qwen3:4b" {
		t.Fatalf("probe model = %q, want qwen3:4b", got)
	}
}

func TestOllamaProbe_NoGPUData_FallsBackToSystemMemory(t *testing.T) {
	obs := NodeObservation{
		SystemTotalBytes: 64 * probeGB,
		SystemUsedBytes:  16 * probeGB,
		LoadedModelBytes: []uint64{4 * probeGB},
		ResidentModel:    "qwen3:4b",
	}
	f := newOllamaFake(t, obs)
	f.setGrow(200 * probeMB)

	m, err := measureWith(t, f, []string{"qwen3:4b"}, 0)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if m.FreeMB != 49152 {
		t.Fatalf("FreeMB = %d, want 49152 (system free)", m.FreeMB)
	}
	if m.PerRequestMB != 200 {
		t.Fatalf("PerRequestMB = %d, want 200", m.PerRequestMB)
	}
}

func TestOllamaProbe_ResidentFromPS_WhenObserverSilent(t *testing.T) {
	obs := baseObservation()
	obs.ResidentModel = ""
	f := newOllamaFake(t, obs)
	f.setGrow(300 * probeMB)
	f.setResident("qwen3:4b")

	m, err := measureWith(t, f, []string{"qwen3:4b", "qwen3:35b"}, 0)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if m.ResidentModel != "qwen3:4b" {
		t.Fatalf("ResidentModel = %q, want qwen3:4b from /api/ps", m.ResidentModel)
	}
	if f.hits.Load() != 2 { // /api/ps and one /api/generate
		t.Fatalf("hits = %d, want 2", f.hits.Load())
	}
}

func TestOllamaProbe_NoGrowthAfterProbe_IsError(t *testing.T) {
	f := newOllamaFake(t, baseObservation())
	f.setGrow(0)

	_, err := measureWith(t, f, []string{"qwen3:4b"}, 0)
	if err == nil || !strings.Contains(err.Error(), "no growth") {
		t.Fatalf("err = %v, want a no-growth error (a zero figure would read requests as free)", err)
	}
}

func TestOllamaProbe_NoObserverData_IsError(t *testing.T) {
	f := newOllamaFake(t, NodeObservation{})
	_, err := measureWith(t, f, []string{"qwen3:4b"}, 0)
	if err == nil {
		t.Fatal("err = nil, want no-observer-data error")
	}
	if f.hits.Load() != 0 {
		t.Fatalf("hits = %d, want 0 when the observer has no data", f.hits.Load())
	}
}

func TestOllamaProbe_NoObserverReader_IsError(t *testing.T) {
	probe := NewOllamaHeadroomProbe(nil, nil)
	if _, _, err := probe.ReadFreeMB(context.Background(), measureTestNode()); err == nil {
		t.Fatal("ReadFreeMB err = nil with no observer reader")
	}
}

func TestOllamaProbe_NoFreeMemoryData_IsError(t *testing.T) {
	obs := NodeObservation{SystemTotalBytes: 8 * probeGB, SystemUsedBytes: 8 * probeGB, ResidentModel: "qwen3:4b"}
	f := newOllamaFake(t, obs)
	probe := NewOllamaHeadroomProbe(f.observe, f.srv.Client())
	if _, _, err := probe.ReadFreeMB(context.Background(), fakeNode(f.srv)); err == nil {
		t.Fatal("ReadFreeMB err = nil with no free memory")
	}
}

func TestOllamaProbe_NoAddress_IsError(t *testing.T) {
	obs := baseObservation()
	obs.ResidentModel = ""
	f := newOllamaFake(t, obs)
	probe := NewOllamaHeadroomProbe(f.observe, nil)
	if _, _, err := probe.ReadFreeMB(context.Background(), Node{Name: "gpu-a"}); err == nil {
		t.Fatal("ReadFreeMB err = nil for a node with no address and no reported model")
	}
}

func TestOllamaProbe_DefaultClient_Works(t *testing.T) {
	f := newOllamaFake(t, baseObservation())
	f.setGrow(300 * probeMB)
	probe := NewOllamaHeadroomProbe(f.observe, nil)
	if _, err := probe.RequestMemoryMB(context.Background(), fakeNode(f.srv), "qwen3:4b"); err != nil {
		t.Fatalf("RequestMemoryMB with default client: %v", err)
	}
}

func TestOllamaProbe_PSAndGenerateFailures_AreErrors(t *testing.T) {
	obs := baseObservation()
	obs.ResidentModel = ""
	f := newOllamaFake(t, obs)
	probe := NewOllamaHeadroomProbe(f.observe, f.srv.Client())
	node := fakeNode(f.srv)

	// /api/ps lists no model: resident is "" with no error, which Measure refuses.
	f.mu.Lock()
	f.resident = "" // empty list path returns "" with no error
	f.mu.Unlock()
	if _, res, err := probe.ReadFreeMB(context.Background(), node); err != nil || res != "" {
		t.Fatalf("no model loaded: resident=%q err=%v, want empty and nil", res, err)
	}

	// Transport failure: a closed server refuses the connection.
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	dead := Node{Name: "gpu-dead", Kind: KindOllama, Address: closed.URL}
	if _, err := probe.RequestMemoryMB(context.Background(), dead, "qwen3:4b"); err == nil {
		t.Fatal("RequestMemoryMB err = nil for an unreachable node")
	}
	if _, _, err := probe.ReadFreeMB(context.Background(), dead); err == nil {
		// observer still answers, resident comes from /api/ps, which is unreachable
		t.Fatal("ReadFreeMB err = nil for an unreachable /api/ps")
	}

	// Malformed /api/ps body and non-200 /api/ps.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ps" {
			_, _ = w.Write([]byte("not json"))
			return
		}
		http.Error(w, "nope", http.StatusTeapot)
	}))
	t.Cleanup(bad.Close)
	badNode := Node{Name: "gpu-bad", Kind: KindOllama, Address: bad.URL}
	if _, _, err := probe.ReadFreeMB(context.Background(), badNode); err == nil {
		t.Fatal("ReadFreeMB err = nil for malformed /api/ps")
	}
	if _, err := probe.RequestMemoryMB(context.Background(), badNode, "qwen3:4b"); err == nil {
		t.Fatal("RequestMemoryMB err = nil for HTTP 418 from generate")
	}
	status := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(status.Close)
	if _, _, err := probe.ReadFreeMB(context.Background(), Node{Name: "gpu-503", Kind: KindOllama, Address: status.URL}); err == nil {
		t.Fatal("ReadFreeMB err = nil for HTTP 503 from /api/ps")
	}
}
