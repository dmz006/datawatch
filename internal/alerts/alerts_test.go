package alerts

import (
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "alerts.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewStore(t *testing.T) {
	s := tempStore(t)
	if s == nil {
		t.Fatal("NewStore returned nil")
	}
}

func TestNewStore_InvalidPath(t *testing.T) {
	_, err := NewStore("/nonexistent/dir/alerts.json")
	// Should succeed (creates on first write, not on open)
	if err != nil {
		t.Logf("NewStore with bad path: %v (expected if load fails)", err)
	}
}

func TestStore_Add(t *testing.T) {
	s := tempStore(t)
	a := s.Add(LevelInfo, "test alert", "body text", "sess-123")
	if a == nil {
		t.Fatal("Add returned nil")
	}
	if a.Level != LevelInfo {
		t.Errorf("expected level info, got %s", a.Level)
	}
	if a.Title != "test alert" {
		t.Errorf("expected title 'test alert', got %q", a.Title)
	}
	if a.Body != "body text" {
		t.Errorf("expected body 'body text', got %q", a.Body)
	}
	if a.SessionID != "sess-123" {
		t.Errorf("expected session 'sess-123', got %q", a.SessionID)
	}
	if a.Read {
		t.Error("expected unread")
	}
	if a.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestStore_List(t *testing.T) {
	s := tempStore(t)
	s.Add(LevelInfo, "first", "", "")
	s.Add(LevelWarn, "second", "", "")
	s.Add(LevelInfo, "third", "", "")

	alerts := s.List()
	if len(alerts) != 3 {
		t.Fatalf("expected 3, got %d", len(alerts))
	}
}

func TestStore_MarkRead(t *testing.T) {
	s := tempStore(t)
	a := s.Add(LevelInfo, "test", "", "")

	if err := s.MarkRead(a.ID); err != nil {
		t.Fatalf("MarkRead error: %v", err)
	}

	alerts := s.List()
	for _, al := range alerts {
		if al.ID == a.ID && !al.Read {
			t.Error("expected alert to be marked read")
		}
	}
}

func TestStore_MarkRead_NotFound(t *testing.T) {
	s := tempStore(t)
	err := s.MarkRead("nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent ID")
	}
}

func TestStore_UnreadCount(t *testing.T) {
	s := tempStore(t)
	s.Add(LevelInfo, "1", "", "")
	s.Add(LevelInfo, "2", "", "")
	if s.UnreadCount() != 2 {
		t.Fatalf("expected 2 unread, got %d", s.UnreadCount())
	}

	alerts := s.List()
	s.MarkRead(alerts[0].ID)
	if s.UnreadCount() != 1 {
		t.Fatalf("expected 1 unread after mark, got %d", s.UnreadCount())
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.json")

	// Create and add
	s1, _ := NewStore(path)
	s1.Add(LevelInfo, "persisted", "body", "s1")

	// Reload
	s2, _ := NewStore(path)
	alerts := s2.List()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 persisted alert, got %d", len(alerts))
	}
	if alerts[0].Title != "persisted" {
		t.Errorf("expected 'persisted', got %q", alerts[0].Title)
	}
}

func TestStore_Encrypted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.enc")
	key := make([]byte, 32)

	s, err := NewStoreEncrypted(path, key)
	if err != nil {
		t.Fatal(err)
	}
	s.Add(LevelInfo, "secret", "", "")

	// File should exist and not be plain JSON
	data, _ := os.ReadFile(path)
	if len(data) > 0 && data[0] == '[' {
		t.Error("encrypted store should not be plain JSON")
	}
}

func TestStore_AddListener(t *testing.T) {
	s := tempStore(t)
	var received *Alert
	s.AddListener(func(a *Alert) {
		received = a
	})
	s.Add(LevelWarn, "listened", "", "")
	if received == nil {
		t.Fatal("listener not called")
	}
	if received.Title != "listened" {
		t.Errorf("expected 'listened', got %q", received.Title)
	}
}

func TestLevels(t *testing.T) {
	if LevelInfo != "info" {
		t.Errorf("expected 'info', got %q", LevelInfo)
	}
	if LevelWarn != "warn" {
		t.Errorf("expected 'warn', got %q", LevelWarn)
	}
}
