// BL321 unit tests — OpenCodeAPI adapter.

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

func TestOpenCodeAPI_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"content": "response from opencode",
					},
				},
			},
		})
	}))
	defer ts.Close()

	a := &OpenCodeAPI{}
	node := &compute.Node{Name: "oc-node", Address: ts.URL, Kind: compute.KindOpenAICompat}
	llm := &inference.LLM{
		Name:  "opencode-llm",
		Kind:  inference.KindOpenCodeAPI,
		Model: "claude-3-5-sonnet",
	}

	resp, err := a.Infer(context.Background(), node, llm, inference.Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "response from opencode" {
		t.Errorf("expected 'response from opencode', got %q", resp.Text)
	}
	if resp.UsedModel != "claude-3-5-sonnet" {
		t.Errorf("expected model=claude-3-5-sonnet, got %q", resp.UsedModel)
	}
}

func TestOpenCodeAPI_MissingAddress(t *testing.T) {
	a := &OpenCodeAPI{}
	llm := &inference.LLM{
		Name:  "opencode-llm",
		Kind:  inference.KindOpenCodeAPI,
		Model: "claude-3-5-sonnet",
	}

	// nil node
	_, err := a.Infer(context.Background(), nil, llm, inference.Request{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for nil node")
	}

	// node with empty address
	node := &compute.Node{Name: "empty-addr", Kind: compute.KindOpenAICompat}
	_, err = a.Infer(context.Background(), node, llm, inference.Request{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for empty address")
	}
}
