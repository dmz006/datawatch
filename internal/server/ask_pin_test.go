// Model-pin tests for /api/ask and /api/assist. A fake Ollama records every
// model it is asked to run, so each test can assert that a pinned node only
// ever receives its pinned name, and that a refused swap never reaches it.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/inference/adapters"
)

// pinFakeOllama records the model of every /api/generate call it receives.
type pinFakeOllama struct {
	mu     sync.Mutex
	models []string
}

func newPinFakeOllama(t *testing.T) (*httptest.Server, *pinFakeOllama) {
	t.Helper()
	fake := &pinFakeOllama{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		fake.mu.Lock()
		fake.models = append(fake.models, body.Model)
		fake.mu.Unlock()
		_, _ = w.Write([]byte(`{"response":"fake answer"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, fake
}

func (f *pinFakeOllama) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.models...)
}

// pinnedAskServer points the ollama host at addr and registers one compute
// node at that address carrying the pin.
func pinnedAskServer(t *testing.T, addr, defaultModel, pin string, swap bool) *Server {
	t.Helper()
	s := bl90Server(t)
	s.cfg.Ollama.Host = addr
	s.cfg.Ollama.Model = defaultModel
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
	s.SetComputeRegistry(reg)
	return s
}

func postAsk(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleAsk(rr, req)
	return rr
}

func TestAskPin_ImplicitRequestRunsPinnedModel(t *testing.T) {
	fake, rec := newPinFakeOllama(t)
	s := pinnedAskServer(t, fake.URL, "default:7b", "pinned:1b", false)

	if rr := postAsk(t, s, `{"question":"hi"}`); rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rec.seen(); len(got) != 1 || got[0] != "pinned:1b" {
		t.Fatalf("fake received %v, want only [pinned:1b]", got)
	}
}

func TestAskPin_ExplicitPinnedModelRunsPin(t *testing.T) {
	fake, rec := newPinFakeOllama(t)
	s := pinnedAskServer(t, fake.URL, "default:7b", "pinned:1b", false)

	if rr := postAsk(t, s, `{"question":"hi","model":"pinned:1b"}`); rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rec.seen(); len(got) != 1 || got[0] != "pinned:1b" {
		t.Fatalf("fake received %v, want only [pinned:1b]", got)
	}
}

func TestAskPin_ExplicitOtherModelRefusedWithClearError(t *testing.T) {
	fake, rec := newPinFakeOllama(t)
	s := pinnedAskServer(t, fake.URL, "default:7b", "pinned:1b", false)

	rr := postAsk(t, s, `{"question":"hi","model":"other:70b"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	var out AskErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	for _, want := range []string{"pinned to pinned:1b", "other:70b", "allow_model_swap"} {
		if !strings.Contains(out.Error, want) {
			t.Errorf("error %q does not contain %q", out.Error, want)
		}
	}
	if got := rec.seen(); len(got) != 0 {
		t.Fatalf("refused swap reached the node: %v", got)
	}
}

func TestAskPin_ExplicitSwapRunsWhenAllowed(t *testing.T) {
	fake, rec := newPinFakeOllama(t)
	s := pinnedAskServer(t, fake.URL, "default:7b", "pinned:1b", true)

	if rr := postAsk(t, s, `{"question":"hi","model":"other:70b"}`); rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rec.seen(); len(got) != 1 || got[0] != "other:70b" {
		t.Fatalf("fake received %v, want [other:70b]", got)
	}
}

func TestAskPin_UnmatchedHostPassesDefaultThrough(t *testing.T) {
	fake, rec := newPinFakeOllama(t)
	s := bl90Server(t)
	s.cfg.Ollama.Host = fake.URL
	s.cfg.Ollama.Model = "default:7b"
	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("compute registry: %v", err)
	}
	// The pinned node sits at another address, so this host carries no pin.
	if err := reg.Add(&compute.Node{
		Name:            "elsewhere",
		Kind:            compute.KindOllama,
		Address:         "http://elsewhere:11434",
		NodePinnedModel: "pinned:1b",
	}); err != nil {
		t.Fatalf("add node: %v", err)
	}
	s.SetComputeRegistry(reg)

	if rr := postAsk(t, s, `{"question":"hi"}`); rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rec.seen(); len(got) != 1 || got[0] != "default:7b" {
		t.Fatalf("fake received %v, want [default:7b]", got)
	}
}

// Production path: an ollama LLM routed through the dispatcher to the pinned
// compute node. The pin must hold here too, not only on the legacy path.
func TestAskPin_DispatcherPathPinsAndRefuses(t *testing.T) {
	fake, rec := newPinFakeOllama(t)
	s := pinnedAskServer(t, fake.URL, "default:7b", "pinned:1b", false)

	llmReg := inference.NewRegistry()
	if err := llmReg.Add(&inference.LLM{
		Name:         "ollama",
		Kind:         inference.KindOllama,
		Model:        "default:7b",
		ComputeNodes: []string{"datawatch-ollama"},
	}); err != nil {
		t.Fatalf("llm registry: %v", err)
	}
	disp := inference.NewDispatcher(llmReg, s.computeReg.Get)
	disp.RegisterAdapter(&adapters.Ollama{})
	s.SetInference(llmReg, disp)

	if rr := postAsk(t, s, `{"question":"hi"}`); rr.Code != http.StatusOK {
		t.Fatalf("implicit: status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := postAsk(t, s, `{"question":"hi","model":"other:70b"}`); rr.Code != http.StatusConflict {
		t.Fatalf("explicit swap: status=%d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if got := rec.seen(); len(got) != 1 || got[0] != "pinned:1b" {
		t.Fatalf("fake received %v, want only [pinned:1b]", got)
	}
}

func TestAssistPin_RefusedAssistantModelReturnsConflict(t *testing.T) {
	fake, rec := newPinFakeOllama(t)
	s := pinnedAskServer(t, fake.URL, "default:7b", "pinned:1b", false)
	s.cfg.Session.AssistantModel = "other:70b"

	req := httptest.NewRequest(http.MethodPost, "/api/assist", strings.NewReader(`{"question":"hi"}`))
	rr := httptest.NewRecorder()
	s.handleAssist(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "pinned to pinned:1b") {
		t.Errorf("body %q does not state the pin", rr.Body.String())
	}
	if got := rec.seen(); len(got) != 0 {
		t.Fatalf("refused assistant model reached the node: %v", got)
	}
}
