// BL98 — KG contradiction detection tests.

package memory

import (
	"path/filepath"
	"testing"
)

func kgFixture(t *testing.T) *KnowledgeGraph {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewKnowledgeGraph(store)
}

func TestSetFunctionalPredicates_RoundTrip(t *testing.T) {
	defer SetFunctionalPredicates(nil)
	SetFunctionalPredicates([]string{"owns", "current_status"})
	got := FunctionalPredicates()
	if len(got) != 2 {
		t.Fatalf("got %v want 2 entries", got)
	}
	// Replacement semantics — second call wipes the first.
	SetFunctionalPredicates([]string{"only_one"})
	if len(FunctionalPredicates()) != 1 {
		t.Errorf("replace failed: %v", FunctionalPredicates())
	}
}

func TestFindContradictions_NoFunctionalsRegistered(t *testing.T) {
	defer SetFunctionalPredicates(nil)
	SetFunctionalPredicates(nil)
	kg := kgFixture(t)
	_, _ = kg.AddTriple("alice", "current_status", "active", "", "src")
	_, _ = kg.AddTriple("alice", "current_status", "vacation", "", "src")
	got, err := kg.FindContradictions()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("no functionals → no contradictions, got %v", got)
	}
}

func TestFindContradictions_DetectsConflictingObjects(t *testing.T) {
	defer SetFunctionalPredicates(nil)
	SetFunctionalPredicates([]string{"current_status"})
	kg := kgFixture(t)
	_, _ = kg.AddTriple("alice", "current_status", "active", "", "src1")
	_, _ = kg.AddTriple("alice", "current_status", "vacation", "", "src2")
	_, _ = kg.AddTriple("bob", "current_status", "active", "", "src3") // bob has only one — no conflict

	got, err := kg.FindContradictions()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("contradictions=%d want 1; got=%v", len(got), got)
	}
	if got[0].Subject != "alice" || got[0].Predicate != "current_status" {
		t.Errorf("unexpected contradiction: %+v", got[0])
	}
	if len(got[0].Triples) != 2 {
		t.Errorf("triples in conflict=%d want 2", len(got[0].Triples))
	}
}

func TestFindContradictions_IgnoresInvalidatedTriples(t *testing.T) {
	defer SetFunctionalPredicates(nil)
	SetFunctionalPredicates([]string{"owns"})
	kg := kgFixture(t)
	_, _ = kg.AddTriple("alice", "owns", "car", "", "src")
	_, _ = kg.AddTriple("alice", "owns", "bike", "", "src")
	_ = kg.Invalidate("alice", "owns", "car", "2026-01-01")

	got, _ := kg.FindContradictions()
	if len(got) != 0 {
		t.Errorf("invalidated triple still flagged: %v", got)
	}
}

func TestFindContradictions_NonFunctionalSkipped(t *testing.T) {
	defer SetFunctionalPredicates(nil)
	SetFunctionalPredicates([]string{"owns"})
	kg := kgFixture(t)
	// "likes" is multi-valued — fine to have many.
	_, _ = kg.AddTriple("alice", "likes", "tea", "", "src")
	_, _ = kg.AddTriple("alice", "likes", "coffee", "", "src")
	got, _ := kg.FindContradictions()
	if len(got) != 0 {
		t.Errorf("non-functional predicate flagged: %v", got)
	}
}

func TestResolveContradictionLatestWins(t *testing.T) {
	defer SetFunctionalPredicates(nil)
	SetFunctionalPredicates([]string{"current_status"})
	kg := kgFixture(t)
	_, _ = kg.AddTriple("alice", "current_status", "active", "", "src1")
	_, _ = kg.AddTriple("alice", "current_status", "vacation", "", "src2")

	contras, _ := kg.FindContradictions()
	if len(contras) != 1 {
		t.Fatalf("setup: contradictions=%d want 1", len(contras))
	}
	if err := kg.ResolveContradictionLatestWins(contras[0]); err != nil {
		t.Fatal(err)
	}

	// After resolve, only one active triple should remain.
	again, _ := kg.FindContradictions()
	if len(again) != 0 {
		t.Errorf("post-resolve contradictions=%v want empty", again)
	}
}

func TestResolveContradiction_NoOpOnSingleTriple(t *testing.T) {
	defer SetFunctionalPredicates(nil)
	SetFunctionalPredicates([]string{"x"})
	kg := kgFixture(t)
	c := Contradiction{Subject: "s", Predicate: "x", Triples: []KGTriple{{ID: 1}}}
	if err := kg.ResolveContradictionLatestWins(c); err != nil {
		t.Errorf("single-triple resolve should be no-op: %v", err)
	}
}
