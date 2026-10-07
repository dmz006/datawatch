// GH#180 — PUT /api/autonomous/config only updated the autonomous
// manager's own in-memory Config; GET /api/config (and config.yaml)
// kept serving the stale s.cfg.Autonomous, so a client that gates the
// Automata tab on /api/config (every mobile client) never saw the
// change. PUT /api/config's own autonomous.* cases already sync forward
// into the manager; this is the reverse direction.

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

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// fakeAutonomousConfigStore is a minimal AutonomousAPI fake whose
// Config()/SetConfig() behave like the real autonomous.Manager's: an
// incoming SetConfig patch merges onto whatever was already stored
// (not a fresh zero-valued map), same contract the real manager
// documents (v8.36.9) and the same thing api.go's own PUT /api/config
// -> manager sync direction already relies on.
type fakeAutonomousConfigStore struct {
	fakeOrchAutonomous
	cfg map[string]any
}

func (f *fakeAutonomousConfigStore) Config() any { return f.cfg }

func (f *fakeAutonomousConfigStore) SetConfig(v any) error {
	raw, ok := v.(json.RawMessage)
	if !ok {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		raw = b
	}
	var patch map[string]any
	if err := json.Unmarshal(raw, &patch); err != nil {
		return err
	}
	if f.cfg == nil {
		f.cfg = map[string]any{}
	}
	for k, val := range patch {
		f.cfg[k] = val
	}
	return nil
}

func newAutonomousConfigSyncServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	hub := NewHub()
	cfg := &config.Config{}
	srv := NewServer(hub, sm, "h", "", nil, cfg, cfgPath)
	srv.SetAutonomousAPI(&fakeAutonomousConfigStore{})
	return srv, cfgPath
}

func TestHandleAutonomousConfig_PUT_SyncsEnabledToServerConfig(t *testing.T) {
	srv, cfgPath := newAutonomousConfigSyncServer(t)

	req := httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(`{"enabled":true}`))
	w := httptest.NewRecorder()
	srv.handleAutonomousConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: status %d body: %s", w.Code, w.Body.String())
	}

	if !srv.cfg.Autonomous.Enabled {
		t.Fatal("s.cfg.Autonomous.Enabled was not synced after PUT /api/autonomous/config — GET /api/config would still report false")
	}

	// Must also persist to disk, not just update the in-memory copy —
	// otherwise a daemon restart silently reverts the operator's change.
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config.yaml was not written: %v", err)
	}
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload saved config: %v", err)
	}
	if !saved.Autonomous.Enabled {
		t.Fatal("config.yaml on disk does not reflect autonomous.enabled=true")
	}
}

func TestHandleAutonomousConfig_PUT_SyncsEnabledFalseToServerConfig(t *testing.T) {
	// Enabled has no `omitempty` specifically so an explicit false
	// round-trips correctly through the marshal/unmarshal merge — the
	// same class of bug already found live for other autonomous bool
	// fields (see Config's own AutoApproveChildren doc comment).
	srv, _ := newAutonomousConfigSyncServer(t)
	srv.cfg.Autonomous.Enabled = true

	req := httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(`{"enabled":false}`))
	w := httptest.NewRecorder()
	srv.handleAutonomousConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: status %d body: %s", w.Code, w.Body.String())
	}
	if srv.cfg.Autonomous.Enabled {
		t.Fatal("s.cfg.Autonomous.Enabled should have been synced to false")
	}
}
