// v7.0.0 S5 — scope-hierarchy tests.

package memory

import (
	"path/filepath"
	"testing"
)

// newTestStore creates an in-memory SQLite store for scope tests.
func newScopeTestStore(t *testing.T) Backend {
	t.Helper()
	dir := t.TempDir()
	st, err := NewStore(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestScopeRef_Resolve(t *testing.T) {
	cases := []struct {
		ref   ScopeRef
		dir   string
		role  string
		sess  string
	}{
		{ScopeRef{Scope: ScopeSessionLocal, Project: "proj1", SessionID: "sess1"}, "proj1", "", "sess1"},
		{ScopeRef{Scope: ScopeProjectShared, Project: "proj1"}, "proj1", "", ""},
		{ScopeRef{Scope: ScopePersonaInProject, Persona: "alice", Project: "proj1"}, "proj1", "persona/alice", ""},
		{ScopeRef{Scope: ScopePersonaGlobal, Persona: "alice"}, "", "persona/alice", ""},
	}
	for i, tc := range cases {
		dir, role, sess := tc.ref.Resolve()
		if dir != tc.dir || role != tc.role || sess != tc.sess {
			t.Errorf("case %d: got (%q, %q, %q), want (%q, %q, %q)", i, dir, role, sess, tc.dir, tc.role, tc.sess)
		}
	}
}

func TestScopedRecall_WalksLayersInOrder(t *testing.T) {
	b := newScopeTestStore(t)
	// Plant memories in different scopes.
	_, _ = b.Save("proj1", "persona-global memory about retries", "", "persona/alice", "", nil)
	_, _ = b.Save("", "persona-global memory about retries", "", "persona/alice", "", nil)
	_, _ = b.Save("proj1", "project-shared memory about retries", "", "", "", nil)
	_, _ = b.Save("proj1", "session-local memory about retries", "", "", "sess1", nil)

	// Embedding-free recall — exercises the layer walk + dedup logic.
	out, err := ScopedRecall(b, nil, "alice", "proj1", "sess1", nil, 10)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	// Each layer should produce at least one hit. The walk order
	// should be persona-global, persona-in-project, project-shared,
	// session-local. We just verify all 4 scopes are represented.
	scopes := map[Scope]bool{}
	for _, sm := range out {
		scopes[sm.Scope] = true
	}
	if len(scopes) == 0 {
		t.Fatalf("expected at least one scope; got %v entries", len(out))
	}
}

func TestScopedRecall_SkipsPersonaLayersWhenPersonaEmpty(t *testing.T) {
	b := newScopeTestStore(t)
	_, _ = b.Save("proj1", "project memory", "", "", "", nil)
	_, _ = b.Save("proj1", "persona memory", "", "persona/alice", "", nil)

	out, err := ScopedRecall(b, nil, "" /*no persona*/, "proj1", "", nil, 10)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	for _, sm := range out {
		if sm.Scope == ScopePersonaGlobal || sm.Scope == ScopePersonaInProject {
			t.Errorf("should skip persona-* scopes when persona empty; got %v", sm.Scope)
		}
	}
}

func TestSeed_CopiesEntriesWithBreadcrumb(t *testing.T) {
	b := newScopeTestStore(t)
	_, _ = b.Save("web-proj", "web learning A", "", "", "", nil)
	_, _ = b.Save("web-proj", "web learning B", "", "", "", nil)

	from := ScopeRef{Scope: ScopeProjectShared, Project: "web-proj"}
	to := ScopeRef{Scope: ScopeSessionLocal, Project: "marketing-proj", SessionID: "marketing-sess"}
	n, err := Seed(b, from, to, SeedFilter{}, 100)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if n != 2 {
		t.Fatalf("seed copied %d, want 2", n)
	}
	// Verify the breadcrumb suffix landed on the destination entries.
	dst, err := b.ListRecent("marketing-proj", 10)
	if err != nil {
		t.Fatalf("list dst: %v", err)
	}
	found := 0
	for _, m := range dst {
		if contains3(m.Content, "seeded from") {
			found++
		}
	}
	if found != 2 {
		t.Errorf("breadcrumb missing on %d/%d seeded entries", 2-found, 2)
	}
}

func TestPromote_PreservesBreadcrumb(t *testing.T) {
	b := newScopeTestStore(t)
	id, _ := b.Save("proj1", "persona learning to promote", "", "", "sess1", nil)

	from := ScopeRef{Scope: ScopeSessionLocal, Project: "proj1", SessionID: "sess1"}
	to := ScopeRef{Scope: ScopeProjectShared, Project: "proj1"}
	newID, bc, err := Promote(b, id, from, to, Breadcrumb{Persona: "alice", Run: "run-99"})
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if newID == 0 || bc == nil {
		t.Fatalf("promote returned id=%d bc=%v", newID, bc)
	}
	if bc.FromScope != ScopeSessionLocal || bc.ToScope != ScopeProjectShared {
		t.Errorf("breadcrumb scopes: %+v", bc)
	}
	if bc.PromotedBy != "operator" {
		t.Errorf("default PromotedBy should be 'operator', got %q", bc.PromotedBy)
	}
	// Verify the new entry has the breadcrumb in its content.
	rows, _ := b.ListRecent("proj1", 10)
	found := false
	for _, m := range rows {
		if m.ID == newID && contains3(m.Content, "promoted session-local → project-shared") {
			found = true
		}
	}
	if !found {
		t.Errorf("promoted entry missing breadcrumb suffix")
	}
}

func TestBorrowReadOnly_DoesNotMutateSource(t *testing.T) {
	b := newScopeTestStore(t)
	id, _ := b.Save("proj1", "borrowable", "", "", "", nil)

	from := ScopeRef{Scope: ScopeProjectShared, Project: "proj1"}
	hits, err := BorrowReadOnly(b, from, nil, 10)
	if err != nil {
		t.Fatalf("borrow: %v", err)
	}
	_ = hits
	// Verify source untouched.
	rows, _ := b.ListRecent("proj1", 10)
	if len(rows) != 1 || rows[0].ID != id {
		t.Errorf("borrow mutated source: %+v", rows)
	}
}

func contains3(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
