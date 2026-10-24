package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

// TestNodeObservationReadsObserverDetail serves a fake observer payload from a
// node's monitoring endpoint and checks the figures the slot count reads: the
// GPUs' memory, the VRAM of the loaded models, and the busiest GPU.
func TestNodeObservationReadsObserverDetail(t *testing.T) {
	const payload = `{
		"gpu": [
			{"util_pct": 42, "mem_used_bytes": 1000, "mem_total_bytes": 24000},
			{"util_pct": 10, "mem_used_bytes": 0, "mem_total_bytes": 24000}
		],
		"envelopes": [
			{"id": "ollama_model:qwen", "gpu_mem_bytes": 20000},
			{"id": "session:abc", "gpu_mem_bytes": 5000},
			{"id": "ollama_model:cpu-only", "gpu_mem_bytes": 0}
		]
	}`
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer stub.Close()

	reg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.Add(&compute.Node{Name: "gpubox", Kind: "ollama", Address: "http://gpubox:11434", MonitoringEndpoint: stub.URL}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	hs := &HTTPServer{api: &Server{computeReg: reg}}

	obs, ok := hs.NodeObservation("gpubox")
	if !ok {
		t.Fatal("NodeObservation: ok = false, want true")
	}
	if len(obs.GPUs) != 2 {
		t.Fatalf("GPUs = %d, want 2", len(obs.GPUs))
	}
	if obs.GPUs[0] != (compute.GPUMemory{TotalBytes: 24000, UsedBytes: 1000}) {
		t.Errorf("GPU 0 = %+v, want total 24000 used 1000", obs.GPUs[0])
	}
	if obs.MaxUtilPct != 42 {
		t.Errorf("MaxUtilPct = %v, want 42", obs.MaxUtilPct)
	}
	if len(obs.LoadedModelBytes) != 1 || obs.LoadedModelBytes[0] != 20000 {
		t.Errorf("LoadedModelBytes = %v, want [20000] (only the model with VRAM)", obs.LoadedModelBytes)
	}
}
