// A relaunch frees the leases of its previous attempt before admission, so
// placement chooses the node instead of Acquire returning at once for a holder
// that is already admitted.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run 'TestReleaseForRelaunch'
package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
)

func TestReleaseForRelaunchFreesWorkerLease(t *testing.T) {
	m, led, prd, task := adoptNodeRig(t)
	task.ComputeNode = "third"
	m.adoptCapacity(prd, task)

	if got := m.releaseForRelaunch(task); got != "third" {
		t.Fatalf("previous node = %q, want third", got)
	}
	if led.Held(task.ID) {
		t.Fatalf("worker lease still held after releaseForRelaunch")
	}
}

func TestReleaseForRelaunchFreesVerifierLease(t *testing.T) {
	m, led, _, task := adoptNodeRig(t)
	led.Adopt(capacity.Request{
		Holder: VerifyHolder(task.ID), Pools: []string{"host", "node:second"}, Node: "second",
	}, "live-sess")

	if got := m.releaseForRelaunch(task); got != "second" {
		t.Fatalf("previous node = %q, want second", got)
	}
	if led.Held(VerifyHolder(task.ID)) {
		t.Fatalf("verifier lease still held after releaseForRelaunch")
	}
}

func TestReleaseForRelaunchFallsBackToRecordedNode(t *testing.T) {
	m, _, _, task := adoptNodeRig(t)
	task.ComputeNode = "third"

	if got := m.releaseForRelaunch(task); got != "third" {
		t.Fatalf("previous node = %q, want the recorded node third", got)
	}
}

func TestReleaseForRelaunchFreshTaskIsNoop(t *testing.T) {
	m, _, _, task := adoptNodeRig(t)

	if got := m.releaseForRelaunch(task); got != "" {
		t.Fatalf("previous node = %q, want none for a task that never ran", got)
	}
}
