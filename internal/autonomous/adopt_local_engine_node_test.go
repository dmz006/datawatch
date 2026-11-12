package autonomous

import "testing"

// A local-engine backend keeps the node its task records even when the
// resolver finds no hosting entry for the model (an unprefixed model name):
// adoption after a restart must not drop its node lease as if the backend
// took no node.
func TestAdoptKeepsRecordedNodeForLocalEngineBackend(t *testing.T) {
	piKeys := func(string, string) ([]string, string) { return []string{"llm:pi-prompt"}, "" }
	m, led, prd, task := poolMoveRig(t, piKeys)
	task.Backend = "pi-prompt"
	_ = m.Store().SaveTask(task)
	m.adoptCapacity(prd, task)
	if got := nodeHeld(led, "node:datawatch"); got != 1 {
		t.Fatalf("node:datawatch Held = %d, want 1 for a pi-prompt task that recorded the node", got)
	}
	if ls := led.LeaseOf(task.ID); ls == nil || ls.Node != "datawatch" {
		t.Fatalf("lease = %+v, want node datawatch", ls)
	}
}
