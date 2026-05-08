// BL297 (v6.22.3) — drafts store smoke tests.

package council

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *DraftsStore {
	t.Helper()
	s, err := NewDraftsStore(filepath.Join(t.TempDir(), "drafts.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestDraftsStoreLifecycle(t *testing.T) {
	s := newTestStore(t)
	d, err := s.New("op-1", "pwa")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if d.Status != DraftInProgress {
		t.Fatalf("status: %v", d.Status)
	}
	d.Name = "carmen"
	d.Role = "data-platform"
	d.Focus = "ETL bottlenecks"
	if err := s.Update(d); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := s.Get(d.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "carmen" || got.Focus != "ETL bottlenecks" {
		t.Fatalf("roundtrip drift: %+v", got)
	}
	all, err := s.List()
	if err != nil || len(all) != 1 {
		t.Fatalf("list: %v %d", err, len(all))
	}
	if err := s.Delete(d.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(d.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDraftsStoreFindActiveAndPurge(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.New("op-A", "pwa")
	b, _ := s.New("op-B", "comm")
	a.Status = DraftDrafted
	_ = s.Update(a)
	hit, err := s.FindActive("op-A", "pwa")
	if err != nil || hit == nil || hit.ID != a.ID {
		t.Fatalf("find active: %v %+v", err, hit)
	}
	miss, err := s.FindActive("op-A", "comm")
	if err != nil || miss != nil {
		t.Fatalf("expected no match: %v %+v", err, miss)
	}
	a.Status = DraftSaved
	_ = s.Update(a)
	if hit, _ := s.FindActive("op-A", "pwa"); hit != nil {
		t.Fatalf("saved drafts should not be active: %+v", hit)
	}
	n, err := s.Purge()
	if err != nil || n != 2 {
		t.Fatalf("purge: %v %d", err, n)
	}
	_ = b
}

func TestDraftsStoreGCRetention(t *testing.T) {
	s := newTestStore(t)
	d, _ := s.New("op", "")
	// retentionDays <= 0 → no-op.
	n, err := s.GC(0)
	if err != nil || n != 0 {
		t.Fatalf("gc(0): %v %d", err, n)
	}
	// Force the row's updated_at to ~30 days ago, then GC@7 should
	// delete it.
	if _, err := s.db.Exec(`UPDATE drafts SET updated_at = ? WHERE id = ?`,
		d.UpdatedAt.Unix()-30*24*3600, d.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	n, err = s.GC(7)
	if err != nil || n != 1 {
		t.Fatalf("gc(7): want 1, got %v %d", err, n)
	}
}

func TestExtractPersonaYAMLAndTags(t *testing.T) {
	in := "```yaml\nname: foo\nrole: bar\nsystem_prompt: |\n  hi\ntags: [a, b, c]\n```\n"
	yaml, tags := ExtractPersonaYAMLAndTags(in)
	if yaml == "" {
		t.Fatalf("yaml empty")
	}
	if tags != "a,b,c" {
		t.Fatalf("tags: %q", tags)
	}
	p, err := ParsePersonaYAML(yaml)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Name != "foo" || p.Role != "bar" {
		t.Fatalf("parsed: %+v", p)
	}
}
