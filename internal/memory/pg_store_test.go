package memory

import (
	"os"
	"testing"
)

// These tests require a running PostgreSQL with pgvector.
// Set PG_TEST_URL=postgres://datawatch:datawatch@127.0.0.1/datawatch to enable.

func pgTestURL() string {
	url := os.Getenv("PG_TEST_URL")
	if url == "" {
		url = "postgres://datawatch:datawatch@127.0.0.1/datawatch"
	}
	return url
}

func skipIfNoPG(t *testing.T) *PGStore {
	t.Helper()
	s, err := NewPGStore(pgTestURL())
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	// Clean test data
	s.pool.Exec(t.Context(), "DELETE FROM memories WHERE project_dir LIKE '/test/%'")     //nolint:errcheck
	s.pool.Exec(t.Context(), "DELETE FROM kg_triples WHERE source = 'test'")               //nolint:errcheck
	s.pool.Exec(t.Context(), "DELETE FROM kg_entities WHERE name LIKE 'Test%'")            //nolint:errcheck
	return s
}

func TestPGStore_SaveAndList(t *testing.T) {
	s := skipIfNoPG(t)
	defer s.Close()

	id, err := s.Save("/test/proj", "pg test memory", "summary", "manual", "", nil)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id <= 0 {
		t.Error("expected positive ID")
	}

	memories, err := s.ListRecent("/test/proj", 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(memories) < 1 {
		t.Fatal("expected at least 1 memory")
	}
	if memories[0].Content != "pg test memory" {
		t.Errorf("content = %q", memories[0].Content)
	}

	// Cleanup
	s.Delete(id)
}

func TestPGStore_Dedup(t *testing.T) {
	s := skipIfNoPG(t)
	defer s.Close()

	id1, _ := s.Save("/test/dedup", "same content", "", "manual", "", nil)
	id2, _ := s.Save("/test/dedup", "same content", "", "manual", "", nil)
	if id1 != id2 {
		t.Errorf("dedup failed: id1=%d, id2=%d", id1, id2)
	}
	s.Delete(id1)
}

func TestPGStore_VectorSearch(t *testing.T) {
	s := skipIfNoPG(t)
	defer s.Close()

	vec1 := []float32{1.0, 0.0, 0.0}
	vec2 := []float32{0.0, 1.0, 0.0}

	id1, _ := s.Save("/test/search", "about dogs", "", "manual", "", vec1)
	id2, _ := s.Save("/test/search", "about cats", "", "manual", "", vec2)

	query := []float32{1.0, 0.0, 0.0}
	results, err := s.Search("/test/search", query, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Content != "about dogs" {
		t.Errorf("top result = %q, want 'about dogs'", results[0].Content)
	}

	s.Delete(id1)
	s.Delete(id2)
}

func TestPGStore_SpatialSearch(t *testing.T) {
	s := skipIfNoPG(t)
	defer s.Close()

	vec := []float32{1.0, 0.0, 0.0}
	id1, _ := s.SaveWithMeta("/test/spatial", "auth login", "", "manual", "", "myapp", "auth", "facts", vec)
	id2, _ := s.SaveWithMeta("/test/spatial", "deploy pipe", "", "manual", "", "myapp", "deploy", "facts", vec)

	results, err := s.SearchFiltered("myapp", "auth", vec, 5)
	if err != nil {
		t.Fatalf("SearchFiltered: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("filtered to auth: expected 1, got %d", len(results))
	}

	s.Delete(id1)
	s.Delete(id2)
}

func TestPGStore_KG(t *testing.T) {
	s := skipIfNoPG(t)
	defer s.Close()

	id, err := s.KGAddTriple("TestAlice", "works_on", "TestProject", "2026-01-01", "test")
	if err != nil {
		t.Fatalf("KGAddTriple: %v", err)
	}
	if id <= 0 {
		t.Error("expected positive ID")
	}

	triples, err := s.KGQueryEntity("TestAlice", "")
	if err != nil {
		t.Fatalf("KGQueryEntity: %v", err)
	}
	if len(triples) < 1 {
		t.Fatal("expected at least 1 triple")
	}
	if triples[0].Predicate != "works_on" {
		t.Errorf("predicate = %q", triples[0].Predicate)
	}

	stats := s.KGStats()
	if stats.TripleCount < 1 {
		t.Error("expected at least 1 triple in stats")
	}
}

func TestPGStore_Stats(t *testing.T) {
	s := skipIfNoPG(t)
	defer s.Close()

	id, _ := s.Save("/test/stats", "stats test", "", "manual", "", nil)
	stats := s.Stats()
	if stats.TotalCount < 1 {
		t.Error("expected at least 1 in total count")
	}
	if stats.DBSizeBytes <= 0 {
		t.Error("expected positive DB size")
	}
	s.Delete(id)
}

func TestPGStore_Encryption(t *testing.T) {
	key := testKey()
	s, err := NewPGStoreEncrypted(pgTestURL(), key)
	if err != nil {
		t.Skipf("PG not available: %v", err)
	}
	defer s.Close()

	if !s.IsEncrypted() {
		t.Error("should report encrypted")
	}

	id, _ := s.Save("/test/enc", "secret pg content", "secret summary", "manual", "", nil)

	// Read should decrypt
	memories, _ := s.ListRecent("/test/enc", 1)
	if len(memories) < 1 {
		t.Fatal("expected 1 memory")
	}
	if memories[0].Content != "secret pg content" {
		t.Errorf("decrypted content = %q", memories[0].Content)
	}

	s.Delete(id)
}
