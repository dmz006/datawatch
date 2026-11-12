// Configuration Accessibility round-trip for autonomous.max_concurrent_tasks
// (daemon-wide default for how many independent tasks of one Automaton
// run at once) and the deprecated, accepted-but-ignored
// autonomous.max_parallel_tasks on both REST config surfaces.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// newMaxConcurrentTasksServer wires a real autonomous manager (through
// autonomous.API, the same adapter main.go uses) so the live-manager
// assertions exercise the real SetConfig merge, not a fake.
func newMaxConcurrentTasksServer(t *testing.T) (*Server, *autonomous.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	mgr, err := autonomous.NewManager(filepath.Join(dir, "autonomous"), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	s := &Server{
		cfg:           config.DefaultConfig(),
		cfgPath:       cfgPath,
		autonomousMgr: newTestAPI(mgr),
	}
	return s, mgr, cfgPath
}

func getAutonomousConfigMap(t *testing.T, s *Server) map[string]json.RawMessage {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous map[string]json.RawMessage `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return got.Autonomous
}

func putJSON(t *testing.T, h http.HandlerFunc, path string, body any) map[string]json.RawMessage {
	t.Helper()
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPut, path, bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT %s %s: want 200, got %d body=%s", path, b, rr.Code, rr.Body.String())
	}
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode PUT response: %v (%s)", err, rr.Body.String())
	}
	return resp
}

func TestMaxConcurrentTasks_GetConfigDefaultIsOne(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	if got := string(getAutonomousConfigMap(t, s)["max_concurrent_tasks"]); got != "1" {
		t.Fatalf("GET /api/config autonomous.max_concurrent_tasks = %q, want 1", got)
	}
}

func TestMaxConcurrentTasks_PutConfigRoundTrip(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	resp := putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.max_concurrent_tasks": 3})
	if _, ok := resp["deprecated_fields"]; ok {
		t.Errorf("deprecated_fields present for a non-deprecated key: %v", resp)
	}
	if got := string(getAutonomousConfigMap(t, s)["max_concurrent_tasks"]); got != "3" {
		t.Fatalf("GET after PUT = %q, want 3", got)
	}
	if got := mgr.Config().MaxConcurrentTasks; got != 3 {
		t.Fatalf("live manager MaxConcurrentTasks = %d, want 3", got)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "max_concurrent_tasks: 3") {
		t.Fatalf("config.yaml lacks max_concurrent_tasks: 3:\n%s", raw)
	}

	// Values below 1 are skipped, not stored.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.max_concurrent_tasks": 0})
	if s.cfg.Autonomous.MaxConcurrentTasks != 3 || mgr.Config().MaxConcurrentTasks != 3 {
		t.Fatalf("0 must be skipped: cfg=%d mgr=%d", s.cfg.Autonomous.MaxConcurrentTasks, mgr.Config().MaxConcurrentTasks)
	}
}

func TestMaxConcurrentTasks_PutAutonomousConfigPersists(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	resp := putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"max_concurrent_tasks": 2})
	if string(resp["status"]) != `"ok"` {
		t.Errorf("status = %s", resp["status"])
	}
	if got := mgr.Config().MaxConcurrentTasks; got != 2 {
		t.Fatalf("live manager MaxConcurrentTasks = %d, want 2", got)
	}
	if got := s.cfg.Autonomous.MaxConcurrentTasks; got != 2 {
		t.Fatalf("s.cfg.Autonomous.MaxConcurrentTasks = %d, want 2 (GH#180 sync-back)", got)
	}
	if got := string(getAutonomousConfigMap(t, s)["max_concurrent_tasks"]); got != "2" {
		t.Fatalf("GET /api/config = %q, want 2", got)
	}
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "max_concurrent_tasks: 2") {
		t.Fatalf("config.yaml lacks max_concurrent_tasks: 2:\n%s", raw)
	}
}

func TestMaxParallelTasks_PutConfigDeprecatedAndIgnored(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.max_concurrent_tasks": 2})
	before := s.cfg.Autonomous.MaxParallelTasks

	resp := putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.max_parallel_tasks": 7})
	var dep map[string]string
	if err := json.Unmarshal(resp["deprecated_fields"], &dep); err != nil {
		t.Fatalf("deprecated_fields missing or malformed: %v (%v)", err, resp)
	}
	if !strings.Contains(dep["autonomous.max_parallel_tasks"], "max_concurrent_tasks") {
		t.Fatalf("deprecated note must name the replacement: %v", dep)
	}
	if s.cfg.Autonomous.MaxParallelTasks != before {
		t.Errorf("max_parallel_tasks was stored: %d", s.cfg.Autonomous.MaxParallelTasks)
	}
	if s.cfg.Autonomous.MaxConcurrentTasks != 2 || mgr.Config().MaxConcurrentTasks != 2 {
		t.Fatalf("max_concurrent_tasks changed: cfg=%d mgr=%d", s.cfg.Autonomous.MaxConcurrentTasks, mgr.Config().MaxConcurrentTasks)
	}
}

func TestMaxParallelTasks_PutAutonomousConfigDeprecatedAndIgnored(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	resp := putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"max_parallel_tasks": 7})
	if string(resp["status"]) != `"ok"` {
		t.Errorf("status = %s", resp["status"])
	}
	var dep map[string]string
	if err := json.Unmarshal(resp["deprecated_fields"], &dep); err != nil {
		t.Fatalf("deprecated_fields missing or malformed: %v (%v)", err, resp)
	}
	if _, ok := dep["max_parallel_tasks"]; !ok {
		t.Fatalf("deprecated_fields lacks max_parallel_tasks: %v", dep)
	}
	if got := mgr.Config().MaxParallelTasks; got == 7 {
		t.Errorf("max_parallel_tasks was stored on the live manager")
	}
	if got := mgr.Config().MaxConcurrentTasks; got != 1 {
		t.Errorf("max_concurrent_tasks changed to %d", got)
	}
}
