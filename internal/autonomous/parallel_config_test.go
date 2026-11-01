package autonomous

import (
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
