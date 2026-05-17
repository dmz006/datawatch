// BL309 — Registry.ResolveKind maps named LLMs to their adapter kind.
// The pipeline executor uses this to route named inference-registry entries
// (e.g. "ollama-datawatch") to the correct session adapter ("ollama").

package inference

import "testing"

func TestResolveKind_KnownLLM(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "ollama-datawatch", Kind: KindOllama})

	kind, ok := reg.ResolveKind("ollama-datawatch")
	if !ok {
		t.Fatal("expected ok=true for known LLM")
	}
	if kind != string(KindOllama) {
		t.Fatalf("kind: got %q, want %q", kind, KindOllama)
	}
}

func TestResolveKind_UnknownLLM(t *testing.T) {
	reg := NewRegistry()
	_, ok := reg.ResolveKind("does-not-exist")
	if ok {
		t.Fatal("expected ok=false for unknown LLM")
	}
}

func TestResolveKind_DisabledLLM(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "old-backend", Kind: KindOpenWebUI, Disabled: true})

	_, ok := reg.ResolveKind("old-backend")
	if ok {
		t.Fatal("disabled LLM should not resolve")
	}
}

func TestResolveKind_MultipleLLMs(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "my-claude", Kind: KindClaudeCode})
	_ = reg.Add(&LLM{Name: "my-ollama", Kind: KindOllama})

	if k, ok := reg.ResolveKind("my-claude"); !ok || k != string(KindClaudeCode) {
		t.Errorf("my-claude: got (%q, %v), want (%q, true)", k, ok, KindClaudeCode)
	}
	if k, ok := reg.ResolveKind("my-ollama"); !ok || k != string(KindOllama) {
		t.Errorf("my-ollama: got (%q, %v), want (%q, true)", k, ok, KindOllama)
	}
}
