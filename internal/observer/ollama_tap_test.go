// BL180 Phase 1 — ollama tap tests. Use a local httptest server
// pretending to be ollama's /api/ps endpoint and assert the tap
// (a) decodes the model list correctly, (b) populates Caller +
// CallerKind on every emitted envelope, and (c) propagates HTTP
// failures via Snapshot's error return.

package observer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOllamaTap_PSDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ps" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"models":[
			{"name":"llama3.1:8b","model":"llama3.1:8b","size":4830000000,"size_vram":4500000000},
			{"name":"qwen2.5-coder:14b","size":9000000000,"size_vram":8500000000}
		]}`)
	}))
	defer srv.Close()

	tap := NewOllamaTap(srv.URL)
	if tap == nil {
		t.Fatal("NewOllamaTap returned nil for non-empty endpoint")
	}
	tap.poll(context.Background())
	envs, err := tap.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot error: %v", err)
	}
	if len(envs) != 2 {
		t.Fatalf("got %d envelopes, want 2", len(envs))
	}
	for _, e := range envs {
		if e.Kind != EnvelopeBackend {
			t.Errorf("envelope %s kind=%s want backend", e.ID, e.Kind)
		}
		if e.Caller == "" || e.CallerKind != "ollama_model" {
			t.Errorf("envelope %s missing Caller/CallerKind: %+v", e.ID, e)
		}
		if e.GPUMemBytes == 0 {
			t.Errorf("envelope %s GPU mem missing", e.ID)
		}
	}
	if envs[0].ID != "ollama_model:llama3.1:8b" {
		t.Errorf("envelope[0].ID = %q", envs[0].ID)
	}
}

func TestOllamaTap_HTTPErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	tap := NewOllamaTap(srv.URL)
	tap.poll(context.Background())
	_, err := tap.Snapshot()
	if err == nil {
		t.Fatal("expected error from HTTP 500")
	}
}

func TestOllamaTap_DisabledByEmptyEndpoint(t *testing.T) {
	if tap := NewOllamaTap(""); tap != nil {
		t.Errorf("NewOllamaTap(\"\") = %v; want nil", tap)
	}
	if tap := NewOllamaTap("   "); tap != nil {
		t.Errorf("NewOllamaTap(whitespace) = %v; want nil", tap)
	}
	// Snapshot on nil tap is also a clean no-op.
	var nilTap *OllamaTap
	envs, err := nilTap.Snapshot()
	if err != nil || envs != nil {
		t.Errorf("nil-tap Snapshot got envs=%v err=%v; want nil,nil", envs, err)
	}
}

func TestOllamaTap_StartCancelsCleanly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	defer srv.Close()
	tap := NewOllamaTap(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	tap.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()
	// Give the goroutine a beat to notice cancellation. No assertion;
	// we're just checking the goroutine doesn't panic on shutdown.
	time.Sleep(50 * time.Millisecond)
}
