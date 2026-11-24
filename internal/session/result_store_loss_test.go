package session

import (
	"fmt"
	"path/filepath"
	"sync"
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

func TestResultStoreTwoInstancesDeleteDoesNotResurrect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	first, _ := NewResultStore(path)
	second, _ := NewResultStore(path)
	if _, err := first.Put("a", map[string]any{"x": 1}, 0); err != nil {
		t.Fatal(err)
	}
	// second never saw "a" in memory; delete must still find it on disk.
	if err := second.Delete("a"); err != nil {
		t.Fatalf("delete a via second: %v", err)
	}
	if _, err := second.Put("b", map[string]any{}, 0); err != nil {
		t.Fatal(err)
	}
	fresh, _ := NewResultStore(path)
	if _, ok := fresh.Get("a"); ok {
		t.Errorf("a should be deleted")
	}
	if _, ok := fresh.Get("b"); !ok {
		t.Errorf("b missing")
	}
}

func TestResultStoreConcurrentInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		s, err := NewResultStore(path)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(i int, s *ResultStore) {
			defer wg.Done()
			if _, err := s.Put(fmt.Sprintf("k%d", i), map[string]any{"i": i}, 0); err != nil {
				t.Errorf("put: %v", err)
			}
		}(i, s)
	}
	wg.Wait()
	fresh, _ := NewResultStore(path)
	if got := len(fresh.List("")); got != n {
		t.Errorf("want %d entries, got %d", n, got)
	}
}
