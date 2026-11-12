// v7.0.0 S1 — ComputeNode registry tests.

package compute

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	return r
}

func validNode(name string) *Node {
	return &Node{
		Name:    name,
		Kind:    KindRemote,
		Address: "https://" + name + ":11434",
		DeclaredCapacity: DeclaredCapacity{
			MaxConcurrentModels: 2,
			GPUMemGB:            24,
		},
		SchedulingPriority: 75,
	}
}

func TestNodeValidate(t *testing.T) {
	cases := []struct {
		name string
		n    *Node
		err  string
	}{
		{"empty name", &Node{Kind: KindLocal}, "name required"},
		{"bad chars in name", &Node{Name: "GPU 1", Kind: KindLocal}, "invalid name"},
		{"unknown kind", &Node{Name: "x", Kind: "void"}, "unknown kind"},
		{"remote needs address", &Node{Name: "x", Kind: KindRemote}, "requires address"},
		{"local ok no address", &Node{Name: "x", Kind: KindLocal}, ""},
		{"priority OOR", &Node{Name: "x", Kind: KindLocal, SchedulingPriority: 200}, "out of range"},
		{"negative capacity", &Node{Name: "x", Kind: KindLocal, DeclaredCapacity: DeclaredCapacity{GPUs: -1}}, ">= 0"},
		{"reverse maint window", &Node{Name: "x", Kind: KindLocal, MaintenanceWindows: []MaintenanceWindow{{From: time.Now().Add(time.Hour), To: time.Now()}}}, "before .from"},
		{"happy path", validNode("gpu-1"), ""},
	}
	for _, tc := range cases {
		err := tc.n.Validate()
		if tc.err == "" {
			if err != nil {
				t.Errorf("%s: unexpected err: %v", tc.name, err)
			}
		} else {
			if err == nil || !contains(err.Error(), tc.err) {
				t.Errorf("%s: want err containing %q, got %v", tc.name, tc.err, err)
			}
		}
	}
}

func TestRegistryCRUD(t *testing.T) {
	r := newTestRegistry(t)
	if got := r.List(); len(got) != 0 {
		t.Fatalf("empty list: got %d", len(got))
	}
	n := validNode("gpu-1")
	if err := r.Add(n); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := r.Add(n); err != ErrConflict {
		t.Fatalf("re-add: want ErrConflict, got %v", err)
	}
	got, err := r.Get("gpu-1")
	if err != nil || got.Address != n.Address {
		t.Fatalf("get: %v %+v", err, got)
	}
	if got.SchedulingPriority != 75 {
		t.Fatalf("priority not preserved: %d", got.SchedulingPriority)
	}
	got.DeclaredCapacity.MaxConcurrentModels = 4
	if err := r.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, _ := r.Get("gpu-1")
	if got2.DeclaredCapacity.MaxConcurrentModels != 4 {
		t.Fatalf("update not persisted: %d", got2.DeclaredCapacity.MaxConcurrentModels)
	}
	if err := r.Delete("gpu-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := r.Get("gpu-1"); err != ErrNotFound {
		t.Fatalf("get after delete: want ErrNotFound, got %v", err)
	}
}

func TestRegistryPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.json")
	r1, _ := NewRegistry(path)
	_ = r1.Add(validNode("gpu-1"))
	_ = r1.Add(validNode("gpu-2"))

	// Re-open — entries should reload.
	r2, err := NewRegistry(path)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	if got := r2.List(); len(got) != 2 {
		t.Fatalf("re-load: got %d, want 2", len(got))
	}
	if _, err := r2.Get("gpu-1"); err != nil {
		t.Fatalf("re-load gpu-1: %v", err)
	}
}

func TestRegistrySeed(t *testing.T) {
	r := newTestRegistry(t)
	r.Seed([]Node{*validNode("gpu-1"), *validNode("gpu-2")})
	if got := r.List(); len(got) != 2 {
		t.Fatalf("seed: got %d, want 2", len(got))
	}
	// Re-seeding should not overwrite operator-edited entries.
	current, _ := r.Get("gpu-1")
	current.Tags = []string{"operator-set"}
	_ = r.Update(current)
	r.Seed([]Node{*validNode("gpu-1")})
	after, _ := r.Get("gpu-1")
	if len(after.Tags) != 1 || after.Tags[0] != "operator-set" {
		t.Fatalf("seed overwrote operator edits: %+v", after.Tags)
	}
}

func TestBindObservedPeer_NoNodeCreatesNothing(t *testing.T) {
	r := newTestRegistry(t)
	bound, err := r.BindObservedPeer("gpu-remote")
	if err != nil || bound {
		t.Fatalf("bound=%v err=%v, want false,nil", bound, err)
	}
	if got := r.List(); len(got) != 0 {
		t.Fatalf("registration created nodes: %+v", got)
	}
	view := r.ObservedOnly([]ObservedPeer{{Name: "gpu-remote", Shape: "B"}})
	if len(view) != 1 || view[0].Name != "gpu-remote" || view[0].Shape != "B" {
		t.Fatalf("observed-only view = %+v", view)
	}
}

func TestBindObservedPeer_ExistingNodeKeepsAddress(t *testing.T) {
	r := newTestRegistry(t)
	n := validNode("gpu-remote")
	n.Kind = KindOllama
	n.Address = "http://192.0.2.5:11434"
	if err := r.Add(n); err != nil {
		t.Fatalf("add: %v", err)
	}
	// The registration's source address (192.0.2.99) is never an input.
	bound, err := r.BindObservedPeer("gpu-remote")
	if err != nil || !bound {
		t.Fatalf("bound=%v err=%v", bound, err)
	}
	got, _ := r.Get("gpu-remote")
	if got.ObserverPeer != "gpu-remote" {
		t.Errorf("ObserverPeer = %q", got.ObserverPeer)
	}
	if got.Address != "http://192.0.2.5:11434" || got.Kind != KindOllama {
		t.Errorf("address/kind changed: %q %q", got.Address, got.Kind)
	}
	if view := r.ObservedOnly([]ObservedPeer{{Name: "gpu-remote", Shape: "B"}}); len(view) != 0 {
		t.Errorf("bound peer still observed-only: %+v", view)
	}
}

func TestBindObservedPeer_NeverOverwritesAddress(t *testing.T) {
	r := newTestRegistry(t)
	n := validNode("gpu-remote")
	n.Address = "http://192.0.2.5:11434"
	n.ObserverPeer = "other-peer"
	if err := r.Add(n); err != nil {
		t.Fatalf("add: %v", err)
	}
	before, _ := r.Get("gpu-remote")
	for i := 0; i < 2; i++ {
		if _, err := r.BindObservedPeer("gpu-remote"); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := r.Get("gpu-remote")
	if after.Address != "http://192.0.2.5:11434" {
		t.Errorf("address overwritten: %q", after.Address)
	}
	if after.ObserverPeer != "other-peer" {
		t.Errorf("existing binding changed: %q", after.ObserverPeer)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("node rewritten though nothing changed")
	}
}

func TestObservedOnly_BoundByObserverPeerField(t *testing.T) {
	r := newTestRegistry(t)
	n := validNode("node-a")
	n.ObserverPeer = "peer-x"
	if err := r.Add(n); err != nil {
		t.Fatal(err)
	}
	view := r.ObservedOnly([]ObservedPeer{{Name: "peer-x"}, {Name: "peer-y", Shape: "C"}})
	if len(view) != 1 || view[0].Name != "peer-y" {
		t.Fatalf("view = %+v", view)
	}
}

func TestNodeInMaintenance(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	n := &Node{
		MaintenanceWindows: []MaintenanceWindow{
			{From: now.Add(-time.Hour), To: now.Add(time.Hour)},
		},
	}
	if !n.InMaintenance(now) {
		t.Fatal("should be in maintenance")
	}
	if n.InMaintenance(now.Add(2 * time.Hour)) {
		t.Fatal("should not be in maintenance after window")
	}
	// Open-ended window.
	n2 := &Node{MaintenanceWindows: []MaintenanceWindow{{From: now.Add(-time.Hour)}}}
	if !n2.InMaintenance(now) || !n2.InMaintenance(now.Add(100*time.Hour)) {
		t.Fatal("open-ended window should always trigger")
	}
}

func TestNodeAllowsConsumer(t *testing.T) {
	cases := []struct {
		perm Permissions
		c    string
		want bool
	}{
		{Permissions{}, "council", true},                                                    // empty allow = all
		{Permissions{AllowedConsumers: []string{"council"}}, "council", true},               // explicit allow
		{Permissions{AllowedConsumers: []string{"council"}}, "ask", false},                  // not in allow
		{Permissions{AllowedConsumers: []string{"*"}}, "anything", true},                    // wildcard
		{Permissions{DeniedConsumers: []string{"council"}}, "council", false},               // denied wins
		{Permissions{AllowedConsumers: []string{"*"}, DeniedConsumers: []string{"ask"}}, "ask", false}, // denied wins over allow-all
	}
	for i, tc := range cases {
		n := &Node{Permissions: tc.perm}
		if got := n.AllowsConsumer(tc.c); got != tc.want {
			t.Errorf("case %d: AllowsConsumer(%q) = %v, want %v", i, tc.c, got, tc.want)
		}
	}
}

// TestNormalizeAddress covers the scheme-normalization helper added in v8.25.8.
func TestNormalizeAddress(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"http://host:11434", "http://host:11434"},
		{"https://host:11434", "https://host:11434"},
		{"host:11434", "http://host:11434"},
		{"127.0.0.1:53106", "http://127.0.0.1:53106"},
		{"datawatch:11434", "http://datawatch:11434"},
	}
	for _, tc := range cases {
		if got := normalizeAddress(tc.in); got != tc.want {
			t.Errorf("normalizeAddress(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNodeValidate_NormalizesAddress verifies that Validate mutates a
// scheme-less Address to http:// so the stored node is always valid for
// use as a URL base. This is the v8.25.8 regression guard.
func TestNodeValidate_NormalizesAddress(t *testing.T) {
	n := &Node{Name: "gpu-1", Kind: KindRemote, Address: "gpu-1:11434"}
	if err := n.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if n.Address != "http://gpu-1:11434" {
		t.Errorf("Address = %q, want http://gpu-1:11434", n.Address)
	}
}

// TestRegistryAdd_NormalizesAddress verifies that Add normalizes a scheme-less
// address before persisting so all downstream consumers receive a valid URL.
func TestRegistryAdd_NormalizesAddress(t *testing.T) {
	r := newTestRegistry(t)
	n := &Node{Name: "gpu-1", Kind: KindRemote, Address: "gpu-1:11434"}
	if err := r.Add(n); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, _ := r.Get("gpu-1")
	if got.Address != "http://gpu-1:11434" {
		t.Errorf("stored Address = %q, want http://gpu-1:11434", got.Address)
	}
}

// TestSetDrainPersisted verifies SetDrain goes through the same save path
// as the other node setters: re-opening the registry restores the fields,
// and an expired drain reads as not active without auto-clearing the
// stored fields.
func TestSetDrainPersisted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.json")
	r1, _ := NewRegistry(path)
	_ = r1.Add(validNode("n1"))
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	until := now.Add(time.Hour)
	if err := r1.SetDrain("n1", true, until, now, "maintenance"); err != nil {
		t.Fatalf("set drain: %v", err)
	}
	r2, err := NewRegistry(path)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	n, _ := r2.Get("n1")
	if !n.Drained || !n.DrainUntil.Equal(until) || n.DrainReason != "maintenance" {
		t.Fatalf("drain not persisted: %+v", n)
	}
	// Expired drain: still stored, only reads as inactive.
	if n.DrainActive(now.Add(24 * time.Hour)) {
		t.Fatal("expired drain must read as not active")
	}
	if !n.Drained || !n.DrainUntil.Equal(until) {
		t.Fatal("expired drain must keep stored fields (no auto-clear)")
	}
}

// TestSetDrainClear verifies undrain clears Drained, DrainUntil and
// DrainReason on the stored node.
func TestSetDrainClear(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Add(validNode("n1"))
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	until := now.Add(2 * time.Hour)

	if err := r.SetDrain("n1", true, until, now, "maintenance"); err != nil {
		t.Fatalf("set drain: %v", err)
	}
	n, _ := r.Get("n1")
	if !n.Drained || !n.DrainUntil.Equal(until) || n.DrainReason != "maintenance" {
		t.Fatalf("drain not stored: %+v", n)
	}

	if err := r.SetDrain("n1", false, time.Time{}, now.Add(time.Hour), ""); err != nil {
		t.Fatalf("undrain: %v", err)
	}
	n, _ = r.Get("n1")
	if n.Drained || !n.DrainUntil.IsZero() || n.DrainReason != "" {
		t.Fatalf("undrain did not clear fields: %+v", n)
	}
	if n.DrainActive(now) {
		t.Fatal("undrained node must not report drain active")
	}
}

// TestSetDrainPastUntilRejected verifies a non-zero until that is not
// after now is rejected with a clear message, and that the unknown-node
// path returns ErrNotFound.
func TestSetDrainPastUntilRejected(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Add(validNode("n1"))
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

	for _, until := range []time.Time{now.Add(-time.Minute), now} {
		if err := r.SetDrain("n1", true, until, now, "late"); err == nil {
			t.Fatalf("until %s not after now %s: want error", until, now)
		}
	}
	n, _ := r.Get("n1")
	if n.Drained || !n.DrainUntil.IsZero() || n.DrainReason != "" {
		t.Fatalf("rejected drain must not mutate the node: %+v", n)
	}

	// Far-future until is fine.
	if err := r.SetDrain("n1", true, now.Add(time.Hour), now, "ok"); err != nil {
		t.Fatalf("future until rejected: %v", err)
	}
	// Unknown node.
	if err := r.SetDrain("ghost", true, time.Time{}, now, "x"); err != ErrNotFound {
		t.Fatalf("unknown node: want ErrNotFound, got %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
