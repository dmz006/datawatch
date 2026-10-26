package autonomous

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// A blank or failed pane capture on an adopted session gives the start bound no
// evidence either way. The adopted wait skips such a tick and the window keeps
// running, so no run of blank captures alone ends the session. This pins that
// behaviour on the adopted wait itself, with a fake clock that moves well past
// the limit over the run.
func TestAdoptedStartEmptyCaptureIsNotJudged(t *testing.T) {
	cases := []struct {
		name string
		pane func(string) (string, error)
	}{
		{"blank capture", func(string) (string, error) { return "", nil }},
		{"failed capture", func(string) (string, error) { return "", fmt.Errorf("capture failed") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := &adoptStepClock{at: time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC), step: adoptedTestStep}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			probes := 0
			var killed []string
			sc := &adoptedStartWatch{
				paneFn: tc.pane,
				stateFn: func(string) string {
					// Thirty probes at a 10s step span 290s, far past the 60s limit.
					// The run ends by cancelling the wait, never by the bound.
					probes++
					if probes == 30 {
						cancel()
					}
					return "waiting_input"
				},
				killFn:   func(id, _, _ string) error { killed = append(killed, id); return nil },
				limit:    adoptedTestLimit,
				now:      clock.now,
				interval: time.Millisecond,
			}
			err := waitForAdoptedSession(ctx, blockUntilCancelled, nil, AdoptedWait{SessionID: "sess-blank"}, sc)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("want the wait to end on the cancel, got %v", err)
			}
			if errors.Is(err, ErrSessionNeverStarted) || len(killed) != 0 {
				t.Fatalf("blank captures ended the session: err=%v killed=%v", err, killed)
			}
			if probes < 30 {
				t.Fatalf("wait ended after %d probes, want the full run of 30", probes)
			}
			if tail := sc.failureTail(); tail != nil {
				t.Fatalf("failure tail recorded from blank captures: %v", tail)
			}
		})
	}
}
