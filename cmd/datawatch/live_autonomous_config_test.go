package main

import (
	"encoding/json"
	"reflect"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// bootConfigFixture is the boot-time literal main.go builds (amgrCfg) with
// the audit's runtime-tunable fields set to values the live set will
// replace.
func bootConfigFixture() autonomouspkg.Config {
	return autonomouspkg.Config{
		PlanningBackend:      "ollama",
		PlanningModel:        "boot-planning-model",
		ExecutionBackend:     "claude-code",
		VerificationBackend:  "ollama",
		VerificationModel:    "boot-verify-model",
		VerificationBackends: []string{"boot-pool"},
		VerificationEffort:   "low",
		StaleTaskSeconds:     600,
		VerifierDiffMaxBytes: 4096,
	}
}

// newTestManager wires a real Manager the same way main.go does (NewManager
// with the boot-time config and a stub decompose).
func newTestManager(t *testing.T, cfg autonomouspkg.Config) *autonomouspkg.Manager {
	t.Helper()
	mgr, err := autonomouspkg.NewManager(t.TempDir(), cfg, func(autonomouspkg.DecomposeRequest) (string, error) {
		return "", nil
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr
}

// TestEffectiveAutonomousConfig_LiveSetConfigNoClosureRebuild reproduces
// main.go's wiring exactly: the closure is captured ONCE before the
// Manager exists (falling back to the boot literal), the Manager is
// attached later (BL244's `autonomousMgrRef = amgr`), and then a runtime
// config set via Manager.SetConfig — the same sink PUT
// /api/autonomous/config and MCP autonomous_config_set both funnel into.
// The next closure call must read the NEW values. No restart, no
// reconstruction of the closure.
func TestEffectiveAutonomousConfig_LiveSetConfigNoClosureRebuild(t *testing.T) {
	boot := bootConfigFixture()
	var ref *autonomouspkg.Manager // mirrors main.go's autonomousMgrRef
	// Captured once, never re-created — same shape as main.go's
	// liveAutonomousConfig closure.
	live := func() autonomouspkg.Config { return effectiveAutonomousConfig(ref, boot) }

	// Before the Manager exists: boot-time literal.
	if got := live(); got.VerificationBackend != "ollama" || got.VerificationModel != "boot-verify-model" {
		t.Fatalf("pre-manager read got %+v, want boot values", got)
	}

	ref = newTestManager(t, boot)

	// Manager attached, no config set yet: still boot values.
	if got := live(); got.VerificationBackend != "ollama" || !reflect.DeepEqual(got.VerificationBackends, []string{"boot-pool"}) {
		t.Fatalf("pre-set read got %+v, want boot values", got)
	}

	// Runtime set, exact same sink the REST/MCP paths use.
	ncfg := boot
	ncfg.VerificationBackend = "openwebui"
	ncfg.VerificationModel = "live-verify-model"
	ncfg.VerificationBackends = []string{"live-pool-a", "live-pool-b"}
	ref.SetConfig(ncfg)

	got := live() // next call — same closure, no re-creation
	if got.VerificationBackend != "openwebui" {
		t.Errorf("VerificationBackend = %q, want openwebui", got.VerificationBackend)
	}
	if got.VerificationModel != "live-verify-model" {
		t.Errorf("VerificationModel = %q, want live-verify-model", got.VerificationModel)
	}
	if !reflect.DeepEqual(got.VerificationBackends, []string{"live-pool-a", "live-pool-b"}) {
		t.Errorf("VerificationBackends = %v, want the live pools", got.VerificationBackends)
	}

	// Every other field the audit routed through the live read must flip
	// too, with one more SetConfig.
	fcfg := ncfg
	fcfg.PlanningBackend = "openwebui"
	fcfg.PlanningModel = "live-planning-model"
	fcfg.ExecutionBackend = "opencode"
	fcfg.VerificationEffort = "max"
	fcfg.StaleTaskSeconds = 1200
	fcfg.VerifierDiffMaxBytes = 8192
	ref.SetConfig(fcfg)

	fgot := live()
	if fgot.PlanningBackend != "openwebui" || fgot.PlanningModel != "live-planning-model" {
		t.Errorf("Planning = %q/%q, want openwebui/live-planning-model", fgot.PlanningBackend, fgot.PlanningModel)
	}
	if fgot.ExecutionBackend != "opencode" {
		t.Errorf("ExecutionBackend = %q, want opencode", fgot.ExecutionBackend)
	}
	if fgot.VerificationEffort != "max" {
		t.Errorf("VerificationEffort = %q, want max", fgot.VerificationEffort)
	}
	if fgot.StaleTaskSeconds != 1200 || fgot.VerifierDiffMaxBytes != 8192 {
		t.Errorf("StaleTaskSeconds/VerifierDiffMaxBytes = %d/%d, want 1200/8192", fgot.StaleTaskSeconds, fgot.VerifierDiffMaxBytes)
	}
}

// TestEffectiveAutonomousConfig_AutonomousConfigSetPath drives the
// SAME entry point autonomous_config_set uses (autonomous.API.SetConfig
// with a partial JSON payload) and checks the live read the closures
// perform on their next call — pool / global backend / global model
// included.
func TestEffectiveAutonomousConfig_AutonomousConfigSetPath(t *testing.T) {
	boot := bootConfigFixture()
	var ref *autonomouspkg.Manager
	live := func() autonomouspkg.Config { return effectiveAutonomousConfig(ref, boot) }
	ref = newTestManager(t, boot)
	api := newTestAPI(ref)

	// Partial update, exactly the shape the MCP/REST set sends.
	err := api.SetConfig(json.RawMessage(`{
		"verification_backend": "openwebui",
		"verification_model": "live-verify-model",
		"verification_backends": ["live-pool-a", "live-pool-b"],
		"planning_backend": "openwebui",
		"stale_task_seconds": 1200,
		"stuck_running_seconds": 1500,
		"verifier_diff_max_bytes": 8192
	}`))
	if err != nil {
		t.Fatalf("API.SetConfig: %v", err)
	}

	got := live() // next call of the same closure
	pool, backend, model := effectiveVerifierSelection(got)
	if backend != "openwebui" || model != "live-verify-model" {
		t.Errorf("effective selection = %q/%q, want openwebui/live-verify-model", backend, model)
	}
	if !reflect.DeepEqual(pool, []string{"live-pool-a", "live-pool-b"}) {
		t.Errorf("effective pool = %v, want live pools", pool)
	}
	if got.PlanningBackend != "openwebui" {
		t.Errorf("PlanningBackend = %q, want openwebui", got.PlanningBackend)
	}
	if got.StaleTaskSeconds != 1200 || got.VerifierDiffMaxBytes != 8192 {
		t.Errorf("StaleTaskSeconds/VerifierDiffMaxBytes = %d/%d, want 1200/8192", got.StaleTaskSeconds, got.VerifierDiffMaxBytes)
	}
	if got.StuckRunningSeconds != 1500 {
		t.Errorf("StuckRunningSeconds = %d, want 1500", got.StuckRunningSeconds)
	}
	// Partial update: a field OMITTED must keep its boot value (the
	// merge-onto-current design API.SetConfig documents).
	if got.ExecutionBackend != "claude-code" {
		t.Errorf("ExecutionBackend = %q, want boot value claude-code (untouched by partial set)", got.ExecutionBackend)
	}
}
