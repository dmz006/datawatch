package autonomous

import (
	"context"
	"strings"
	"testing"
)

// A run whose push was held or never confirmed delivered nothing, and the
// loop's record never calls it merged.
func TestSupervisorLoopNeverReportsAnUnpushedRunAsMerged(t *testing.T) {
	for name, c := range map[string]struct {
		res  SupervisorMergeResult
		want []string
	}{
		"only a held run": {SupervisorMergeResult{Merged: []string{"held-branch"}, NotMerged: []string{"held-branch"},
			Note: "stopped after held-branch: its push did not happen"},
			[]string{"merged 0 branches", "not merged (held or not pushed): held-branch", "its push did not happen"}},
		"one delivered, then a held one": {SupervisorMergeResult{Merged: []string{"a", "held-branch"}, NotMerged: []string{"held-branch"},
			Note: "stopped after held-branch: its push did not happen"},
			[]string{"merged 1 branches", "not merged (held or not pushed): held-branch", "its push did not happen"}},
		"a conflict after a held run": {SupervisorMergeResult{Merged: []string{"held-branch"}, NotMerged: []string{"held-branch"}, Conflict: "b"},
			[]string{"stopped after merging 0", "held-branch"}},
	} {
		t.Run(name, func(t *testing.T) {
			res := c.res
			_, steps, _ := newStepsRig(t, SupervisorLoopHooks{
				MergeUntilConflict: func(context.Context) (SupervisorMergeResult, error) { return res, nil },
			})
			got := resultFor(t, steps.RunPass(context.Background()), stepRunMergeUntilConflict).Detail
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("detail %q lacks %q", got, want)
				}
			}
		})
	}
}
