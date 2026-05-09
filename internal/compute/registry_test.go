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

func TestRegistryEnsureFromStatsPeer(t *testing.T) {
	r := newTestRegistry(t)
	n, created, err := r.EnsureFromStatsPeer("gpu-remote", "10.0.0.5:9001", "B")
	if err != nil || !created || n.Name != "gpu-remote" || n.Kind != KindRemote || !n.AutoCreated {
		t.Fatalf("first peer: %v created=%v node=%+v", err, created, n)
	}
	// Second push from same peer should NOT re-create.
	_, created2, _ := r.EnsureFromStatsPeer("gpu-remote", "10.0.0.5:9001", "B")
	if created2 {
		t.Fatalf("second peer push should not auto-create")
	}
	// Address change should refresh.
	n3, _, _ := r.EnsureFromStatsPeer("gpu-remote", "10.0.0.6:9001", "B")
	if n3.Address != "10.0.0.6:9001" {
		t.Fatalf("address refresh: %s", n3.Address)
	}
	// Shape C → KindK8s.
	nk8s, created3, _ := r.EnsureFromStatsPeer("k8s-pod-1", "10.42.0.1:9001", "C")
	if !created3 || nk8s.Kind != KindK8s {
		t.Fatalf("shape C: created=%v kind=%s", created3, nk8s.Kind)
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

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
