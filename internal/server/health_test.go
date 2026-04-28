package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// fakeMemAPI implements MemoryAPI just enough to satisfy /readyz.
type fakeMemAPI struct {
	statsErr  bool   // panic out of Stats() to simulate dead backend
	statsNil  bool   // return nil to simulate degraded backend
	statsBody map[string]interface{}
}

func (f *fakeMemAPI) Stats() map[string]interface{} {
	if f.statsErr {
		panic("simulated stats failure")
	}
	if f.statsNil {
		return nil
	}
	if f.statsBody != nil {
		return f.statsBody
	}
	return map[string]interface{}{"backend": "fake"}
}
func (f *fakeMemAPI) ListRecent(string, int) ([]map[string]interface{}, error) { return nil, nil }
func (f *fakeMemAPI) ListFiltered(string, string, string, int) ([]map[string]interface{}, error) {
	return nil, nil
}
func (f *fakeMemAPI) Search(string, int) ([]map[string]interface{}, error)   { return nil, nil }
func (f *fakeMemAPI) SearchInNamespaces(string, []string, int) ([]map[string]interface{}, error) {
	return nil, nil
}
func (f *fakeMemAPI) Delete(int64) error                     { return nil }
func (f *fakeMemAPI) SetPinned(int64, bool) error            { return nil }
func (f *fakeMemAPI) Remember(string, string) (int64, error) { return 0, nil }
func (f *fakeMemAPI) Export(io.Writer) error                 { return nil }
func (f *fakeMemAPI) Import(io.Reader) (int, error)          { return 0, nil }
func (f *fakeMemAPI) WALRecent(int) []map[string]interface{}                       { return nil }
func (f *fakeMemAPI) Reindex() (int, error)                                        { return 0, nil }
func (f *fakeMemAPI) ListLearnings(string, string, int) ([]map[string]interface{}, error) {
	return nil, nil
}
func (f *fakeMemAPI) Research(string, int) ([]map[string]interface{}, error) { return nil, nil }

// newTestServer builds a Server with a real session.Manager rooted in t.TempDir
// and whatever MemoryAPI / mcpDocsFunc the caller wants to wire.
func newTestServer(t *testing.T, mem MemoryAPI, mcpFn func() interface{}) *Server {
	t.Helper()
	mgr, err := session.NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	s := &Server{
		manager:     mgr,
		hostname:    "testhost",
		memoryAPI:   mem,
		mcpDocsFunc: mcpFn,
	}
	return s
}

func decodeReadyz(t *testing.T, rr *httptest.ResponseRecorder) (status string, subs map[string]map[string]string) {
	t.Helper()
	var body struct {
		Status     string                       `json:"status"`
		Subsystems map[string]map[string]string `json:"subsystems"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rr.Body.String())
	}
	return body.Status, body.Subsystems
}

func TestHealthz_AlwaysOK(t *testing.T) {
	s := newTestServer(t, nil, nil)
	rr := httptest.NewRecorder()
	s.handleHealthz(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rr.Code)
	}
}

func TestReadyz_AllOK_OnlyManager(t *testing.T) {
	s := newTestServer(t, nil, nil)
	rr := httptest.NewRecorder()
	s.handleReadyz(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	status, subs := decodeReadyz(t, rr)
	if status != "ready" {
		t.Errorf("status=%q want ready", status)
	}
	if subs["manager"]["status"] != "ok" {
		t.Errorf("manager status=%q want ok", subs["manager"]["status"])
	}
	if subs["memory"]["status"] != "disabled" {
		t.Errorf("memory status=%q want disabled (no mem wired)", subs["memory"]["status"])
	}
	if subs["mcp"]["status"] != "disabled" {
		t.Errorf("mcp status=%q want disabled (no mcp fn)", subs["mcp"]["status"])
	}
}

func TestReadyz_MemoryEnabled_OK(t *testing.T) {
	s := newTestServer(t, &fakeMemAPI{}, func() interface{} { return nil })
	rr := httptest.NewRecorder()
	s.handleReadyz(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	_, subs := decodeReadyz(t, rr)
	if subs["memory"]["status"] != "ok" {
		t.Errorf("memory status=%q want ok", subs["memory"]["status"])
	}
	if subs["mcp"]["status"] != "ok" {
		t.Errorf("mcp status=%q want ok", subs["mcp"]["status"])
	}
}

func TestReadyz_MemoryStatsPanic_Down(t *testing.T) {
	s := newTestServer(t, &fakeMemAPI{statsErr: true}, nil)
	rr := httptest.NewRecorder()
	s.handleReadyz(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503", rr.Code)
	}
	status, subs := decodeReadyz(t, rr)
	if status != "not_ready" {
		t.Errorf("status=%q want not_ready", status)
	}
	if subs["memory"]["status"] != "down" {
		t.Errorf("memory status=%q want down", subs["memory"]["status"])
	}
	if subs["manager"]["status"] != "ok" {
		t.Errorf("manager should remain ok, got %q", subs["manager"]["status"])
	}
}

func TestReadyz_MemoryStatsNil_Down(t *testing.T) {
	s := newTestServer(t, &fakeMemAPI{statsNil: true}, nil)
	rr := httptest.NewRecorder()
	s.handleReadyz(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503 (nil stats)", rr.Code)
	}
}

func TestReadyz_NoManager_Down(t *testing.T) {
	s := &Server{} // deliberately uninitialized
	rr := httptest.NewRecorder()
	s.handleReadyz(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503 (no manager)", rr.Code)
	}
	_, subs := decodeReadyz(t, rr)
	if subs["manager"]["status"] != "down" {
		t.Errorf("manager status=%q want down", subs["manager"]["status"])
	}
}

func TestGetConfig_ExposesWorkspaceRoot(t *testing.T) {
	// F10 audit: ensure workspace_root is readable via /api/config so it
	// has parity with default_project_dir and other session fields.
	s := newTestServer(t, nil, nil)
	s.cfg = &config.Config{}
	s.cfg.Session.WorkspaceRoot = "/workspace"
	s.cfg.Hostname = "h"

	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sess, ok := body["session"].(map[string]interface{})
	if !ok {
		t.Fatalf("session block missing: %v", body)
	}
	if sess["workspace_root"] != "/workspace" {
		t.Errorf("workspace_root=%v want /workspace", sess["workspace_root"])
	}
	// Sanity: the analogous default_project_dir field is also there
	if _, ok := sess["default_project_dir"]; !ok {
		t.Errorf("default_project_dir missing — sanity check failed, GetConfig regressed")
	}
}

func TestReadyz_BodyIncludesUptimeAndVersion(t *testing.T) {
	s := newTestServer(t, nil, nil)
	rr := httptest.NewRecorder()
	s.handleReadyz(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	var body map[string]interface{}
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if _, ok := body["uptime_seconds"]; !ok {
		t.Errorf("missing uptime_seconds")
	}
	if body["version"] != Version {
		t.Errorf("version=%v want %s", body["version"], Version)
	}
	// uptime should be small but non-negative
	if u, ok := body["uptime_seconds"].(float64); !ok || u < 0 {
		t.Errorf("bad uptime: %v", body["uptime_seconds"])
	}
	_ = time.Now() // silence unused import if Manager ctor changes
}
