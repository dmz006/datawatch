package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
)

// forceSpyAutonomous records the run calls the handler makes and refuses a
// plain run the way the shipped-work rule does.
type forceSpyAutonomous struct {
	spyAutonomous
	runs   []string
	forced []string
}

func (f *forceSpyAutonomous) Run(id string) error {
	f.runs = append(f.runs, id)
	return errors.New("automaton " + id + " already shipped in v9.183.0 (commit 77304085e)")
}

func (f *forceSpyAutonomous) RunForced(id, actor, reason string) error {
	f.forced = append(f.forced, id+"|"+actor+"|"+reason)
	if strings.TrimSpace(reason) == "" {
		return errors.New("force needs a reason: say why shipped work should run again")
	}
	return nil
}

func postRun(s *Server, id, body string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+id+"/run", strings.NewReader(body))
	s.handleAutonomousPRDs(rr, req)
	return rr
}

func TestRunRefusalReasonReachesTheClient(t *testing.T) {
	spy := &forceSpyAutonomous{}
	s := &Server{autonomousMgr: spy}
	rr := postRun(s, "aaaa0001", "")
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "already shipped in v9.183.0") {
		t.Fatalf("status %d body %q, want 400 naming the shipped release", rr.Code, rr.Body.String())
	}
	if len(spy.forced) != 0 {
		t.Fatalf("a plain run must never force: %v", spy.forced)
	}
}

func TestRunForceNeedsAReasonAndIsAudited(t *testing.T) {
	spy := &forceSpyAutonomous{}
	al, err := audit.NewAt(t.TempDir() + "/audit.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	s := &Server{autonomousMgr: spy, auditLog: al}

	if rr := postRun(s, "aaaa0001", `{"force":true}`); rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "reason") {
		t.Fatalf("force with no reason: status %d body %q", rr.Code, rr.Body.String())
	}
	rr := postRun(s, "aaaa0001", `{"force":true,"reason":"the release was reverted"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("forced run: status %d body %q", rr.Code, rr.Body.String())
	}
	if len(spy.runs) != 0 {
		t.Fatalf("a forced run goes through RunForced only, plain Run saw %v", spy.runs)
	}
	if got := spy.forced[len(spy.forced)-1]; got != "aaaa0001|operator|the release was reverted" {
		t.Fatalf("forced call = %q", got)
	}
	d := lastAuditDetails(t, al)
	if d["force"] != true || d["reason"] != "the release was reverted" {
		t.Fatalf("audit details = %#v, want force and the reason", d)
	}
}

func TestRunForceNotAvailableWithoutTheOptionalInterface(t *testing.T) {
	s := &Server{autonomousMgr: &spyAutonomous{}}
	if rr := postRun(s, "aaaa0001", `{"force":true,"reason":"x"}`); rr.Code != http.StatusNotImplemented {
		t.Fatalf("status %d, want 501 when the manager cannot force", rr.Code)
	}
}
