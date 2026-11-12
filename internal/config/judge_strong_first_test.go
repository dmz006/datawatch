package config

import (
	"reflect"
	"testing"
)

// TestJudgeSizeStrongFirst is the table test of the judge-policy ordering for
// one measured diff size: below, at and above the threshold, the rule off,
// an empty strong backend, and a strong backend that is not in the policy.
// It is pure — no backends, no git, no network.
func TestJudgeSizeStrongFirst(t *testing.T) {
	cases := []struct {
		name        string
		policy      []string
		strong      string
		lines       int
		threshold   int
		wantOrdered []string
		wantRule    string
	}{
		{
			name:        "below threshold keeps policy order",
			policy:      []string{"pi", "opencode", "claude-code"},
			strong:      "claude-code",
			lines:       399,
			threshold:   400,
			wantOrdered: []string{"pi", "opencode", "claude-code"},
			wantRule:    "default order: 399 changed lines is at or below the threshold of 400",
		},
		{
			name:        "at threshold keeps policy order",
			policy:      []string{"pi", "opencode", "claude-code"},
			strong:      "claude-code",
			lines:       400,
			threshold:   400,
			wantOrdered: []string{"pi", "opencode", "claude-code"},
			wantRule:    "default order: 400 changed lines is at or below the threshold of 400",
		},
		{
			name:        "above threshold with a configured strong backend moves it first",
			policy:      []string{"pi", "opencode", "claude-code"},
			strong:      "claude-code",
			lines:       1000,
			threshold:   400,
			wantOrdered: []string{"claude-code", "pi", "opencode"},
			wantRule:    "size rule: 1000 changed lines is above the threshold of 400, claude-code tried first",
		},
		{
			name:        "above threshold with an empty strong backend uses the last policy entry",
			policy:      []string{"pi", "opencode", "claude-code"},
			strong:      "",
			lines:       1000,
			threshold:   400,
			wantOrdered: []string{"claude-code", "pi", "opencode"},
			wantRule:    "size rule: 1000 changed lines is above the threshold of 400, claude-code tried first",
		},
		{
			name:        "above threshold with a different configured strong backend moves that one first",
			policy:      []string{"claude-code", "pi", "ollama"},
			strong:      "ollama",
			lines:       900,
			threshold:   400,
			wantOrdered: []string{"ollama", "claude-code", "pi"},
			wantRule:    "size rule: 900 changed lines is above the threshold of 400, ollama tried first",
		},
		{
			name:        "rule disabled with threshold zero keeps policy order even for a huge diff",
			policy:      []string{"pi", "claude-code"},
			strong:      "claude-code",
			lines:       1000000,
			threshold:   0,
			wantOrdered: []string{"pi", "claude-code"},
			wantRule:    "default order: size routing is off (threshold 0)",
		},
		{
			name:        "empty strong backend and empty policy keeps the empty order",
			policy:      nil,
			strong:      "",
			lines:       1000,
			threshold:   400,
			wantOrdered: []string{},
			wantRule:    "default order: 1000 changed lines is above the threshold of 400 but no strong judge backend is configured",
		},
		{
			name:        "strong backend not in the policy does not enter it",
			policy:      []string{"pi", "ollama"},
			strong:      "claude-code",
			lines:       1000,
			threshold:   400,
			wantOrdered: []string{"pi", "ollama"},
			wantRule:    "default order: 1000 changed lines is above the threshold of 400 but claude-code is not in the chain",
		},
		{
			name:        "blank entries and duplicates collapse and the strong backend dedupes",
			policy:      []string{"pi", "", "claude-code", "pi", "claude-code"},
			strong:      "claude-code",
			lines:       1000,
			threshold:   400,
			wantOrdered: []string{"claude-code", "pi"},
			wantRule:    "size rule: 1000 changed lines is above the threshold of 400, claude-code tried first",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotOrdered, gotRule := JudgeSizeStrongFirst(c.policy, c.lines, c.threshold, c.strong)
			if want := []string(c.wantOrdered); !reflect.DeepEqual(gotOrdered, want) {
				t.Fatalf("ordered = %v, want %v", gotOrdered, want)
			}
			if gotRule != c.wantRule {
				t.Fatalf("rule = %q, want %q", gotRule, c.wantRule)
			}
		})
	}
}

func TestJudgeSizeStrongFirstDoesNotMutatePolicy(t *testing.T) {
	policy := []string{"pi", "claude-code", "pi", "ollama"}
	orig := append([]string(nil), policy...)
	got, rule := JudgeSizeStrongFirst(policy, 1000, 400, "ollama")
	if !reflect.DeepEqual(policy, orig) {
		t.Fatalf("policy mutated: %v, want %v", policy, orig)
	}
	want := []string{"ollama", "pi", "claude-code"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered = %v, want %v", got, want)
	}
	if wantRule := "size rule: 1000 changed lines is above the threshold of 400, ollama tried first"; rule != wantRule {
		t.Fatalf("rule = %q, want %q", rule, wantRule)
	}
}

func TestJudgeStrongBackendResolution(t *testing.T) {
	if got, want := JudgeStrongBackend([]string{"pi", "claude-code"}, "ollama"), "ollama"; got != want {
		t.Fatalf("configured strong = %q, want %q", got, want)
	}
	if got, want := JudgeStrongBackend([]string{"pi", "claude-code"}, ""), "claude-code"; got != want {
		t.Fatalf("empty strong, policy set = %q, want the last policy entry %q", got, want)
	}
	if got, want := JudgeStrongBackend(nil, "  "), ""; got != want {
		t.Fatalf("blank strong, empty policy = %q, want %q", got, want)
	}
}
