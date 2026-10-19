// Decompose outcome explicitness for depends_on — SetStories rejects a
// plan whose depends_on names an entry no story/task matches, or a plan
// whose resolved dependencies form a cycle, naming the bad entry and
// storing nothing. Both decompose paths must route that rejection through
// the retry-then-fail path with source decompose-depends-on, leaving the
// PRD in draft with a recorded failure and no stories stored.
package autonomous

import (
	"strings"
	"testing"
)

func TestDecompose_UnresolvableDependsOnRecordsFailure(t *testing.T) {
	// T2 points at "s9-t9" — a positional code out of range for this
	// one-story plan — and T3 points at a title that exists nowhere.
	// Either alone rejects; the first bad entry is named in the error.
	fn := func(req DecomposeRequest) (string, error) {
		return `{"title":"Ship it","stories":[{"title":"A","tasks":[{"title":"T1","spec":"do x"},{"title":"T2","spec":"do y","depends_on":["s9-t9"]},{"title":"T3","spec":"do z","depends_on":["Not A Real Task"]}]}]}`, nil
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "", "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err == nil {
		t.Fatal("Decompose: want error for unresolvable depends_on, got nil")
	}
	if updated != nil && updated.Status != PRDDraft {
		t.Fatalf("status = %s, want draft (retryable)", updated.Status)
	}
	stored, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing from store")
	}
	if stored.Status != PRDDraft {
		t.Fatalf("stored status = %s, want draft", stored.Status)
	}
	if stored.LastDecomposeFailure == nil {
		t.Fatal("LastDecomposeFailure = nil, want recorded failure")
	}
	if stored.LastDecomposeFailure.Source != "decompose-depends-on" {
		t.Fatalf("LastDecomposeFailure.Source = %q, want decompose-depends-on", stored.LastDecomposeFailure.Source)
	}
	if stored.LastDecomposeFailure.Detail == "" {
		t.Fatal("LastDecomposeFailure.Detail is empty")
	}
	if !strings.Contains(stored.LastDecomposeFailure.Detail, "depends_on") {
		t.Fatalf("failure detail %q does not mention depends_on", stored.LastDecomposeFailure.Detail)
	}
	if !strings.Contains(stored.LastDecomposeFailure.Detail, "s9-t9") {
		t.Fatalf("failure detail %q does not name the bad entry s9-t9", stored.LastDecomposeFailure.Detail)
	}
	if n := len(storedStoriesFor(m, prd.ID)); n != 0 {
		t.Fatalf("stored stories = %d, want 0", n)
	}
}

func TestDecompose_DependsOnCycleRecordsFailure(t *testing.T) {
	// T1 -> T2 -> T1: a two-task dependency cycle entirely within the
	// plan, referenced by sibling title.
	fn := func(req DecomposeRequest) (string, error) {
		return `{"title":"Ship it","stories":[{"title":"A","tasks":[{"title":"T1","spec":"do x","depends_on":["T2"]},{"title":"T2","spec":"do y","depends_on":["T1"]}]}]}`, nil
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "", "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err == nil {
		t.Fatal("Decompose: want error for depends_on cycle, got nil")
	}
	if updated != nil && updated.Status != PRDDraft {
		t.Fatalf("status = %s, want draft (retryable)", updated.Status)
	}
	stored, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing from store")
	}
	if stored.Status != PRDDraft {
		t.Fatalf("stored status = %s, want draft", stored.Status)
	}
	if stored.LastDecomposeFailure == nil {
		t.Fatal("LastDecomposeFailure = nil, want recorded failure")
	}
	if stored.LastDecomposeFailure.Source != "decompose-depends-on" {
		t.Fatalf("LastDecomposeFailure.Source = %q, want decompose-depends-on", stored.LastDecomposeFailure.Source)
	}
	if !strings.Contains(stored.LastDecomposeFailure.Detail, "cycle") {
		t.Fatalf("failure detail %q does not mention a cycle", stored.LastDecomposeFailure.Detail)
	}
	if n := len(storedStoriesFor(m, prd.ID)); n != 0 {
		t.Fatalf("stored stories = %d, want 0", n)
	}
}
