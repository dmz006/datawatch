package memory

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

// ── F10 S6.3 — sync-back upload protocol ─────────────────────────────

// ExportSince includes only rows newer than the cutoff. Used by
// worker on session-end to package "what I learned this session".
func TestStore_ExportSince_TimeFilter(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()
	emb := []float32{1, 0, 0}

	// Old row.
	if _, err := s.SaveWithNamespace("ns", "/p", "old", "", "manual", "", "", "", "", emb); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().UTC()
	time.Sleep(1100 * time.Millisecond) // SQLite CURRENT_TIMESTAMP is second-grained
	// New rows after cutoff.
	if _, err := s.SaveWithNamespace("ns", "/p", "new1", "", "manual", "", "", "", "", emb); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveWithNamespace("ns", "/p", "new2", "", "manual", "", "", "", "", emb); err != nil {
		t.Fatal(err)
	}

	// Sanity: confirm all three rows actually persisted.
	var nrows int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&nrows)
	if nrows != 3 {
		t.Fatalf("expected 3 rows in memories, got %d", nrows)
	}

	var buf bytes.Buffer
	if err := s.ExportSince(&buf, "ns", cutoff); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	// JSON encoder pretty-prints with ": " (with space) — assert on
	// the bare value strings to be format-agnostic.
	if strings.Contains(body, `"old"`) {
		t.Errorf("ExportSince leaked pre-cutoff row:\n%s", body)
	}
	if !strings.Contains(body, `"new1"`) || !strings.Contains(body, `"new2"`) {
		t.Errorf("ExportSince missing post-cutoff rows (cutoff=%v):\n%s", cutoff, body)
	}
}

// ExportSince filters by namespace when one is supplied.
func TestStore_ExportSince_NamespaceFilter(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()
	emb := []float32{1, 0, 0}
	old := time.Now().UTC().Add(-time.Hour)

	_, _ = s.SaveWithNamespace("alice", "/p", "alice doc", "", "manual", "", "", "", "", emb)
	_, _ = s.SaveWithNamespace("bob", "/p", "bob doc", "", "manual", "", "", "", "", emb)

	var buf bytes.Buffer
	if err := s.ExportSince(&buf, "alice", old); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if !strings.Contains(body, "alice doc") || strings.Contains(body, "bob doc") {
		t.Errorf("namespace filter not applied:\n%s", body)
	}
}

// Round-trip: a sync-back batch from a "worker" Store imports
// into a "parent" Store with namespace + spatial metadata intact.
func TestStore_ExportImport_PreservesNamespaceAndSpatial(t *testing.T) {
	worker, _ := tempDB(t)
	defer worker.Close()
	parent, _ := tempDB(t)
	defer parent.Close()
	emb := []float32{1, 0, 0}
	old := time.Now().UTC().Add(-time.Hour)

	if _, err := worker.SaveWithNamespace("project-foo", "/p", "fact1", "sum", "manual",
		"sess-1", "wing-foo", "room-x", "facts", emb); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := worker.ExportSince(&buf, "project-foo", old); err != nil {
		t.Fatal(err)
	}
	imported, err := parent.Import(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if imported != 1 {
		t.Fatalf("imported=%d want 1", imported)
	}
	res, err := parent.SearchInNamespaces([]string{"project-foo"}, emb, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("parent should hold 1 row in project-foo, got %d", len(res))
	}
	got := res[0]
	if got.Wing != "wing-foo" || got.Room != "room-x" || got.Hall != "facts" {
		t.Errorf("spatial metadata lost: %+v", got)
	}
	if got.Namespace != "project-foo" {
		t.Errorf("namespace lost: %q", got.Namespace)
	}
}
