package memory

import (
	"bytes"
	"context"
	"testing"
)

// ── BL63: Deduplication ──────────────────────────────────────────────────────

func TestDeduplication(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	id1, err := s.Save("/proj", "unique content", "", "manual", "", nil)
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}

	// Same content should return same ID (dedup)
	id2, err := s.Save("/proj", "unique content", "", "manual", "", nil)
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if id2 != id1 {
		t.Errorf("dedup failed: got id=%d, want %d", id2, id1)
	}

	// Different content should get new ID
	id3, err := s.Save("/proj", "different content", "", "manual", "", nil)
	if err != nil {
		t.Fatalf("Save 3: %v", err)
	}
	if id3 == id1 {
		t.Error("different content should get different ID")
	}

	// Count should be 2, not 3
	count, _ := s.CountAll()
	if count != 2 {
		t.Errorf("expected 2 memories after dedup, got %d", count)
	}
}

func TestFindDuplicate(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.Save("/proj", "test content", "", "manual", "", nil)

	// Case-insensitive dedup via normalization
	id := s.FindDuplicate("/proj", "  Test Content  ")
	if id == 0 {
		t.Error("FindDuplicate should match normalized content")
	}

	// Different project = no match
	id2 := s.FindDuplicate("/other", "test content")
	if id2 != 0 {
		t.Error("FindDuplicate should not match across projects")
	}
}

// ── BL62: Write-Ahead Log ────────────────────────────────────────────────────

func TestWAL_LogsOnSave(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.Save("/proj", "wal test", "", "manual", "", nil)

	entries := s.WALRecent(10)
	if len(entries) != 1 {
		t.Fatalf("expected 1 WAL entry, got %d", len(entries))
	}
	if entries[0].Operation != "save" {
		t.Errorf("WAL op = %q, want 'save'", entries[0].Operation)
	}
}

func TestWAL_LogsOnDelete(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	id, _ := s.Save("/proj", "to delete", "", "manual", "", nil)
	s.Delete(id)

	entries := s.WALRecent(10)
	if len(entries) != 2 {
		t.Fatalf("expected 2 WAL entries (save + delete), got %d", len(entries))
	}
	if entries[1].Operation != "delete" {
		t.Errorf("second WAL op = %q, want 'delete'", entries[1].Operation)
	}
}

// ── BL50: Embedding Cache ────────────────────────────────────────────────────

type mockEmbedder struct {
	callCount int
}

func (m *mockEmbedder) Name() string    { return "mock" }
func (m *mockEmbedder) Dimensions() int { return 3 }
func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	m.callCount++
	return []float32{1.0, 0.0, 0.0}, nil
}

func TestCachedEmbedder_HitsMiss(t *testing.T) {
	inner := &mockEmbedder{}
	cached := NewCachedEmbedder(inner, 100)

	// First call = miss
	cached.Embed(context.Background(), "hello")
	if inner.callCount != 1 {
		t.Errorf("expected 1 inner call, got %d", inner.callCount)
	}

	// Second call = hit (no inner call)
	cached.Embed(context.Background(), "hello")
	if inner.callCount != 1 {
		t.Errorf("expected still 1 inner call (cache hit), got %d", inner.callCount)
	}

	// Different text = miss
	cached.Embed(context.Background(), "world")
	if inner.callCount != 2 {
		t.Errorf("expected 2 inner calls, got %d", inner.callCount)
	}

	if cached.CacheSize() != 2 {
		t.Errorf("cache size = %d, want 2", cached.CacheSize())
	}

	rate := cached.CacheHitRate()
	// 3 calls: 2 misses + 1 hit = 33.3%
	if rate < 33 || rate > 34 {
		t.Errorf("cache hit rate = %.1f%%, want ~33.3%%", rate)
	}
}

func TestCachedEmbedder_Eviction(t *testing.T) {
	inner := &mockEmbedder{}
	cached := NewCachedEmbedder(inner, 2) // max 2 entries

	cached.Embed(context.Background(), "a")
	cached.Embed(context.Background(), "b")
	cached.Embed(context.Background(), "c") // evicts "a"

	if cached.CacheSize() != 2 {
		t.Errorf("cache size = %d, want 2 after eviction", cached.CacheSize())
	}

	// "a" should be evicted — this should be a miss
	inner.callCount = 0
	cached.Embed(context.Background(), "a")
	if inner.callCount != 1 {
		t.Error("expected cache miss for evicted entry 'a'")
	}
}

// ── BL46: Export/Import ──────────────────────────────────────────────────────

func TestExportImport_Roundtrip(t *testing.T) {
	s1, _ := tempDB(t)
	defer s1.Close()

	s1.Save("/proj", "memory one", "sum1", "manual", "", nil)
	s1.Save("/proj", "memory two", "", "session", "s1", nil)

	var buf bytes.Buffer
	if err := s1.Export(&buf); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Import into a fresh store
	s2, _ := tempDB(t)
	defer s2.Close()

	n, err := s2.Import(&buf)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if n != 2 {
		t.Errorf("imported %d, want 2", n)
	}

	count, _ := s2.CountAll()
	if count != 2 {
		t.Errorf("count after import = %d, want 2", count)
	}
}

func TestImport_SkipsDuplicates(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.Save("/proj", "existing", "", "manual", "", nil)

	// Import with a duplicate
	data := `[{"id":1,"session_id":"","project_dir":"/proj","content":"existing","summary":"","role":"manual","created_at":"2026-01-01T00:00:00Z"},{"id":2,"session_id":"","project_dir":"/proj","content":"new one","summary":"","role":"manual","created_at":"2026-01-01T00:00:00Z"}]`
	n, err := s.Import(bytes.NewReader([]byte(data)))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if n != 1 {
		t.Errorf("imported %d, want 1 (duplicate skipped)", n)
	}
}

// ── BL48: ListFiltered ───────────────────────────────────────────────────────

func TestListFiltered(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.Save("/proj", "manual note", "", "manual", "", nil)
	s.Save("/proj", "session sum", "", "session", "s1", nil)
	s.Save("/proj", "a learning", "", "learning", "s1", nil)
	s.Save("/other", "other proj", "", "manual", "", nil)

	// Filter by role
	results, err := s.ListFiltered("/proj", "manual", "", 10)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("filter by role=manual: got %d, want 1", len(results))
	}

	// Filter by project
	results, err = s.ListFiltered("/other", "", "", 10)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("filter by project=/other: got %d, want 1", len(results))
	}

	// No filters = all
	results, err = s.ListFiltered("", "", "", 10)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	if len(results) != 4 {
		t.Errorf("no filters: got %d, want 4", len(results))
	}
}

func TestDistinctProjects(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.Save("/proj-a", "a", "", "manual", "", nil)
	s.Save("/proj-b", "b", "", "manual", "", nil)
	s.Save("/proj-a", "c", "", "manual", "", nil)

	projects, err := s.DistinctProjects()
	if err != nil {
		t.Fatalf("DistinctProjects: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("expected 2 distinct projects, got %d", len(projects))
	}
}
