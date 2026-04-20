// BL35 — project summary endpoint tests.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func TestBL35_ProjectSummary_RejectsPost(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/project/summary?dir=/tmp", nil)
	rr := httptest.NewRecorder()
	s.handleProjectSummary(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestBL35_ProjectSummary_RequiresDir(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/project/summary", nil)
	rr := httptest.NewRecorder()
	s.handleProjectSummary(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 missing dir, got %d", rr.Code)
	}
}

func TestBL35_ProjectSummary_RequiresAbsoluteDir(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet,
		"/api/project/summary?dir=relative/path", nil)
	rr := httptest.NewRecorder()
	s.handleProjectSummary(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 for relative dir, got %d", rr.Code)
	}
}

func TestBL35_ProjectSummary_NoGitNoSessions(t *testing.T) {
	s := bl90Server(t)
	dir := t.TempDir()
	req := httptest.NewRequest(http.MethodGet,
		"/api/project/summary?dir="+dir, nil)
	rr := httptest.NewRecorder()
	s.handleProjectSummary(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got ProjectSummary
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.IsGitRepo {
		t.Errorf("empty tempdir should not be a git repo")
	}
	if got.Stats.TotalSessions != 0 {
		t.Errorf("expected 0 sessions, got %d", got.Stats.TotalSessions)
	}
}

func TestBL35_ProjectSummary_WithSessions(t *testing.T) {
	s := bl90Server(t)
	dir := t.TempDir()

	// Seed two sessions for this dir, one for elsewhere.
	_ = s.manager.SaveSession(&session.Session{
		ID: "aa", FullID: "h-aa", ProjectDir: dir,
		State: session.StateComplete, UpdatedAt: time.Now(),
	})
	_ = s.manager.SaveSession(&session.Session{
		ID: "bb", FullID: "h-bb", ProjectDir: dir,
		State: session.StateFailed, UpdatedAt: time.Now(),
	})
	_ = s.manager.SaveSession(&session.Session{
		ID: "cc", FullID: "h-cc", ProjectDir: "/other",
		State: session.StateComplete, UpdatedAt: time.Now(),
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/project/summary?dir="+dir, nil)
	rr := httptest.NewRecorder()
	s.handleProjectSummary(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got ProjectSummary
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Stats.TotalSessions != 2 {
		t.Errorf("total=%d want 2 (other dir's session must be excluded)", got.Stats.TotalSessions)
	}
	if got.Stats.Completed != 1 || got.Stats.Failed != 1 {
		t.Errorf("split mismatch: %+v", got.Stats)
	}
	if got.Stats.SuccessRate != 0.5 {
		t.Errorf("success_rate=%v want 0.5", got.Stats.SuccessRate)
	}
}

func TestBL35_ProjectSummary_GitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	mustGit := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	mustGit("init", "-q")
	mustGit("config", "user.email", "test@example.com")
	mustGit("config", "user.name", "test")
	mustGit("commit", "--allow-empty", "-m", "initial")

	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet,
		"/api/project/summary?dir="+dir, nil)
	rr := httptest.NewRecorder()
	s.handleProjectSummary(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got ProjectSummary
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if !got.IsGitRepo {
		t.Errorf("expected IsGitRepo=true, got false")
	}
	if got.Branch == "" {
		t.Errorf("expected non-empty branch")
	}
	if len(got.RecentCommits) == 0 {
		t.Errorf("expected at least one recent commit")
	}
}
