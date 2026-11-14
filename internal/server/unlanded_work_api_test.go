// Tests for the Housekeeping action-list routes: empty states read as empty
// lists, each action reaches the Manager with the right arguments, delete
// needs confirm, and errors map to the right status.

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

type unlandedSpy struct {
	fakeOrchAutonomous
	list       []autonomous.UnlandedBranch
	err        error
	kept       string
	archived   string
	deleted    string
	confirm    bool
	diff       string
	commitMsg  string
	fixedPaths []string
}

func (u *unlandedSpy) UnlandedWork(context.Context) ([]autonomous.UnlandedBranch, error) {
	return u.list, u.err
}
func (u *unlandedSpy) KeepUnlanded(_ context.Context, b, _ string) error { u.kept = b; return u.err }
func (u *unlandedSpy) ArchiveUnlanded(_ context.Context, b string) (string, error) {
	u.archived = b
	return "refs/archive/" + b, u.err
}
func (u *unlandedSpy) DeleteUnlandedWorktree(_ context.Context, b string, c bool) (autonomous.DeleteWorktreeResult, error) {
	u.deleted, u.confirm = b, c
	return autonomous.DeleteWorktreeResult{Branch: b, BundlePath: "/x.bundle"}, u.err
}
func (u *unlandedSpy) UncommittedWorkDiff(context.Context, string) (string, error) {
	return u.diff, u.err
}
func (u *unlandedSpy) CommitUncommittedWork(_ context.Context, _, m string) (string, error) {
	u.commitMsg = m
	return "abc123", u.err
}
func (u *unlandedSpy) FixPermissions(p []string) (int, error) { u.fixedPaths = p; return 4, u.err }

func hkDo(s *Server, h http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

func TestUnlandedWorkListEmptyIsList(t *testing.T) {
	s := &Server{autonomousMgr: &unlandedSpy{}}
	rec := hkDo(s, s.handleUnlandedWork, http.MethodGet, "/api/autonomous/unlanded-work", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"branches":[]`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}

func TestUnlandedWorkListReturnsBranches(t *testing.T) {
	spy := &unlandedSpy{list: []autonomous.UnlandedBranch{{Branch: "automaton/x", Recommendation: autonomous.UnlandedLanded}}}
	s := &Server{autonomousMgr: spy}
	rec := hkDo(s, s.handleUnlandedWork, http.MethodGet, "/", "")
	if !strings.Contains(rec.Body.String(), "automaton/x") {
		t.Fatalf("missing branch: %s", rec.Body.String())
	}
}

func TestHousekeepingGates(t *testing.T) {
	s := &Server{}
	if rec := hkDo(s, s.handleUnlandedWork, http.MethodGet, "/", ""); rec.Code != 503 {
		t.Fatalf("disabled: %d", rec.Code)
	}
	s = &Server{autonomousMgr: &unlandedSpy{}}
	if rec := hkDo(s, s.handleUnlandedWork, http.MethodPost, "/", ""); rec.Code != 405 {
		t.Fatalf("method: %d", rec.Code)
	}
	if rec := hkDo(s, s.handleUncommittedCommit, http.MethodGet, "/", ""); rec.Code != 405 {
		t.Fatalf("commit method: %d", rec.Code)
	}
	s = &Server{autonomousMgr: &fakeOrchAutonomous{}}
	if rec := hkDo(s, s.handleUnlandedWork, http.MethodGet, "/", ""); rec.Code != 503 {
		t.Fatalf("no surface: %d", rec.Code)
	}
}

func TestUnlandedActions(t *testing.T) {
	spy := &unlandedSpy{}
	s := &Server{autonomousMgr: spy}
	if rec := hkDo(s, s.handleUnlandedAction("keep"), http.MethodPost, "/", `{"branch":"b1","note":"n"}`); rec.Code != 200 || spy.kept != "b1" {
		t.Fatalf("keep: %d %s", rec.Code, rec.Body.String())
	}
	if rec := hkDo(s, s.handleUnlandedAction("archive"), http.MethodPost, "/", `{"branch":"b2"}`); rec.Code != 200 || spy.archived != "b2" || !strings.Contains(rec.Body.String(), "refs/archive/b2") {
		t.Fatalf("archive: %d %s", rec.Code, rec.Body.String())
	}
	if rec := hkDo(s, s.handleUnlandedAction("keep"), http.MethodPost, "/", `{}`); rec.Code != 400 {
		t.Fatalf("missing branch: %d", rec.Code)
	}
	if rec := hkDo(s, s.handleUnlandedAction("keep"), http.MethodPost, "/", `{bad`); rec.Code != 400 {
		t.Fatalf("bad json: %d", rec.Code)
	}
}

func TestDeleteWorktreeNeedsConfirm(t *testing.T) {
	spy := &unlandedSpy{}
	s := &Server{autonomousMgr: spy}
	h := s.handleUnlandedAction("delete")
	if rec := hkDo(s, h, http.MethodPost, "/", `{"branch":"b"}`); rec.Code != 400 || spy.deleted != "" {
		t.Fatalf("unconfirmed delete reached manager: %d", rec.Code)
	}
	if rec := hkDo(s, h, http.MethodPost, "/", `{"branch":"b","confirm":true}`); rec.Code != 200 || spy.deleted != "b" || !spy.confirm {
		t.Fatalf("confirmed delete: %d %s", rec.Code, rec.Body.String())
	}
}

func TestHousekeepingErrorStatuses(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{autonomous.ErrUnlandedNotListed, 404},
		{autonomous.ErrNoBaseRepo, 400},
		{autonomous.ErrUncommittedOutsideBase, 400},
		{errors.New("git failed"), 409},
	}
	for _, c := range cases {
		s := &Server{autonomousMgr: &unlandedSpy{err: c.err}}
		rec := hkDo(s, s.handleUnlandedAction("archive"), http.MethodPost, "/", `{"branch":"b"}`)
		if rec.Code != c.want {
			t.Errorf("%v: got %d want %d", c.err, rec.Code, c.want)
		}
	}
}

func TestUncommittedDiffAndCommit(t *testing.T) {
	spy := &unlandedSpy{diff: ""}
	s := &Server{autonomousMgr: spy}
	rec := hkDo(s, s.handleUncommittedDiff, http.MethodGet, "/api/autonomous/uncommitted-work/diff?worktree=/w", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"clean":true`) {
		t.Fatalf("diff: %d %s", rec.Code, rec.Body.String())
	}
	if rec := hkDo(s, s.handleUncommittedDiff, http.MethodGet, "/", ""); rec.Code != 400 {
		t.Fatalf("diff without worktree: %d", rec.Code)
	}
	rec = hkDo(s, s.handleUncommittedCommit, http.MethodPost, "/", `{"worktree":"/w","message":"save work"}`)
	if rec.Code != 200 || spy.commitMsg != "save work" || !strings.Contains(rec.Body.String(), "abc123") {
		t.Fatalf("commit: %d %s", rec.Code, rec.Body.String())
	}
	if rec := hkDo(s, s.handleUncommittedCommit, http.MethodPost, "/", `{"message":"m"}`); rec.Code != 400 {
		t.Fatalf("commit without worktree: %d", rec.Code)
	}
	spy.err = autonomous.ErrUncommittedEmptyMessage
	if rec := hkDo(s, s.handleUncommittedCommit, http.MethodPost, "/", `{"worktree":"/w"}`); rec.Code != 400 {
		t.Fatalf("empty message: %d", rec.Code)
	}
}

func TestFixPermissions(t *testing.T) {
	spy := &unlandedSpy{}
	s := &Server{autonomousMgr: spy}
	rec := hkDo(s, s.handleFixPermissions, http.MethodPost, "/", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"changed":4`) || len(spy.fixedPaths) != 0 {
		t.Fatalf("empty body: %d %s", rec.Code, rec.Body.String())
	}
	hkDo(s, s.handleFixPermissions, http.MethodPost, "/", `{"paths":["/a","/b"]}`)
	if len(spy.fixedPaths) != 2 {
		t.Fatalf("paths: %v", spy.fixedPaths)
	}
	spy.err = errors.New("outside the worktree base")
	if rec := hkDo(s, s.handleFixPermissions, http.MethodPost, "/", ""); rec.Code != 409 {
		t.Fatalf("error: %d", rec.Code)
	}
}

func TestManagerHousekeepingEmptyWithoutRepo(t *testing.T) {
	var m *autonomous.Manager
	list, err := m.UnlandedWork(context.Background())
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("nil manager: %v %v", list, err)
	}
}

// TestRealAPIWrapperServesUnlanded drives the route through the production
// wiring: the gate type-asserts AutonomousAPI (*autonomous.API wrapping the
// real *Manager) to the unlanded surface, which it did not implement before
// the delegating methods landed and every one of these reads returned 503.
func TestRealAPIWrapperServesUnlanded(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "Unlanded Server Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "unlanded@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Unlanded Server Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "unlanded@example.invalid")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	repo := t.TempDir()
	rgit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	w := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rgit("init", "-q", "-b", "main")
	w("base.txt", "base\n")
	rgit("add", "base.txt")
	rgit("commit", "-q", "-m", "base")

	// landed: the change also lands on main with a different subject.
	rgit("checkout", "-q", "-b", "automaton/uwl01")
	w("done.txt", "same\n")
	rgit("add", "done.txt")
	rgit("commit", "-q", "-m", "work A")
	rgit("checkout", "-q", "main")
	w("done.txt", "same\n")
	rgit("add", "done.txt")
	rgit("commit", "-q", "-m", "merge of A")

	// needs_decision: work not on main, nothing replaces it, no verdict filed.
	rgit("checkout", "-q", "-b", "automaton/uwl02")
	w("fresh.txt", "fresh\n")
	rgit("add", "fresh.txt")
	rgit("commit", "-q", "-m", "work B")
	rgit("checkout", "-q", "main")

	// superseded: main rewrote the file after the branch forked.
	rgit("checkout", "-q", "-b", "automaton/uwl03")
	w("shared.txt", "branch\n")
	rgit("add", "shared.txt")
	rgit("commit", "-q", "-m", "work C")
	rgit("checkout", "-q", "main")
	w("shared.txt", "main rewrote\n")
	rgit("add", "shared.txt")
	rgit("commit", "-q", "-m", "replace")

	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	cfg := mgr.Config()
	cfg.WorktreeBaseRepo = repo
	mgr.SetConfig(cfg)
	for _, br := range []string{"automaton/uwl01", "automaton/uwl02", "automaton/uwl03"} {
		p, err := mgr.CreatePRD("unlanded fixture work", t.TempDir(), "", "", autonomous.EffortNormal)
		if err != nil {
			t.Fatalf("CreatePRD: %v", err)
		}
		p.Status = autonomous.PRDArchived
		p.Git.Branch = br
		if err := mgr.Store().SavePRD(p); err != nil {
			t.Fatalf("SavePRD: %v", err)
		}
	}

	s := &Server{autonomousMgr: autonomous.NewAPI(mgr)}
	rec := hkDo(s, s.handleUnlandedWork, http.MethodGet, "/api/autonomous/unlanded-work", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("real API wrapper: status = %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"merge_state":"needs_decision"`, `"merge_state":"landed"`, `"merge_state":"superseded"`,
		"re-judge first", "delete worktree", "archive branch", "automaton/uwl01",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %s:\n%s", want, body)
		}
	}
}
