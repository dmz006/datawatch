// BL347 — Session lineage: GetChildren ordering, cascade kill, no-cascade default.

package session

import (
	"testing"
	"time"
)

func TestGetChildren_Empty(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	children := mgr.GetChildren("host-parent01")
	if len(children) != 0 {
		t.Errorf("expected 0 children, got %d", len(children))
	}
}

func TestGetChildren_ReturnsOnlyDirectChildren(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	parent := &Session{
		ID: "pp01", FullID: "host-pp01", Hostname: "host",
		State: StateRunning, UpdatedAt: time.Now(), CreatedAt: time.Now(),
	}
	child1 := &Session{
		ID: "cc01", FullID: "host-cc01", Hostname: "host", ParentID: "host-pp01",
		State: StateRunning, UpdatedAt: time.Now(), CreatedAt: time.Now().Add(time.Second),
	}
	child2 := &Session{
		ID: "cc02", FullID: "host-cc02", Hostname: "host", ParentID: "host-pp01",
		State: StateComplete, UpdatedAt: time.Now(), CreatedAt: time.Now().Add(2 * time.Second),
	}
	unrelated := &Session{
		ID: "uu01", FullID: "host-uu01", Hostname: "host",
		State: StateRunning, UpdatedAt: time.Now(), CreatedAt: time.Now(),
	}

	for _, s := range []*Session{parent, child1, child2, unrelated} {
		if err := mgr.SaveSession(s); err != nil {
			t.Fatal(err)
		}
	}

	children := mgr.GetChildren("host-pp01")
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
	if children[0].ID != "cc01" || children[1].ID != "cc02" {
		t.Errorf("children should be sorted oldest-first: got %s, %s", children[0].ID, children[1].ID)
	}
}

func TestGetChildren_SortedOldestFirst(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	base := time.Now()
	for i, id := range []string{"dd03", "dd01", "dd02"} {
		mgr.SaveSession(&Session{ //nolint:errcheck
			ID: id, FullID: "host-" + id, Hostname: "host", ParentID: "host-parent",
			State: StateRunning, UpdatedAt: base,
			CreatedAt: base.Add(time.Duration(i+1) * time.Second),
		})
	}
	// dd03 created at +1s, dd01 at +2s, dd02 at +3s
	children := mgr.GetChildren("host-parent")
	if len(children) != 3 {
		t.Fatalf("expected 3, got %d", len(children))
	}
	if children[0].ID != "dd03" || children[1].ID != "dd01" || children[2].ID != "dd02" {
		t.Errorf("order wrong: %s %s %s", children[0].ID, children[1].ID, children[2].ID)
	}
}

func TestKill_CascadeWhenKillChildrenTrue(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	parent := &Session{
		ID: "pp10", FullID: "host-pp10", Hostname: "host",
		TmuxSession: "cs-pp10", LogFile: t.TempDir() + "/pp10.log",
		State: StateRunning, KillChildren: true,
		UpdatedAt: time.Now(), CreatedAt: time.Now(),
	}
	child := &Session{
		ID: "cc10", FullID: "host-cc10", Hostname: "host",
		TmuxSession: "cs-cc10", LogFile: t.TempDir() + "/cc10.log",
		ParentID: "host-pp10",
		State:    StateRunning,
		UpdatedAt: time.Now(), CreatedAt: time.Now().Add(time.Second),
	}

	for _, s := range []*Session{parent, child} {
		if err := mgr.SaveSession(s); err != nil {
			t.Fatal(err)
		}
	}

	if err := mgr.Kill("host-pp10"); err != nil {
		t.Fatalf("Kill parent: %v", err)
	}

	// Child should now be killed.
	updated, ok := mgr.store.Get("host-cc10")
	if !ok || updated == nil {
		t.Fatal("child session not found after cascade kill")
	}
	if updated.State != StateKilled {
		t.Errorf("child state = %q, want killed", updated.State)
	}
}

func TestKill_NoCascadeByDefault(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	parent := &Session{
		ID: "pp20", FullID: "host-pp20", Hostname: "host",
		TmuxSession: "cs-pp20", LogFile: t.TempDir() + "/pp20.log",
		State: StateRunning, KillChildren: false,
		UpdatedAt: time.Now(), CreatedAt: time.Now(),
	}
	child := &Session{
		ID: "cc20", FullID: "host-cc20", Hostname: "host",
		TmuxSession: "cs-cc20", LogFile: t.TempDir() + "/cc20.log",
		ParentID:  "host-pp20",
		State:     StateRunning,
		UpdatedAt: time.Now(), CreatedAt: time.Now().Add(time.Second),
	}

	for _, s := range []*Session{parent, child} {
		if err := mgr.SaveSession(s); err != nil {
			t.Fatal(err)
		}
	}

	if err := mgr.Kill("host-pp20"); err != nil {
		t.Fatalf("Kill parent: %v", err)
	}

	// Child should still be running (not cascade-killed).
	updated, ok := mgr.store.Get("host-cc20")
	if !ok || updated == nil {
		t.Fatal("child session not found after parent kill")
	}
	if updated.State != StateRunning {
		t.Errorf("child state = %q, want running (no cascade)", updated.State)
	}
}

func TestSession_ParentIDPersists(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	s := &Session{
		ID: "ee01", FullID: "host-ee01", Hostname: "host",
		ParentID: "host-parent99", KillChildren: true,
		State: StateRunning, UpdatedAt: time.Now(), CreatedAt: time.Now(),
	}
	if err := mgr.SaveSession(s); err != nil {
		t.Fatal(err)
	}

	got, ok := mgr.store.Get("host-ee01")
	if !ok || got == nil {
		t.Fatal("session not found")
	}
	if got.ParentID != "host-parent99" {
		t.Errorf("ParentID = %q, want host-parent99", got.ParentID)
	}
	if !got.KillChildren {
		t.Error("KillChildren should be true")
	}
}
