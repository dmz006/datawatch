// Tests for the Housekeeping action-list routes: empty states read as empty
// lists, each action reaches the Manager with the right arguments, delete
// needs confirm, and errors map to the right status.

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
