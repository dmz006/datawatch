// scan_fixloop_route_test.go — the fix_loop_enabled gate on the
// /api/autonomous/prds/{id}/scan/fixloop route, driven through the
// AutonomusAPI seam with a fake manager (no LLM, no session, no real
// scan). fix_loop_enabled = false means the loop never runs: the route
// degrades to a plain scan (no corrective task, no hold, same shape as
// /scan). fix_loop_enabled = true means the route hands off to the
// manager's scan-fix loop.

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/autonomous/scan"
)

// fixLoopRouteFake implements the two seams the route reads
// (GetScanConfig, RunScan) plus the ScanFixLoop handoff, and counts
// which path was taken.
type fixLoopRouteFake struct {
	fakeOrchAutonomous
	cfg           scan.Config
	runScanHits   int
	runScanPRDIDs []string
	loopHits      int
	loopPRDIDs    []string
	runScanErr    error
	loopReport    *autonomous.FixLoopReport
	loopErr       error
}

func (f *fixLoopRouteFake) RunScan(prdID string) (any, error) {
	f.runScanHits++
	f.runScanPRDIDs = append(f.runScanPRDIDs, prdID)
	if f.runScanErr != nil {
		return nil, f.runScanErr
	}
	return &scan.Result{PRDID: prdID, Pass: true, Verdict: "pass", Findings: []scan.Finding{}}, nil
}

func (f *fixLoopRouteFake) GetScanConfig() any { return f.cfg }

func (f *fixLoopRouteFake) ScanFixLoop(prdID string) (any, error) {
	f.loopHits++
	f.loopPRDIDs = append(f.loopPRDIDs, prdID)
	if f.loopErr != nil {
		// No report is a true (untyped) nil — the route distinguishes "the
		// loop died before it could report" from "a report survives the
		// failed step".
		if f.loopReport == nil {
			return nil, f.loopErr
		}
		return f.loopReport, f.loopErr
	}
	rep := f.loopReport
	if rep == nil {
		rep = &autonomous.FixLoopReport{PRDID: prdID, Outcome: autonomous.FixLoopOutcomeFixed}
	}
	return rep, nil
}

func postFixloop(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/aaaa0001/scan/fixloop", nil)
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, req)
	return rec
}

func TestScanFixloopDisabledRunsPlainScan(t *testing.T) {
	f := &fixLoopRouteFake{cfg: scan.Config{FixLoopEnabled: false, FixLoopMaxRetries: 4}}
	s := &Server{autonomousMgr: f}
	rec := postFixloop(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if f.runScanHits != 1 || f.runScanPRDIDs[0] != "aaaa0001" {
		t.Fatalf("expected exactly one plain scan of aaaa0001, got %d (%v)", f.runScanHits, f.runScanPRDIDs)
	}
	if f.loopHits != 0 {
		t.Fatalf("loop was run %d time(s) though fix_loop_enabled is false", f.loopHits)
	}
	// The plain scan reply: a scan Result for the right Automaton.
	if !strings.Contains(rec.Body.String(), `"pass":true`) || !strings.Contains(rec.Body.String(), "aaaa0001") {
		t.Fatalf("reply is not the plain scan result: %s", rec.Body.String())
	}
}

func TestScanFixloopDisabledPlainScanErrorIsA400(t *testing.T) {
	f := &fixLoopRouteFake{cfg: scan.Config{FixLoopEnabled: false}}
	f.runScanErr = errScanBoom
	s := &Server{autonomousMgr: f}
	rec := postFixloop(t, s)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if f.loopHits != 0 {
		t.Errorf("loop ran even though the plain scan failed: %d time(s)", f.loopHits)
	}
}

func TestScanFixloopEnabledRunsTheLoop(t *testing.T) {
	f := &fixLoopRouteFake{cfg: scan.Config{FixLoopEnabled: true, FixLoopMaxRetries: 2}}
	s := &Server{autonomousMgr: f}
	rec := postFixloop(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if f.loopHits != 1 || f.loopPRDIDs[0] != "aaaa0001" {
		t.Fatalf("expected exactly one loop run for aaaa0001, got %d (%v)", f.loopHits, f.loopPRDIDs)
	}
	if f.runScanHits != 0 {
		t.Errorf("route ran its own plain scan even though the loop owns the scans: %d time(s)", f.runScanHits)
	}
	// The reply is the loop report, not a raw scan result.
	if !strings.Contains(rec.Body.String(), `"outcome":"fixed"`) || !strings.Contains(rec.Body.String(), "aaaa0001") {
		t.Fatalf("reply is not the loop report: %s", rec.Body.String())
	}
}

func TestScanFixloopEnabledLoopReportSurvivesFailedStep(t *testing.T) {
	rep := &autonomous.FixLoopReport{PRDID: "aaaa0001", Outcome: autonomous.FixLoopOutcomeHeld, Held: true, HoldReason: "findings remain: r1"}
	f := &fixLoopRouteFake{cfg: scan.Config{FixLoopEnabled: true}, loopReport: rep}
	f.loopErr = errFixloopBoom
	s := &Server{autonomousMgr: f}
	rec := postFixloop(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (the report survives a failed step): %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"outcome":"held"`) {
		t.Fatalf("reply is not the loop report: %s", rec.Body.String())
	}
}

func TestScanFixloopEnabledLoopErrorWithoutReportIsA500(t *testing.T) {
	f := &fixLoopRouteFake{cfg: scan.Config{FixLoopEnabled: true}}
	f.loopErr = errFixloopBoom
	s := &Server{autonomousMgr: f}
	rec := postFixloop(t, s)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: %s", rec.Code, rec.Body.String())
	}
}

func TestScanFixloopRejectsNonPOST(t *testing.T) {
	f := &fixLoopRouteFake{cfg: scan.Config{FixLoopEnabled: true}}
	s := &Server{autonomousMgr: f}
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/aaaa0001/scan/fixloop", nil)
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", rec.Code)
	}
	if f.runScanHits+f.loopHits != 0 {
		t.Fatalf("a rejected method ran scan/loop work: scans=%d loop=%d", f.runScanHits, f.loopHits)
	}
}

var (
	errScanBoom    = &boomErr{"scan refused"}
	errFixloopBoom = &boomErr{"loop refused"}
)

// boomErr is a small distinct error type so the routes can't be told
// apart by message.
type boomErr struct{ msg string }

func (e *boomErr) Error() string { return e.msg }
