package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// reapRefT1 is a realistic backup ref name for the fixture branch.
const reapRefT1 = "refs/datawatch/reap-backup/automaton-prd-a--tasks-fix-t1/1757220000"

// reapSpy is the double for the branch reaper surface: a fixed preview, a
// fixed pass result and a fixed restore, with call counts so a refused
// request is proven never to reach the manager.
type reapSpy struct {
	fakeOrchAutonomous
	preview      autonomous.ReapReport
	backups      []autonomous.ReapBackup
	previewErr   error
	run          autonomous.ReapReport
	runErr       error
	restoreRef   string
	restoreSHA   string
	restoreErr   error
	previewCalls int
	runCalls     int
	restoreCalls int
}

func (s *reapSpy) ReapPreview(context.Context) (autonomous.ReapReport, []autonomous.ReapBackup, error) {
	s.previewCalls++
	return s.preview, s.backups, s.previewErr
}

func (s *reapSpy) ReapRun(context.Context) (autonomous.ReapReport, error) {
	s.runCalls++
	return s.run, s.runErr
}

func (s *reapSpy) ReapRestore(context.Context, string) (string, string, error) {
	s.restoreCalls++
	return s.restoreRef, s.restoreSHA, s.restoreErr
}

func reapFixture() (autonomous.ReapReport, []autonomous.ReapBackup) {
	rep := autonomous.ReapReport{
		Reaped: []autonomous.ReapEntry{
			{AutomatonID: "prd-a", Branch: "automaton/prd-a--tasks-fix-t1",
				BackupRef: reapRefT1, ContainedIn: "main",
				Reason: "would be reaped: every commit of automaton/prd-a--tasks-fix-t1 is on main"},
		},
		Waiting: []autonomous.ReapEntry{
			{AutomatonID: "prd-b", Branch: "automaton/prd-b--tasks-keep",
				Reason: "1 commit of automaton/prd-b--tasks-keep is not in main"},
		},
	}
	backups := []autonomous.ReapBackup{
		{Branch: "automaton/prd-a--tasks-fix-t1", Ref: reapRefT1, SHA: "abc123"},
	}
	return rep, backups
}

func reapCall(t *testing.T, s *Server, method, path, body string) (int, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	switch path {
	case "/api/autonomous/branch-reap":
		s.handleAutonomousBranchReap(rec, req)
	case "/api/autonomous/branch-reap/run":
		s.handleAutonomousBranchReapRun(rec, req)
	case "/api/autonomous/branch-reap/restore":
		s.handleAutonomousBranchReapRestore(rec, req)
	default:
		t.Fatalf("unknown branch reap route %s", path)
	}
	return rec.Code, rec
}

func reapGetKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not a JSON object: %v: %s", err, raw)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestBranchReapReport_ServesPreviewWithBackupsAndDays(t *testing.T) {
	rep, backups := reapFixture()
	spy := &reapSpy{preview: rep, backups: backups}
	s := &Server{
		autonomousMgr: spy,
		cfg:           &config.Config{Autonomous: config.AutonomousConfig{}}, // unset reads 30
	}
	code, rec := reapCall(t, s, http.MethodGet, "/api/autonomous/branch-reap", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", code, rec.Body)
	}
	if got := reapGetKeys(t, rec.Body.Bytes()); strings.Join(got, ",") != "backup_days,backups,computed_at,reaped,waiting" {
		t.Fatalf("top-level keys = %v, want backup_days,backups,computed_at,reaped,waiting", got)
	}
	var body autonomous.ReapSurfaceReport
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if body.BackupDays != 30 {
		t.Fatalf("backup_days = %d, want 30 (unset reads the default)", body.BackupDays)
	}
	if len(body.Backups) != 1 {
		t.Fatalf("backups = %v, want one entry", body.Backups)
	}
	if body.Backups[0].Ref != reapRefT1 || body.Backups[0].SHA != "abc123" || body.Backups[0].Branch != "automaton/prd-a--tasks-fix-t1" {
		t.Fatalf("backup entry = %+v, want ref/sha/branch of the fixture", body.Backups[0])
	}
	if len(body.Reaped) != 1 || len(body.Waiting) != 1 {
		t.Fatalf("report split = %d reaped / %d waiting, want one of each", len(body.Reaped), len(body.Waiting))
	}
	if body.Reaped[0].Branch != "automaton/prd-a--tasks-fix-t1" || body.Reaped[0].BackupRef != reapRefT1 {
		t.Fatalf("reaped entry = %+v, want the fixture branch with its backup ref", body.Reaped[0])
	}
	if spy.previewCalls != 1 {
		t.Fatalf("preview calls = %d, want 1", spy.previewCalls)
	}
}

func TestBranchReapReport_Refusals(t *testing.T) {
	t.Run("method refusal", func(t *testing.T) {
		spy := &reapSpy{}
		rec := httptest.NewRecorder()
		s := &Server{autonomousMgr: spy}
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/branch-reap", nil)
		s.handleAutonomousBranchReap(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != http.MethodGet {
			t.Fatalf("Allow = %q, want GET", got)
		}
		if spy.previewCalls != 0 {
			t.Fatalf("the refused call reached the manager (%d calls)", spy.previewCalls)
		}
	})
	t.Run("no manager installed", func(t *testing.T) {
		s := &Server{autonomousMgr: nil}
		req := httptest.NewRequest(http.MethodGet, "/api/autonomous/branch-reap", nil)
		rec := httptest.NewRecorder()
		s.handleAutonomousBranchReap(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})
	t.Run("no base repo is configured", func(t *testing.T) {
		spy := &reapSpy{previewErr: autonomous.ErrMergeNoBaseRepo}
		s := &Server{autonomousMgr: spy}
		code, rec := reapCall(t, s, http.MethodGet, "/api/autonomous/branch-reap", "")
		if code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503: %s", code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), "base repo") {
			t.Fatalf("body does not say the base repo is missing: %s", rec.Body)
		}
	})
}

func TestBranchReapRun_ServesThePass(t *testing.T) {
	rep, _ := reapFixture()
	rep.Reaped[0].Reason = "every commit of automaton/prd-a--tasks-fix-t1 is on main"
	rep.Reaped[0].Commits = 2
	spy := &reapSpy{run: rep}
	s := &Server{autonomousMgr: spy}
	code, rec := reapCall(t, s, http.MethodPost, "/api/autonomous/branch-reap/run", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", code, rec.Body)
	}
	keys := reapGetKeys(t, rec.Body.Bytes())
	if strings.Join(keys, ",") != "reaped,waiting" {
		t.Fatalf("run body keys = %v, want reaped,waiting", keys)
	}
	var body autonomous.ReapReport
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if len(body.Reaped) != 1 || body.Reaped[0].BackupRef != reapRefT1 {
		t.Fatalf("reaped = %+v, want the fixture branch with its backup ref", body.Reaped)
	}
	if spy.runCalls != 1 {
		t.Fatalf("run calls = %d, want 1", spy.runCalls)
	}
}

func TestBranchReapRun_Refusals(t *testing.T) {
	t.Run("method refusal", func(t *testing.T) {
		spy := &reapSpy{}
		rec := httptest.NewRecorder()
		s := &Server{autonomousMgr: spy}
		req := httptest.NewRequest(http.MethodGet, "/api/autonomous/branch-reap/run", nil)
		s.handleAutonomousBranchReapRun(rec, req)
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
			t.Fatalf("status = %d allow %q, want 405 with Allow POST", rec.Code, rec.Header().Get("Allow"))
		}
	})
	t.Run("a body is rejected", func(t *testing.T) {
		spy := &reapSpy{}
		s := &Server{autonomousMgr: spy}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/branch-reap/run", strings.NewReader("{}"))
		s.handleAutonomousBranchReapRun(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		if spy.runCalls != 0 {
			t.Fatalf("the refused call reached the manager (%d calls)", spy.runCalls)
		}
	})
	t.Run("already running answers 409", func(t *testing.T) {
		spy := &reapSpy{runErr: autonomous.ErrReapBusy}
		s := &Server{autonomousMgr: spy}
		code, rec := reapCall(t, s, http.MethodPost, "/api/autonomous/branch-reap/run", "")
		if code != http.StatusConflict {
			t.Fatalf("status = %d, want 409: %s", code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), "already running") {
			t.Fatalf("body does not say a pass is already running: %s", rec.Body)
		}
	})
	t.Run("no base repo answers 503", func(t *testing.T) {
		spy := &reapSpy{runErr: autonomous.ErrMergeNoBaseRepo}
		s := &Server{autonomousMgr: spy}
		code, _ := reapCall(t, s, http.MethodPost, "/api/autonomous/branch-reap/run", "")
		if code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", code)
		}
	})
}

func TestBranchReapRestore_Serves(t *testing.T) {
	spy := &reapSpy{restoreRef: reapRefT1, restoreSHA: "abc123"}
	s := &Server{autonomousMgr: spy}
	code, rec := reapCall(t, s, http.MethodPost, "/api/autonomous/branch-reap/restore", `{"branch":"automaton/prd-a--tasks-fix-t1"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", code, rec.Body)
	}
	var res autonomous.ReapRestoreResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if res.Branch != "automaton/prd-a--tasks-fix-t1" || res.BackupRef != reapRefT1 || res.SHA != "abc123" {
		t.Fatalf("restore result = %+v, want branch/backup ref/commit of the fixture", res)
	}
	if spy.restoreCalls != 1 {
		t.Fatalf("restore calls = %d, want 1", spy.restoreCalls)
	}
}

func TestBranchReapRestore_Refusals(t *testing.T) {
	t.Run("method refusal", func(t *testing.T) {
		spy := &reapSpy{}
		rec := httptest.NewRecorder()
		s := &Server{autonomousMgr: spy}
		req := httptest.NewRequest(http.MethodGet, "/api/autonomous/branch-reap/restore", nil)
		s.handleAutonomousBranchReapRestore(rec, req)
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
			t.Fatalf("status = %d allow %q, want 405 with Allow POST", rec.Code, rec.Header().Get("Allow"))
		}
	})
	t.Run("an empty body is rejected", func(t *testing.T) {
		spy := &reapSpy{}
		s := &Server{autonomousMgr: spy}
		code, _ := reapCall(t, s, http.MethodPost, "/api/autonomous/branch-reap/restore", "")
		if code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", code)
		}
		if spy.restoreCalls != 0 {
			t.Fatalf("the refused call reached the manager (%d calls)", spy.restoreCalls)
		}
	})
	t.Run("a branchless body is rejected", func(t *testing.T) {
		spy := &reapSpy{}
		s := &Server{autonomousMgr: spy}
		code, _ := reapCall(t, s, http.MethodPost, "/api/autonomous/branch-reap/restore", `{"other":"x"}`)
		if code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", code)
		}
	})
	t.Run("never reaped answers 404", func(t *testing.T) {
		spy := &reapSpy{restoreErr: autonomous.ErrNoReapBackup}
		s := &Server{autonomousMgr: spy}
		code, rec := reapCall(t, s, http.MethodPost, "/api/autonomous/branch-reap/restore", `{"branch":"automaton/prd-z--tasks-gone"}`)
		if code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), "no backup ref") {
			t.Fatalf("body does not say the branch was never reaped: %s", rec.Body)
		}
	})
	t.Run("no base repo answers 503", func(t *testing.T) {
		spy := &reapSpy{restoreErr: autonomous.ErrMergeNoBaseRepo}
		s := &Server{autonomousMgr: spy}
		code, _ := reapCall(t, s, http.MethodPost, "/api/autonomous/branch-reap/restore", `{"branch":"b"}`)
		if code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", code)
		}
	})
}
