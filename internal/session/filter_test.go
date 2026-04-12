package session

import (
	"path/filepath"
	"testing"
)

func TestFilterStore_New(t *testing.T) {
	s, err := NewFilterStore(filepath.Join(t.TempDir(), "filters.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("expected non-nil")
	}
}

func TestFilterStore_AddAndList(t *testing.T) {
	s, _ := NewFilterStore(filepath.Join(t.TempDir(), "filters.json"))
	_, err := s.Add("error.*fatal", FilterActionAlert, "critical error")
	if err != nil {
		t.Fatal(err)
	}
	filters := s.List()
	if len(filters) != 1 {
		t.Fatalf("expected 1, got %d", len(filters))
	}
	if filters[0].Pattern != "error.*fatal" {
		t.Errorf("expected pattern, got %q", filters[0].Pattern)
	}
	if filters[0].Action != FilterActionAlert {
		t.Errorf("expected alert action, got %q", filters[0].Action)
	}
}

func TestFilterStore_Delete(t *testing.T) {
	s, _ := NewFilterStore(filepath.Join(t.TempDir(), "filters.json"))
	fp, _ := s.Add("pattern", FilterActionDetectPrompt, "")

	if err := s.Delete(fp.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 {
		t.Error("expected empty after delete")
	}
}

func TestFilterStore_Persistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filters.json")
	s1, _ := NewFilterStore(path)
	s1.Add("persist-pat", FilterActionSendInput, "yes")

	s2, _ := NewFilterStore(path)
	if len(s2.List()) != 1 {
		t.Fatalf("expected 1 persisted, got %d", len(s2.List()))
	}
}

func TestFilterActions(t *testing.T) {
	// Verify action constants exist
	_ = FilterActionSendInput
	_ = FilterActionAlert
	_ = FilterActionSchedule
	_ = FilterActionDetectPrompt
}
