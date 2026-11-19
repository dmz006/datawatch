package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// archiveFake answers the archive routes; every other manager call would
// panic on the nil embedded interface, which the routes must never reach.
type archiveFake struct {
	AutonomousAPI
	runs    int
	planned int
	readErr error
	runErr  error
}

func (f *archiveFake) ReadAutomatonArchive(id string) (*autonomous.PRDArchive, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return &autonomous.PRDArchive{Version: 1, ID: id, Decisions: []autonomous.Decision{{Kind: "run"}}}, nil
}
func (f *archiveFake) PlanAutomatonArchive() autonomous.ArchivePlan {
	f.planned++
	return autonomous.ArchivePlan{DryRun: true, Eligible: []autonomous.ArchiveResult{{ID: "aa11"}}}
}
func (f *archiveFake) RunAutomatonArchive() (autonomous.ArchiveReport, error) {
	f.runs++
	return autonomous.ArchiveReport{Archived: []autonomous.ArchiveResult{{ID: "aa11"}}}, f.runErr
}

func archiveRequest(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/autonomous/prds/{id}/archive", s.handlePRDArchive)
	mux.HandleFunc("/api/autonomous/archive", s.handleAutonomousArchive)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestArchiveRESTReadOnAsk(t *testing.T) {
	f := &archiveFake{}
	s := &Server{autonomousMgr: f}
	rr := archiveRequest(t, s, http.MethodGet, "/api/autonomous/prds/aa11/archive", "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"id":"aa11"`) || !strings.Contains(rr.Body.String(), `"decisions"`) {
		t.Fatalf("GET: %d %s", rr.Code, rr.Body.String())
	}
	f.readErr = autonomous.ErrNotArchived
	if rr := archiveRequest(t, s, http.MethodGet, "/api/autonomous/prds/aa11/archive", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("not archived: %d", rr.Code)
	}
	f.readErr = errors.New("automaton zz not found")
	if rr := archiveRequest(t, s, http.MethodGet, "/api/autonomous/prds/zz/archive", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", rr.Code)
	}
	if rr := archiveRequest(t, s, http.MethodPost, "/api/autonomous/prds/aa11/archive", ""); rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST on the read route: %d", rr.Code)
	}
}

func TestArchiveRESTDryRunIsTheDefault(t *testing.T) {
	f := &archiveFake{}
	s := &Server{autonomousMgr: f}
	for _, body := range []string{"", "{}", `{"dry_run":true}`} {
		rr := archiveRequest(t, s, http.MethodPost, "/api/autonomous/archive", body)
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"dry_run":true`) {
			t.Fatalf("body %q: %d %s", body, rr.Code, rr.Body.String())
		}
	}
	if f.runs != 0 || f.planned != 3 {
		t.Fatalf("a dry run archived: runs=%d planned=%d", f.runs, f.planned)
	}
	rr := archiveRequest(t, s, http.MethodPost, "/api/autonomous/archive", `{"dry_run":false}`)
	if rr.Code != http.StatusOK || f.runs != 1 || !strings.Contains(rr.Body.String(), `"archived"`) {
		t.Fatalf("apply: %d %s runs=%d", rr.Code, rr.Body.String(), f.runs)
	}
	f.runErr = errors.New("backup did not verify")
	if rr := archiveRequest(t, s, http.MethodPost, "/api/autonomous/archive", `{"dry_run":false}`); rr.Code != http.StatusConflict {
		t.Fatalf("failed run: %d", rr.Code)
	}
	if rr := archiveRequest(t, s, http.MethodPost, "/api/autonomous/archive", `{bad`); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad body: %d", rr.Code)
	}
	if rr := archiveRequest(t, s, http.MethodGet, "/api/autonomous/archive", ""); rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET on the run route: %d", rr.Code)
	}
}

func TestArchiveRESTUnavailableWithoutManager(t *testing.T) {
	s := &Server{}
	if rr := archiveRequest(t, s, http.MethodPost, "/api/autonomous/archive", ""); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("no manager: %d", rr.Code)
	}
	s = &Server{autonomousMgr: struct{ AutonomousAPI }{}}
	if rr := archiveRequest(t, s, http.MethodGet, "/api/autonomous/prds/x/archive", ""); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("manager without archive: %d", rr.Code)
	}
}
