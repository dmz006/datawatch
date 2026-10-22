// Model-pin tests for the ollama chat backend. A fake Ollama records the model
// of every chat turn, so each test can assert that a pinned node only ever
// receives its pinned name, and that a refusal never reaches it.

package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

// pinFakeChat answers /api/chat with one final chunk and records each model.
type pinFakeChat struct {
	mu     sync.Mutex
	models []string
}

func newPinFakeChat(t *testing.T) (*httptest.Server, *pinFakeChat) {
	t.Helper()
	fake := &pinFakeChat{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
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
		_, _ = w.Write([]byte(`{"message":{"content":"ok"},"done":true}` + "\n"))
	}))
	t.Cleanup(srv.Close)
	return srv, fake
}

func (f *pinFakeChat) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.models...)
}

// setResolver installs resolver for one test and restores the previous one.
func setResolver(t *testing.T, resolver func(host, model string) (string, error)) {
	t.Helper()
	prev := modelResolver
	SetModelResolver(resolver)
	t.Cleanup(func() { modelResolver = prev })
}

// pinResolverFor builds the daemon's resolver over one compute node at addr.
func pinResolverFor(t *testing.T, addr, pin string) func(host, model string) (string, error) {
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
	}); err != nil {
		t.Fatalf("add node: %v", err)
	}
	pins := inference.NewHostPins(reg)
	return func(host, model string) (string, error) {
		return pins.ResolveModel(host, model, "")
	}
}

func TestBackend_ChatTurnRunsPinnedModel(t *testing.T) {
	fake, rec := newPinFakeChat(t)
	setResolver(t, pinResolverFor(t, fake.URL, "pinned:1b"))

	b := NewWithHost("default:7b", "ollama", fake.URL).(*Backend)
	if err := b.sendAndStream(context.Background(), "cs-pin-chat", "hi", false); err != nil {
		t.Fatalf("chat turn: %v", err)
	}
	got := rec.seen()
	if len(got) != 1 || got[0] != "pinned:1b" {
		t.Fatalf("fake received %v, want only [pinned:1b]", got)
	}
}

func TestBackend_ChatTurnWithoutResolverKeepsDefault(t *testing.T) {
	fake, rec := newPinFakeChat(t)
	setResolver(t, nil)

	b := NewWithHost("default:7b", "ollama", fake.URL).(*Backend)
	if err := b.sendAndStream(context.Background(), "cs-pin-chat-plain", "hi", false); err != nil {
		t.Fatalf("chat turn: %v", err)
	}
	got := rec.seen()
	if len(got) != 1 || got[0] != "default:7b" {
		t.Fatalf("fake received %v, want [default:7b]", got)
	}
}

func TestBackend_RefusedTurnNeverReachesNode(t *testing.T) {
	fake, rec := newPinFakeChat(t)
	refusal := errors.New("refused: node datawatch-ollama is pinned to pinned:1b")
	setResolver(t, func(host, model string) (string, error) { return "", refusal })

	b := NewWithHost("default:7b", "ollama", fake.URL).(*Backend)
	if err := b.sendAndStream(context.Background(), "cs-pin-refused", "hi", false); !errors.Is(err, refusal) {
		t.Fatalf("chat turn error = %v, want the refusal", err)
	}
	if got := rec.seen(); len(got) != 0 {
		t.Fatalf("refused turn reached the node: %v", got)
	}
}

func TestBackend_LaunchRefusedBeforeStartingSession(t *testing.T) {
	refusal := errors.New("refused: node datawatch-ollama is pinned to pinned:1b")
	setResolver(t, func(host, model string) (string, error) { return "", refusal })

	b := NewWithHost("default:7b", "ollama", "").(*Backend)
	err := b.Launch(context.Background(), "task", "cs-pin-launch", t.TempDir(), "")
	if !errors.Is(err, refusal) {
		t.Fatalf("launch error = %v, want the refusal", err)
	}
	if _, registered := backendRegistry.Load("cs-pin-launch"); registered {
		t.Fatal("a refused launch must not register a session backend")
	}
}
