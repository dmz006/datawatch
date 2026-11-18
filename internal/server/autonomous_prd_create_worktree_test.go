// Found live 2026-10-11 while creating BL404's PRD: the create-PRD
// validation (v5.26.19) hard-required project_dir or project_profile,
// unaware that BL407 Phase 1's worktree auto-assignment covers an
// empty project_dir at Run() time when autonomous.worktree_base_repo
// is configured. Fixed by loosening the creation-time check for
// exactly that case.

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

type createPRDSpy struct {
	fakeOrchAutonomous
	called     bool
	projectDir string
}

func (s *createPRDSpy) CreatePRDWithParent(string, string, string, string, string, string) (any, error) {
	return s.CreatePRD("", "", "", "", "")
}

func (s *createPRDSpy) CreatePRD(spec, projectDir, backend, model, effort string) (any, error) {
	s.called = true
	s.projectDir = projectDir
	return map[string]any{"id": "prd-1", "status": "draft"}, nil
}

func TestCreatePRD_EmptyProjectDir_RejectedWhenWorktreeModeNotConfigured(t *testing.T) {
	spy := &createPRDSpy{}
	s := &Server{autonomousMgr: spy, cfg: &config.Config{}}
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds", strings.NewReader(`{"spec":"do something"}`))
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (no work source, no worktree mode), got %d body=%s", rr.Code, rr.Body.String())
	}
	if spy.called {
		t.Fatal("CreatePRD must not be called when rejected")
	}
}

func TestCreatePRD_EmptyProjectDir_AllowedWhenWorktreeModeConfigured(t *testing.T) {
	spy := &createPRDSpy{}
	s := &Server{
		autonomousMgr: spy,
		cfg: &config.Config{
			Autonomous: config.AutonomousConfig{WorktreeBaseRepo: "/home/<user>/myrepo"},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds", strings.NewReader(`{"spec":"do something"}`))
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200 (worktree mode covers empty project_dir), got %d body=%s", rr.Code, rr.Body.String())
	}
	if !spy.called {
		t.Fatal("CreatePRD must be called")
	}
	if spy.projectDir != "" {
		t.Errorf("project_dir must still be passed through empty (Run() backfills it), got %q", spy.projectDir)
	}
}

func TestCreatePRD_EmptyProjectDir_ClusterProfileAloneStillRejected(t *testing.T) {
	spy := &createPRDSpy{}
	s := &Server{
		autonomousMgr: spy,
		cfg: &config.Config{
			Autonomous: config.AutonomousConfig{WorktreeBaseRepo: "/home/<user>/myrepo"},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds", strings.NewReader(`{"spec":"do something","cluster_profile":"k8s-default"}`))
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("cluster_profile alone must still need project_profile even with worktree mode configured, got %d body=%s", rr.Code, rr.Body.String())
	}
	if spy.called {
		t.Fatal("CreatePRD must not be called when rejected")
	}
}

func TestCreatePRD_ExplicitProjectDir_UnaffectedByWorktreeConfig(t *testing.T) {
	spy := &createPRDSpy{}
	s := &Server{
		autonomousMgr: spy,
		cfg: &config.Config{
			Autonomous: config.AutonomousConfig{WorktreeBaseRepo: "/home/<user>/myrepo"},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds", strings.NewReader(`{"spec":"do something","project_dir":"/explicit/dir"}`))
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if spy.projectDir != "/explicit/dir" {
		t.Errorf("explicit project_dir must pass through unchanged, got %q", spy.projectDir)
	}
}
