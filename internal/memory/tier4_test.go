package memory

import (
	"strings"
	"testing"
	"time"
)

// ── BL47: Retention Policies ─────────────────────────────────────────────────

func TestPruneByRole(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.Save("/proj", "old session", "", "session", "", nil)
	s.Save("/proj", "old chunk", "", "output_chunk", "", nil)
	s.Save("/proj", "keep manual", "", "manual", "", nil)

	// Backdate session and chunk
	s.db.Exec("UPDATE memories SET created_at = ? WHERE role = 'session'", time.Now().Add(-100*24*time.Hour))
	s.db.Exec("UPDATE memories SET created_at = ? WHERE role = 'output_chunk'", time.Now().Add(-40*24*time.Hour))

	// Prune sessions older than 90 days
	n, err := s.PruneByRole("session", 90*24*time.Hour)
	if err != nil {
		t.Fatalf("PruneByRole: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d sessions, want 1", n)
	}

	// Prune chunks older than 30 days
	n2, _ := s.PruneByRole("output_chunk", 30*24*time.Hour)
	if n2 != 1 {
		t.Errorf("pruned %d chunks, want 1", n2)
	}

	// Manual should still exist
	count, _ := s.Count("/proj")
	if count != 1 {
		t.Errorf("expected 1 remaining (manual), got %d", count)
	}
}

func TestApplyRetention(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.Save("/proj", "s1", "", "session", "", nil)
	s.Save("/proj", "c1", "", "output_chunk", "", nil)
	s.Save("/proj", "m1", "", "manual", "", nil)
	s.Save("/proj", "l1", "", "learning", "", nil)

	// Backdate all
	s.db.Exec("UPDATE memories SET created_at = ?", time.Now().Add(-100*24*time.Hour))

	total := s.ApplyRetention(RetentionPolicy{
		SessionDays: 90, ChunkDays: 30, ManualDays: 0, LearningDays: 0,
	})
	if total != 2 {
		t.Errorf("total pruned = %d, want 2 (session + chunk)", total)
	}

	// Manual and learning should survive
	count, _ := s.CountAll()
	if count != 2 {
		t.Errorf("remaining = %d, want 2", count)
	}
}

// ── BL51: Batch Reindex ──────────────────────────────────────────────────────

func TestListForReindex(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	s.Save("/proj", "mem1", "", "manual", "", nil)
	s.Save("/proj", "mem2", "", "session", "", nil)

	items, err := s.ListForReindex()
	if err != nil {
		t.Fatalf("ListForReindex: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestUpdateEmbedding(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	id, _ := s.Save("/proj", "test", "", "manual", "", nil)
	vec := []float32{1.0, 0.0, 0.0}
	if err := s.UpdateEmbedding(id, vec); err != nil {
		t.Fatalf("UpdateEmbedding: %v", err)
	}

	// Verify search finds it
	results, err := s.Search("/proj", vec, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result after embedding update, got %d", len(results))
	}
}

// ── BL64: Cross-Project Tunnels ──────────────────────────────────────────────

func TestFindTunnels(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	// Same room "auth" in two different wings
	s.SaveWithMeta("/projA", "auth in A", "", "manual", "", "projA", "auth", "", nil)
	s.SaveWithMeta("/projB", "auth in B", "", "manual", "", "projB", "auth", "", nil)
	// Unique room
	s.SaveWithMeta("/projA", "deploy in A", "", "manual", "", "projA", "deploy", "", nil)

	tunnels, err := s.FindTunnels()
	if err != nil {
		t.Fatalf("FindTunnels: %v", err)
	}
	if len(tunnels) != 1 {
		t.Errorf("expected 1 tunnel (auth), got %d", len(tunnels))
	}
	if wings, ok := tunnels["auth"]; ok {
		if len(wings) != 2 {
			t.Errorf("auth tunnel should span 2 wings, got %d", len(wings))
		}
	} else {
		t.Error("expected 'auth' tunnel")
	}
}

// ── BL59: Conversation Mining ────────────────────────────────────────────────

func TestParseGenericJSON(t *testing.T) {
	data := `[{"role":"user","content":"hello"},{"role":"assistant","content":"hi there"}]`
	msgs, err := parseGenericJSON([]byte(data))
	if err != nil {
		t.Fatalf("parseGenericJSON: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Error("roles not parsed correctly")
	}
}

func TestParseClaudeTranscript(t *testing.T) {
	data := `{"message":{"role":"user","content":"what is 2+2?"}}
{"message":{"role":"assistant","content":"4"}}
`
	msgs, err := parseClaudeTranscript([]byte(data))
	if err != nil {
		t.Fatalf("parseClaudeTranscript: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2, got %d", len(msgs))
	}
}

func TestMineConversation(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()
	r := NewRetriever(s, nil, 5)

	data := `[{"role":"user","content":"write tests"},{"role":"assistant","content":"here are the tests"}]`
	count, err := r.MineConversation("/proj", strings.NewReader(data), FormatGeneric)
	if err != nil {
		t.Fatalf("MineConversation: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 exchange mined, got %d", count)
	}

	memories, _ := s.ListRecent("/proj", 5)
	if len(memories) != 1 {
		t.Errorf("expected 1 memory stored, got %d", len(memories))
	}
}
