package autonomous

import (
	"context"
	"errors"
	"testing"

	"github.com/dmz006/datawatch/internal/config"

	"github.com/dmz006/datawatch/internal/session"
)

// An adopted worker whose session records a failed launch ends the wait as a
// launch failure, which the infrastructure retry path recognises. It never ends
// the wait as finished, even with a completion line in its output.
func TestWaitAdoptedReportsALaunchFailure(t *testing.T) {
	want := LaunchFailureErrorWithReason("h-1", "claude-code", "failed", "launch failed: made-up dialog")
	err := WaitAdopted(context.Background(), AdoptedWait{PRDID: "p", TaskID: "t", SessionID: "h-1"}, AdoptedWaitPorts{
		Status:        func(string) (session.State, string, bool) { return session.StateFailed, "claude-code", true },
		Completed:     func(string) bool { return true },
		LaunchFailure: func(string) error { return want },
	})
	if !errors.Is(err, ErrSessionKilledBeforeDelivery) || !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("err = %v, want the launch failure", err)
	}
	if class, ok := ClassifyInfraText(err.Error()); !ok || class != config.InfraRetryClassSessionKilled {
		t.Errorf("infra class = %q %v for %q", class, ok, err)
	}
}
