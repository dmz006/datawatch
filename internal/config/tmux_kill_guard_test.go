package config

import "testing"

// TestTmuxKillGuardDefaultOn proves the three-way semantics of
// autonomous.tmux_kill_guard: unset => guard enabled (default ON),
// explicit false => guard disabled (operator opt-out), explicit
// true => guard enabled.
func TestTmuxKillGuardDefaultOn(t *testing.T) {
	// Unset: pointer is nil and the resolver reports enabled.
	cfg := loadYAML(t, "autonomous:\n  enabled: true\n")
	if cfg.Autonomous.TmuxKillGuard != nil {
		t.Fatalf("unset tmux_kill_guard = %v, want nil", *cfg.Autonomous.TmuxKillGuard)
	}
	if !cfg.Autonomous.TmuxKillGuardEnabled() {
		t.Fatal("unset tmux_kill_guard: TmuxKillGuardEnabled = false, want true (default ON)")
	}

	// Explicit false: pointer is non-nil and the resolver reports
	// disabled.
	f := loadYAML(t, "autonomous:\n  tmux_kill_guard: false\n")
	if f.Autonomous.TmuxKillGuard == nil {
		t.Fatal("explicit tmux_kill_guard: false lost in Load, want *bool(false)")
	}
	if *f.Autonomous.TmuxKillGuard != false {
		t.Fatalf("explicit tmux_kill_guard = %v, want false", *f.Autonomous.TmuxKillGuard)
	}
	if f.Autonomous.TmuxKillGuardEnabled() {
		t.Fatal("explicit tmux_kill_guard false: TmuxKillGuardEnabled = true, want false")
	}

	// Explicit true: pointer is non-nil and the resolver reports
	// enabled.
	tr := loadYAML(t, "autonomous:\n  tmux_kill_guard: true\n")
	if tr.Autonomous.TmuxKillGuard == nil {
		t.Fatal("explicit tmux_kill_guard: true lost in Load, want *bool(true)")
	}
	if *tr.Autonomous.TmuxKillGuard != true {
		t.Fatalf("explicit tmux_kill_guard = %v, want true", *tr.Autonomous.TmuxKillGuard)
	}
	if !tr.Autonomous.TmuxKillGuardEnabled() {
		t.Fatal("explicit tmux_kill_guard true: TmuxKillGuardEnabled = false, want true")
	}
}
