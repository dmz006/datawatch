// v9.1.3 unit tests — OpenCode (ask-path) adapter. opencode registry
// entries carry opencode's "ollama/<model>" provider prefix, which the
// raw Ollama /api/generate endpoint rejects with 404; Infer must strip it
// from whichever model wins (LLM.Model or the request-level override).

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

func TestOpenCode_Infer_ModelPrefix(t *testing.T) {
	cases := []struct {
		name      string
		llmModel  string
		override  string
		wantModel string
	}{
		{name: "prefixed registry model", llmModel: "ollama/qwen3:1.7b", wantModel: "qwen3:1.7b"},
		{name: "bare registry model", llmModel: "qwen3:1.7b", wantModel: "qwen3:1.7b"},
		{name: "prefixed request override wins", llmModel: "ollama/qwen3:1.7b", override: "ollama/granite4.2:8b", wantModel: "granite4.2:8b"},
		{name: "bare request override wins", llmModel: "ollama/qwen3:1.7b", override: "granite4.2:8b", wantModel: "granite4.2:8b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotModel, gotPath string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				gotModel, _ = body["model"].(string)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"response": " OK "})
			}))
			defer ts.Close()

			a := &OpenCode{}
			node := &compute.Node{Name: "oc-node", Address: ts.URL, Kind: compute.KindOllama}
			llm := &inference.LLM{Name: "opencode-llm", Kind: inference.KindOpenCode, Model: tc.llmModel}

			resp, err := a.Infer(context.Background(), node, llm,
				inference.Request{Prompt: "hi", ModelOverride: tc.override})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotPath != "/api/generate" {
				t.Errorf("path = %q, want /api/generate", gotPath)
			}
			if gotModel != tc.wantModel {
				t.Errorf("wire model = %q, want %q", gotModel, tc.wantModel)
			}
			if resp.UsedModel != tc.wantModel {
				t.Errorf("UsedModel = %q, want %q", resp.UsedModel, tc.wantModel)
			}
			if resp.Backend != inference.KindOpenCode {
				t.Errorf("Backend = %q, want %q", resp.Backend, inference.KindOpenCode)
			}
			if resp.Text != "OK" {
				t.Errorf("Text = %q, want OK", resp.Text)
			}
		})
	}
}
