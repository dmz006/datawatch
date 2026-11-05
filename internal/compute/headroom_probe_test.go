package compute

import (
	"context"
	"errors"
	"fmt"
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

func testNode() Node { return Node{Name: "gpu-a", Kind: KindOllama} }

func baseDeps(p HeadroomProbe) MeasureDeps {
	return MeasureDeps{
		Probe:         p,
		Now:           func() time.Time { return fixedNow },
		Held:          0,
		AllowedModels: []string{"llama3.1:8b", "qwen3:32b"}, // smallest first
		Node:          testNode(),
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
