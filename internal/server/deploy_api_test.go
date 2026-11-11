package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/deploy"
)

func deployReq(s *Server, h http.HandlerFunc, method string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(method, "/api/autonomous/deploy", nil))
	return rec
}

func TestDeployAPIMethodChecks(t *testing.T) {
	s := &Server{autonomousMgr: &mergeRunSpy{repo: t.TempDir()}}
	rec := deployReq(s, s.handleAutonomousDeploy, http.MethodPost)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST status = %d allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
	rec = deployReq(s, s.handleAutonomousDeployRun, http.MethodGet)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET run status = %d allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestDeployAPIAutonomousDisabled(t *testing.T) {
	s := &Server{}
	if rec := deployReq(s, s.handleAutonomousDeploy, http.MethodGet); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET = %d", rec.Code)
	}
	if rec := deployReq(s, s.handleAutonomousDeployRun, http.MethodPost); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST = %d", rec.Code)
	}
}

func TestDeployAPIBusyIs409(t *testing.T) {
	s := &Server{autonomousMgr: &mergeRunSpy{repo: t.TempDir()}}
	mergeRunMu.Lock()
	defer mergeRunMu.Unlock()
	if rec := deployReq(s, s.handleAutonomousDeployRun, http.MethodPost); rec.Code != http.StatusConflict {
		t.Fatalf("busy POST = %d, want 409", rec.Code)
	}
}

func TestDeployAPIGetNeverIncludesSnapshotContents(t *testing.T) {
	data := t.TempDir()
	t.Setenv("DATAWATCH_DATA_DIR", data)
	dir := filepath.Join(data, "deploy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const secret = "SNAPSHOT-BINARY-CONTENTS"
	snap := filepath.Join(dir, "binary.snapshot")
	if err := os.WriteFile(snap, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Now().UTC().Truncate(time.Second)
	if err := (deploy.FileStore{Path: filepath.Join(dir, "state.json")}).Save(deploy.State{
		Version: deploy.StateVersion, Phase: deploy.PhaseDeferred, Reason: "final-gate tasks running",
		UpdatedAt: when, SnapshotPath: snap, SnapshotHash: "abc123", SnapshotTime: when,
	}); err != nil {
		t.Fatal(err)
	}
	if err := (deploy.OutcomeFile{Path: filepath.Join(dir, "outcomes.jsonl")}).Append(deploy.OutcomeRecord{Outcome: deploy.OutcomeOK}); err != nil {
		t.Fatal(err)
	}
	s := &Server{autonomousMgr: &mergeRunSpy{repo: t.TempDir()}}
	rec := deployReq(s, s.handleAutonomousDeploy, http.MethodGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, secret) || strings.Contains(body, snap) {
		t.Fatalf("snapshot contents or path leaked: %s", body)
	}
	var out deployStatusBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Phase != "deferred" || out.Outcome != "ok" || out.Deferred == nil || out.Deferred.Reason == "" ||
		out.Snapshot == nil || out.Snapshot.SHA256 != "abc123" {
		t.Fatalf("body = %+v", out)
	}
}

func TestDeployConfigRoundTripEveryKey(t *testing.T) {
	keys := map[string]string{
		"build_command":   "go build ./...",
		"install_command": "cp datawatch /usr/local/bin/datawatch",
		"binary_path":     "/usr/local/bin/datawatch",
		"restart_command": "systemctl --user restart datawatch", // service-scope-ok: test only asserts the configured command text, never runs it
		"health_check":    "curl -fsk https://localhost:8443/healthz",
		"drain_timeout":   "5m",
		"health_timeout":  "30s",
	}
	for k, want := range keys {
		t.Run(k, func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			patch := map[string]any{deployKeyPrefix + k: want}
			if k == "install_command" {
				patch[deployKeyPrefix+"binary_path"] = keys["binary_path"]
			}
			if rec := putMergePipelineConfig(s, patch); rec.Code != http.StatusOK {
				t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
			}
			var m map[string]string
			if err := json.Unmarshal(getAutonomousConfigMap(t, s)["deploy"], &m); err != nil {
				t.Fatal(err)
			}
			if m[k] != want {
				t.Fatalf("GET %s = %q, want %q", k, m[k], want)
			}
		})
	}
}

func TestDeployConfigRefusesBadValues(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	for _, patch := range []map[string]any{
		{"autonomous.deploy.drain_timeout": "soon"},
		{"autonomous.deploy.binary_path": "relative/path"},
		{"autonomous.deploy.nope": "x"},
	} {
		if rec := putMergePipelineConfig(s, patch); rec.Code != http.StatusBadRequest {
			t.Fatalf("PUT %v = %d, want 400", patch, rec.Code)
		}
	}
}
