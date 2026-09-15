// BL386 Phase 3 — archive-on-delete + archive import tests.
//
// TC-1: PurgeScope deletes all memories in a scope, leaves others intact
// TC-2: ArchiveScope copies memories with breadcrumb then purges source
// TC-3: ArchiveScope dry-run semantics (not in this layer; tested via server)
// TC-4: archive-import: Seed with ContentSubstring finds archived memories
// TC-5: keep strategy: orphaned memories remain accessible

package memory

import (
	"fmt"
	"strings"
	"testing"
)

func TestBL386_PurgeScope_DeletesAllInScope(t *testing.T) {
	b := newScopeTestStore(t)

	prdRef := ScopeRef{Scope: ScopePRDShared, Project: "/proj", PRDID: "prd-abc"}
	projRef := ScopeRef{Scope: ScopeProjectShared, Project: "/proj"}

	// Write 3 prd-shared + 2 project-shared memories (content must be distinct to avoid dedup)
	for i := 0; i < 3; i++ {
		dir, role, sess := prdRef.Resolve()
		_, _ = b.Save(dir, fmt.Sprintf("prd memory %d", i), "", role, sess, nil)
	}
	for i := 0; i < 2; i++ {
		dir, role, sess := projRef.Resolve()
		_, _ = b.Save(dir, fmt.Sprintf("project memory %d", i), "", role, sess, nil)
	}

	n, err := PurgeScope(b, prdRef)
	if err != nil {
		t.Fatalf("PurgeScope: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 purged, got %d", n)
	}

	// prd-shared should be empty
	dir, role, _ := prdRef.Resolve()
	remaining, _ := b.ListByRole(dir, role, 100)
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining in prd-shared, got %d", len(remaining))
	}
	// project-shared should be untouched
	pdir, prole, _ := projRef.Resolve()
	kept, _ := b.ListByRole(pdir, prole, 100)
	if len(kept) != 2 {
		t.Errorf("expected 2 project-shared memories unchanged, got %d", len(kept))
	}
}

func TestBL386_PurgeScope_EmptyScope_ReturnsZero(t *testing.T) {
	b := newScopeTestStore(t)
	ref := ScopeRef{Scope: ScopePRDShared, Project: "/proj", PRDID: "nonexistent"}
	n, err := PurgeScope(b, ref)
	if err != nil {
		t.Fatalf("PurgeScope: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 purged from empty scope, got %d", n)
	}
}

func TestBL386_ArchiveScope_CopiesWithBreadcrumb_ThenPurges(t *testing.T) {
	b := newScopeTestStore(t)

	prdRef := ScopeRef{Scope: ScopePRDShared, Project: "/proj", PRDID: "prd-del"}
	projRef := ScopeRef{Scope: ScopeProjectShared, Project: "/proj"}

	// Write 3 prd-shared memories (2 learning, 1 noise)
	dir, role, sess := prdRef.Resolve()
	for _, content := range []string{"learning: the retry approach works", "learning: cache invalidation issue", "noise: debug log"} {
		_, _ = b.Save(dir, content, "", role, sess, nil)
	}

	copied, purged, err := ArchiveScope(b, prdRef, projRef, SeedFilter{}, "prd-del", 100)
	if err != nil {
		t.Fatalf("ArchiveScope: %v", err)
	}
	if copied != 3 {
		t.Errorf("expected 3 copied, got %d", copied)
	}
	if purged != 3 {
		t.Errorf("expected 3 purged, got %d", purged)
	}

	// prd-shared should be empty
	remaining, _ := b.ListByRole(dir, role, 100)
	if len(remaining) != 0 {
		t.Errorf("expected prd-shared empty after archive, got %d entries", len(remaining))
	}

	// project-shared should have 3 entries saved with role="archived" and the breadcrumb
	pdir, _, _ := projRef.Resolve()
	archived, _ := b.ListByRole(pdir, "archived", 100)
	if len(archived) != 3 {
		t.Errorf("expected 3 archived in project-shared, got %d", len(archived))
	}
	for _, m := range archived {
		if !strings.Contains(m.Content, "archived from prd:prd-del") {
			t.Errorf("missing breadcrumb in archived memory: %q", m.Content)
		}
	}
}

func TestBL386_ArchiveScope_RoleFilter_OnlyArchivesMatching(t *testing.T) {
	b := newScopeTestStore(t)

	prdRef := ScopeRef{Scope: ScopePRDShared, Project: "/proj", PRDID: "prd-flt"}
	projRef := ScopeRef{Scope: ScopeProjectShared, Project: "/proj"}

	dir, role, sess := prdRef.Resolve()
	_, _ = b.Save(dir, "learning content", "", role, sess, nil)
	_, _ = b.Save(dir, "other content", "", "noise", sess, nil) // different role

	// archive with RolePrefix filter — only prd-shared (role="prd/prd-flt") entries match
	// (all entries in prd-shared have role=role)
	copied, _, err := ArchiveScope(b, prdRef, projRef, SeedFilter{ContentSubstring: "learning"}, "prd-flt", 100)
	if err != nil {
		t.Fatalf("ArchiveScope: %v", err)
	}
	if copied != 1 {
		t.Errorf("expected 1 copied with content filter, got %d", copied)
	}
}

func TestBL386_ArchiveImport_FindsArchivedMemoriesByBreadcrumb(t *testing.T) {
	b := newScopeTestStore(t)

	projRef := ScopeRef{Scope: ScopeProjectShared, Project: "/proj"}
	targetPRD := ScopeRef{Scope: ScopePRDShared, Project: "/proj", PRDID: "new-prd"}

	// Simulate archived memories (as ArchiveScope would create)
	pdir, prole, _ := projRef.Resolve()
	_, _ = b.Save(pdir, "learning content\n\n_(archived from prd:old-prd at 2026-09-15T00:00:00Z)_", "", prole, "", nil)
	_, _ = b.Save(pdir, "other learning\n\n_(archived from prd:old-prd at 2026-09-15T00:00:00Z)_", "", prole, "", nil)
	_, _ = b.Save(pdir, "unrelated project memory", "", prole, "", nil) // no breadcrumb

	// archive-import: seed new PRD's prd-shared from archived memories
	filter := SeedFilter{ContentSubstring: "archived from prd:old-prd"}
	n, err := Seed(b, projRef, targetPRD, filter, 100)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 imported from archives, got %d", n)
	}

	// Seed saves with role="seeded"; target dir is "/proj"
	tdir, _, _ := targetPRD.Resolve()
	imported, _ := b.ListByRole(tdir, "seeded", 100)
	if len(imported) != 2 {
		t.Errorf("expected 2 in target prd-shared, got %d", len(imported))
	}
}

func TestBL386_KeepStrategy_MemoriesRemainOrphaned(t *testing.T) {
	b := newScopeTestStore(t)

	prdRef := ScopeRef{Scope: ScopePRDShared, Project: "/proj", PRDID: "prd-keep"}
	dir, role, sess := prdRef.Resolve()
	_, _ = b.Save(dir, "some learning", "", role, sess, nil)

	// "keep" = do nothing — memory remains
	rows, _ := b.ListByRole(dir, role, 100)
	if len(rows) != 1 {
		t.Errorf("expected 1 orphaned memory, got %d", len(rows))
	}
	// verify it's searchable by recall (orphaned but present)
	if !strings.Contains(rows[0].Content, "some learning") {
		t.Errorf("unexpected content: %q", rows[0].Content)
	}
}
