// Tests for the planner-session record hook on the one-shot reader: the
// planner's session id and output file must land on the Automaton record as
// soon as the session is spawned, before the reader's first read, and a spawn
// that fails must record nothing.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// plannerStarter is a fake session starter standing in for the daemon's
// /api/sessions/start. When fail is true it refuses the start with a 500, so
// no session is created. started flips once a start request succeeds.
func plannerStarter(t *testing.T, fail bool, started *atomic.Bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/start", func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		started.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "planner-sess-1"})
	})
	mux.HandleFunc("/api/sessions/planner-sess-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"state": "running"})
	})
	mux.HandleFunc("/api/sessions/kill", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestPlannerRecord_SpawnRecordsIDAndPathBeforeFirstRead runs the real
// one-shot reader against a fake session starter. The reader's first read
// after the spawn must already see the session id and the output path on the
// Automaton record; the test reads the record from disk at that moment.
func TestPlannerRecord_SpawnRecordsIDAndPathBeforeFirstRead(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := t.TempDir()
	m, err := autonomouspkg.NewManager(dataDir, autonomouspkg.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("plan me", projectDir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	outputFile := filepath.Join(projectDir, ".decompose-output-test.json")

	var started atomic.Bool
	srv := plannerStarter(t, false, &started)
	withFakeLoopback(t, srv)
	cfg := &config.Config{DataDir: dataDir}

	type snapshot struct{ id, file string }
	var atFirstRead *snapshot
	reader := func(_, _, _, _ string) (string, bool) {
		if !started.Load() {
			return "", false // the pre-spawn read: nothing written yet
		}
		if atFirstRead == nil {
			st, err := autonomouspkg.NewStore(dataDir)
			if err != nil {
				t.Errorf("NewStore: %v", err)
				return "", false
			}
			got, _ := st.GetPRD(prd.ID)
			atFirstRead = &snapshot{id: got.PlannerSessionID, file: got.PlannerOutputFile}
		}
		return `{"ok":true}`, true
	}

	onSpawned := func(sessionID string) {
		m.RecordPlannerSession(prd.ID, sessionID, outputFile)
	}
	_, sid, err := runOneShotJSONSessionWithSpawnHook(cfg, prd.ID, projectDir, "claude-code", "m", "",
		"autonomous:decompose", "task", outputFile, "decompose-output.json", 30*time.Second, reader, "",
		launchTagsForDecompose(prd.ID), onSpawned)
	if err != nil {
		t.Fatalf("one-shot session: %v", err)
	}
	if sid != "planner-sess-1" {
		t.Fatalf("session id = %q, want planner-sess-1", sid)
	}
	if atFirstRead == nil {
		t.Fatal("reader never read after the spawn")
	}
	if atFirstRead.id != "planner-sess-1" {
		t.Errorf("record at first post-spawn read has PlannerSessionID %q, want planner-sess-1", atFirstRead.id)
	}
	if atFirstRead.file != ".decompose-output-test.json" {
		t.Errorf("record at first post-spawn read has PlannerOutputFile %q, want .decompose-output-test.json", atFirstRead.file)
	}
}

// TestPlannerRecord_FailedSpawnRecordsNothing checks that when the session
// starter refuses the request, the hook never runs and the Automaton record
// keeps no planner fields.
func TestPlannerRecord_FailedSpawnRecordsNothing(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := t.TempDir()
	m, err := autonomouspkg.NewManager(dataDir, autonomouspkg.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("plan me", projectDir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	outputFile := filepath.Join(projectDir, ".decompose-output-fail.json")

	var started atomic.Bool
	srv := plannerStarter(t, true, &started)
	withFakeLoopback(t, srv)
	cfg := &config.Config{DataDir: dataDir}

	hookCalled := false
	onSpawned := func(sessionID string) {
		hookCalled = true
		m.RecordPlannerSession(prd.ID, sessionID, outputFile)
	}
	_, _, err = runOneShotJSONSessionWithSpawnHook(cfg, prd.ID, projectDir, "claude-code", "m", "",
		"autonomous:decompose", "task", outputFile, "decompose-output.json", 30*time.Second, noOutputReader, "",
		launchTagsForDecompose(prd.ID), onSpawned)
	if err == nil {
		t.Fatal("a refused spawn must return an error")
	}
	if hookCalled {
		t.Error("spawn hook ran although the session start failed")
	}
	st, err := autonomouspkg.NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	got, ok := st.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing")
	}
	if got.PlannerSessionID != "" || got.PlannerOutputFile != "" {
		t.Errorf("failed spawn recorded planner fields: id=%q file=%q", got.PlannerSessionID, got.PlannerOutputFile)
	}
}
