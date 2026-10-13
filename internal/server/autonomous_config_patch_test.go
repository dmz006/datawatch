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

	// BL418 — per_prd_guardrails (PRD-completion rules review opt-in)
	// round-trips the same two shapes as its task/story siblings.
	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.per_prd_guardrails": []interface{}{"rules-review"},
	})
	if got := cfg.Autonomous.PerPRDGuardrails; len(got) != 1 || got[0] != "rules-review" {
		t.Fatalf("per_prd_guardrails (array): got %v, want [rules-review]", got)
	}
	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.per_prd_guardrails": "",
	})
	if got := cfg.Autonomous.PerPRDGuardrails; len(got) != 0 {
		t.Fatalf("per_prd_guardrails empty csv should clear: got %v", got)
	}

	// BL410 Phase 1 — retry-policy config.
	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.retry_policy":               map[string]interface{}{"infra": float64(4), "scope": float64(0)},
		"autonomous.retry_ceiling_total":        float64(5),
		"autonomous.infra_retry_escalate_after": float64(2),
		"autonomous.content_auto_fix_enabled":   true,
		"autonomous.auto_fix_skip_approval":     true,
	})
	if got := cfg.Autonomous.RetryPolicy; got["infra"] != 4 || got["scope"] != 0 {
		t.Fatalf("retry_policy: got %v, want infra=4 scope=0", got)
	}
	if cfg.Autonomous.RetryCeilingTotal != 5 {
		t.Fatalf("retry_ceiling_total: got %d, want 5", cfg.Autonomous.RetryCeilingTotal)
	}
	if cfg.Autonomous.InfraRetryEscalateAfter != 2 {
		t.Fatalf("infra_retry_escalate_after: got %d, want 2", cfg.Autonomous.InfraRetryEscalateAfter)
	}
	if !cfg.Autonomous.ContentAutoFixEnabled {
		t.Fatal("content_auto_fix_enabled: got false, want true")
	}
	if !cfg.Autonomous.AutoFixSkipApproval {
		t.Fatal("auto_fix_skip_approval: got false, want true")
	}
	// Explicit clear-to-false must take effect (no omitempty elision).
	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.content_auto_fix_enabled": false,
		"autonomous.auto_fix_skip_approval":   false,
	})
	if cfg.Autonomous.ContentAutoFixEnabled || cfg.Autonomous.AutoFixSkipApproval {
		t.Fatal("explicit clear-to-false must take effect, not be silently elided")
	}
}

// BL406 — verify rules_file/context_file/upstream_repos round-trip
// through applyConfigPatch (confirming them in AGENT.md's
// Configuration Accessibility Rule REST surface, not just the YAML
// struct tags).
func TestApplyConfigPatch_BL406ProjectRulesFields(t *testing.T) {
	cfg := &config.Config{}

	applyConfigPatch(cfg, map[string]interface{}{"autonomous.rules_file": "AGENT.md"})
	if cfg.Autonomous.RulesFile != "AGENT.md" {
		t.Fatalf("rules_file: got %q, want AGENT.md", cfg.Autonomous.RulesFile)
	}

	applyConfigPatch(cfg, map[string]interface{}{"autonomous.context_file": "DATAWATCH-CONTEXT.md"})
	if cfg.Autonomous.ContextFile != "DATAWATCH-CONTEXT.md" {
		t.Fatalf("context_file: got %q, want DATAWATCH-CONTEXT.md", cfg.Autonomous.ContextFile)
	}

	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.upstream_repos": []interface{}{
			map[string]interface{}{"name": "app", "owner_repo": "dmz006/datawatch-app"},
		},
	})
	repos := cfg.Autonomous.UpstreamRepos
	if len(repos) != 1 || repos[0].Name != "app" || repos[0].OwnerRepo != "dmz006/datawatch-app" {
		t.Fatalf("upstream_repos: got %+v", repos)
	}
}

// BL407 Phase 1 — verify worktree_base_repo/worktree_dir round-trip
// through applyConfigPatch, same precedent as BL406's rules_file etc.
func TestApplyConfigPatch_BL407WorktreeFields(t *testing.T) {
	cfg := &config.Config{}

	applyConfigPatch(cfg, map[string]interface{}{"autonomous.worktree_base_repo": "/home/op/myrepo"})
	if cfg.Autonomous.WorktreeBaseRepo != "/home/op/myrepo" {
		t.Fatalf("worktree_base_repo: got %q, want /home/op/myrepo", cfg.Autonomous.WorktreeBaseRepo)
	}

	applyConfigPatch(cfg, map[string]interface{}{"autonomous.worktree_dir": "/home/op/.datawatch/wt"})
	if cfg.Autonomous.WorktreeDir != "/home/op/.datawatch/wt" {
		t.Fatalf("worktree_dir: got %q, want /home/op/.datawatch/wt", cfg.Autonomous.WorktreeDir)
	}
}

// B119 — found live 2026-10-11: autonomous.execution_backend existed as
// a live internal/autonomous.Config field (BL203, the fallback backend
// for a task/PRD with no explicit Backend) but had zero config surface
// anywhere — not YAML, not PUT /api/config, not the daemon-startup
// bridge. Verify the applyConfigPatch half of the fix here, same
// precedent as every other single-field case in this function.
func TestApplyConfigPatch_ExecutionBackend(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.execution_backend": "claude-code"})
	if cfg.Autonomous.ExecutionBackend != "claude-code" {
		t.Fatalf("execution_backend: got %q, want claude-code", cfg.Autonomous.ExecutionBackend)
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
