package multiserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestStore_AddGetDelete(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	entry := &Entry{Name: "prod", URL: "http://prod.example.com:8080", Enabled: true}
	if err := s.Add(entry); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok := s.Get("prod")
	if !ok {
		t.Fatal("Get: entry not found after Add")
	}
	if got.URL != "http://prod.example.com:8080" {
		t.Errorf("URL = %q, want %q", got.URL, "http://prod.example.com:8080")
	}
	if got.Builtin {
		t.Error("runtime entry should not have Builtin=true")
	}

	list := s.List()
	if len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}

	if err := s.Delete("prod"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get("prod"); ok {
		t.Error("entry still present after Delete")
	}
}

func TestStore_ConflictOnDuplicate(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	e := &Entry{Name: "dup", URL: "http://a.example.com"}
	if err := s.Add(e); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if err := s.Add(e); err != ErrConflict {
		t.Errorf("second Add: got %v, want ErrConflict", err)
	}
}

func TestStore_BuiltinCannotDelete(t *testing.T) {
	dir := t.TempDir()
	seeds := []config.RemoteServerConfig{
		{Name: "yaml-server", URL: "http://yaml.example.com", Enabled: true},
	}
	s, err := NewStore(dir, seeds)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Builtin entry should be visible in the list.
	got, ok := s.Get("yaml-server")
	if !ok {
		t.Fatal("builtin entry not found")
	}
	if !got.Builtin {
		t.Error("builtin entry should have Builtin=true")
	}

	// Attempting to delete a builtin must fail.
	if err := s.Delete("yaml-server"); err != ErrBuiltin {
		t.Errorf("Delete builtin: got %v, want ErrBuiltin", err)
	}
}

func TestStore_BuiltinCannotUpdate(t *testing.T) {
	dir := t.TempDir()
	seeds := []config.RemoteServerConfig{
		{Name: "yaml-server", URL: "http://yaml.example.com", Enabled: true},
	}
	s, err := NewStore(dir, seeds)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	updated := &Entry{Name: "yaml-server", URL: "http://changed.example.com", Enabled: false}
	if err := s.Update("yaml-server", updated); err != ErrBuiltin {
		t.Errorf("Update builtin: got %v, want ErrBuiltin", err)
	}
}

func TestStore_Persist(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	if err := s.Add(&Entry{Name: "srv1", URL: "http://srv1.example.com", Enabled: true}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Reload from disk.
	s2, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("reload NewStore: %v", err)
	}
	got, ok := s2.Get("srv1")
	if !ok {
		t.Fatal("entry not found after reload")
	}
	if got.URL != "http://srv1.example.com" {
		t.Errorf("persisted URL = %q, want %q", got.URL, "http://srv1.example.com")
	}
	// Make sure builtins are NOT persisted to disk.
	storePath := filepath.Join(dir, "servers.json")
	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	if string(data) == "null" || len(data) == 0 {
		t.Error("store file is empty after Add")
	}
}

func TestStore_BuiltinsNotPersisted(t *testing.T) {
	dir := t.TempDir()
	seeds := []config.RemoteServerConfig{
		{Name: "builtin1", URL: "http://builtin1.example.com", Enabled: true},
	}
	// Create with a seed.
	_, err := NewStore(dir, seeds)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Reload without any seeds — builtin should NOT reappear.
	s2, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := s2.Get("builtin1"); ok {
		t.Error("builtin entry was incorrectly persisted to disk")
	}
}

func TestStore_Test(t *testing.T) {
	// Spin up a tiny test HTTP server that returns a health response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"version":"7.0.0-smoke"}`)) //nolint:errcheck
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(&Entry{Name: "smoke", URL: srv.URL, Enabled: true}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	latency, version, err := s.Test(context.Background(), "smoke")
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if latency < 0 {
		t.Errorf("latency %d < 0", latency)
	}
	if version != "7.0.0-smoke" {
		t.Errorf("version = %q, want %q", version, "7.0.0-smoke")
	}
}

func TestStore_TestNotFound(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	_, _, err = s.Test(context.Background(), "nonexistent")
	if err != ErrNotFound {
		t.Errorf("Test nonexistent: got %v, want ErrNotFound", err)
	}
}

func TestStore_UpdateNotFound(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	err = s.Update("ghost", &Entry{Name: "ghost", URL: "http://ghost.example.com"})
	if err != ErrNotFound {
		t.Errorf("Update nonexistent: got %v, want ErrNotFound", err)
	}
}

func TestStore_DeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Delete("ghost"); err != ErrNotFound {
		t.Errorf("Delete nonexistent: got %v, want ErrNotFound", err)
	}
}

func TestStore_AddRequiresNameAndURL(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(&Entry{}); err == nil {
		t.Error("Add with empty entry: expected error")
	}
	if err := s.Add(&Entry{Name: "no-url"}); err == nil {
		t.Error("Add with no URL: expected error")
	}
}
