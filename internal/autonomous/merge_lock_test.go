package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestLocks(t *testing.T) (*MergeLocks, string) {
	t.Helper()
	dir := t.TempDir()
	m, err := NewMergeLocks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m, dir
}

func TestMergeLockSecondHolderRefused(t *testing.T) {
	m, _ := newTestLocks(t)
	if _, err := m.Acquire("/r", MergeLockHolder{RunID: "run1", AutomatonID: "a1", Branches: []string{"b1"}}); err != nil {
		t.Fatal(err)
	}
	_, err := m.Acquire("/r", MergeLockHolder{RunID: "run2", AutomatonID: "a2", Branches: []string{"b2"}})
	if !errors.Is(err, ErrMergeLockHeld) {
		t.Fatalf("want ErrMergeLockHeld, got %v", err)
	}
	if !strings.Contains(err.Error(), "run1") || !strings.Contains(err.Error(), "a1") {
		t.Fatalf("error should name the holder: %v", err)
	}
}

func TestMergeLockSameHolderAcquiresAgain(t *testing.T) {
	m, _ := newTestLocks(t)
	h := MergeLockHolder{RunID: "run1", AutomatonID: "a1", Branches: []string{"b1", "b2"}}
	first, err := m.Acquire("/r", h)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Acquire("/r", h)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Since.Equal(first.Since) {
		t.Fatal("re-acquire must keep the original start time")
	}
	// A resumed run has a new run id but the same automaton and branches.
	resumed := MergeLockHolder{RunID: "run1b", AutomatonID: "a1", Branches: []string{"b1", "b2"}}
	if _, err := m.Acquire("/r", resumed); err != nil {
		t.Fatal(err)
	}
	// Same automaton with different branches is refused.
	other := MergeLockHolder{RunID: "run1c", AutomatonID: "a1", Branches: []string{"x"}}
	if _, err := m.Acquire("/r", other); !errors.Is(err, ErrMergeLockHeld) {
		t.Fatalf("want refusal, got %v", err)
	}
}

func TestMergeLockRelease(t *testing.T) {
	m, _ := newTestLocks(t)
	h := MergeLockHolder{RunID: "run1", AutomatonID: "a1"}
	if _, err := m.Acquire("/r", h); err != nil {
		t.Fatal(err)
	}
	if err := m.Release("/r", "other"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Status("/r"); !ok {
		t.Fatal("release by another run must not free the lock")
	}
	if err := m.Release("/r", "run1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Status("/r"); ok {
		t.Fatal("lock should be free")
	}
	if err := m.Release("/r", "run1"); err != nil {
		t.Fatal(err)
	}
	if len(m.List()) != 0 {
		t.Fatal("second release changed state")
	}
	if _, err := m.Acquire("/r", MergeLockHolder{RunID: "run2", AutomatonID: "a2"}); err != nil {
		t.Fatal(err)
	}
}

func TestMergeLockEmptyRunCannotRelease(t *testing.T) {
	m, _ := newTestLocks(t)
	// A holder that recorded no run id still takes the lock.
	if _, err := m.Acquire("/r", MergeLockHolder{AutomatonID: "a1"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Release("/r", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Status("/r"); !ok {
		t.Fatal("an empty run id must not free a lock")
	}
}

func TestMergeLockReposIndependent(t *testing.T) {
	m, _ := newTestLocks(t)
	if _, err := m.Acquire("/r1", MergeLockHolder{RunID: "run1", AutomatonID: "a1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Acquire("/r2", MergeLockHolder{RunID: "run2", AutomatonID: "a2"}); err != nil {
		t.Fatal(err)
	}
	if got := m.List(); len(got) != 2 || got[0].Repo != "/r1" || got[1].Repo != "/r2" {
		t.Fatalf("list = %+v", got)
	}
	if err := m.Release("/r1", "run1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Status("/r2"); !ok {
		t.Fatal("/r2 should stay locked")
	}
}

func TestMergeLockSurvivesRestart(t *testing.T) {
	m, dir := newTestLocks(t)
	if _, err := m.Acquire("/r", MergeLockHolder{RunID: "run1", AutomatonID: "a1", Branches: []string{"b"}}); err != nil {
		t.Fatal(err)
	}
	m2, err := NewMergeLocks(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := m2.Status("/r")
	if !ok || st.RunID != "run1" || st.AutomatonID != "a1" || len(st.Branches) != 1 {
		t.Fatalf("status after restart = %+v ok=%v", st, ok)
	}
	if _, err := m2.Acquire("/r", MergeLockHolder{RunID: "run2", AutomatonID: "a2"}); !errors.Is(err, ErrMergeLockHeld) {
		t.Fatalf("want refusal after restart, got %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "merge-locks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestMergeLockCorruptFileIsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "merge-locks.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMergeLocks(dir); err == nil {
		t.Fatal("corrupt file must be an error")
	}
}
