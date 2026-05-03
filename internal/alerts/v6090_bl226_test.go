package alerts

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempStore226(t *testing.T) *Store {
	t.Helper()
	f := filepath.Join(t.TempDir(), "alerts.json")
	s, err := NewStore(f)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestAlert_SourceField(t *testing.T) {
	s := tempStore226(t)
	a := s.Add(LevelInfo, "session alert", "body", "sess-123")
	if a.Source != "" {
		t.Errorf("Add: expected empty source, got %q", a.Source)
	}
	b := s.AddSystem(LevelWarn, "system alert", "ebpf failed")
	if b.Source != "system" {
		t.Errorf("AddSystem: expected source=system, got %q", b.Source)
	}
	if b.SessionID != "" {
		t.Errorf("AddSystem: expected empty session_id, got %q", b.SessionID)
	}
}

func TestListBySource(t *testing.T) {
	s := tempStore226(t)
	s.Add(LevelInfo, "sess alert", "", "sess-1")
	s.AddSystem(LevelWarn, "sys alert 1", "")
	s.AddSystem(LevelError, "sys alert 2", "")

	sys := s.ListBySource("system")
	if len(sys) != 2 {
		t.Fatalf("ListBySource(system): want 2, got %d", len(sys))
	}
	// newest first
	if sys[0].Title != "sys alert 2" {
		t.Errorf("want newest first, got %q", sys[0].Title)
	}

	none := s.ListBySource("unknown")
	if len(none) != 0 {
		t.Errorf("ListBySource(unknown): want 0, got %d", len(none))
	}
}

func TestSetGlobal_EmitSystem(t *testing.T) {
	s := tempStore226(t)
	SetGlobal(s)
	defer func() {
		globalMu.Lock()
		globalStore = nil
		globalMu.Unlock()
	}()

	EmitSystem(LevelError, "global emit", "from test")

	sys := s.ListBySource("system")
	if len(sys) != 1 {
		t.Fatalf("want 1 system alert, got %d", len(sys))
	}
	if sys[0].Title != "global emit" {
		t.Errorf("wrong title: %q", sys[0].Title)
	}
}

func TestEmitSystem_NoopWithoutGlobal(t *testing.T) {
	// Ensure global is nil
	globalMu.Lock()
	prev := globalStore
	globalStore = nil
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalStore = prev
		globalMu.Unlock()
	}()

	// Should not panic
	EmitSystem(LevelError, "no store", "should be dropped")
}

func TestAddSystem_Persisted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.json")
	s, _ := NewStore(path)
	s.AddSystem(LevelWarn, "persisted", "body")

	// Reload
	s2, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	all := s2.List()
	if len(all) != 1 || all[0].Source != "system" {
		t.Errorf("persisted alert missing or wrong source: %v", all)
	}
}

func TestAddSystem_RespectsLimit(t *testing.T) {
	s := tempStore226(t)
	// Add more than 500 alerts
	for i := 0; i < 510; i++ {
		s.AddSystem(LevelInfo, "alert", "body")
	}
	all := s.List()
	if len(all) > 500 {
		t.Errorf("want <= 500 alerts, got %d", len(all))
	}
}

func TestAddSystem_FiresListeners(t *testing.T) {
	s := tempStore226(t)
	got := make(chan *Alert, 1)
	s.AddListener(func(a *Alert) { got <- a })

	s.AddSystem(LevelError, "fired", "listener test")

	select {
	case a := <-got:
		if a.Source != "system" {
			t.Errorf("listener got wrong source: %q", a.Source)
		}
	case <-time.After(time.Second):
		t.Error("listener not called")
	}
}

func TestAddSystem_JSONRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.json")
	s, _ := NewStore(path)
	s.AddSystem(LevelError, "ebpf fail", "CAP_BPF missing")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) == "" {
		t.Fatal("empty file")
	}
	if !contains(string(data), `"source"`) || !contains(string(data), `"system"`) {
		t.Errorf("source field not in JSON: %s", data)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsAt(s, sub))
}

func containsAt(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
