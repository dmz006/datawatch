package session

// Placement launch facts: the LaunchRole, LaunchKind and LaunchStage fields
// on the session record, and Manager.SetLaunchFacts, which records them on
// an active or archived session.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLaunchFactsRoundTripThroughStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	sess := &Session{ID: "lf01", FullID: "h-lf01", State: StateRunning,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		LaunchRole: "reviewer", LaunchKind: "claude-code", LaunchStage: "verify"}
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewStore(path)
	if err != nil {
		t.Fatalf("reopen NewStore: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	got, ok := reopened.Get("h-lf01")
	if !ok {
		t.Fatal("session not found after reopen")
	}
	if got.LaunchRole != "reviewer" || got.LaunchKind != "claude-code" || got.LaunchStage != "verify" {
		t.Errorf("launch facts = (%q,%q,%q), want (reviewer,claude-code,verify)",
			got.LaunchRole, got.LaunchKind, got.LaunchStage)
	}
}

func TestLaunchFactsOlderRecordLoadsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	// A record written before the launch fields existed.
	legacy := `[{"id":"old1","full_id":"h-old1","task":"legacy task","state":"running",` +
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy store: %v", err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()
	got, ok := store.Get("h-old1")
	if !ok {
		t.Fatal("legacy session not loaded")
	}
	if got.LaunchRole != "" || got.LaunchKind != "" || got.LaunchStage != "" {
		t.Errorf("legacy launch facts = (%q,%q,%q), want all empty",
			got.LaunchRole, got.LaunchKind, got.LaunchStage)
	}
	// omitempty: a record with empty facts must not write the keys at all.
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"launch_role", "launch_kind", "launch_stage"} {
		if _, present := m[k]; present {
			t.Errorf("empty %s was written; want omitted", k)
		}
	}
}

func TestSetLaunchFactsActiveSession(t *testing.T) {
	m := newTestManager(t)
	sess := &Session{ID: "act1", FullID: "h-act1", State: StateRunning,
		CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := m.store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := m.SetLaunchFacts("h-act1", "worker", "pi-prompt", "build"); err != nil {
		t.Fatalf("SetLaunchFacts: %v", err)
	}
	got, ok := m.store.Get("h-act1")
	if !ok {
		t.Fatal("active session missing")
	}
	if got.LaunchRole != "worker" || got.LaunchKind != "pi-prompt" || got.LaunchStage != "build" {
		t.Errorf("active facts = (%q,%q,%q), want (worker,pi-prompt,build)",
			got.LaunchRole, got.LaunchKind, got.LaunchStage)
	}
}

func TestSetLaunchFactsArchivedSession(t *testing.T) {
	m := newTestManager(t)
	now := time.Now()
	sess := &Session{ID: "arc1", FullID: "h-arc1", State: StateComplete,
		CreatedAt: now.Add(-40 * 24 * time.Hour), UpdatedAt: now.Add(-30 * 24 * time.Hour)}
	if err := m.store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if n, err := m.store.ArchiveOnce(now); err != nil || n != 1 {
		t.Fatalf("ArchiveOnce = %d, %v; want 1, nil", n, err)
	}
	if _, ok := m.GetSession("h-arc1"); ok {
		t.Fatal("session still active; expected it archived")
	}
	if err := m.SetLaunchFacts("h-arc1", "verifier", "claude-code", "review"); err != nil {
		t.Fatalf("SetLaunchFacts on archived: %v", err)
	}
	got, ok := m.store.GetArchived("h-arc1")
	if !ok {
		t.Fatal("archived session missing")
	}
	if got.LaunchRole != "verifier" || got.LaunchKind != "claude-code" || got.LaunchStage != "review" {
		t.Errorf("archived facts = (%q,%q,%q), want (verifier,claude-code,review)",
			got.LaunchRole, got.LaunchKind, got.LaunchStage)
	}
}

func TestSetLaunchFactsUnknownSession(t *testing.T) {
	m := newTestManager(t)
	if err := m.SetLaunchFacts("nope", "a", "b", "c"); err == nil {
		t.Fatal("SetLaunchFacts on unknown id: want error, got nil")
	}
}
