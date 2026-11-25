package server

// The daemon wires *autonomous.API (not *Manager) into the server. Each route
// that depends on an optional interface of that object is driven through it
// here; when a method is missing the route answers 503 or drops data.

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/pause"
)

func liveAPIServer(t *testing.T, baseRepo string) (*Server, *autonomous.Manager) {
	t.Helper()
	cfg := autonomous.DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	mgr, err := autonomous.NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	dir := t.TempDir()
	return &Server{autonomousMgr: autonomous.NewAPI(mgr), cfg: &config.Config{DataDir: dir}}, mgr
}

func liveDo(h http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rr
}

func TestLiveAPI_GateRepairReadAndRun(t *testing.T) {
	s, _ := liveAPIServer(t, "")
	for name, h := range map[string]http.HandlerFunc{"gate-repairs": s.handleGateRepairs, "gate-repair": s.handleGateRepair} {
		if rr := liveDo(h, http.MethodGet, "/api/autonomous/"+name, ""); rr.Code != http.StatusOK {
			t.Errorf("GET %s: %d %s", name, rr.Code, rr.Body.String())
		}
	}
	if rr := liveDo(s.handleGateRepairRun, http.MethodPost, "/api/autonomous/gate-repair/run", ""); rr.Code != http.StatusOK {
		t.Errorf("POST gate-repair/run: %d %s", rr.Code, rr.Body.String())
	}
}

func TestLiveAPI_MergeAndDeployRunPassTheInterfaceGate(t *testing.T) {
	s, _ := liveAPIServer(t, "")
	s.cfg.Autonomous.MergeDeployRunEnabled = true
	for name, h := range map[string]http.HandlerFunc{"merge/run": s.handleAutonomousMergeRun, "deploy/run": s.handleAutonomousDeployRun} {
		rr := liveDo(h, http.MethodPost, "/api/autonomous/"+name, `{}`)
		// With no base repository the route must say so, not "unavailable".
		if !strings.Contains(rr.Body.String(), autonomous.ErrMergeNoBaseRepo.Error()) {
			t.Errorf("POST %s: %d %s", name, rr.Code, rr.Body.String())
		}
	}
}

// Wiring these routes must not start work the operator paused.
func TestLiveAPI_MergeAndDeployRunWaitForTheOperatorPause(t *testing.T) {
	repo := t.TempDir()
	for _, scope := range []pause.Scope{pause.ScopeGlobal, pause.ScopeProject} {
		s, _ := liveAPIServer(t, repo)
		ps, err := pause.Open(filepath.Join(t.TempDir(), "pause.json"), false, nil)
		if err != nil {
			t.Fatal(err)
		}
		project := ""
		if scope == pause.ScopeProject {
			project = repo
		}
		if _, err := ps.Pause(scope, project, "test pause", "operator"); err != nil {
			t.Fatalf("pause: %v", err)
		}
		s.SetPause(ps)
		s.cfg.Autonomous.MergeDeployRunEnabled = true
		for name, h := range map[string]http.HandlerFunc{"merge/run": s.handleAutonomousMergeRun, "deploy/run": s.handleAutonomousDeployRun} {
			rr := liveDo(h, http.MethodPost, "/api/autonomous/"+name, `{}`)
			if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "test pause") {
				t.Errorf("%s pause, POST %s: want 409 naming the pause, got %d %s", scope, name, rr.Code, rr.Body.String())
			}
		}
		if !s.deferredDeployRetryHeld(repo) {
			t.Errorf("%s pause: the deferred deploy retry at start is not held", scope)
		}
	}
	s, _ := liveAPIServer(t, repo)
	if s.deferredDeployRetryHeld(repo) {
		t.Error("the deferred deploy retry is held with no pause")
	}
}

func TestLiveAPI_EventPreviewReadsLifecycleHistory(t *testing.T) {
	s, _ := liveAPIServer(t, "")
	rr := liveDo(s.handleEventPreview, http.MethodPost, "/api/autonomous/subscriptions/preview", `{"kinds":["task.looping"]}`)
	if rr.Code != http.StatusOK {
		t.Errorf("preview: %d %s", rr.Code, rr.Body.String())
	}
}

func TestLiveAPI_ReleaseTagsReadTheBaseRepository(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "one"},
		{"tag", "v1.2.3"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	s, _ := liveAPIServer(t, repo)
	rr := liveDo(s.handleAutonomousReleaseTags, http.MethodGet, "/api/autonomous/release-tags", "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "v1.2.3") {
		t.Errorf("release tags: %d %s", rr.Code, rr.Body.String())
	}
}

func TestLiveAPI_SessionStatusCarriesTheMemoryCeiling(t *testing.T) {
	s, _ := liveAPIServer(t, "")
	if s.sessionMemoryCeiling() == nil {
		t.Error("the session status has no memory ceiling block")
	}
}
