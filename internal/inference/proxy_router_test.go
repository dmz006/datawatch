// BL318 unit tests — ProxyRouter using httptest.Server.

package inference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProxyRouter_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text":        "hello from peer",
			"used_model":  "llama3",
			"duration_ms": int64(42),
		})
	}))
	defer ts.Close()

	router := &ProxyRouter{PeerURL: ts.URL}
	resp, err := router.Infer(context.Background(), "my-llm", Request{
		Prompt: "say hi",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "hello from peer" {
		t.Errorf("expected 'hello from peer', got %q", resp.Text)
	}
	if resp.UsedModel != "llama3" {
		t.Errorf("expected 'llama3', got %q", resp.UsedModel)
	}
	if resp.Backend != "datawatch-proxy" {
		t.Errorf("expected backend=datawatch-proxy, got %q", resp.Backend)
	}
}

func TestProxyRouter_500_Transient(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	router := &ProxyRouter{PeerURL: ts.URL}
	_, err := router.Infer(context.Background(), "my-llm", Request{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !IsTransient(err) {
		t.Errorf("expected ErrTransient for 500, got: %v", err)
	}
}

func TestProxyRouter_404_FinalError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	router := &ProxyRouter{PeerURL: ts.URL}
	_, err := router.Infer(context.Background(), "my-llm", Request{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if IsTransient(err) {
		t.Errorf("expected final (non-transient) error for 404, got transient: %v", err)
	}
}

func TestProxyRouter_401_FinalError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer ts.Close()

	router := &ProxyRouter{PeerURL: ts.URL}
	_, err := router.Infer(context.Background(), "my-llm", Request{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if IsTransient(err) {
		t.Errorf("expected final error for 401, got transient: %v", err)
	}
}

func TestProxyRouter_EmptyPeerURL(t *testing.T) {
	router := &ProxyRouter{}
	_, err := router.Infer(context.Background(), "my-llm", Request{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for empty peer URL")
	}
}
