// Call-time API key secret resolution through the dispatcher: a
// ${secret:name} reference on an APIKeyRef-kind entry is resolved and the
// adapter receives the resolved value in a shallow copy of the entry, the
// registry entry is left untouched, and a missing secret is a FINAL error
// (no failover, no adapter call) that names the secret.

package inference

import (
	"context"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

// keyAdapter records the APIKeyRef of every LLM handed to Infer and returns
// a fixed success response.
type keyAdapter struct {
	kind    Kind
	gotRefs []string
}

func (k *keyAdapter) Kind() Kind { return k.kind }

func (k *keyAdapter) Infer(_ context.Context, _ *compute.Node, llm *LLM, _ Request) (Response, error) {
	k.gotRefs = append(k.gotRefs, llm.APIKeyRef)
	return Response{Text: "ok"}, nil
}

// type report = one callback invocation: [0] llm entry name, [1] secret
// name, [2] outcome.
type report [3]string

// newDispatcherForTest builds a registry with one claude LLM entry and a
// dispatcher whose only adapter is a keyAdapter for KindClaude. The claude
// kind is a cloud kind, so no ComputeNodes are needed.
func newDispatcherForTest(t *testing.T, entry *LLM) (*Registry, *Dispatcher, *keyAdapter, *[]report) {
	t.Helper()
	reg := NewRegistry()
	if err := reg.Add(entry); err != nil {
		t.Fatalf("registry add: %v", err)
	}
	d := NewDispatcher(reg, func(string) (*compute.Node, error) {
		return nil, compute.ErrNotFound
	})
	mock := &keyAdapter{kind: KindClaude}
	d.RegisterAdapter(mock)
	reports := []report{}
	d.SetSecretsStore(nil, func(llmName, secretName, outcome string) {
		reports = append(reports, report{llmName, secretName, outcome})
	})
	return reg, d, mock, &reports
}

func reportContains(t *testing.T, reports []report, want report) {
	t.Helper()
	for _, r := range reports {
		if r == want {
			return
		}
	}
	t.Fatalf("report %v not found in %v", want, reports)
}

func TestDispatcherAPIKey_SecretRefResolvesBeforeAdapterCall(t *testing.T) {
	const stored = "RESOLVED-ANTHROPIC-KEY-42"
	_, d, mock, reports := newDispatcherForTest(t, &LLM{
		Name: "claude-ref", Kind: KindClaude, Model: "claude-sonnet",
		APIKeyRef: "${secret:anthropic-key}",
	})
	d.SetSecretsStore(&fakeSecretStore{secrets: map[string]string{"anthropic-key": stored}},
		func(llmName, secretName, outcome string) {
			*reports = append(*reports, report{llmName, secretName, outcome})
		})

	resp, err := d.Call(context.Background(), "claude-ref", Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.Text != "ok" {
		t.Fatalf("response: %+v", resp)
	}
	if len(mock.gotRefs) != 1 {
		t.Fatalf("adapter should be called exactly once, got %d", len(mock.gotRefs))
	}
	if mock.gotRefs[0] != stored {
		t.Fatalf("adapter saw %q, want the resolved secret value %q (secret ref must resolve before the adapter call)", mock.gotRefs[0], stored)
	}
	reportContains(t, *reports, report{"claude-ref", "anthropic-key", "ok"})
}

func TestDispatcherAPIKey_MissingSecretIsFinalAndAdaptersUntouched(t *testing.T) {
	_, d, mock, reports := newDispatcherForTest(t, &LLM{
		Name: "claude-missing", Kind: KindClaude, Model: "claude-sonnet",
		APIKeyRef: "${secret:no-such-secret}",
	})
	// fakeSecretStore.Get returns ErrSecretNotFound for unknown names.
	d.SetSecretsStore(newFakeStore(), func(llmName, secretName, outcome string) {
		*reports = append(*reports, report{llmName, secretName, outcome})
	})

	_, err := d.Call(context.Background(), "claude-missing", Request{Prompt: "ping"})
	if err == nil {
		t.Fatal("expected an error for a missing secret, got nil")
	}
	if IsTransient(err) {
		t.Fatalf("a missing secret must be FINAL (no failover to another backend), got transient: %v", err)
	}
	if !strings.Contains(err.Error(), "no-such-secret") {
		t.Fatalf("error must name the secret: %v", err)
	}
	if strings.Contains(err.Error(), "SUPERSECRETVALUE123") {
		t.Fatalf("error must never contain a secret value: %v", err)
	}
	if len(mock.gotRefs) != 0 {
		t.Fatalf("adapter must NOT be called on a resolution failure, but was (%v)", mock.gotRefs)
	}
	reportContains(t, *reports, report{"claude-missing", "no-such-secret", "missing"})
}

func TestDispatcherAPIKey_NilStoreRefusesSecretRef(t *testing.T) {
	// SetSecretsStore was called with a nil store at construction: a
	// ${secret:name} reference cannot be resolved and is a final error.
	_, d, mock, reports := newDispatcherForTest(t, &LLM{
		Name: "claude-nostore", Kind: KindClaude, Model: "claude-sonnet",
		APIKeyRef: "${secret:anthropic-key}",
	})

	_, err := d.Call(context.Background(), "claude-nostore", Request{Prompt: "ping"})
	if err == nil {
		t.Fatal("expected an error with no store set, got nil")
	}
	if IsTransient(err) {
		t.Fatalf("must be final, not transient: %v", err)
	}
	if !strings.Contains(err.Error(), "anthropic-key") {
		t.Fatalf("error must name the secret: %v", err)
	}
	if len(mock.gotRefs) != 0 {
		t.Fatalf("adapter must not be called: %v", mock.gotRefs)
	}
	reportContains(t, *reports, report{"claude-nostore", "anthropic-key", "missing"})
}

func TestDispatcherAPIKey_LiteralKeyPassesThrough(t *testing.T) {
	const literal = "sk-live-literal-123"
	reg, d, mock, reports := newDispatcherForTest(t, &LLM{
		Name: "claude-literal", Kind: KindClaude, Model: "claude-sonnet",
		APIKeyRef: literal,
	})
	// Store is set (with an unrelated secret); a literal key must pass
	// through unchanged, regardless.
	d.SetSecretsStore(newFakeStore(), func(llmName, secretName, outcome string) {
		*reports = append(*reports, report{llmName, secretName, outcome})
	})

	resp, err := d.Call(context.Background(), "claude-literal", Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.Text != "ok" {
		t.Fatalf("response: %+v", resp)
	}
	if len(mock.gotRefs) != 1 || mock.gotRefs[0] != literal {
		t.Fatalf("literal key must pass through unchanged; adapter saw %v", mock.gotRefs)
	}
	// A literal key names no secret: no audit report may fire for it.
	if len(*reports) != 0 {
		t.Fatalf("a literal key names no secret and must produce no audit report, got %v", *reports)
	}
	if got, gerr := reg.Get("claude-literal"); gerr == nil && got != nil && got.APIKeyRef != literal {
		t.Fatalf("registry entry APIKeyRef changed: %q", got.APIKeyRef)
	}
}

func TestDispatcherAPIKey_RegistryEntryUnchangedAfterResolution(t *testing.T) {
	const stored = "RESOLVED-ANTHROPIC-KEY-42"
	reg, d, mock, _ := newDispatcherForTest(t, &LLM{
		Name: "claude-reg", Kind: KindClaude, Model: "claude-sonnet",
		APIKeyRef: "${secret:anthropic-key}",
	})
	d.SetSecretsStore(&fakeSecretStore{secrets: map[string]string{"anthropic-key": stored}}, nil)

	if _, err := d.Call(context.Background(), "claude-reg", Request{Prompt: "ping"}); err != nil {
		t.Fatalf("call: %v", err)
	}
	if mock.gotRefs[0] != stored {
		t.Fatalf("adapter saw %q, want %q", mock.gotRefs[0], stored)
	}
	got, err := reg.Get("claude-reg")
	if err != nil || got == nil {
		t.Fatalf("registry get after call: %v", err)
	}
	if got.APIKeyRef != "${secret:anthropic-key}" {
		t.Fatalf("registry entry must keep the ${secret:} reference, never the resolved value; got %q", got.APIKeyRef)
	}
	if got != nil && got.Name != "claude-reg" {
		t.Fatalf("registry entry identity changed: %+v", got)
	}
}

func TestDispatcherAPIKey_MalformedRefRefused(t *testing.T) {
	// A ref with surrounding text is refused by the resolver; the report
	// carries no name (there is no clean secret name to audit) but the call
	// still fails final before any adapter call.
	reg, d, mock, _ := newDispatcherForTest(t, &LLM{
		Name: "claude-malformed", Kind: KindClaude, Model: "claude-sonnet",
		APIKeyRef: "prefix ${secret:anthropic-key} suffix",
	})
	d.SetSecretsStore(newFakeStore(), nil)

	if _, err := d.Call(context.Background(), "claude-malformed", Request{Prompt: "ping"}); err == nil {
		t.Fatal("expected a refusal for a malformed ref, got nil")
	}
	if len(mock.gotRefs) != 0 {
		t.Fatalf("adapter must not be called: %v", mock.gotRefs)
	}
	if got, gerr := reg.Get("claude-malformed"); gerr == nil && got != nil && got.APIKeyRef != "prefix ${secret:anthropic-key} suffix" {
		t.Fatalf("registry entry changed: %q", got.APIKeyRef)
	}
}
