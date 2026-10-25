package compute

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// TestNodeSlotOverrideRejectsNegative checks that a negative session_slots is
// refused by the node record and by the registry paths that store it. Zero is
// accepted and means "derive from observer data".
func TestNodeSlotOverrideRejectsNegative(t *testing.T) {
	bad := &Node{Name: "gpubox", Kind: KindOllama, Address: "http://gpubox:11434", SessionSlots: -1}
	if err := bad.Validate(); err == nil {
		t.Fatal("Validate with session_slots -1: want an error, got nil")
	}

	reg, err := NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.Add(&Node{Name: "gpubox", Kind: KindOllama, Address: "http://gpubox:11434", SessionSlots: -2}); err == nil {
		t.Fatal("Add with session_slots -2: want an error, got nil")
	}

	ok := &Node{Name: "gpubox", Kind: KindOllama, Address: "http://gpubox:11434"}
	if err := reg.Add(ok); err != nil {
		t.Fatalf("Add with session_slots absent: %v", err)
	}
	ok.SessionSlots = -1
	if err := reg.Update(ok); err == nil {
		t.Fatal("Update with session_slots -1: want an error, got nil")
	}
	got, err := reg.Get("gpubox")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SessionSlots != 0 {
		t.Errorf("stored session_slots = %d after rejected update, want 0", got.SessionSlots)
	}

	zero := &Node{Name: "zero", Kind: KindOllama, Address: "http://zero:11434", SessionSlots: 0}
	if err := zero.Validate(); err != nil {
		t.Errorf("Validate with session_slots 0: %v, want nil (0 means derive)", err)
	}
	if err := reg.Add(zero); err != nil {
		t.Errorf("Add with session_slots 0: %v, want nil", err)
	}
}

// TestNodeSlotOverrideAbsentKeepsDerived checks that a node record with no
// session_slots key decodes to 0 and the slot count is the derived one, the
// same count a node that never had the field gets.
func TestNodeSlotOverrideAbsentKeepsDerived(t *testing.T) {
	const record = `{"name":"gpubox","kind":"ollama","address":"http://gpubox:11434"}`
	var n Node
	if err := json.Unmarshal([]byte(record), &n); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if n.SessionSlots != 0 {
		t.Fatalf("SessionSlots = %d for a record without the key, want 0", n.SessionSlots)
	}

	obs := NodeObservation{GPUs: fakeGPUs(24, 0, 0), LoadedModelBytes: []uint64{20 * testGiB}}
	want := NodeSlots(0, 0, 1, 0, obs)
	if want != 2 {
		t.Fatalf("derived slots for two 24 GiB GPUs with a 20 GiB model = %d, want 2", want)
	}
	if got := NodeSlots(n.SessionSlots, 0, 1, 0, obs); got != want {
		t.Errorf("slots with the override absent = %d, want the derived %d", got, want)
	}

	// The record also omits the key when it is re-encoded, so a node that
	// never set an override does not gain a session_slots field on save.
	out, err := json.Marshal(&n)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(out, &fields); err != nil {
		t.Fatalf("Unmarshal of marshaled node: %v", err)
	}
	if _, ok := fields["session_slots"]; ok {
		t.Errorf("marshaled node has session_slots = %s, want the key left out", fields["session_slots"])
	}
}
