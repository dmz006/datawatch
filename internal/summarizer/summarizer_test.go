package summarizer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// TestSummarize_Disabled verifies that a disabled summarizer returns empty
// string without error.
func TestSummarize_Disabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Session.Summarizer.Enabled = false
	cfg.Session.Summarizer.LLMRef = "my-llm"

	svc := New(cfg, nil)
	result, err := svc.Summarize(context.Background(), "some session output")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty string when disabled, got %q", result)
	}
}

// TestSummarize_NoLLMRef verifies that a missing LLMRef returns empty string.
func TestSummarize_NoLLMRef(t *testing.T) {
	cfg := &config.Config{}
	cfg.Session.Summarizer.Enabled = true
	cfg.Session.Summarizer.LLMRef = ""

	svc := New(cfg, nil)
	result, err := svc.Summarize(context.Background(), "some session output")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty string when LLMRef is empty, got %q", result)
	}
}

// TestSummarize_OllamaRoundTrip uses an httptest.Server that mimics Ollama's
// API, verifies the request format, and confirms the response is parsed correctly.
func TestSummarize_OllamaRoundTrip(t *testing.T) {
	const wantSummary = "The session completed successfully with no errors."
	var gotModel, gotPrompt string
	gotStream := true // should be set to false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		gotModel = body.Model
		gotPrompt = body.Prompt
		gotStream = body.Stream
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"response": wantSummary}) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Session.Summarizer.Enabled = true
	cfg.Session.Summarizer.LLMRef = "test-ollama"
	cfg.Ollama.Enabled = true
	cfg.Ollama.Host = srv.URL
	cfg.Ollama.Model = "llama3"

	// Build a minimal inference registry with an ollama entry.
	reg := inference.NewRegistry()
	err := reg.Add(&inference.LLM{
		Name:  "test-ollama",
		Kind:  inference.KindOllama,
		Model: "llama3",
	})
	if err != nil {
		t.Fatalf("registry add: %v", err)
	}

	svc := New(cfg, reg)
	result, err := svc.Summarize(context.Background(), "session output here")
	if err != nil {
		t.Fatalf("Summarize error: %v", err)
	}
	if result != wantSummary {
		t.Errorf("summary mismatch: got %q, want %q", result, wantSummary)
	}
	if gotModel != "llama3" {
		t.Errorf("model sent: got %q, want %q", gotModel, "llama3")
	}
	if gotStream {
		t.Errorf("stream should be false, got true")
	}
	if !strings.Contains(gotPrompt, DefaultSummarizerPrompt) {
		t.Errorf("prompt should contain DefaultSummarizerPrompt, got: %q", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "session output here") {
		t.Errorf("prompt should contain the session text, got: %q", gotPrompt)
	}
}

// TestSummarize_DefaultPrompt verifies that the DefaultSummarizerPrompt
// constant is not empty.
func TestSummarize_DefaultPrompt(t *testing.T) {
	if strings.TrimSpace(DefaultSummarizerPrompt) == "" {
		t.Fatal("DefaultSummarizerPrompt must not be empty")
	}
}

// TestSummarize_CustomPrompt verifies that a custom prompt is used when set.
func TestSummarize_CustomPrompt(t *testing.T) {
	const customPrompt = "One sentence summary only."
	var gotPrompt string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt string `json:"prompt"`
		}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		gotPrompt = body.Prompt
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"response": "done"}) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Session.Summarizer.Enabled = true
	cfg.Session.Summarizer.LLMRef = "test-ollama"
	cfg.Session.Summarizer.Prompt = customPrompt
	cfg.Ollama.Enabled = true
	cfg.Ollama.Host = srv.URL
	cfg.Ollama.Model = "llama3"

	reg := inference.NewRegistry()
	err := reg.Add(&inference.LLM{Name: "test-ollama", Kind: inference.KindOllama, Model: "llama3"})
	if err != nil {
		t.Fatalf("registry add: %v", err)
	}

	svc := New(cfg, reg)
	_, _ = svc.Summarize(context.Background(), "output")

	if !strings.Contains(gotPrompt, customPrompt) {
		t.Errorf("expected custom prompt %q in request, got %q", customPrompt, gotPrompt)
	}
	if strings.Contains(gotPrompt, DefaultSummarizerPrompt) {
		t.Errorf("should not use default prompt when custom prompt is set")
	}
}
