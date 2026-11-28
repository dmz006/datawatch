// Per-Automaton archive POST: POST /api/autonomous/prds/{id}/archive must
// reach the case "archive" action of handleAutonomousPRDs (optional outcome body
// and archive audit intact), while GET keeps
// reading the one Automaton's archive.
package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// archivePostFake stands in for the manager behind the per-Automaton archive
// action: the outcome forms, with or without a landing decision, and a failed
// archive. The read surface is implemented only so the non-POST methods still
// answer 405, and no POST call may reach it — each read call sets readTouched
// and fails.
type archivePostFake struct {
	AutonomousAPI
	readTouched bool
	lastKind    string
	lastID      string
	lastIn      autonomous.OutcomeInput
	lastDec     string
	lastRsn     string
	failErr     error
}

func (f *archivePostFake) ArchiveWithOutcome(id string, in autonomous.OutcomeInput) (any, error) {
	f.lastKind, f.lastID, f.lastIn = "outcome", id, in
	if f.failErr != nil {
		return nil, f.failErr
	}
	return map[string]any{"id": id, "archived": true}, nil
}

func (f *archivePostFake) ArchiveDecided(id, decision, reason string) (any, error) {
	f.lastKind, f.lastID, f.lastDec, f.lastRsn = "decided", id, decision, reason
	if f.failErr != nil {
		return nil, f.failErr
	}
	return map[string]any{"id": id, "decision": decision}, nil
}

func (f *archivePostFake) ArchiveDecidedWithOutcome(id, decision, reason string, in autonomous.OutcomeInput) (any, error) {
	f.lastKind, f.lastID, f.lastDec, f.lastRsn, f.lastIn = "decided-outcome", id, decision, reason, in
	if f.failErr != nil {
		return nil, f.failErr
	}
	return map[string]any{"id": id, "decision": decision}, nil
}

func (f *archivePostFake) ReadAutomatonArchive(id string) (*autonomous.PRDArchive, error) {
	f.readTouched = true
	return nil, errors.New("the POST route must not read the archive store")
}

func (f *archivePostFake) PlanAutomatonArchive() autonomous.ArchivePlan {
	f.readTouched = true
	return autonomous.ArchivePlan{}
}

func (f *archivePostFake) RunAutomatonArchive() (autonomous.ArchiveReport, error) {
	f.readTouched = true
	return autonomous.ArchiveReport{}, nil
}

// prdArchivePostRequest serves {id}/archive the way server.go mounts it.
func prdArchivePostRequest(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/autonomous/prds/{id}/archive", s.handlePRDArchive)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestPrdArchivePOSTRunsTheArchiveAction(t *testing.T) {
	f := &archivePostFake{}
	s := &Server{autonomousMgr: f}

	// A bare POST is the plain archive action with an empty outcome; the read
	// surface stays untouched.
	if rr := prdArchivePostRequest(t, s, http.MethodPost, "/api/autonomous/prds/aa11/archive", ""); rr.Code != http.StatusOK {
		t.Fatalf("bare POST: %d %s", rr.Code, rr.Body.String())
	}
	if f.readTouched || f.lastKind != "outcome" || f.lastID != "aa11" || f.lastIn != (autonomous.OutcomeInput{}) {
		t.Fatalf("bare POST: read=%v kind=%s id=%q in=%+v", f.readTouched, f.lastKind, f.lastID, f.lastIn)
	}

	// An outcome in the body rides along with the action.
	if rr := prdArchivePostRequest(t, s, http.MethodPost, "/api/autonomous/prds/aa11/archive", `{"outcome":"done","delivered_via":"twin","note":"shipped"}`); rr.Code != http.StatusOK {
		t.Fatalf("outcome POST: %d %s", rr.Code, rr.Body.String())
	}
	if f.lastIn != (autonomous.OutcomeInput{Outcome: "done", DeliveredVia: "twin", Note: "shipped"}) {
		t.Fatalf("outcome not carried: %+v", f.lastIn)
	}

	// A landing decision with its reason reaches the decided form.
	if rr := prdArchivePostRequest(t, s, http.MethodPost, "/api/autonomous/prds/aa11/archive", `{"decision":"drop","reason":"shipped elsewhere"}`); rr.Code != http.StatusOK {
		t.Fatalf("decision POST: %d %s", rr.Code, rr.Body.String())
	}
	if f.lastKind != "decided" || f.lastDec != "drop" || f.lastRsn != "shipped elsewhere" {
		t.Fatalf("decision not carried: kind=%s dec=%q rsn=%q", f.lastKind, f.lastDec, f.lastRsn)
	}

	// A decision and an outcome in one step reach the combined form.
	if rr := prdArchivePostRequest(t, s, http.MethodPost, "/api/autonomous/prds/aa11/archive", `{"outcome":"done","decision":"drop","reason":"shipped elsewhere"}`); rr.Code != http.StatusOK {
		t.Fatalf("combined POST: %d %s", rr.Code, rr.Body.String())
	}
	if f.lastKind != "decided-outcome" || f.lastIn.Outcome != "done" || f.lastDec != "drop" {
		t.Fatalf("combined not carried: kind=%s in=%+v dec=%q", f.lastKind, f.lastIn, f.lastDec)
	}
}

func TestPrdArchivePOSTStillRefusesBadForms(t *testing.T) {
	f := &archivePostFake{}
	s := &Server{autonomousMgr: f}

	if rr := prdArchivePostRequest(t, s, http.MethodPost, "/api/autonomous/prds/aa11/archive", `{bad`); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad body: %d %s", rr.Code, rr.Body.String())
	}
	if f.readTouched {
		t.Fatal("a bad body must not touch the read surface")
	}

	f.failErr = errors.New("not archived")
	if rr := prdArchivePostRequest(t, s, http.MethodPost, "/api/autonomous/prds/aa11/archive", ""); rr.Code != http.StatusBadRequest {
		t.Fatalf("failed archive: %d %s", rr.Code, rr.Body.String())
	}

	rec := prdArchivePostRequest(t, s, http.MethodPut, "/api/autonomous/prds/aa11/archive", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	if allow := rec.Header().Get("Allow"); allow != "GET, POST" {
		t.Fatalf("Allow: %q", allow)
	}
}
