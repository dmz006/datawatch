package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// helper: spin up a Manager with just enough plumbing for reconcile tests.
func newReconcileManager(t *testing.T, dataDir string) *Manager {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dataDir, "sessions"), 0755); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(filepath.Join(dataDir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Manager{
		hostname:          "host",
		dataDir:           dataDir,
		store:             store,
		promptFirstSeen:   make(map[string]time.Time),
		promptLastNotify:  make(map[string]time.Time),
		promptOscillation: make(map[string][]time.Time),
		monitors:          make(map[string]context.CancelFunc),
		trackers:          make(map[string]*Tracker),
	}
}

// helper: write a session.json into <dataDir>/sessions/<fullID>/.
func writeSessionDir(t *testing.T, dataDir, fullID string, sess *Session) {
	t.Helper()
	dir := filepath.Join(dataDir, "sessions", fullID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReconcile_DryRun_FindsOrphans(t *testing.T) {
	dir := t.TempDir()
	m := newReconcileManager(t, dir)

	writeSessionDir(t, dir, "host-orph1", makeTestSession("orph1", "host", StateKilled))
	writeSessionDir(t, dir, "host-orph2", makeTestSession("orph2", "host", StateComplete))

	res, err := m.ReconcileSessions(false)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(res.Orphaned) != 2 {
		t.Fatalf("orphans: got %v want 2", res.Orphaned)
	}
	if len(res.Imported) != 0 {
		t.Errorf("dry-run imported nonzero: %v", res.Imported)
	}
	// Registry must be untouched in dry-run mode.
	if n := len(m.store.List()); n != 0 {
		t.Errorf("dry-run mutated store: %d sessions", n)
	}
}

func TestReconcile_AutoImport_AddsToStore(t *testing.T) {
	dir := t.TempDir()
	m := newReconcileManager(t, dir)

	writeSessionDir(t, dir, "host-auto1", makeTestSession("auto1", "host", StateKilled))

	res, err := m.ReconcileSessions(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Imported) != 1 || res.Imported[0] != "host-auto1" {
		t.Fatalf("imported: got %v want [host-auto1]", res.Imported)
	}
	if len(res.Orphaned) != 0 {
		t.Errorf("auto-import should clear orphans: %v", res.Orphaned)
	}

	// Re-open store from disk — the registry must show the session
	// because Save is write-through (BL92).
	store2, _ := NewStore(filepath.Join(dir, "sessions.json"))
	if _, ok := store2.Get("host-auto1"); !ok {
		t.Error("imported session missing from on-disk registry")
	}
}

func TestReconcile_KnownSessions_NotDuplicated(t *testing.T) {
	dir := t.TempDir()
	m := newReconcileManager(t, dir)

	sess := makeTestSession("known", "host", StateRunning)
	_ = m.store.Save(sess)
	writeSessionDir(t, dir, sess.FullID, sess)

	res, err := m.ReconcileSessions(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Imported) != 0 || len(res.Orphaned) != 0 {
		t.Errorf("known session reported: imp=%v orph=%v", res.Imported, res.Orphaned)
	}
}

func TestReconcile_BadSessionJSON_RecordedAsError(t *testing.T) {
	dir := t.TempDir()
	m := newReconcileManager(t, dir)

	subdir := filepath.Join(dir, "sessions", "host-broken")
	_ = os.MkdirAll(subdir, 0755)
	_ = os.WriteFile(filepath.Join(subdir, "session.json"), []byte("not json"), 0644)

	res, err := m.ReconcileSessions(false)
	if err != nil {
		t.Fatalf("reconcile should not abort on bad file: %v", err)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("errors: got %v want 1", res.Errors)
	}
}

func TestReconcile_MissingSessionsDir_NoError(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{
		hostname:          "host",
		dataDir:           dir, // no sessions/ subdir
		store:             mustStore(t, dir),
		promptFirstSeen:   make(map[string]time.Time),
		promptLastNotify:  make(map[string]time.Time),
		promptOscillation: make(map[string][]time.Time),
		monitors:          make(map[string]context.CancelFunc),
		trackers:          make(map[string]*Tracker),
	}
	res, err := m.ReconcileSessions(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Imported) != 0 || len(res.Orphaned) != 0 {
		t.Errorf("expected empty result, got %+v", res)
	}
}

func TestReconcile_DirWithoutSessionJSON_Ignored(t *testing.T) {
	dir := t.TempDir()
	m := newReconcileManager(t, dir)

	// Create a stray subdir with no session.json (e.g. user mkdir'd
	// it, or it's a partial cleanup leftover).
	stray := filepath.Join(dir, "sessions", "stray-dir")
	_ = os.MkdirAll(stray, 0755)
	_ = os.WriteFile(filepath.Join(stray, "README.md"), []byte("x"), 0644)

	res, err := m.ReconcileSessions(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Orphaned) != 0 {
		t.Errorf("stray dir misreported: %v", res.Orphaned)
	}
}

func TestImportSessionDir_HappyPath(t *testing.T) {
	dir := t.TempDir()
	m := newReconcileManager(t, dir)

	sess := makeTestSession("imp01", "host", StateKilled)
	writeSessionDir(t, dir, sess.FullID, sess)

	got, imported, err := m.ImportSessionDir(filepath.Join(dir, "sessions", sess.FullID))
	if err != nil {
		t.Fatal(err)
	}
	if !imported {
		t.Error("expected imported=true on first call")
	}
	if got.FullID != sess.FullID {
		t.Errorf("FullID mismatch: got %q want %q", got.FullID, sess.FullID)
	}

	// Idempotency — second call must not error and must signal
	// already-known.
	_, imported2, err := m.ImportSessionDir(filepath.Join(dir, "sessions", sess.FullID))
	if err != nil {
		t.Fatal(err)
	}
	if imported2 {
		t.Error("expected imported=false on second call")
	}
}

func TestImportSessionDir_MissingFile(t *testing.T) {
	dir := t.TempDir()
	m := newReconcileManager(t, dir)

	_, _, err := m.ImportSessionDir(filepath.Join(dir, "sessions", "does-not-exist"))
	if err == nil {
		t.Error("expected error for missing dir")
	}
}

func TestImportSessionDir_MissingFullID(t *testing.T) {
	dir := t.TempDir()
	m := newReconcileManager(t, dir)

	subdir := filepath.Join(dir, "sessions", "host-noid")
	_ = os.MkdirAll(subdir, 0755)
	_ = os.WriteFile(filepath.Join(subdir, "session.json"), []byte(`{"id":"x"}`), 0644)

	_, _, err := m.ImportSessionDir(subdir)
	if err == nil {
		t.Error("expected error for session.json missing full_id")
	}
}

func mustStore(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(dir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
