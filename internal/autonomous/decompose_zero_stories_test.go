// Decompose outcome explicitness — a finished decompose must either store
// stories or fail with a recorded reason. A valid-JSON plan with zero
// stories (or stories with no tasks) must never land the PRD in
// needs_review empty and with its failure cleared.
package autonomous

import (
	"strings"
	"testing"
)

// storedStoriesFor returns the stories currently held by the store for
// prdID. Tests are in-package, so this reads the store's own map under
// its lock rather than relying on a public accessor.
func storedStoriesFor(m *Manager, prdID string) []*Story {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	var out []*Story
	for _, st := range m.store.stories {
		if st.PRDID == prdID {
			out = append(out, st)
		}
	}
	return out
}

func TestDecompose_ValidOutputStoresStories(t *testing.T) {
	fn := func(req DecomposeRequest) (string, error) {
		return `{"title":"Ship it","stories":[{"title":"A","tasks":[{"title":"T","spec":"do x"}]}]}`, nil
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "", "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", updated.Status)
	}
	stories := storedStoriesFor(m, prd.ID)
	if len(stories) != 1 {
		t.Fatalf("stored stories = %d, want 1", len(stories))
	}
	if len(stories[0].Tasks) != 1 {
		t.Fatalf("stored tasks = %d, want 1", len(stories[0].Tasks))
	}
	if updated.LastDecomposeFailure != nil {
		t.Fatalf("LastDecomposeFailure should be nil on success, got %+v", updated.LastDecomposeFailure)
	}
}

func TestDecompose_ZeroStoriesRecordsFailure(t *testing.T) {
	fn := func(req DecomposeRequest) (string, error) {
		return `{"title":"Ship it","stories":[]}`, nil
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "", "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err == nil {
		t.Fatal("Decompose: want error for zero-story output, got nil")
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
	if stored.LastDecomposeFailure.Detail == "" {
		t.Fatal("LastDecomposeFailure.Detail is empty")
	}
	if !strings.Contains(stored.LastDecomposeFailure.Detail, "stories") {
		t.Fatalf("failure detail %q does not mention stories", stored.LastDecomposeFailure.Detail)
	}
	if n := len(storedStoriesFor(m, prd.ID)); n != 0 {
		t.Fatalf("stored stories = %d, want 0", n)
	}
}
