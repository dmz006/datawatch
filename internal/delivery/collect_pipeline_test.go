package delivery

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pipeline"
)

func TestInterruptedAndFailedPipelinesAreListed(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	runs := []pipeline.RunRecord{
		{ID: "pipe-1", State: pipeline.StateRunning, StartedAt: now, Interrupted: true},
		{ID: "pipe-2", State: pipeline.StateFailed, StartedAt: now, EndedAt: &now},
		{ID: "pipe-3", State: pipeline.StateCompleted, StartedAt: now, EndedAt: &now},
		{ID: "pipe-4", State: pipeline.StateRunning, StartedAt: now},
	}
	cs := PipelineCandidates(runs)
	if len(cs) != 2 {
		t.Fatalf("candidates=%v", cs)
	}
	for i, want := range []string{"interrupted", "failed"} {
		if cs[i].Kind != KindPipeline || cs[i].State != want || !cs[i].Sticky || cs[i].Invariant != InvFailed || cs[i].Problem == "" {
			t.Errorf("%d: %+v", i, cs[i])
		}
	}
}
