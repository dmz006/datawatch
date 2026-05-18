// BL321 unit tests — Gemini adapter.

package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

func TestGemini_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]string{
							{"text": "Hello, world!"},
						},
					},
				},
			},
		})
	}))
	defer ts.Close()

	a := &Gemini{}
	node := &compute.Node{Name: "gemini-node", Address: ts.URL, Kind: compute.KindOpenAICompat}
	llm := &inference.LLM{
		Name:      "gemini-test",
		Kind:      inference.KindGeminiAPI,
		Model:     "gemini-1.5-flash",
		APIKeyRef: "test-key",
	}

	resp, err := a.Infer(context.Background(), node, llm, inference.Request{Prompt: "Hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", resp.Text)
	}
	if resp.UsedModel != "gemini-1.5-flash" {
		t.Errorf("expected model=gemini-1.5-flash, got %q", resp.UsedModel)
	}
}

func TestGemini_MissingAPIKey(t *testing.T) {
	a := &Gemini{}
	llm := &inference.LLM{
		Name:  "gemini-test",
		Kind:  inference.KindGeminiAPI,
		Model: "gemini-1.5-flash",
		// APIKeyRef intentionally empty
	}
	_, err := a.Infer(context.Background(), nil, llm, inference.Request{Prompt: "Hi"})
	if err == nil {
		t.Fatal("expected error for missing api_key_ref")
	}
}

func TestGemini_500_Transient(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	a := &Gemini{}
	node := &compute.Node{Name: "gemini-node", Address: ts.URL, Kind: compute.KindOpenAICompat}
	llm := &inference.LLM{
		Name:      "gemini-test",
		Kind:      inference.KindGeminiAPI,
		Model:     "gemini-1.5-flash",
		APIKeyRef: "test-key",
	}
	_, err := a.Infer(context.Background(), node, llm, inference.Request{Prompt: "Hi"})
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !inference.IsTransient(err) {
		t.Errorf("expected ErrTransient for 500, got: %v", err)
	}
}
