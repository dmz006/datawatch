package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestEffectiveReadOnlyTasks(t *testing.T) {
	tr, fa := true, false
	tests := []struct {
		name string
		in   config.AutonomousParallelConfig
		want bool
	}{
		{"unset reads default true", config.AutonomousParallelConfig{}, true},
		{"explicit true", config.AutonomousParallelConfig{ReadOnlyTasks: &tr}, true},
		{"explicit false", config.AutonomousParallelConfig{ReadOnlyTasks: &fa}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveReadOnlyTasks(tc.in); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEffectiveWorktreePerTask(t *testing.T) {
	tr, fa := true, false
	if !EffectiveWorktreePerTask(config.AutonomousParallelConfig{}) {
		t.Fatal("unset must read true")
	}
	if !EffectiveWorktreePerTask(config.AutonomousParallelConfig{WorktreePerTask: &tr}) {
		t.Fatal("explicit true must read true")
	}
	if EffectiveWorktreePerTask(config.AutonomousParallelConfig{WorktreePerTask: &fa}) {
		t.Fatal("explicit false must stay false")
	}
}

func TestEffectiveMergeConflictPolicy(t *testing.T) {
	for in, want := range map[string]string{
		"":                    "additive_then_model",
		"additive_then_model": "additive_then_model",
		"block":               "block",
		"triage":              "triage",
		"bogus":               "additive_then_model",
	} {
		got := EffectiveMergeConflictPolicy(config.AutonomousParallelConfig{MergeConflictPolicy: in})
		if got != want {
			t.Fatalf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestEffectiveMergeConflictModel(t *testing.T) {
	if got := EffectiveMergeConflictModel(config.AutonomousParallelConfig{}); got != "" {
		t.Fatalf("unset: got %q", got)
	}
	if got := EffectiveMergeConflictModel(config.AutonomousParallelConfig{MergeConflictModel: " m1 "}); got != "m1" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateMergeConflictPolicy(t *testing.T) {
	for _, ok := range []string{"", "additive_then_model", "block", "triage"} {
		if err := config.ValidateMergeConflictPolicy(ok); err != nil {
			t.Fatalf("%q refused: %v", ok, err)
		}
	}
	err := config.ValidateMergeConflictPolicy("nope")
	if err == nil || !strings.Contains(err.Error(), "merge_conflict_policy") {
		t.Fatalf("unknown value must be refused, got %v", err)
	}
}
