package session

import (
	"path/filepath"
	"testing"
)

// TestResultStoreTwoInstancesKeepBothEntries reproduces the result-store loss:
// two ResultStore instances opened over the same file each hold their own
// in-memory copy and every Put rewrites the whole file. Entry "a" written via
// the first instance is erased when the second instance (which never saw "a")
// saves its stale copy containing only "b".
//
// This test is intentionally RED on the current code. It is the regression
// guard for the single-writer task: it stays failing until a shared writer
// lands, and must pass afterwards without modification.
func TestResultStoreTwoInstancesKeepBothEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")

	first, err := NewResultStore(path)
	if err != nil {
		t.Fatalf("open first store: %v", err)
	}
	second, err := NewResultStore(path)
	if err != nil {
		t.Fatalf("open second store: %v", err)
	}

	if _, err := first.Put("a", map[string]any{"from": "first"}, 0); err != nil {
		t.Fatalf("put a via first: %v", err)
	}
	if _, err := second.Put("b", map[string]any{"from": "second"}, 0); err != nil {
		t.Fatalf("put b via second: %v", err)
	}

	fresh, err := NewResultStore(path)
	if err != nil {
		t.Fatalf("open fresh store: %v", err)
	}
	if _, ok := fresh.Get("a"); !ok {
		t.Errorf("entry a lost: second instance's stale copy overwrote the file")
	}
	if _, ok := fresh.Get("b"); !ok {
		t.Errorf("entry b missing from fresh load")
	}
}
