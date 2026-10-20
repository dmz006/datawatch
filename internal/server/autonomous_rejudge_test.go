// REST contract for POST /api/autonomous/prds/{id}/rejudge — the
// operator-triggered re-judgment of the PRD-completion "rules-review"
// guardrail. The manager side (pass / block / no-verdict / refusal
// outcomes, judge wiring, Decision recording) is covered in
// internal/autonomous/rejudge_test.go; here the fake manager stands in
// for the judge and we pin the handler's HTTP contract:
//
//   - non-POST is rejected with 405,
//   - a refusal from the manager is a 400 that carries the manager's
//     error text verbatim (no Decision written, no Run() call),
//   - a pass returns 200 with the updated PRD and a message pointing at
//     the PRD's decision log, and the manager is called exactly once
//     with the actor defaulting to "operator",
//   - the run executor is NEVER triggered by the rejudge endpoint.

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// rejudgeAPI embeds the fake AutonomousAPI and lets a test steer the
// RejudgeCompletionGuardrail outcome (fake PRD for a pass, error for a
// refusal) while recording the (id, actor) it was called with.
type rejudgeAPI struct {
	fakeOrchAutonomous
	calls []struct{ id, actor string }
	err   error
	prd   any // the fake PRD (a *autonomous.PRD) returned on a pass
}

func (a *rejudgeAPI) RejudgeCompletionGuardrail(prdID, actor string) (any, error) {
	a.calls = append(a.calls, struct{ id, actor string }{prdID, actor})
	if a.err != nil {
		return nil, a.err
	}
	// A real *autonomous.PRD (not a map) so the handler's
	// updated.(*autonomous.PRD) assertion keeps it for the reply.
	a.prd = &autonomous.PRD{ID: prdID, Status: autonomous.PRDCompleted}
	return a.prd, nil
}

func doRejudge(t *testing.T, s *Server, method, url string, body string) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, httptest.NewRequest(method, url, rdr))
	return rr.Code, rr.Body.String()
}

// Wrong method — the action is POST-only.
func TestRejudgeHandler_WrongMethodIs405(t *testing.T) {
	api := &rejudgeAPI{}
	s := &Server{autonomousMgr: api}

	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		code, _ := doRejudge(t, s, method, "/api/autonomous/prds/prd-rej-1/rejudge", "")
		if code != http.StatusMethodNotAllowed {
			t.Errorf("%s rejudge: status=%d, want 405", method, code)
		}
	}
	if len(api.calls) != 0 {
		t.Errorf("manager called %d times on a wrong-method request, want 0: %+v", len(api.calls), api.calls)
	}
}

// Refusal — the manager rejected the re-judge (e.g. PRD not blocked or a
// task not completed): 400 with the manager's error text, manager called
// exactly once.
func TestRejudgeHandler_RefusalIs400WithManagerErrorText(t *testing.T) {
	msg := `rejudge completion guardrail: prd prd-rej-2 is approved, not blocked; rejudgment only applies to a PRDBlocked PRD`
	api := &rejudgeAPI{err: &rejudgeErr{msg: msg}}
	s := &Server{autonomousMgr: api}

	code, body := doRejudge(t, s, http.MethodPost, "/api/autonomous/prds/prd-rej-2/rejudge", `{"actor":"operator"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("refusal: status=%d, want 400 (body=%s)", code, body)
	}
	if !strings.Contains(body, msg) {
		t.Errorf("400 body %q does not carry the manager's error text %q", body, msg)
	}
	if len(api.calls) != 1 || api.calls[0].id != "prd-rej-2" || api.calls[0].actor != "operator" {
		t.Errorf("manager calls = %+v, want one call for prd-rej-2 as operator", api.calls)
	}
}

// Pass — 200 with the updated PRD and a message pointing at the decision
// log; the manager gets the actor from the body.
func TestRejudgeHandler_PassIs200WithPRDAndDecisionLogMessage(t *testing.T) {
	api := &rejudgeAPI{}
	s := &Server{autonomousMgr: api}

	code, body := doRejudge(t, s, http.MethodPost, "/api/autonomous/prds/prd-rej-3/rejudge", `{"actor":"alice"}`)
	if code != http.StatusOK {
		t.Fatalf("pass: status=%d, want 200 (body=%s)", code, body)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, body)
	}
	if got["id"] != "prd-rej-3" || got["status"] != "completed" {
		t.Errorf("pass body id/status = %v/%v, want prd-rej-3/completed", got["id"], got["status"])
	}
	if len(api.calls) != 1 || api.calls[0].actor != "alice" {
		t.Errorf("manager calls = %+v, want one call as alice", api.calls)
	}
}

// Pass with an empty body — the actor must default to "operator".
func TestRejudgeHandler_PassDefaultsActorToOperator(t *testing.T) {
	api := &rejudgeAPI{}
	s := &Server{autonomousMgr: api}

	code, _ := doRejudge(t, s, http.MethodPost, "/api/autonomous/prds/prd-rej-4/rejudge", "")
	if code != http.StatusOK {
		t.Fatalf("status=%d, want 200", code)
	}
	if len(api.calls) != 1 || api.calls[0].actor != "operator" {
		t.Errorf("manager calls = %+v, want actor defaulted to operator", api.calls)
	}
}

func TestRejudgeHandler_PassCompactViewNamesDecisionLog(t *testing.T) {
	// view=compact routes the reply through autonomous.BuildWriteView and
	// is where the handler's message is surfaced. The message must point
	// the operator at the PRD's decision log (the verdict + the
	// guardrail_rejudge Decision both live there).
	api := &rejudgeAPI{}
	s := &Server{autonomousMgr: api}

	code, body := doRejudge(t, s, http.MethodPost, "/api/autonomous/prds/prd-rej-5/rejudge?view=compact", "")
	if code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (body=%s)", code, body)
	}
	if !strings.Contains(strings.ToLower(body), "decision log") {
		t.Errorf("compact pass body %q does not point at the decision log", body)
	}
}

// rejudgeErr is a plain error carrying the manager's refusal text.
type rejudgeErr struct{ msg string }

func (e *rejudgeErr) Error() string { return e.msg }
