// BL97 — agent diary helper tests.

package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func diaryStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAgentWing(t *testing.T) {
	if AgentWing("") != "" {
		t.Errorf("empty agent ID should return empty wing")
	}
	if got := AgentWing("abcd"); got != "agent-abcd" {
		t.Errorf("AgentWing(abcd)=%q want agent-abcd", got)
	}
}

func TestAppendDiary_RequiresAgentID(t *testing.T) {
	s := diaryStore(t)
	if _, err := AppendDiary(s, "", "/p", "decisions", "events", "x"); err == nil {
		t.Error("expected error for empty agent_id")
	}
}

func TestAppendDiary_RequiresContent(t *testing.T) {
	s := diaryStore(t)
	if _, err := AppendDiary(s, "ag1", "/p", "", "", "  "); err == nil {
		t.Error("expected error for empty content")
	}
}

func TestAppendDiary_DefaultsHallToEvents(t *testing.T) {
	s := diaryStore(t)
	id, err := AppendDiary(s, "ag1", "/p", "decisions", "", "chose option B because A was slower")
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Errorf("id=%d want >0", id)
	}
	entries, err := ListDiary(s, "ag1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%d want 1", len(entries))
	}
	if entries[0].Hall != "events" {
		t.Errorf("hall=%q want events (default)", entries[0].Hall)
	}
}

func TestListDiary_FiltersByAgentWing(t *testing.T) {
	s := diaryStore(t)
	_, _ = AppendDiary(s, "ag1", "/p", "decisions", "events", "ag1 chose option A")
	_, _ = AppendDiary(s, "ag2", "/p", "decisions", "events", "ag2 chose option B")
	_, _ = AppendDiary(s, "ag1", "/p", "edits", "facts", "ag1 modified main.go")

	got, err := ListDiary(s, "ag1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("ag1 entries=%d want 2", len(got))
	}
	for _, e := range got {
		if e.AgentID != "ag1" {
			t.Errorf("entry leaked from another agent: %+v", e)
		}
		if !strings.HasPrefix(e.Content, "ag1") {
			t.Errorf("ag2 content leaked: %q", e.Content)
		}
	}
}

func TestListDiary_ChronologicalOrder(t *testing.T) {
	s := diaryStore(t)
	_, _ = AppendDiary(s, "ag1", "/p", "r", "events", "first")
	_, _ = AppendDiary(s, "ag1", "/p", "r", "events", "second")
	_, _ = AppendDiary(s, "ag1", "/p", "r", "events", "third")

	got, err := ListDiary(s, "ag1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("entries=%d want 3", len(got))
	}
	if got[0].Content != "first" || got[2].Content != "third" {
		t.Errorf("not in chronological order: %+v", got)
	}
}

func TestListDiary_RequiresAgentID(t *testing.T) {
	s := diaryStore(t)
	if _, err := ListDiary(s, "", 0); err == nil {
		t.Error("expected error for empty agent_id")
	}
}

func TestClampDiaryLimit(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 200},
		{-5, 200},
		{50, 50},
		{10000, 5000},
	}
	for _, c := range cases {
		if got := clampDiaryLimit(c.in); got != c.want {
			t.Errorf("clampDiaryLimit(%d)=%d want %d", c.in, got, c.want)
		}
	}
}
