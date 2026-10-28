package config

import "fmt"

// DefaultLoopDetectTurns is the default for autonomous.loop_detect_turns:
// how many of the worker's most recent assistant turns the live loop
// detector (internal/autonomous.DetectLoop, driven from
// cmd/datawatch/progress_refresh.go) compares. It was previously the
// hardcoded `const loopDetectTurns = 3` in progress_refresh.go; this
// constant is the effective default now that the knob is operator-settable.
const DefaultLoopDetectTurns = 3

// LoopDetectTurns is stored as *int so "unset" (nil — no key in the config
// file) is distinguishable from an explicit 0:
//
//	   nil  → default (DefaultLoopDetectTurns), loop detection stays on
//	   0    → loop detection OFF (explicit operator choice)
//	   n>0  → compare the last n turns
//
// A plain int could not express all three, which is why this field is a
// pointer (same shape as the CapacityEnabled / ReservedInteractive knobs).

// ValidateLoopDetectTurns rejects negative values at every write edge
// (REST /api/config PUT, /api/autonomous/config, MCP, CLI, comm channel —
// they all funnel through the same helpers). 0 (off) and positive n are
// valid; nil (absent) is valid.
func ValidateLoopDetectTurns(v *int) error {
	if v != nil && *v < 0 {
		return fmt.Errorf("autonomous.loop_detect_turns must be >= 0 (0 turns loop detection off; default is %d)",
			DefaultLoopDetectTurns)
	}
	return nil
}

// LoopDetectTurnsEffective resolves a stored value to the int the consumer
// (the refresh/loop-detection wait loop) actually uses:
// nil → DefaultLoopDetectTurns; 0 → 0 (off); n → n.
func LoopDetectTurnsEffective(v *int) int {
	if v == nil {
		return DefaultLoopDetectTurns
	}
	return *v
}
