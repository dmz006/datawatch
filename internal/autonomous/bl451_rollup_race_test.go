package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newBL451Store(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s, dir
}

func TestBL451RevSavePRDBumpsMonotonically(t *testing.T) {
	s, _ := newBL451Store(t)
	p, err := s.CreatePRD("spec", "/tmp/x", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prev := p.Rev
	for i := 0; i < 3; i++ {
		if err := s.SavePRD(p); err != nil {
			t.Fatal(err)
		}
		if p.Rev != prev+1 {
			t.Fatalf("rev = %d, want %d", p.Rev, prev+1)
		}
		prev = p.Rev
	}
	if r, ok := s.PRDRev(p.ID); !ok || r != prev {
		t.Fatalf("PRDRev = %d,%v want %d", r, ok, prev)
	}
}

func TestBL451RevCommitSucceedsAtCurrentRev(t *testing.T) {
	s, _ := newBL451Store(t)
	p, _ := s.CreatePRD("spec", "/tmp/x", "", "", "")
	rev := p.Rev
	err := s.CommitPRDIfRev(p.ID, rev, func(p *PRD) bool { p.Status = PRDCompleted; return true })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetPRD(p.ID)
	if got.Status != PRDCompleted || got.Rev != rev+1 {
		t.Fatalf("status=%s rev=%d", got.Status, got.Rev)
	}
}

func TestBL451RevCommitStaleWhenRevMoved(t *testing.T) {
	s, _ := newBL451Store(t)
	p, _ := s.CreatePRD("spec", "/tmp/x", "", "", "")
	rev := p.Rev
	before := p.Status
	_ = s.SavePRD(p)
	called := false
	err := s.CommitPRDIfRev(p.ID, rev, func(p *PRD) bool { called = true; p.Status = PRDCompleted; return true })
	if !errors.Is(err, ErrPRDStale) {
		t.Fatalf("err = %v, want ErrPRDStale", err)
	}
	if called {
		t.Fatal("mutate called on stale rev")
	}
	if got, _ := s.GetPRD(p.ID); got.Status != before {
		t.Fatalf("status changed to %s", got.Status)
	}
}

func TestBL451RevCommitMutateFalseDoesNotPersist(t *testing.T) {
	s, dir := newBL451Store(t)
	p, _ := s.CreatePRD("spec", "/tmp/x", "", "", "")
	rev := p.Rev
	path := filepath.Join(dir, "autonomous", "prds.jsonl")
	before, _ := os.ReadFile(path)
	err := s.CommitPRDIfRev(p.ID, rev, func(p *PRD) bool { return false })
	if !errors.Is(err, ErrPRDStale) {
		t.Fatalf("err = %v, want ErrPRDStale", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("store persisted despite mutate=false")
	}
	if r, _ := s.PRDRev(p.ID); r != rev {
		t.Fatalf("rev moved to %d", r)
	}
}

func TestBL451RevCommitMissingPRD(t *testing.T) {
	s, _ := newBL451Store(t)
	if err := s.CommitPRDIfRev("nope", 0, func(*PRD) bool { return true }); err == nil {
		t.Fatal("expected error for missing PRD")
	}
}

func TestBL451RevPersistsAcrossReload(t *testing.T) {
	s, dir := newBL451Store(t)
	p, _ := s.CreatePRD("spec", "/tmp/x", "", "", "")
	_ = s.SavePRD(p)
	want := p.Rev
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := s2.PRDRev(p.ID); !ok || r != want {
		t.Fatalf("reloaded rev = %d,%v want %d", r, ok, want)
	}
}

func TestBL451RevLegacyJSONLoadsAsZero(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "autonomous")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"id":"legacy1","spec":"old","status":"draft","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "prds.jsonl"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := s.PRDRev("legacy1"); !ok || r != 0 {
		t.Fatalf("legacy rev = %d,%v want 0,true", r, ok)
	}
}
