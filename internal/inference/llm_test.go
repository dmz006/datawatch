package inference

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

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

func TestLLMBudgetRoundTrip(t *testing.T) {
	budget := &BudgetWindow{
		CeilingUSD:    75.5,
		Weekday:       "friday",
		Time:          "08:00",
		Zone:          "America/New_York",
		AllocationPct: true,
		PeerWindow:    []string{"peer-a"},
	}
	l := LLM{Name: "cc", Kind: KindClaudeCode, Budget: budget}

	t.Run("json", func(t *testing.T) {
		raw, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"budget":{"ceiling_usd":75.5`) {
			t.Fatalf("json missing budget: %s", raw)
		}
		var got LLM
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Budget, budget) {
			t.Fatalf("json round trip = %+v, want %+v", got.Budget, budget)
		}
	})

	t.Run("yaml", func(t *testing.T) {
		raw, err := yaml.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "budget:") || !strings.Contains(string(raw), "ceiling_usd: 75.5") {
			t.Fatalf("yaml missing budget: %s", raw)
		}
		var got LLM
		if err := yaml.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Budget, budget) {
			t.Fatalf("yaml round trip = %+v, want %+v", got.Budget, budget)
		}
	})

	t.Run("nil budget omitted", func(t *testing.T) {
		none := LLM{Name: "cc", Kind: KindClaudeCode}
		raw, _ := json.Marshal(none)
		if strings.Contains(string(raw), "budget") {
			t.Errorf("json should omit nil budget: %s", raw)
		}
		raw, _ = yaml.Marshal(none)
		if strings.Contains(string(raw), "budget") {
			t.Errorf("yaml should omit nil budget: %s", raw)
		}
		if err := none.Validate(); err != nil {
			t.Errorf("nil budget must validate: %v", err)
		}
	})

	t.Run("registry add and update", func(t *testing.T) {
		r := NewRegistry()
		good := &LLM{Name: "cc", Kind: KindClaudeCode, Budget: &BudgetWindow{CeilingUSD: 10, Hours: 5}}
		if err := r.Add(good); err != nil {
			t.Fatalf("add valid budget: %v", err)
		}
		got, err := r.Get("cc")
		if err != nil || got.Budget == nil || got.Budget.CeilingUSD != 10 {
			t.Fatalf("stored budget = %+v, err %v", got, err)
		}

		bad := &LLM{Name: "bad", Kind: KindClaudeCode, Budget: &BudgetWindow{CeilingUSD: 10}}
		err = r.Add(bad)
		if err == nil || !strings.Contains(err.Error(), "budget") {
			t.Fatalf("add invalid budget: want budget error, got %v", err)
		}
		if r.Exists("bad") {
			t.Error("rejected LLM must not be stored")
		}

		upd := &LLM{Name: "cc", Kind: KindClaudeCode, Budget: &BudgetWindow{CeilingUSD: 20, Weekday: "monday"}}
		if err := r.Update(upd); err != nil {
			t.Fatalf("update valid budget: %v", err)
		}
		badUpd := &LLM{Name: "cc", Kind: KindClaudeCode, Budget: &BudgetWindow{CeilingUSD: 20, Hours: 5, Weekday: "monday"}}
		if err := r.Update(badUpd); err == nil || !strings.Contains(err.Error(), "budget") {
			t.Fatalf("update invalid budget: want budget error, got %v", err)
		}
		got, _ = r.Get("cc")
		if got.Budget == nil || got.Budget.Weekday != "monday" || got.Budget.CeilingUSD != 20 {
			t.Fatalf("failed update must leave stored budget alone, got %+v", got.Budget)
		}
	})
}
