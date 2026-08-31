// BL368 — unit tests for internal/vision/service.go.
//
// TS-BL368-V1: New validates required fields
// TS-BL368-V2: New defaults (backend, prompt, maxBytes)
// TS-BL368-V3: Describe rejects oversized image
// TS-BL368-V4: Describe routes unknown backend
// TS-BL368-V5: describeOllama happy path
// TS-BL368-V6: describeOllama non-200 response
// TS-BL368-V7: describeOllama empty response field
// TS-BL368-V8: describeOpenAI happy path
// TS-BL368-V9: describeOpenAI empty choices
// TS-BL368-V10: describeOpenAI non-200 response

package vision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TS-BL368-V1
func TestNew_MissingEndpoint(t *testing.T) {
	_, err := New(Config{Model: "llava"})
	if err == nil || !strings.Contains(err.Error(), "endpoint") {
		t.Fatalf("want endpoint error, got %v", err)
	}
}

func TestNew_MissingModel(t *testing.T) {
	_, err := New(Config{Endpoint: "http://localhost:11434"})
	if err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("want model error, got %v", err)
	}
}

// TS-BL368-V2
func TestNew_Defaults(t *testing.T) {
	v, err := New(Config{Endpoint: "http://localhost:11434", Model: "llava"})
	if err != nil {
		t.Fatal(err)
	}
	if v.cfg.Backend != "ollama" {
		t.Errorf("default backend: got %q want ollama", v.cfg.Backend)
	}
	if v.cfg.DefaultPrompt != defaultPrompt {
		t.Errorf("default prompt: got %q want %q", v.cfg.DefaultPrompt, defaultPrompt)
	}
	if v.cfg.MaxImageBytes != defaultMaxBytes {
		t.Errorf("default maxBytes: got %d want %d", v.cfg.MaxImageBytes, defaultMaxBytes)
	}
}

// TS-BL368-V3
func TestDescribe_TooLarge(t *testing.T) {
	v, _ := New(Config{Endpoint: "http://localhost:11434", Model: "llava"})
	v.cfg.MaxImageBytes = 4
	_, err := v.Describe(context.Background(), []byte("hello"), "image/jpeg", "")
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("want too-large error, got %v", err)
	}
}

// TS-BL368-V4
func TestDescribe_UnknownBackend(t *testing.T) {
	v, _ := New(Config{Endpoint: "http://localhost:11434", Model: "llava", Backend: "magic"})
	_, err := v.Describe(context.Background(), []byte("px"), "image/jpeg", "")
	if err == nil || !strings.Contains(err.Error(), "unknown backend") {
		t.Fatalf("want unknown backend error, got %v", err)
	}
}

// TS-BL368-V5
func TestDescribeOllama_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			http.Error(w, "bad path", 404)
			return
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "llava" {
			http.Error(w, "bad model", 400)
			return
		}
		if _, ok := body["images"]; !ok {
			http.Error(w, "missing images", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "a green circle on white"})
	}))
	defer srv.Close()

	v, _ := New(Config{Endpoint: srv.URL, Model: "llava", Backend: "ollama"})
	desc, err := v.Describe(context.Background(), []byte("fake-image-bytes"), "image/png", "")
	if err != nil {
		t.Fatal(err)
	}
	if desc != "a green circle on white" {
		t.Errorf("got %q", desc)
	}
}

// TS-BL368-V6
func TestDescribeOllama_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()

	v, _ := New(Config{Endpoint: srv.URL, Model: "llava", Backend: "ollama"})
	_, err := v.Describe(context.Background(), []byte("x"), "image/jpeg", "")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want 404 error, got %v", err)
	}
}

// TS-BL368-V7
func TestDescribeOllama_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "   "})
	}))
	defer srv.Close()

	v, _ := New(Config{Endpoint: srv.URL, Model: "llava", Backend: "ollama"})
	desc, err := v.Describe(context.Background(), []byte("x"), "image/jpeg", "")
	if err != nil {
		t.Fatal(err)
	}
	if desc != "" {
		t.Errorf("expected empty (trimmed), got %q", desc)
	}
}

// TS-BL368-V8
func TestDescribeOpenAI_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "bad path", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "a red square"}},
			},
		})
	}))
	defer srv.Close()

	v, _ := New(Config{Endpoint: srv.URL, Model: "gpt-4o", Backend: "openai"})
	desc, err := v.Describe(context.Background(), []byte("fake"), "image/png", "describe it")
	if err != nil {
		t.Fatal(err)
	}
	if desc != "a red square" {
		t.Errorf("got %q", desc)
	}
}

// TS-BL368-V9
func TestDescribeOpenAI_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []interface{}{}})
	}))
	defer srv.Close()

	v, _ := New(Config{Endpoint: srv.URL, Model: "gpt-4o", Backend: "openai"})
	_, err := v.Describe(context.Background(), []byte("x"), "image/jpeg", "")
	if err == nil || !strings.Contains(err.Error(), "empty choices") {
		t.Fatalf("want empty-choices error, got %v", err)
	}
}

// TS-BL368-V10
func TestDescribeOpenAI_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	v, _ := New(Config{Endpoint: srv.URL, Model: "gpt-4o", Backend: "openai_compat"})
	_, err := v.Describe(context.Background(), []byte("x"), "image/jpeg", "")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("want 401 error, got %v", err)
	}
}
