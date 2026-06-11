package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBL356_AddAndList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exit_hooks.json")
	store, err := NewExitHookStore(path)
	if err != nil {
		t.Fatalf("NewExitHookStore: %v", err)
	}

	e1, err := store.Add("agent-1", ExitHookRestart, "", "", 300)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	e2, err := store.Add("agent-2", ExitHookNotify, "monitor", "agent-2 exited", 60)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	list := store.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}
	if e1.ID == "" || e2.ID == "" {
		t.Error("IDs should be non-empty")
	}
	if e1.Name != "agent-1" || e1.Action != ExitHookRestart {
		t.Errorf("e1 fields wrong: %+v", e1)
	}
	if e2.Action != ExitHookNotify || e2.NotifySession != "monitor" {
		t.Errorf("e2 fields wrong: %+v", e2)
	}
	if !e1.Enabled || !e2.Enabled {
		t.Error("both hooks should be enabled by default")
	}

	// Reload from disk
	store2, err := NewExitHookStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(store2.List()) != 2 {
		t.Errorf("after reload expected 2 entries, got %d", len(store2.List()))
	}
}

func TestBL356_Delete(t *testing.T) {
	dir := t.TempDir()
	store, err := NewExitHookStore(filepath.Join(dir, "exit_hooks.json"))
	if err != nil {
		t.Fatalf("NewExitHookStore: %v", err)
	}

	e, err := store.Add("worker", ExitHookRestart, "", "", 300)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := store.Delete(e.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(store.List()) != 0 {
		t.Errorf("expected 0 entries after delete, got %d", len(store.List()))
	}
	// Delete non-existent should error
	if err := store.Delete("nonexistent"); err == nil {
		t.Error("expected error deleting non-existent hook")
	}
}

func TestBL356_MarkFired_Cooldown(t *testing.T) {
	dir := t.TempDir()
	store, err := NewExitHookStore(filepath.Join(dir, "exit_hooks.json"))
	if err != nil {
		t.Fatalf("NewExitHookStore: %v", err)
	}

	e, err := store.Add("worker", ExitHookRestart, "", "", 300)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Before firing: not cooling down
	if store.IsCoolingDown(e) {
		t.Error("should not be cooling down before first fire")
	}

	// After MarkFired: cooling down (cooldown=300s)
	if err := store.MarkFired(e.ID); err != nil {
		t.Fatalf("MarkFired: %v", err)
	}
	// Reload entry from store
	reloaded, ok := store.Get(e.ID)
	if !ok {
		t.Fatal("entry not found after MarkFired")
	}
	if !store.IsCoolingDown(reloaded) {
		t.Error("should be cooling down immediately after fire")
	}

	// Simulate cooldown expired by setting LastFiredAt to past
	reloaded.LastFiredAt = time.Now().Add(-400 * time.Second)
	if store.IsCoolingDown(reloaded) {
		t.Error("should NOT be cooling down after cooldown period elapsed")
	}
}

func TestBL356_SetEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exit_hooks.json")
	store, err := NewExitHookStore(path)
	if err != nil {
		t.Fatalf("NewExitHookStore: %v", err)
	}

	e, err := store.Add("worker", ExitHookRestart, "", "", 300)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !e.Enabled {
		t.Error("should be enabled by default")
	}

	if err := store.SetEnabled(e.ID, false); err != nil {
		t.Fatalf("SetEnabled(false): %v", err)
	}
	reloaded, _ := store.Get(e.ID)
	if reloaded.Enabled {
		t.Error("should be disabled after SetEnabled(false)")
	}

	// Reload from disk verifies persistence
	store2, _ := NewExitHookStore(path)
	r2, _ := store2.Get(e.ID)
	if r2.Enabled {
		t.Error("should still be disabled after reload from disk")
	}

	if err := store.SetEnabled(e.ID, true); err != nil {
		t.Fatalf("SetEnabled(true): %v", err)
	}
	reloaded2, _ := store.Get(e.ID)
	if !reloaded2.Enabled {
		t.Error("should be enabled after SetEnabled(true)")
	}
}

func TestBL356_GetBySessionName(t *testing.T) {
	dir := t.TempDir()
	store, err := NewExitHookStore(filepath.Join(dir, "exit_hooks.json"))
	if err != nil {
		t.Fatalf("NewExitHookStore: %v", err)
	}

	// Add two enabled hooks for "agent-x", one disabled, one for different name
	e1, _ := store.Add("agent-x", ExitHookRestart, "", "", 300)
	e2, _ := store.Add("agent-x", ExitHookNotify, "monitor", "msg", 60)
	e3, _ := store.Add("agent-y", ExitHookRestart, "", "", 300)
	// Disable e2
	_ = store.SetEnabled(e2.ID, false)
	// Disable e3 (different name, just to be safe)
	_ = e3

	results := store.GetBySessionName("agent-x")
	if len(results) != 1 {
		t.Fatalf("expected 1 enabled hook for agent-x, got %d", len(results))
	}
	if results[0].ID != e1.ID {
		t.Errorf("expected hook %s, got %s", e1.ID, results[0].ID)
	}

	// Different name returns nothing matching disabled
	results2 := store.GetBySessionName("agent-z")
	if len(results2) != 0 {
		t.Errorf("expected 0 for unknown name, got %d", len(results2))
	}
}

func TestBL356_DefaultCooldown(t *testing.T) {
	dir := t.TempDir()
	store, err := NewExitHookStore(filepath.Join(dir, "exit_hooks.json"))
	if err != nil {
		t.Fatalf("NewExitHookStore: %v", err)
	}

	// cooldownSeconds <= 0 should default to 300
	e, err := store.Add("worker", ExitHookRestart, "", "", 0)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if e.CooldownSeconds != 300 {
		t.Errorf("expected default cooldown 300, got %d", e.CooldownSeconds)
	}
}

func TestBL356_StoreFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exit_hooks.json")

	// File should not exist yet
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should not exist before first Add")
	}

	store, _ := NewExitHookStore(path)
	store.Add("a", ExitHookRestart, "", "", 300) //nolint:errcheck

	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should exist after Add: %v", err)
	}
}
