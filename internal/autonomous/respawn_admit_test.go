// A respawned verifier and a task that comes back from an external wait go
// through placement like a new request. The fixture is the one the relaunch
// placement tests use: two nodes with one engine each, a pi-prompt entry that
// lists both, and a resolver that names no node pool.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run 'TestRespawnAdmit'
package autonomous

import (
	"context"
	"testing"
	"time"
)

// admitVerifier runs AdmitJudge for the task's verifier holder on the fixture's
// pi-prompt entry, with no worker lease to hand over.
func (r *relaunchRig) admitVerifier(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	node, release, err := r.m.AdmitJudge(ctx, JudgeAdmit{
		Holder: VerifyHolder(r.task.ID), PRDID: r.prd.ID, Backend: "pi-prompt", Model: "m",
		Wait: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	t.Cleanup(release)
	return node
}

// The earlier verifier of the task never released its lease, so its node reads
// 2 of 1 next to a live session. The respawned verifier gives that lease up and
// is placed again: it lands on the free node and the first node is left with
// the live session alone.
func TestRespawnAdmit_VerifierRespawnReplacesHeldLease(t *testing.T) {
	r := newRelaunchRig(t, relaunchNodeFirst)
	r.holdOn("live-x", relaunchNodeFirst)
	r.holdOn(VerifyHolder(r.task.ID), relaunchNodeFirst)
	if p, _ := poolOf(r.led.Snapshot(), "node:"+relaunchNodeFirst); p.Held != 2 {
		t.Fatalf("fixture: node %s holds %d, want the incident shape of 2", relaunchNodeFirst, p.Held)
	}

	node := r.admitVerifier(t)

	if node != relaunchNodeSecond {
		t.Fatalf("verifier placed on %q, want %s", node, relaunchNodeSecond)
	}
	if ls := r.led.LeaseOf(VerifyHolder(r.task.ID)); ls == nil || ls.Node != relaunchNodeSecond {
		t.Fatalf("verifier lease = %+v, want a lease on %s", ls, relaunchNodeSecond)
	}
	st := r.led.Snapshot()
	p, _ := poolOf(st, "node:"+relaunchNodeFirst)
	if p.Held != 1 || p.Limit != 1 {
		t.Fatalf("node %s reads %d of %d, want 1 of 1", relaunchNodeFirst, p.Held, p.Limit)
	}
	if h := holdersOf(t, st, "node:"+relaunchNodeFirst); len(h) != 1 || h[0] != "live-x" {
		t.Fatalf("node %s holders = %v, want live-x only", relaunchNodeFirst, h)
	}
}

// Both nodes are free and the earlier verifier lease is on the node listed
// second. Load is equal, so that node wins the tie rather than the first listed.
func TestRespawnAdmit_VerifierRespawnPrefersFreePreviousNode(t *testing.T) {
	r := newRelaunchRig(t, relaunchNodeSecond)
	r.holdOn(VerifyHolder(r.task.ID), relaunchNodeSecond)

	node := r.admitVerifier(t)

	if node != relaunchNodeSecond {
		t.Fatalf("verifier placed on %q, want it to stay on %s", node, relaunchNodeSecond)
	}
	st := r.led.Snapshot()
	for _, n := range []string{relaunchNodeFirst, relaunchNodeSecond} {
		if p, _ := poolOf(st, "node:"+n); p.Held > p.Limit {
			t.Errorf("node %s reads %d of %d", n, p.Held, p.Limit)
		}
	}
}

// A task leaves its node for an external wait and is admitted again. With both
// nodes free it returns to the node it released; when another session takes
// that node in the meantime it goes to the free one.
func TestRespawnAdmit_AwaitExternalReadmitPrefersPreviousNode(t *testing.T) {
	cases := []struct {
		name     string
		takeNode string // node another session takes while the task waits
		want     string
	}{
		{"both nodes free", "", relaunchNodeSecond},
		{"previous node taken meanwhile", relaunchNodeSecond, relaunchNodeFirst},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRelaunchRig(t, relaunchNodeSecond)
			r.holdOn(r.task.ID, relaunchNodeSecond)
			// The release audit event fires after the lease is freed and before
			// the task is admitted again.
			r.m.auditFn = func(action string, _ map[string]any) {
				if action == "autonomous_idle_wait_release" && tc.takeNode != "" {
					r.holdOn("live-x", tc.takeNode)
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := r.m.AwaitExternal(ctx, r.prd, r.task, "CI run #42"); err != nil {
				t.Fatalf("AwaitExternal: %v", err)
			}

			ls := r.led.LeaseOf(r.task.ID)
			if ls == nil || ls.Node != tc.want {
				t.Fatalf("task lease = %+v, want a lease on %s", ls, tc.want)
			}
			st := r.led.Snapshot()
			for _, n := range []string{relaunchNodeFirst, relaunchNodeSecond} {
				if p, _ := poolOf(st, "node:"+n); p.Held > p.Limit {
					t.Errorf("node %s reads %d of %d", n, p.Held, p.Limit)
				}
			}
		})
	}
}
