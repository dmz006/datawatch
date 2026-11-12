// Model-pin tests for the summarizer's Ollama calls. A fake Ollama records the
// endpoint and model of every call, so each test can assert that a pinned node
// only ever receives its pinned name, and that a refused swap never reaches it.

package summarizer

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// pinCall is one request the fake Ollama received.
type pinCall struct {
	Path  string
	Model string
}

// pinFakeOllama answers the chat, generate and show endpoints and records each
// call's model.
type pinFakeOllama struct {
	mu    sync.Mutex
	calls []pinCall
}

func newPinFakeOllama(t *testing.T) (*httptest.Server, *pinFakeOllama) {
	t.Helper()
	fake := &pinFakeOllama{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
			Name  string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		model := body.Model
		if r.URL.Path == "/api/show" {
			model = body.Name
		}
		fake.mu.Lock()
		fake.calls = append(fake.calls, pinCall{Path: r.URL.Path, Model: model})
		fake.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/chat":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]string{"content": "===SHORT===\nDone.\n===LONG===\nMore."},
			})
		case "/api/generate":
			_ = json.NewEncoder(w).Encode(map[string]string{"response": "===SHORT===\nDone.\n===LONG===\nMore."})
		case "/api/show":
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, fake
}

func (f *pinFakeOllama) seen() []pinCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]pinCall(nil), f.calls...)
}

// pinConfig enables the summarizer with the Ollama fallback pointed at host.
func pinConfig(host string) *config.Config {
	cfg := &config.Config{}
	cfg.Ollama.Enabled = true
	cfg.Ollama.Host = host
	cfg.Ollama.Model = "default:7b"
	cfg.Session.Summarizer.Enabled = true
	cfg.Session.Summarizer.LLMRef = "summary-llm"
	return cfg
}

// pinRegistry builds a compute registry with one node at addr carrying the pin.
func pinRegistry(t *testing.T, addr, pin string, swap bool) *compute.Registry {
	t.Helper()
	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("compute registry: %v", err)
	}
	if err := reg.Add(&compute.Node{
		Name:            "datawatch-ollama",
		Kind:            compute.KindOllama,
		Address:         addr,
		NodePinnedModel: pin,
		AllowModelSwap:  swap,
	}); err != nil {
		t.Fatalf("add node: %v", err)
	}
	return reg
}

// pinLLMRegistry registers the summarizer's LLM as an ollama entry with its own
// default model, so the registry path is taken.
func pinLLMRegistry(t *testing.T, llmModel string) *inference.Registry {
	t.Helper()
	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{
		Name:  "summary-llm",
		Kind:  inference.KindOllama,
		Model: llmModel,
	}); err != nil {
		t.Fatalf("llm registry: %v", err)
	}
	return reg
}

// assertOnlyModel fails unless every recorded call used the wanted model.
func assertOnlyModel(t *testing.T, calls []pinCall, want string) {
	t.Helper()
	if len(calls) == 0 {
		t.Fatalf("fake Ollama received no calls, want model %q", want)
	}
	for _, c := range calls {
		if c.Model != want {
			t.Fatalf("call %s used model %q, want only %q (all calls: %+v)", c.Path, c.Model, want, calls)
		}
	}
}

func TestSummarize_FallbackSubstitutesPinnedModel(t *testing.T) {
	fake, rec := newPinFakeOllama(t)
	svc := New(pinConfig(fake.URL), nil)
	svc.SetComputeRegistry(pinRegistry(t, fake.URL, "pinned:1b", false))

	if _, err := svc.Summarize(context.Background(), "tests passed"); err != nil {
		t.Fatalf("summarize: %v", err)
	}
	assertOnlyModel(t, rec.seen(), "pinned:1b")
}

func TestSummarizeDual_ExplicitSwapRefusedAndLogged(t *testing.T) {
	fake, rec := newPinFakeOllama(t)
	cfg := pinConfig(fake.URL)
	cfg.Session.Summarizer.Model = "other:70b"
	svc := New(cfg, nil)
	svc.SetComputeRegistry(pinRegistry(t, fake.URL, "pinned:1b", false))

	var logs bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prev) })

	short, long, err := svc.SummarizeDual(context.Background(), "tests passed", "")
	if err != nil {
		t.Fatalf("summarize dual: %v", err)
	}
	if short != "Summary unavailable — LLM offline." || long != "" {
		t.Fatalf("refused call must degrade to the stub, got short=%q long=%q", short, long)
	}
	if got := rec.seen(); len(got) != 0 {
		t.Fatalf("refused swap reached the node: %+v", got)
	}
	for _, want := range []string{"pinned to pinned:1b", "other:70b", "allow_model_swap"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log %q does not contain %q", logs.String(), want)
		}
	}
}

func TestSummarizeDual_ExplicitSwapRunsWhenAllowed(t *testing.T) {
	fake, rec := newPinFakeOllama(t)
	cfg := pinConfig(fake.URL)
	cfg.Session.Summarizer.Model = "other:70b"
	svc := New(cfg, nil)
	svc.SetComputeRegistry(pinRegistry(t, fake.URL, "pinned:1b", true))

	if _, _, err := svc.SummarizeDual(context.Background(), "tests passed", ""); err != nil {
		t.Fatalf("summarize dual: %v", err)
	}
	assertOnlyModel(t, rec.seen(), "other:70b")
}

func TestSummarize_RegistryPathSubstitutesPinnedModel(t *testing.T) {
	fake, rec := newPinFakeOllama(t)
	svc := New(pinConfig(fake.URL), pinLLMRegistry(t, "llm:3b"))
	svc.SetComputeRegistry(pinRegistry(t, fake.URL, "pinned:1b", false))

	if _, err := svc.Summarize(context.Background(), "tests passed"); err != nil {
		t.Fatalf("summarize: %v", err)
	}
	assertOnlyModel(t, rec.seen(), "pinned:1b")
}

func TestContextLines_ProbesPinnedModel(t *testing.T) {
	fake, rec := newPinFakeOllama(t)
	svc := New(pinConfig(fake.URL), pinLLMRegistry(t, "llm:3b"))
	svc.SetComputeRegistry(pinRegistry(t, fake.URL, "pinned:1b", false))

	_ = svc.ContextLines()
	calls := rec.seen()
	if len(calls) != 1 || calls[0].Path != "/api/show" {
		t.Fatalf("want one /api/show probe, got %+v", calls)
	}
	assertOnlyModel(t, calls, "pinned:1b")
}

func TestSummarize_UnpinnedHostUsesConfiguredDefault(t *testing.T) {
	fake, rec := newPinFakeOllama(t)
	svc := New(pinConfig(fake.URL), nil)

	if _, err := svc.Summarize(context.Background(), "tests passed"); err != nil {
		t.Fatalf("summarize: %v", err)
	}
	assertOnlyModel(t, rec.seen(), "default:7b")
}
