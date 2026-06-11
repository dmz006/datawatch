package session

// BL360 — structured agent result store tests.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBL360_Put(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.json")
	store, err := NewResultStore(path)
	if err != nil {
		t.Fatalf("NewResultStore: %v", err)
	}

	payload := map[string]any{"answer": 42, "status": "ok"}
	entry, err := store.Put("task-1", payload, 0)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if entry.Name != "task-1" {
		t.Errorf("Name = %q, want %q", entry.Name, "task-1")
	}
	if entry.ExpiresAt != nil {
		t.Error("ExpiresAt should be nil for ttl=0")
	}

	got, ok := store.Get("task-1")
	if !ok {
		t.Fatal("Get: not found after Put")
	}
	// payload values may be int or float64 depending on whether a JSON
	// round-trip has occurred; compare via fmt to be type-agnostic.
	if fmt.Sprintf("%v", got.Payload["answer"]) != "42" {
		t.Errorf("payload answer = %v, want 42", got.Payload["answer"])
	}
}

func TestBL360_PutUpsert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.json")
	store, err := NewResultStore(path)
	if err != nil {
		t.Fatalf("NewResultStore: %v", err)
	}

	_, err = store.Put("my-result", map[string]any{"v": 1}, 0)
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}

	// Overwrite with new payload
	_, err = store.Put("my-result", map[string]any{"v": 2, "extra": "yes"}, 0)
	if err != nil {
		t.Fatalf("second Put (upsert): %v", err)
	}

	got, ok := store.Get("my-result")
	if !ok {
		t.Fatal("Get: not found after upsert")
	}
	if fmt.Sprintf("%v", got.Payload["v"]) != "2" {
		t.Errorf("payload v = %v, want 2", got.Payload["v"])
	}
	if got.Payload["extra"] != "yes" {
		t.Errorf("payload extra = %v, want yes", got.Payload["extra"])
	}

	// Only one entry should exist
	list := store.List("")
	if len(list) != 1 {
		t.Errorf("List returned %d entries, want 1", len(list))
	}
}

func TestBL360_GetExpired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.json")
	store, err := NewResultStore(path)
	if err != nil {
		t.Fatalf("NewResultStore: %v", err)
	}

	// Store with 1-second TTL then manually backdate the expiry
	entry, err := store.Put("expiring", map[string]any{"x": 1}, 60)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Backdate the expiry so it's already expired
	past := time.Now().Add(-1 * time.Second)
	entry.ExpiresAt = &past
	// Write the backdated entry directly to the store map (for test purposes)
	store.mu.Lock()
	store.entries["expiring"] = entry
	store.mu.Unlock()

	got, ok := store.Get("expiring")
	if ok || got != nil {
		t.Error("Get should return false for expired entry")
	}

	// List should also skip expired entries
	list := store.List("")
	if len(list) != 0 {
		t.Errorf("List returned %d entries, want 0 (all expired)", len(list))
	}
}

func TestBL360_ListPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.json")
	store, err := NewResultStore(path)
	if err != nil {
		t.Fatalf("NewResultStore: %v", err)
	}

	entries := []string{"alpha/1", "alpha/2", "beta/1", "beta/2", "gamma"}
	for _, name := range entries {
		if _, err := store.Put(name, map[string]any{"name": name}, 0); err != nil {
			t.Fatalf("Put %q: %v", name, err)
		}
	}

	// Filter by "alpha/"
	alphas := store.List("alpha/")
	if len(alphas) != 2 {
		t.Errorf("List(alpha/) returned %d entries, want 2", len(alphas))
	}
	for _, e := range alphas {
		if e.Name != "alpha/1" && e.Name != "alpha/2" {
			t.Errorf("unexpected entry in alpha/ list: %q", e.Name)
		}
	}

	// Filter by "beta"
	betas := store.List("beta")
	if len(betas) != 2 {
		t.Errorf("List(beta) returned %d entries, want 2", len(betas))
	}

	// Empty prefix = all
	all := store.List("")
	if len(all) != 5 {
		t.Errorf("List('') returned %d entries, want 5", len(all))
	}

	// Verify sorted order
	for i := 1; i < len(all); i++ {
		if all[i-1].Name >= all[i].Name {
			t.Errorf("List not sorted: %q >= %q", all[i-1].Name, all[i].Name)
		}
	}
}

func TestBL360_Delete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.json")
	store, err := NewResultStore(path)
	if err != nil {
		t.Fatalf("NewResultStore: %v", err)
	}

	if _, err := store.Put("to-delete", map[string]any{"k": "v"}, 0); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := store.Delete("to-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, ok := store.Get("to-delete"); ok {
		t.Error("entry should be gone after Delete")
	}

	// Delete non-existent should return error
	if err := store.Delete("does-not-exist"); err == nil {
		t.Error("Delete of non-existent entry should return error")
	}

	// List should be empty
	if list := store.List(""); len(list) != 0 {
		t.Errorf("List returned %d entries after delete, want 0", len(list))
	}
}

func TestBL360_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.json")

	// Write entries in first store instance
	store1, err := NewResultStore(path)
	if err != nil {
		t.Fatalf("NewResultStore (1): %v", err)
	}
	if _, err := store1.Put("persist-a", map[string]any{"val": "hello"}, 0); err != nil {
		t.Fatalf("Put a: %v", err)
	}
	if _, err := store1.Put("persist-b", map[string]any{"val": "world"}, 0); err != nil {
		t.Fatalf("Put b: %v", err)
	}

	// Verify file was created
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		t.Fatal("result store file was not created")
	}

	// Load a fresh store from same path
	store2, err := NewResultStore(path)
	if err != nil {
		t.Fatalf("NewResultStore (2): %v", err)
	}

	a, ok := store2.Get("persist-a")
	if !ok {
		t.Fatal("persist-a not found after reload")
	}
	if a.Payload["val"] != "hello" {
		t.Errorf("persist-a payload val = %v, want hello", a.Payload["val"])
	}

	b, ok := store2.Get("persist-b")
	if !ok {
		t.Fatal("persist-b not found after reload")
	}
	if b.Payload["val"] != "world" {
		t.Errorf("persist-b payload val = %v, want world", b.Payload["val"])
	}

	// Count check
	list := store2.List("")
	if len(list) != 2 {
		t.Errorf("reloaded store has %d entries, want 2", len(list))
	}
}
