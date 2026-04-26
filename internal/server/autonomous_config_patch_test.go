// v5.17.0 — verify the BL191 Q4 (recursion) + Q6 (guardrails) config
// keys land in the YAML/REST/PWA path. Pre-v5.17.0 these silently
// no-op'd through PUT /api/config.

package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestApplyConfigPatch_AutonomousRecursionAndGuardrails(t *testing.T) {
	cfg := &config.Config{}

	// Number — max_recursion_depth.
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.max_recursion_depth": 7})
	if cfg.Autonomous.MaxRecursionDepth != 7 {
		t.Fatalf("max_recursion_depth: got %d, want 7", cfg.Autonomous.MaxRecursionDepth)
	}

	// Bool — auto_approve_children.
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.auto_approve_children": true})
	if !cfg.Autonomous.AutoApproveChildren {
		t.Fatalf("auto_approve_children: got false, want true")
	}

	// JSON-array path — per_task_guardrails as []interface{}.
	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.per_task_guardrails": []interface{}{"rules", "security"},
	})
	if got := cfg.Autonomous.PerTaskGuardrails; len(got) != 2 || got[0] != "rules" || got[1] != "security" {
		t.Fatalf("per_task_guardrails (array): got %v, want [rules security]", got)
	}

	// Comma-string path — per_story_guardrails as a single CSV string
	// (the PWA's text-input convention).
	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.per_story_guardrails": "rules, security , release-readiness",
	})
	if got := cfg.Autonomous.PerStoryGuardrails; len(got) != 3 || got[0] != "rules" || got[1] != "security" || got[2] != "release-readiness" {
		t.Fatalf("per_story_guardrails (csv): got %v", got)
	}

	// Empty-string CSV must clear the slice (nil), not produce
	// []string{""}.
	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.per_story_guardrails": "",
	})
	if got := cfg.Autonomous.PerStoryGuardrails; len(got) != 0 {
		t.Fatalf("empty csv should clear: got %v", got)
	}
}

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{"rules", []string{"rules"}},
		{"rules,security", []string{"rules", "security"}},
		{"  rules ,  security  ", []string{"rules", "security"}},
		{"rules,,security", []string{"rules", "security"}}, // empty entry dropped
	}
	for _, tc := range cases {
		got := splitCSV(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitCSV(%q): len = %d, want %d", tc.in, len(got), len(tc.want))
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitCSV(%q)[%d]: got %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
