package main

// Restart ordering end to end: with the ledger's adoption hold set the way
// wireCapacity sets it, a request made before the boot-time resume pass has
// adopted the surviving sessions waits, and is then placed on the node the
// resumed sessions do not occupy. Without the hold it would take alpha, the
// node listed first, and the adoption that followed would put a second engine
// on it.

import (
	"context"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

func TestRestartAdoptionGateHoldsRequestUntilResumePassAdopts(t *testing.T) {
	sessions := map[string]*session.Session{
		"sess-one": {ComputeNodeRef: "alpha"},
	}
	r := newRestartRig(t, sessions)
	one := r.runningTask("one", "sess-one")

	r.led.BeginAdoption()
	early := r.admit(r.request("early", ""))
	r.waitQueued("early")
	if w := waiterOf(r.led.Snapshot(), "early"); w == nil || !strings.Contains(w.Reason, "adopted") {
		t.Fatalf("early waiter = %+v, want it held back for adoption", w)
	}
	if r.led.Held("early") {
		t.Fatal("a request was granted before the resumed sessions were adopted")
	}

	// The resume pass adopts the session and then ends the hold itself.
	r.api.SetExecutors(func(_ context.Context, req autonomous.SpawnRequest) (autonomous.SpawnResult, error) {
		t.Errorf("task %s was spawned, want its surviving session adopted", req.TaskID)
		return autonomous.SpawnResult{}, context.Canceled
	}, func(ctx context.Context, _ *autonomous.PRD, _ *autonomous.Task) (autonomous.VerificationResult, error) {
		<-ctx.Done()
		return autonomous.VerificationResult{}, ctx.Err()
	})
	r.waitFor("the session to be adopted", func() bool { return r.led.LeaseOf(one) != nil })

	r.expectGranted(early, "early", "beta")
	st := r.led.Snapshot()
	for _, p := range st.Pools {
		if strings.HasPrefix(p.Name, "node:") && p.Held+p.External > p.Limit {
			t.Errorf("%s is over its limit: %d/%d", p.Name, p.Held+p.External, p.Limit)
		}
	}
	if p, ok := poolNamed(st, "node:alpha"); !ok || p.Held != 1 {
		t.Errorf("node:alpha = %+v, want the resumed session as its one engine", p)
	}
}
