// Landing state parity across the operator surfaces, one shared contract:
//
//   keep — pending -> kept   POST /api/autonomous/prds/{id}/landing/keep
//   drop — pending -> dropped  POST /api/autonomous/prds/{id}/landing/drop
//
// REST is the core: both decisions are exercised semantically against the
// route handler (405, 501, 400, 200 paths, the body the other surfaces
// send). MCP, CLI, comm and the web app are the surrounding parity surface:
// their code is checked mechanically to call the exact routes with the
// exact body, and each landing tool/verb/command is checked for presence
// and for absence of siblings it must not bring. The semantic keep/drop
// moves live in internal/autonomous (landing_test.go) and the comm render
// in internal/router (landing_comm_test.go).

package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/router"
)

// Shared contract literals — the one place the action/tool/verb names are
// spelled out; the surrounding checks below are checked against them.
const (
	ldKeepAction = "landing/keep"
	ldDropAction = "landing/drop"
	ldKeepTool   = "autonomous_landing_keep"
	ldDropTool   = "autonomous_landing_drop"
)

func TestLandingParity_EnumeratesBothKeepAndDrop(t *testing.T) {
	ops := LandingRESTOperations()
	if len(ops) != 2 {
		t.Fatalf("landing REST operations = %d, want exactly two (keep and drop)", len(ops))
	}
	byName := map[string]LandingRESTOperation{}
	for _, op := range ops {
		if op.Name == "" || op.Path == "" || op.Method == "" {
			t.Fatalf("landing REST operation incomplete: %+v", op)
		}
		byName[op.Name] = op
	}
	keep, ok := byName[ldKeepAction]
	if !ok {
		t.Fatalf("no landing REST operation named %q: %v", ldKeepAction, ops)
	}
	if keep.Path != "/api/autonomous/prds/{id}/landing/keep" || keep.Method != http.MethodPost {
		t.Errorf("keep operation = {%s %s}, want POST /api/autonomous/prds/{id}/landing/keep", keep.Method, keep.Path)
	}
	drop, ok := byName[ldDropAction]
	if !ok {
		t.Fatalf("no landing REST operation named %q: %v", ldDropAction, ops)
	}
	if drop.Path != "/api/autonomous/prds/{id}/landing/drop" || drop.Method != http.MethodPost {
		t.Errorf("drop operation = {%s %s}, want POST /api/autonomous/prds/{id}/landing/drop", drop.Method, drop.Path)
	}
}

// landingSpy is a manager double that implements the landingDecider
// surface (KeepLanding / DropLanding) on top of the generic orchestrator
// fake, so the route handler can be exercised the way the real daemon
// implements it.
type landingSpy struct {
	fakeOrchAutonomous
	keepID, keepActor, keepReason string
	keepErr                       error
	dropID, dropActor, dropReason string
	dropErr                       error
	prd                           *autonomous.PRD
}

func (s *landingSpy) KeepLanding(id, actor, reason string) (*autonomous.PRD, error) {
	s.keepID, s.keepActor, s.keepReason = id, actor, reason
	if s.keepErr != nil {
		return nil, s.keepErr
	}
	p := s.prd
	if p == nil {
		p = &autonomous.PRD{ID: id}
	}
	p.Landing = &autonomous.Landing{State: autonomous.LandingKept, Actor: actor, Reason: reason}
	return p, nil
}

func (s *landingSpy) DropLanding(id, actor, reason string) (*autonomous.PRD, error) {
	s.dropID, s.dropActor, s.dropReason = id, actor, reason
	if s.dropErr != nil {
		return nil, s.dropErr
	}
	p := s.prd
	if p == nil {
		p = &autonomous.PRD{ID: id}
	}
	p.Landing = &autonomous.Landing{State: autonomous.LandingDropped, Actor: actor, Reason: reason}
	return p, nil
}

// TestLandingParity exercises the shared contract on the REST surface
// (semantically) and checks the MCP, CLI, comm and web-app surfaces call
// it exactly (mechanically, against the contract literals above).
func TestLandingParity(t *testing.T) {
	spy := &landingSpy{}
	s := &Server{autonomousMgr: spy}

	// --- REST: keep -------------------------------------------------------
	{
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-1/landing/keep", strings.NewReader(`{"reason":"keep for reference"}`))
		s.handleAutonomousPRDs(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("keep: status=%d body=%s", rr.Code, rr.Body.String())
		}
		if spy.keepID != "prd-1" || spy.keepReason != "keep for reference" {
			t.Errorf("keep reached the decider with (%q, %q), want (prd-1, keep for reference)", spy.keepID, spy.keepReason)
		}
		if spy.keepActor != "operator" {
			t.Errorf("keep actor = %q, want the REST default operator", spy.keepActor)
		}
		if !strings.Contains(rr.Body.String(), `"state":"kept"`) {
			t.Errorf("keep reply %s, want the updated record", rr.Body.String())
		}
	}

	// --- REST: keep with an explicit actor --------------------------------
	{
		spy.keepActor = ""
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-1/landing/keep", strings.NewReader(`{"reason":"r","actor":"parity-op"}`))
		s.handleAutonomousPRDs(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("keep(actor): status=%d", rr.Code)
		}
		if spy.keepActor != "parity-op" {
			t.Errorf("keep actor = %q, want parity-op passed through", spy.keepActor)
		}
	}

	// --- REST: drop -------------------------------------------------------
	{
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-1/landing/drop", strings.NewReader(`{"reason":"superseded"}`))
		s.handleAutonomousPRDs(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("drop: status=%d", rr.Code)
		}
		if spy.dropID != "prd-1" || spy.dropReason != "superseded" || spy.dropActor != "operator" {
			t.Errorf("drop reached the decider with (%q,%q,%q)", spy.dropID, spy.dropActor, spy.dropReason)
		}
		if !strings.Contains(rr.Body.String(), `"state":"dropped"`) {
			t.Errorf("drop reply %s, want the updated record", rr.Body.String())
		}
	}

	// --- REST: 405 on GET --------------------------------------------------
	{
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/prd-1/landing/keep", nil)
		s.handleAutonomousPRDs(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET keep: status=%d, want 405", rr.Code)
		}
	}

	// --- REST: 400 when the decider refuses --------------------------------
	{
		refused := errors.New("the branch has already landed")
		spy.keepErr = refused
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-1/landing/keep", strings.NewReader(`{"reason":"r"}`))
		s.handleAutonomousPRDs(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("refused keep: status=%d, want 400", rr.Code)
		}
		spy.keepErr = nil
	}

	// --- REST: 501 when the manager does not implement landingDecider ------
	{
		s501 := &Server{autonomousMgr: &fakeOrchAutonomous{}}
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-1/landing/keep", strings.NewReader(`{"reason":"r"}`))
		s501.handleAutonomousPRDs(rr, req)
		if rr.Code != http.StatusNotImplemented {
			t.Errorf("unsupported keep: status=%d, want 501", rr.Code)
		}
	}

	// --- MCP surface: both tools proxy the two routes with that body -------
	srcMustContain(t, "internal/mcp/landing_tools.go",
		ldKeepTool, ldDropTool,
		`"/api/autonomous/prds/"+strings.TrimSpace(id)+"/`+ldKeepAction+`"`,
		`"/api/autonomous/prds/"+strings.TrimSpace(id)+"/`+ldDropAction+`"`,
		`WithString("id"`, `WithString("reason"`, `WithString("actor"`,
	)
	srcMustContain(t, "internal/mcp/server.go",
		"toolAutonomousLandingKeep()", "toolAutonomousLandingDrop()",
	)

	// --- CLI surface: both subcommands post the two routes ---------------
	srcMustContain(t, "cmd/datawatch/cli_autonomous.go",
		"prd-landing-keep <id>", "prd-landing-drop <id>",
		`"/api/autonomous/prds/"+args[0]+"/`+ldKeepAction+`"`,
		`"/api/autonomous/prds/"+args[0]+"/`+ldDropAction+`"`,
		`{"actor": actor, "reason": reason}`,
		`newAutonomousLandingKeepCmd()`, `newAutonomousLandingDropCmd()`,
	)

	// --- comm surface: the verbs dispatch to the two routes ---------------
	srcMustContain(t, "internal/router/landing_comm.go",
		`"/api/autonomous/prds/"+id+"/landing/"+action`,
		`case "landing"`, `case "landing-keep", "landing_keep"`, `case "landing-drop", "landing_drop"`,
	)
	srcMustContain(t, "internal/router/sx2_parity.go",
		`case "landing", "landing-keep", "landing_keep", "landing-drop", "landing_drop"`,
		"landing <id>|landing keep <id> <reason...>|landing drop <id> <reason...>",
		"r.handleLandingVerb(verb, args)",
	)
	// and the verbs parse to the autonomous command.
	for _, text := range []string{
		"autonomous landing prd-1",
		"autonomous landing keep prd-1 keep for reference",
		"autonomous landing drop prd-1 superseded",
	} {
		cmd := router.Parse(text)
		if cmd.Type != router.CmdAutonomous {
			t.Errorf("router.Parse(%q).Type = %v, want CmdAutonomous", text, cmd.Type)
		}
	}

	// --- web app surface: the record renders and the two decisions prompt --
	srcMustContain(t, "internal/server/web/app.js",
		"renderLandingState",
		"landing/keep", "landing/drop",
		"automata_detail_landing_keep_prompt", "automata_detail_landing_drop_prompt",
	)
	for _, loc := range []string{"en", "de", "es", "fr", "ja"} {
		srcMustContain(t, "internal/server/web/locales/"+loc+".json",
			"automata_detail_landing_state_pending",
			"automata_detail_landing_state_landed",
			"automata_detail_landing_state_kept",
			"automata_detail_landing_state_dropped",
			"automata_detail_landing_keep_btn",
			"automata_detail_landing_drop_btn",
		)
	}
}
