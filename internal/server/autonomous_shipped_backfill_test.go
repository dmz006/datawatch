// Pure (no daemon, no real home dir) tests for the shipped-backfill HTTP
// surface: the dry_run flag reaches the manager, bad params and errors are
// refused, non-POST is rejected, and the report travels back as JSON.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// backfillRESTSpy embeds the interface stub and captures the dry-run flag
// the handler passed through; it returns a fixed report or a configured
// error.
type backfillRESTSpy struct {
	fakeOrchAutonomous

	calls []bool
	err   error
}

func (s *backfillRESTSpy) ShippedBackfillOnce(dryRun bool) (any, error) {
	s.calls = append(s.calls, dryRun)
	if s.err != nil {
		return nil, s.err
	}
	return &autonomousReportStub{
		PrdsMarked: 2,
		Unmatched:  []string{"deadbeef"},
	}, nil
}

// autonomousReportStub stands in for the backfill report: the handler must
// serialise whatever the manager returns, so the shape is the server's
// concern only in that it must be JSON.
type autonomousReportStub struct {
	PrdsMarked int      `json:"prds_marked"`
	Unmatched  []string `json:"unmatched_ids"`
}

func postBackfill(s *Server, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleAutonomousShippedBackfill(rec, httptest.NewRequest(http.MethodPost, path, nil))
	return rec
}

func assertBackfillCalls(t *testing.T, spy *backfillRESTSpy, want []bool) {
	t.Helper()
	if len(spy.calls) != len(want) {
		t.Fatalf("ShippedBackfillOnce called %v, want %v", spy.calls, want)
	}
	for i, w := range want {
		if spy.calls[i] != w {
			t.Fatalf("ShippedBackfillOnce[%d] = %v, want %v (calls %v)", i, spy.calls[i], w, spy.calls)
		}
	}
}

func TestShippedBackfillREST_Run(t *testing.T) {
	spy := &backfillRESTSpy{}
	rec := postBackfill(&Server{autonomousMgr: spy}, "/api/autonomous/shipped/backfill")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertBackfillCalls(t, spy, []bool{false})
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("reply is not JSON: %v (%s)", err, rec.Body.String())
	}
	if got["prds_marked"] != float64(2) {
		t.Errorf("reply prds_marked = %v, want 2", got["prds_marked"])
	}
	if ids, ok := got["unmatched_ids"].([]any); !ok || len(ids) != 1 || ids[0] != "deadbeef" {
		t.Errorf("reply unmatched_ids = %v, want [deadbeef]", got["unmatched_ids"])
	}
}

func TestShippedBackfillREST_DryRun(t *testing.T) {
	spy := &backfillRESTSpy{}
	rec := postBackfill(&Server{autonomousMgr: spy}, "/api/autonomous/shipped/backfill?dry_run=true")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertBackfillCalls(t, spy, []bool{true})
}

func TestShippedBackfillREST_DryRunFalseExplicit(t *testing.T) {
	spy := &backfillRESTSpy{}
	rec := postBackfill(&Server{autonomousMgr: spy}, "/api/autonomous/shipped/backfill?dry_run=false")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertBackfillCalls(t, spy, []bool{false})
}

func TestShippedBackfillREST_Refusals(t *testing.T) {
	// Non-boolean dry_run is refused before the manager is called.
	spy := &backfillRESTSpy{}
	rec := postBackfill(&Server{autonomousMgr: spy}, "/api/autonomous/shipped/backfill?dry_run=bogus")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("dry_run=bogus: want 400, got %d", rec.Code)
	}
	assertBackfillCalls(t, spy, nil)

	// Non-POST never runs the pass.
	spy2 := &backfillRESTSpy{}
	rec2 := httptest.NewRecorder()
	srv2 := &Server{autonomousMgr: spy2}
	srv2.handleAutonomousShippedBackfill(rec2, httptest.NewRequest(http.MethodGet, "/api/autonomous/shipped/backfill", nil))
	if rec2.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: want 405, got %d", rec2.Code)
	}
	assertBackfillCalls(t, spy2, nil)

	// A failed pass is a 400 carrying the daemon's reason.
	spy3 := &backfillRESTSpy{err: errors.New("no base repository configured")}
	rec3 := postBackfill(&Server{autonomousMgr: spy3}, "/api/autonomous/shipped/backfill")
	if rec3.Code != http.StatusBadRequest {
		t.Errorf("failed pass: want 400, got %d: %s", rec3.Code, rec3.Body.String())
	}
	if want := "no base repository configured"; !strings.Contains(rec3.Body.String(), want) {
		t.Errorf("reply %q; want the daemon's reason %q", rec3.Body.String(), want)
	}
}
