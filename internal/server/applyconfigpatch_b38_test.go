// B38 (v4.0.8) — regression test for issue #19: applyConfigPatch
// used to silently no-op saves for autonomous.*, plugins.*,
// orchestrator.* because no case-branch existed for those keys.
// The handler still returned 200 so PWA + mobile clients showed
// "saved" but nothing landed in config.yaml or the live Config.

package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestApplyConfigPatch_AutonomousPluginsOrchestrator(t *testing.T) {
	cfg := &config.Config{}
	patch := map[string]interface{}{
		"autonomous.enabled":               true,
		"autonomous.poll_interval_seconds": float64(45),
		"autonomous.max_parallel_tasks":    float64(5),
		"autonomous.decomposition_backend": "claude-code",
		"autonomous.verification_backend":  "ollama",
		"autonomous.auto_fix_retries":      float64(2),
		"autonomous.security_scan":         true,

		"plugins.enabled":    true,
		"plugins.dir":        "/srv/plugins",
		"plugins.timeout_ms": float64(3500),

		"orchestrator.enabled":              true,
		"orchestrator.guardrail_backend":    "ollama",
		"orchestrator.guardrail_timeout_ms": float64(90000),
		"orchestrator.max_parallel_prds":    float64(4),
	}
	applyConfigPatch(cfg, patch)

	// Autonomous.
	if !cfg.Autonomous.Enabled {
		t.Errorf("autonomous.enabled not applied")
	}
	if cfg.Autonomous.PollIntervalSeconds != 45 {
		t.Errorf("poll_interval_seconds = %d, want 45", cfg.Autonomous.PollIntervalSeconds)
	}
	if cfg.Autonomous.MaxParallelTasks != 5 {
		t.Errorf("max_parallel_tasks = %d, want 5", cfg.Autonomous.MaxParallelTasks)
	}
	if cfg.Autonomous.DecompositionBackend != "claude-code" {
		t.Errorf("decomposition_backend = %q, want claude-code", cfg.Autonomous.DecompositionBackend)
	}
	if cfg.Autonomous.VerificationBackend != "ollama" {
		t.Errorf("verification_backend = %q, want ollama", cfg.Autonomous.VerificationBackend)
	}
	if cfg.Autonomous.AutoFixRetries != 2 {
		t.Errorf("auto_fix_retries = %d, want 2", cfg.Autonomous.AutoFixRetries)
	}
	if !cfg.Autonomous.SecurityScan {
		t.Errorf("security_scan not applied")
	}

	// Plugins.
	if !cfg.Plugins.Enabled {
		t.Errorf("plugins.enabled not applied")
	}
	if cfg.Plugins.Dir != "/srv/plugins" {
		t.Errorf("plugins.dir = %q, want /srv/plugins", cfg.Plugins.Dir)
	}
	if cfg.Plugins.TimeoutMs != 3500 {
		t.Errorf("plugins.timeout_ms = %d, want 3500", cfg.Plugins.TimeoutMs)
	}

	// Orchestrator.
	if !cfg.Orchestrator.Enabled {
		t.Errorf("orchestrator.enabled not applied")
	}
	if cfg.Orchestrator.GuardrailBackend != "ollama" {
		t.Errorf("guardrail_backend = %q, want ollama", cfg.Orchestrator.GuardrailBackend)
	}
	if cfg.Orchestrator.GuardrailTimeoutMs != 90000 {
		t.Errorf("guardrail_timeout_ms = %d, want 90000", cfg.Orchestrator.GuardrailTimeoutMs)
	}
	if cfg.Orchestrator.MaxParallelPRDs != 4 {
		t.Errorf("max_parallel_prds = %d, want 4", cfg.Orchestrator.MaxParallelPRDs)
	}
}

func TestApplyConfigPatch_UnknownKeyLogsButDoesNotPanic(t *testing.T) {
	cfg := &config.Config{}
	// Should not panic; unknown key is logged to stderr and ignored.
	applyConfigPatch(cfg, map[string]interface{}{
		"totally.made.up.key":   "whatever",
		"autonomous.enabled":    true,
	})
	if !cfg.Autonomous.Enabled {
		t.Errorf("known key in same patch should still land alongside unknown one")
	}
}

func TestApplyConfigPatch_SessionQuickCommands(t *testing.T) {
	cfg := &config.Config{}
	patch := map[string]interface{}{
		"session.quick_commands": []interface{}{
			map[string]interface{}{
				"label":    "Yes",
				"value":    "yes\n",
				"category": "system",
			},
			map[string]interface{}{
				"label":    "Scroll Up",
				"value":    "key:Page-Up",
				"category": "navigation",
			},
			map[string]interface{}{
				"label":    "Custom Project Cmd",
				"value":    "./build.sh\n",
			},
		},
	}
	applyConfigPatch(cfg, patch)

	if len(cfg.Session.QuickCommands) != 3 {
		t.Errorf("QuickCommands length = %d, want 3", len(cfg.Session.QuickCommands))
	}
	if cfg.Session.QuickCommands[0].Label != "Yes" {
		t.Errorf("QuickCommands[0].Label = %q, want Yes", cfg.Session.QuickCommands[0].Label)
	}
	if cfg.Session.QuickCommands[0].Value != "yes\n" {
		t.Errorf("QuickCommands[0].Value = %q, want yes\\n", cfg.Session.QuickCommands[0].Value)
	}
	if cfg.Session.QuickCommands[0].Category != "system" {
		t.Errorf("QuickCommands[0].Category = %q, want system", cfg.Session.QuickCommands[0].Category)
	}
	if cfg.Session.QuickCommands[1].Label != "Scroll Up" {
		t.Errorf("QuickCommands[1].Label = %q, want Scroll Up", cfg.Session.QuickCommands[1].Label)
	}
	if cfg.Session.QuickCommands[2].Category != "" {
		t.Errorf("QuickCommands[2].Category = %v, want empty string", cfg.Session.QuickCommands[2].Category)
	}
}
