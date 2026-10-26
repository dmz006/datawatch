package autonomous

import (
	"context"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// TestWaiterReplaceJudgeMovesToFreeNode: a judge queued while its entry
// collapses to a busy node A is re-placed onto node B once the compute
// registry lists B and B is free.
func TestWaiterReplaceJudgeMovesToFreeNode(t *testing.T) {
	r := newReplaceRig(t)
	r.setPlacement([]string{"host", "node:A"}, "A", nil)
	r.hold(t, "holder-a", "A")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type result struct {
		node string
		err  error
	}
	done := make(chan result, 1)
	holder := VerifyHolder("judge-task")
	go func() {
		node, release, err := r.m.AdmitJudge(ctx, JudgeAdmit{
			Holder: holder, PRDID: r.prd.ID, Backend: "pi", Model: "ollama/qwen3.8:27b",
		})
		_ = release
		done <- result{node, err}
	}()

	pollUntil(t, 5*time.Second, "judge queued behind node A", func() bool {
		for _, w := range r.led.Snapshot().Waiting {
			if w.Holder == holder {
				return true
			}
		}
		return false
	})
	select {
	case res := <-done:
		t.Fatalf("judge admitted while A is busy: %+v", res)
	case <-time.After(100 * time.Millisecond):
	}

	r.setPlacement([]string{"host"}, "", []capacity.NodeChoice{{Name: "A", Priority: 10}, {Name: "B", Priority: 90}})
	r.led.Release("holder-a")

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("AdmitJudge: %v", res.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting judge was never re-placed onto the free node")
	}
	ls := r.led.LeaseOf(holder)
	if ls == nil || ls.Node != "B" {
		t.Fatalf("judge lease = %+v, want node B", ls)
	}
}
