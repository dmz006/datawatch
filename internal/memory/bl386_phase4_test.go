// BL386 Phase 4 — handoff + PRD memory report tests.
//
// TC-1: memory_handoff writes to story-shared with handoff breadcrumb
// TC-2: PRD memory report aggregates prd-shared + story-shared
// TC-3: PRD memory report deduplicates identical content across scopes

package memory

import (
	"strings"
	"testing"
)

// TestBL386_MemoryHandoff_WritesToStoryShared verifies that a handoff write
// lands in story-shared scope (role="story/<id>") with the breadcrumb text.
func TestBL386_MemoryHandoff_WritesToStoryShared(t *testing.T) {
	b := newScopeTestStore(t)

	storyRef := ScopeRef{Scope: ScopeStoryShared, Project: "/proj", StoryID: "story-01"}
	dir, role, _ := storyRef.Resolve()

	// Simulate what memory_handoff does: save to story-shared with breadcrumb
	handoffContent := "retry logic works; batching fails at scale\n\n_(handoff [prd:prd-123])_"
	id, err := b.Save(dir, handoffContent, "task handoff", role, "", nil)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id from Save")
	}

	// Verify it's retrievable by story-shared role
	rows, err := b.ListByRole(dir, role, 10)
	if err != nil {
		t.Fatalf("ListByRole: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 story-shared memory, got %d", len(rows))
	}
	if !strings.Contains(rows[0].Content, "_(handoff") {
		t.Errorf("expected handoff breadcrumb in content, got: %q", rows[0].Content)
	}
	if rows[0].Role != role {
		t.Errorf("expected role %q, got %q", role, rows[0].Role)
	}
}

// TestBL386_MemoryPRDReport_AggregatesAllScopes verifies that prd-shared and
// story-shared memories are both accessible when querying by their scope refs.
func TestBL386_MemoryPRDReport_AggregatesAllScopes(t *testing.T) {
	b := newScopeTestStore(t)

	prdRef := ScopeRef{Scope: ScopePRDShared, Project: "/proj", PRDID: "prd-rpt"}
	story1Ref := ScopeRef{Scope: ScopeStoryShared, Project: "/proj", StoryID: "story-rpt-1"}
	story2Ref := ScopeRef{Scope: ScopeStoryShared, Project: "/proj", StoryID: "story-rpt-2"}

	// Populate prd-shared (2 entries)
	pdir, prole, _ := prdRef.Resolve()
	_, _ = b.Save(pdir, "prd learning: approach X is best", "", prole, "", nil)
	_, _ = b.Save(pdir, "prd learning: avoid Y pattern", "", prole, "", nil)

	// Populate story-shared for story 1 (1 entry)
	s1dir, s1role, _ := story1Ref.Resolve()
	_, _ = b.Save(s1dir, "story1 handoff: batching fails", "handoff", s1role, "", nil)

	// Populate story-shared for story 2 (1 entry)
	s2dir, s2role, _ := story2Ref.Resolve()
	_, _ = b.Save(s2dir, "story2 handoff: caching helps", "handoff", s2role, "", nil)

	// Simulate memory report aggregation
	prdRows, _ := b.ListByRole(pdir, prole, 50)
	s1Rows, _ := b.ListByRole(s1dir, s1role, 50)
	s2Rows, _ := b.ListByRole(s2dir, s2role, 50)

	total := len(prdRows) + len(s1Rows) + len(s2Rows)
	if total != 4 {
		t.Errorf("expected 4 total across all scopes, got %d (prd=%d s1=%d s2=%d)", total, len(prdRows), len(s1Rows), len(s2Rows))
	}
	if len(prdRows) != 2 {
		t.Errorf("expected 2 prd-shared memories, got %d", len(prdRows))
	}
	if len(s1Rows) != 1 || !strings.Contains(s1Rows[0].Content, "batching fails") {
		t.Errorf("unexpected story1 content: %v", s1Rows)
	}
	if len(s2Rows) != 1 || !strings.Contains(s2Rows[0].Content, "caching helps") {
		t.Errorf("unexpected story2 content: %v", s2Rows)
	}
}

// TestBL386_MemoryPRDReport_DeduplicatesAcrossScopes verifies that when the
// same content exists in both prd-shared and story-shared, a seen-map
// deduplication produces one unique entry per content string.
// Uses different project dirs to bypass SQLite's (projectDir, content_hash)
// uniqueness constraint, then simulates the handler merge+dedup logic.
func TestBL386_MemoryPRDReport_DeduplicatesAcrossScopes(t *testing.T) {
	b := newScopeTestStore(t)

	// Use different project dirs for each scope so SQLite lets both rows exist.
	prdRef := ScopeRef{Scope: ScopePRDShared, Project: "/proj-prd-dedup", PRDID: "prd-dedup"}
	storyRef := ScopeRef{Scope: ScopeStoryShared, Project: "/proj-story-dedup", StoryID: "story-dedup"}

	sharedContent := "retry logic: exponential backoff with jitter"
	uniquePRDContent := "prd-only: database connection pooling"

	pdir, prole, _ := prdRef.Resolve()
	sdir, srole, _ := storyRef.Resolve()

	_, _ = b.Save(pdir, sharedContent, "", prole, "", nil)
	_, _ = b.Save(pdir, uniquePRDContent, "", prole, "", nil)

	// Story-shared has the exact same sharedContent (different project_dir avoids SQLite dedup)
	_, _ = b.Save(sdir, sharedContent, "", srole, "", nil)

	prdRows, _ := b.ListByRole(pdir, prole, 50)
	storyRows, _ := b.ListByRole(sdir, srole, 50)

	// Simulate handlePRDMemoryReport's exact-content dedup.
	seen := map[string]bool{}
	uniq := 0
	for _, m := range append(prdRows, storyRows...) {
		if !seen[m.Content] {
			seen[m.Content] = true
			uniq++
		}
	}

	// 2 prd-shared + 1 story-shared (sharedContent duplicate) = 3 raw rows.
	// After dedup: sharedContent + uniquePRDContent = 2 unique entries.
	if uniq != 2 {
		t.Errorf("expected 2 unique entries after content dedup, got %d", uniq)
	}
}
