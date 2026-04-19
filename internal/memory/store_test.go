package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempDB(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s, path
}

func TestNewStore(t *testing.T) {
	s, path := tempDB(t)
	defer s.Close()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("DB file not created: %v", err)
	}
}

func TestStore_SaveAndListRecent(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	id1, err := s.Save("/proj", "first memory", "summary1", "manual", "", nil)
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	if id1 <= 0 {
		t.Error("expected positive ID")
	}

	id2, err := s.Save("/proj", "second memory", "", "session", "sess-1", nil)
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	memories, err := s.ListRecent("/proj", 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(memories) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(memories))
	}
	// Both IDs should be present
	ids := map[int64]bool{memories[0].ID: true, memories[1].ID: true}
	if !ids[id1] || !ids[id2] {
		t.Errorf("expected IDs %d and %d in results", id1, id2)
	}
}

func TestStore_ListByRole(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.Save("/proj", "manual note", "", "manual", "", nil)
	s.Save("/proj", "session summary", "", "session", "s1", nil)
	s.Save("/proj", "a learning", "", "learning", "s1", nil)
	s.Save("/proj", "another learning", "", "learning", "s2", nil)

	learnings, err := s.ListByRole("/proj", "learning", 10)
	if err != nil {
		t.Fatalf("ListByRole: %v", err)
	}
	if len(learnings) != 2 {
		t.Errorf("expected 2 learnings, got %d", len(learnings))
	}
}

func TestStore_Delete(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	id, _ := s.Save("/proj", "to delete", "", "manual", "", nil)
	if err := s.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	memories, _ := s.ListRecent("/proj", 10)
	if len(memories) != 0 {
		t.Errorf("expected 0 after delete, got %d", len(memories))
	}
}

func TestStore_Count(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.Save("/proj", "one", "", "manual", "", nil)
	s.Save("/proj", "two", "", "manual", "", nil)
	s.Save("/other", "three", "", "manual", "", nil)

	count, err := s.Count("/proj")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Errorf("Count(/proj) = %d, want 2", count)
	}

	total, err := s.CountAll()
	if err != nil {
		t.Fatalf("CountAll: %v", err)
	}
	if total != 3 {
		t.Errorf("CountAll = %d, want 3", total)
	}
}

func TestStore_SearchWithEmbeddings(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	// Create some memories with embeddings
	vec1 := []float32{1.0, 0.0, 0.0}
	vec2 := []float32{0.0, 1.0, 0.0}
	vec3 := []float32{0.9, 0.1, 0.0} // similar to vec1

	s.Save("/proj", "about dogs", "", "manual", "", vec1)
	s.Save("/proj", "about cats", "", "manual", "", vec2)
	s.Save("/proj", "about puppies", "", "manual", "", vec3)

	// Search with query similar to vec1
	query := []float32{1.0, 0.0, 0.0}
	results, err := s.Search("/proj", query, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// "about dogs" should be most similar (exact match)
	if results[0].Content != "about dogs" {
		t.Errorf("top result = %q, want 'about dogs'", results[0].Content)
	}
	if results[0].Similarity < 0.99 {
		t.Errorf("top similarity = %f, want ~1.0", results[0].Similarity)
	}
	// "about puppies" should be second (0.9 similarity)
	if results[1].Content != "about puppies" {
		t.Errorf("second result = %q, want 'about puppies'", results[1].Content)
	}
}

func TestStore_Prune(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.Save("/proj", "old", "", "manual", "", nil)
	// Manually backdate
	s.db.Exec("UPDATE memories SET created_at = ? WHERE content = 'old'",
		time.Now().Add(-48*time.Hour))

	s.Save("/proj", "new", "", "manual", "", nil)

	pruned, err := s.Prune(24 * time.Hour)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned %d, want 1", pruned)
	}

	remaining, _ := s.ListRecent("/proj", 10)
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(remaining))
	}
	if remaining[0].Content != "new" {
		t.Errorf("remaining = %q, want 'new'", remaining[0].Content)
	}
}

func TestEncodeDecodeVector(t *testing.T) {
	original := []float32{1.5, -2.3, 0.0, 3.14159}
	encoded := encodeVector(original)
	decoded := decodeVector(encoded)

	if len(decoded) != len(original) {
		t.Fatalf("decoded length %d != original %d", len(decoded), len(original))
	}
	for i, v := range decoded {
		if v != original[i] {
			t.Errorf("decoded[%d] = %f, want %f", i, v, original[i])
		}
	}
}

// ── F10 S6.1 — namespace enforcement ─────────────────────────────────

// SaveWithNamespace tags a row with the supplied namespace; default
// applies when empty. SearchInNamespaces respects the filter.
func TestStore_NamespaceIsolation(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	emb := []float32{1, 0, 0}

	if _, err := s.SaveWithNamespace("ns-a", "/proj", "alice doc", "", "manual", "", "", "", "", emb); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveWithNamespace("ns-b", "/proj", "bob doc", "", "manual", "", "", "", "", emb); err != nil {
		t.Fatal(err)
	}
	// Empty namespace falls back to DefaultNamespace.
	if _, err := s.SaveWithNamespace("", "/proj", "global doc", "", "manual", "", "", "", "", emb); err != nil {
		t.Fatal(err)
	}

	// Single namespace returns only its rows.
	res, err := s.SearchInNamespaces([]string{"ns-a"}, emb, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Content != "alice doc" {
		t.Errorf("ns-a search: got %v", res)
	}
	if res[0].Namespace != "ns-a" {
		t.Errorf("Namespace field not surfaced: %+v", res[0])
	}

	// Multi-namespace returns the union (Sprint 6 shared-mode pattern).
	res, err = s.SearchInNamespaces([]string{"ns-a", "ns-b"}, emb, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Errorf("ns-a+ns-b union: got %d want 2", len(res))
	}

	// Empty namespaces falls back to DefaultNamespace (NOT a wildcard).
	res, err = s.SearchInNamespaces(nil, emb, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Content != "global doc" {
		t.Errorf("nil namespaces should default to __global__; got %v", res)
	}
}

// SaveWithMeta (legacy) tags rows with DefaultNamespace so existing
// callers don't lose visibility after the migration.
func TestStore_LegacySaveDefaultsToGlobal(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()
	emb := []float32{1, 0, 0}

	if _, err := s.SaveWithMeta("/proj", "legacy doc", "", "manual", "", "", "", "", emb); err != nil {
		t.Fatal(err)
	}
	res, err := s.SearchInNamespaces([]string{DefaultNamespace}, emb, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Namespace != DefaultNamespace {
		t.Errorf("legacy Save should land in __global__; got %v", res)
	}
}
