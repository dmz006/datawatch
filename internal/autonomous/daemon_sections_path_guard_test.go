package autonomous

import (
	"context"
	"testing"
)

func TestDaemonSectionsOnlyChangePathGuard(t *testing.T) {
	cases := []struct {
		name string
		rel  string
		want bool
	}{
		{"traversal", "../CLAUDE.md", false},
		{"nested traversal", "sub/../../CLAUDE.md", false},
		{"nul byte", "CLAUDE.md\x00x", false},
		{"empty", "", false},
		{"in-base daemon-only change", "CLAUDE.md", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, run := dsoSetup(t, dsoDaemon)
			if got := daemonSectionsOnlyChange(context.Background(), dir, tc.rel, run); got != tc.want {
				t.Errorf("daemonSectionsOnlyChange(%q) = %v, want %v", tc.rel, got, tc.want)
			}
		})
	}
}
