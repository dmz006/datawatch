package inference

import "testing"

// #179 — GET /api/llms and the llm_list/llm_get MCP tools must never echo
// a literal api_key_ref in clear text.

func TestLLM_Redacted_ClearsLiteralKey(t *testing.T) {
	l := &LLM{Name: "openwebui", Kind: KindOpenWebUI, APIKeyRef: "sk-abcdef123456"}
	r := l.Redacted()
	if r.APIKeyRef != "" {
		t.Fatalf("literal api_key_ref must be cleared, got %q", r.APIKeyRef)
	}
	if !r.APIKeyRefPresent {
		t.Fatal("APIKeyRefPresent should be true when a literal key was set")
	}
	if r.APIKeyRefPrefix != "sk-a" {
		t.Fatalf("APIKeyRefPrefix = %q, want first 4 chars", r.APIKeyRefPrefix)
	}
	// Original must be untouched.
	if l.APIKeyRef != "sk-abcdef123456" {
		t.Fatal("Redacted() must not mutate the original LLM")
	}
}

func TestLLM_Redacted_LeavesSecretRefAsIs(t *testing.T) {
	l := &LLM{Name: "claude", Kind: KindClaude, APIKeyRef: "${secret:anthropic-key}"}
	r := l.Redacted()
	if r.APIKeyRef != "${secret:anthropic-key}" {
		t.Fatalf("a secret reference names a secret, it isn't one — must be left as-is, got %q", r.APIKeyRef)
	}
	if r.APIKeyRefPresent {
		t.Fatal("APIKeyRefPresent must stay false for a secret reference (nothing was redacted)")
	}
}

func TestLLM_Redacted_EmptyKeyUnaffected(t *testing.T) {
	l := &LLM{Name: "ollama", Kind: KindOllama}
	r := l.Redacted()
	if r.APIKeyRefPresent || r.APIKeyRefPrefix != "" || r.APIKeyRef != "" {
		t.Fatalf("empty api_key_ref should produce no redaction markers, got %+v", r)
	}
}

func TestRedactedList(t *testing.T) {
	in := []*LLM{
		{Name: "a", APIKeyRef: "sk-literal"},
		{Name: "b", APIKeyRef: "${secret:x}"},
	}
	out := RedactedList(in)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0].APIKeyRef != "" || !out[0].APIKeyRefPresent {
		t.Error("entry 0 (literal) should be redacted")
	}
	if out[1].APIKeyRef != "${secret:x}" {
		t.Error("entry 1 (secret ref) should be left as-is")
	}
	// Source slice must be untouched.
	if in[0].APIKeyRef != "sk-literal" {
		t.Fatal("RedactedList must not mutate its input")
	}
}
