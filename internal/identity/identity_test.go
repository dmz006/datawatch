package identity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewManagerMissingFileEmpty(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(filepath.Join(dir, "identity.yaml"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if !m.Get().IsEmpty() {
		t.Fatalf("expected empty identity")
	}
	if got := m.PromptText(); got != "" {
		t.Fatalf("expected empty PromptText, got %q", got)
	}
}

func TestSetWritesAndUpdatesCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.yaml")
	m, _ := NewManager(path)
	got, err := m.Set(Identity{
		Role:           "Senior platform engineer",
		NorthStarGoals: []string{"ship reliable AI work"},
		Values:         []string{"correctness", "honesty"},
		CurrentFocus:   "datawatch v6.x stabilization",
	})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt not stamped")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not written: %v", err)
	}
	// reload from disk and verify
	m2, _ := NewManager(path)
	id := m2.Get()
	if id.Role != "Senior platform engineer" {
		t.Errorf("role: %q", id.Role)
	}
	if len(id.NorthStarGoals) != 1 || id.NorthStarGoals[0] != "ship reliable AI work" {
		t.Errorf("goals: %v", id.NorthStarGoals)
	}
	if len(id.Values) != 2 {
		t.Errorf("values: %v", id.Values)
	}
}

func TestUpdateMergesNonEmptyFields(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(filepath.Join(dir, "identity.yaml"))
	if _, err := m.Set(Identity{Role: "engineer", CurrentFocus: "v6"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := m.Update(Identity{Values: []string{"clarity"}})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Role != "engineer" {
		t.Errorf("role lost: %q", got.Role)
	}
	if got.CurrentFocus != "v6" {
		t.Errorf("focus lost: %q", got.CurrentFocus)
	}
	if len(got.Values) != 1 {
		t.Errorf("values not merged: %v", got.Values)
	}
}

func TestPromptTextNonEmpty(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(filepath.Join(dir, "identity.yaml"))
	_, _ = m.Set(Identity{
		Role:           "engineer",
		NorthStarGoals: []string{"goal A", "goal B"},
		Values:         []string{"X"},
		CurrentFocus:   "F",
	})
	pt := m.PromptText()
	for _, want := range []string{
		"Operator Identity",
		"Role: engineer",
		"goal A",
		"goal B",
		"Current Focus: F",
		"  - X",
	} {
		if !strings.Contains(pt, want) {
			t.Errorf("PromptText missing %q in:\n%s", want, pt)
		}
	}
}

func TestSetFieldKnownAndUnknown(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(filepath.Join(dir, "identity.yaml"))
	if _, err := m.SetField("role", "ops engineer"); err != nil {
		t.Fatalf("SetField role: %v", err)
	}
	if got := m.Get().Role; got != "ops engineer" {
		t.Errorf("role: %q", got)
	}
	if _, err := m.SetField("goals", "alpha, beta, gamma"); err != nil {
		t.Fatalf("SetField goals alias: %v", err)
	}
	if got := m.Get().NorthStarGoals; len(got) != 3 {
		t.Errorf("goals count: %v", got)
	}
	if _, err := m.SetField("nonsense", "x"); err == nil {
		t.Errorf("expected unknown field error")
	}
}

func TestFilePermissions0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.yaml")
	m, _ := NewManager(path)
	_, _ = m.Set(Identity{Role: "x"})
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := st.Mode().Perm(); mode != 0o600 {
		t.Errorf("perm: %o (want 0600)", mode)
	}
}
