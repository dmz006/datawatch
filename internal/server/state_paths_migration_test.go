// The v7 migration status file and the profile-clone workspace root must
// live under the server's configured data directory (Server.statePath),
// never under a home directory derived from the process environment.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestStatePathMigrationAndWorkspaceUnderDataDir pins the migration status
// file and the workspace root to the configured data directory: a Server
// with a temp data directory and a decoy HOME must read and write the
// status file under the data directory, with the decoy HOME tree left
// exactly as it was. No real git clone is performed.
func TestStatePathMigrationAndWorkspaceUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()
	decoyHome := t.TempDir()
	t.Setenv("HOME", decoyHome)
	t.Setenv("DATAWATCH_DATA_DIR", "")
	if err := os.WriteFile(filepath.Join(decoyHome, "preexisting"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pre-seed the location the old code would have used, so the test also
	// fails if the handler regresses to the home-relative path.
	if err := os.MkdirAll(filepath.Join(decoyHome, ".datawatch"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoyHome, ".datawatch", "v7-migration-status.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := statePathTree(t, decoyHome)

	cfg := config.DefaultConfig()
	cfg.DataDir = dataDir
	s := &Server{cfg: cfg, dataDir: dataDir}

	// Status file: the handler's path source (s.statePath) must resolve
	// under the data directory.
	statusPath := s.statePath("v7-migration-status.json")
	rel, err := filepath.Rel(dataDir, statusPath)
	if err != nil || rel == ".." || filepath.IsAbs(rel) {
		t.Fatalf("statePath(%q) not under the data directory: %q", "v7-migration-status.json", statusPath)
	}

	// Seed the status file the way a completed migration run leaves it and
	// read it back through the handler.
	if err := os.WriteFile(statusPath, []byte(`{"migrated":["node-a"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.handleMigrationStatus(rec, httptest.NewRequest(http.MethodGet, "/api/migration/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d: %s", rec.Code, rec.Body)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["show"] != true {
		t.Fatalf("handler did not read the file under the data directory: %s", rec.Body)
	}
	if mg, _ := doc["migrated"].([]any); len(mg) != 1 || mg[0] != "node-a" {
		t.Fatalf("handler served a different status doc: %s", rec.Body)
	}

	// Dismiss through the handler; the file under the data directory is the
	// one removed, the decoy HOME stays untouched.
	rec = httptest.NewRecorder()
	s.handleMigrationStatus(rec, httptest.NewRequest(http.MethodDelete, "/api/migration/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d: %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(statusPath); !os.IsNotExist(err) {
		t.Fatalf("status file under the data directory still present after dismiss: %v", err)
	}

	// Workspace root: the helper's join must land under the data directory
	// without running a real clone.
	cloneRoot := s.statePath("workspaces")
	if rel, err := filepath.Rel(dataDir, cloneRoot); err != nil || rel != "workspaces" {
		t.Fatalf("workspaces root not under the data directory: rel=%q err=%v", rel, err)
	}

	// The decoy HOME must be untouched — nothing written or removed.
	if after := statePathTree(t, decoyHome); !reflect.DeepEqual(before, after) {
		t.Fatalf("decoy HOME tree changed: before=%v after=%v", before, after)
	}
}
