package memory

import (
	"os"
	"path/filepath"
	"testing"
)

// ── BL55: Spatial Organization ───────────────────────────────────────────────

func TestSaveWithMeta_AutoDeriveWing(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	id, err := s.SaveWithMeta("/home/user/projects/myapp", "test content", "", "manual", "", "", "", "", nil)
	if err != nil {
		t.Fatalf("SaveWithMeta: %v", err)
	}

	memories, _ := s.ListRecent("/home/user/projects/myapp", 1)
	if len(memories) == 0 {
		t.Fatal("no memories found")
	}
	if memories[0].Wing != "myapp" {
		t.Errorf("wing = %q, want 'myapp' (auto-derived from path)", memories[0].Wing)
	}
	if memories[0].Hall != "facts" {
		t.Errorf("hall = %q, want 'facts' (auto-derived from role=manual)", memories[0].Hall)
	}
	_ = id
}

func TestSearchFiltered(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	vec1 := []float32{1.0, 0.0, 0.0}
	vec2 := []float32{0.9, 0.1, 0.0}

	s.SaveWithMeta("/proj", "auth login", "", "manual", "", "myapp", "auth", "facts", vec1)
	s.SaveWithMeta("/proj", "deploy pipeline", "", "manual", "", "myapp", "deploy", "facts", vec2)

	// Filtered search by wing+room
	query := []float32{1.0, 0.0, 0.0}
	results, err := s.SearchFiltered("myapp", "auth", query, 5)
	if err != nil {
		t.Fatalf("SearchFiltered: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (filtered to auth room), got %d", len(results))
	}
	if len(results) > 0 && results[0].Room != "auth" {
		t.Errorf("result room = %q, want 'auth'", results[0].Room)
	}

	// Unfiltered search
	all, _ := s.SearchFiltered("", "", query, 5)
	if len(all) != 2 {
		t.Errorf("unfiltered: expected 2 results, got %d", len(all))
	}
}

func TestListWings(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.SaveWithMeta("/proj", "a", "", "manual", "", "proj-a", "", "", nil)
	s.SaveWithMeta("/proj", "b", "", "manual", "", "proj-a", "", "", nil)
	s.SaveWithMeta("/proj", "c", "", "manual", "", "proj-b", "", "", nil)

	wings, err := s.ListWings()
	if err != nil {
		t.Fatalf("ListWings: %v", err)
	}
	if wings["proj-a"] != 2 {
		t.Errorf("proj-a count = %d, want 2", wings["proj-a"])
	}
	if wings["proj-b"] != 1 {
		t.Errorf("proj-b count = %d, want 1", wings["proj-b"])
	}
}

func TestListRooms(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.SaveWithMeta("/proj", "a", "", "manual", "", "myapp", "auth", "", nil)
	s.SaveWithMeta("/proj", "b", "", "manual", "", "myapp", "deploy", "", nil)
	s.SaveWithMeta("/proj", "c", "", "manual", "", "myapp", "auth", "", nil)

	rooms, err := s.ListRooms("myapp")
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(rooms) != 2 {
		t.Errorf("expected 2 distinct rooms, got %d", len(rooms))
	}
}

// ── BL56: 4-Layer Wake-Up Stack ──────────────────────────────────────────────

func TestLayers_L0(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "identity.txt"), []byte("I am a test assistant."), 0600)

	layers := NewLayers(dir, nil)
	l0 := layers.L0()
	if l0 != "I am a test assistant." {
		t.Errorf("L0 = %q, want identity text", l0)
	}
}

func TestLayers_L0_Missing(t *testing.T) {
	layers := NewLayers(t.TempDir(), nil)
	l0 := layers.L0()
	if l0 != "" {
		t.Errorf("L0 with no identity.txt = %q, want empty", l0)
	}
}

func TestLayers_L1(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.Save("/proj", "important learning", "", "learning", "", nil)
	s.Save("/proj", "manual fact", "", "manual", "", nil)

	r := NewRetriever(s, nil, 5)
	layers := NewLayers(t.TempDir(), r)
	l1 := layers.L1("/proj", 2000)
	if l1 == "" {
		t.Error("L1 should return content from learnings + manual facts")
	}
	if !contains(l1, "learning") || !contains(l1, "manual") {
		t.Errorf("L1 missing expected content: %q", l1)
	}
}

func contains(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && (s == sub || len(s) >= len(sub) && searchSubstr(s, sub))
}
func searchSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ── BL57: Knowledge Graph ────────────────────────────────────────────────────

func TestKG_AddAndQuery(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()
	kg := NewKnowledgeGraph(s)

	id, err := kg.AddTriple("Alice", "works_on", "datawatch", "2026-01-01", "manual")
	if err != nil {
		t.Fatalf("AddTriple: %v", err)
	}
	if id <= 0 {
		t.Error("expected positive ID")
	}

	triples, err := kg.QueryEntity("Alice", "")
	if err != nil {
		t.Fatalf("QueryEntity: %v", err)
	}
	if len(triples) != 1 {
		t.Fatalf("expected 1 triple, got %d", len(triples))
	}
	if triples[0].Predicate != "works_on" || triples[0].Object != "datawatch" {
		t.Errorf("unexpected triple: %+v", triples[0])
	}
}

func TestKG_Invalidate(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()
	kg := NewKnowledgeGraph(s)

	kg.AddTriple("Bob", "does", "swimming", "2025-01-01", "")
	kg.Invalidate("Bob", "does", "swimming", "2026-03-01")

	triples, _ := kg.QueryEntity("Bob", "")
	if len(triples) != 1 {
		t.Fatal("expected 1 triple")
	}
	if triples[0].ValidTo != "2026-03-01" {
		t.Errorf("valid_to = %q, want '2026-03-01'", triples[0].ValidTo)
	}
}

func TestKG_Timeline(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()
	kg := NewKnowledgeGraph(s)

	kg.AddTriple("Max", "loves", "chess", "2025-01-01", "")
	kg.AddTriple("Max", "started", "swimming", "2025-06-01", "")

	timeline, err := kg.Timeline("Max")
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(timeline) != 2 {
		t.Errorf("expected 2 timeline entries, got %d", len(timeline))
	}
}

func TestKG_Stats(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()
	kg := NewKnowledgeGraph(s)

	kg.AddTriple("A", "rel", "B", "", "")
	kg.AddTriple("C", "rel", "D", "", "")
	kg.Invalidate("A", "rel", "B", "2026-01-01")

	stats := kg.Stats()
	if stats.TripleCount != 2 {
		t.Errorf("TripleCount = %d, want 2", stats.TripleCount)
	}
	if stats.ActiveCount != 1 {
		t.Errorf("ActiveCount = %d, want 1", stats.ActiveCount)
	}
	if stats.ExpiredCount != 1 {
		t.Errorf("ExpiredCount = %d, want 1", stats.ExpiredCount)
	}
}

// ── BL60: Entity Detection ───────────────────────────────────────────────────

func TestDetectEntities(t *testing.T) {
	text := "Alice Smith works on the datawatch project using Go and Docker. Bob Jones uses PostgreSQL."
	entities := DetectEntities(text)

	names := make(map[string]bool)
	for _, e := range entities {
		names[e.Name] = true
	}

	if !names["Alice Smith"] {
		t.Error("should detect 'Alice Smith' as person")
	}
	if !names["Bob Jones"] {
		t.Error("should detect 'Bob Jones' as person")
	}
	if !names["Go"] {
		t.Error("should detect 'Go' as tool")
	}
	if !names["Docker"] {
		t.Error("should detect 'Docker' as tool")
	}
	if !names["PostgreSQL"] {
		t.Error("should detect 'PostgreSQL' as tool")
	}
}
