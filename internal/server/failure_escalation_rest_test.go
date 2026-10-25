// REST operations on the failure escalation policy of one Automaton:
// set_failure_escalation (Automaton or task override, set or clear) and
// resolve_task_review (retry or cancel a task held for review). These run
// against the real Automaton manager, seeded from a store on disk, so the
// status codes, the audit entries and the stored state are all checked.

package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// heldRESTPRD is an Automaton with one task held for review (t-held) and one
// pending task (t-pending).
func heldRESTPRD(id string) *autonomous.PRD {
	return &autonomous.PRD{
		ID:     id,
		Title:  "held automaton",
		Status: autonomous.PRDBlocked,
		Story: []autonomous.Story{{
			ID:    "s1",
			Title: "story",
			Tasks: []autonomous.Task{
				{
					ID:                 "t-held",
					Title:              "held task",
					Spec:               "make the change",
					Status:             autonomous.TaskReviewNeeded,
					Backend:            "claude-code",
					EscalationFailures: 3,
					Attempts: []autonomous.TaskAttempt{
						{Number: 1, Backend: "claude-code", Outcome: "failed", DurationSeconds: 30},
					},
					ReviewSummary: &autonomous.ReviewSummary{HeldAt: time.Now(), FailureCount: 3, Text: "attempt report"},
				},
				{ID: "t-pending", Title: "pending task", Spec: "other change", Status: autonomous.TaskPending},
			},
		}},
	}
}

// newFailureEscalationRESTServer seeds a store with prds, starts a real manager
// on it and returns a server wired to that manager, with an audit log.
func newFailureEscalationRESTServer(t *testing.T, prds ...*autonomous.PRD) (*Server, *autonomous.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "autonomous")
	st, err := autonomous.NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, p := range prds {
		if err := st.SavePRD(p); err != nil {
			t.Fatalf("SavePRD %s: %v", p.ID, err)
		}
	}
	mgr, err := autonomous.NewManager(dataDir, autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	al, err := audit.NewAt(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	s := &Server{
		cfg:           config.DefaultConfig(),
		cfgPath:       filepath.Join(dir, "config.yaml"),
		autonomousMgr: autonomous.NewAPI(mgr),
		auditLog:      al,
	}
	return s, mgr, filepath.Join(dir, "audit.log")
}

// autoPRD reads an Automaton through the same API the handlers use.
func autoPRD(t *testing.T, s *Server, id string) *autonomous.PRD {
	t.Helper()
	raw, ok := s.autonomousMgr.GetPRD(id)
	if !ok {
		t.Fatalf("automaton %s not found", id)
	}
	p, err := toPRD(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// autoTask returns the task with id from an Automaton read.
func autoTask(t *testing.T, s *Server, prdID, taskID string) *autonomous.Task {
	t.Helper()
	p := autoPRD(t, s, prdID)
	for i := range p.Story {
		for j := range p.Story[i].Tasks {
			if p.Story[i].Tasks[j].ID == taskID {
				return &p.Story[i].Tasks[j]
			}
		}
	}
	t.Fatalf("task %s not in %s", taskID, prdID)
	return nil
}

// postAutomaton sends a POST to one per-Automaton route and returns the recorder.
func postAutomaton(t *testing.T, s *Server, prdID, action, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+prdID+"/"+action, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)
	return rr
}

// auditActions returns the action of every audit entry written so far.
func auditActions(t *testing.T, path string) []string {
	t.Helper()
	l, err := audit.NewAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close() //nolint:errcheck
	entries, err := l.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Action)
	}
	return out
}

func hasAuditAction(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestFailureEscalationRESTSetAutomatonOverrideAndClear(t *testing.T) {
	s, _, auditPath := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))

	rr := postAutomaton(t, s, "a1", "set_failure_escalation", `{"policy":{"max_failures":4,"enabled":false}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("set: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	p := autoPRD(t, s, "a1")
	if p.FailureEscalation == nil || p.FailureEscalation.MaxFailures != 4 || p.FailureEscalation.Enabled == nil || *p.FailureEscalation.Enabled {
		t.Fatalf("Automaton override after set = %+v", p.FailureEscalation)
	}

	rr = postAutomaton(t, s, "a1", "set_failure_escalation", `{"policy":null}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("clear: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if p := autoPRD(t, s, "a1"); p.FailureEscalation != nil {
		t.Fatalf("Automaton override after clear = %+v, want nil", p.FailureEscalation)
	}

	if got := auditActions(t, auditPath); !hasAuditAction(got, "set_failure_escalation") {
		t.Fatalf("no set_failure_escalation audit entry; actions = %v", got)
	}
}

func TestFailureEscalationRESTSetTaskOverrideLeavesAutomatonAlone(t *testing.T) {
	s, _, _ := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))

	rr := postAutomaton(t, s, "a1", "set_failure_escalation",
		`{"task_id":"t-pending","policy":{"steps":[{"backend":"pi-prompt","model":"ollama/qwen3.8:27b","attempts":2}]}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("task set: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	task := autoTask(t, s, "a1", "t-pending")
	if task.FailureEscalation == nil || len(task.FailureEscalation.Steps) != 1 || task.FailureEscalation.Steps[0].Attempts != 2 {
		t.Fatalf("task override = %+v", task.FailureEscalation)
	}
	if p := autoPRD(t, s, "a1"); p.FailureEscalation != nil {
		t.Fatalf("a task override must not set the Automaton override: %+v", p.FailureEscalation)
	}

	rr = postAutomaton(t, s, "a1", "set_failure_escalation", `{"task_id":"t-pending","policy":null}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("task clear: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if task := autoTask(t, s, "a1", "t-pending"); task.FailureEscalation != nil {
		t.Fatalf("task override after clear = %+v", task.FailureEscalation)
	}
}

func TestFailureEscalationRESTSetRejectsBadBodies(t *testing.T) {
	s, _, _ := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))
	cases := map[string]struct {
		body string
		want int
	}{
		"missing policy key":    {`{}`, http.StatusBadRequest},
		"negative max_failures": {`{"policy":{"max_failures":-1}}`, http.StatusBadRequest},
		"unknown policy key":    {`{"policy":{"max_failure":2}}`, http.StatusBadRequest},
		"bad effort in a step":  {`{"policy":{"steps":[{"backend":"claude-code","effort":"bogus"}]}}`, http.StatusBadRequest},
		"not json":              {`{"policy":`, http.StatusBadRequest},
	}
	for name, tc := range cases {
		rr := postAutomaton(t, s, "a1", "set_failure_escalation", tc.body)
		if rr.Code != tc.want {
			t.Errorf("%s: want %d, got %d: %s", name, tc.want, rr.Code, rr.Body.String())
		}
	}
	if p := autoPRD(t, s, "a1"); p.FailureEscalation != nil {
		t.Fatalf("a refused write changed the Automaton: %+v", p.FailureEscalation)
	}
}

func TestFailureEscalationRESTSetUnknownAutomatonAndTaskAre404(t *testing.T) {
	s, _, _ := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))
	if rr := postAutomaton(t, s, "nope", "set_failure_escalation", `{"policy":null}`); rr.Code != http.StatusNotFound {
		t.Errorf("unknown automaton: want 404, got %d", rr.Code)
	}
	if rr := postAutomaton(t, s, "a1", "set_failure_escalation", `{"task_id":"ghost","policy":null}`); rr.Code != http.StatusNotFound {
		t.Errorf("unknown task: want 404, got %d", rr.Code)
	}
}

func TestFailureEscalationRESTResolveRetryRequeuesWithNote(t *testing.T) {
	s, _, auditPath := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))

	rr := postAutomaton(t, s, "a1", "resolve_task_review",
		`{"task_id":"t-held","action":"retry","note":"try the smaller change"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("retry: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	task := autoTask(t, s, "a1", "t-held")
	// A requeued task reads as pending; the store may keep the empty status
	// that stands for pending.
	if task.Status != "" && task.Status != autonomous.TaskPending {
		t.Fatalf("status after retry = %q, want pending", task.Status)
	}
	if task.ReviewSummary != nil {
		t.Errorf("review report survived the retry")
	}
	if !strings.Contains(task.PriorAttemptHint, "try the smaller change") {
		t.Errorf("operator note not handed to the next attempt: %q", task.PriorAttemptHint)
	}
	if got := auditActions(t, auditPath); !hasAuditAction(got, "resolve_task_review") {
		t.Fatalf("no resolve_task_review audit entry; actions = %v", got)
	}
}

func TestFailureEscalationRESTResolveRetryWithChangedSpec(t *testing.T) {
	s, _, _ := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))
	rr := postAutomaton(t, s, "a1", "resolve_task_review",
		`{"task_id":"t-held","action":"retry","spec":"a narrower spec"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("retry with spec: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if task := autoTask(t, s, "a1", "t-held"); task.Spec != "a narrower spec" {
		t.Fatalf("spec after retry = %q", task.Spec)
	}
}

func TestFailureEscalationRESTResolveCancel(t *testing.T) {
	s, _, _ := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))
	rr := postAutomaton(t, s, "a1", "resolve_task_review", `{"task_id":"t-held","action":"cancel"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("cancel: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if task := autoTask(t, s, "a1", "t-held"); task.Status != autonomous.TaskCancelled {
		t.Fatalf("status after cancel = %q, want cancelled", task.Status)
	}
}

func TestFailureEscalationRESTResolveErrors(t *testing.T) {
	s, _, _ := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))

	// A task that is not held answers 409, and it is left as it was.
	rr := postAutomaton(t, s, "a1", "resolve_task_review", `{"task_id":"t-pending","action":"retry"}`)
	if rr.Code != http.StatusConflict {
		t.Errorf("task not held: want 409, got %d: %s", rr.Code, rr.Body.String())
	}
	if task := autoTask(t, s, "a1", "t-pending"); task.Status != autonomous.TaskPending {
		t.Errorf("a refused resolve changed the task to %q", task.Status)
	}

	if rr := postAutomaton(t, s, "a1", "resolve_task_review", `{"task_id":"t-held","action":"explode"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("bad action: want 400, got %d", rr.Code)
	}
	if rr := postAutomaton(t, s, "a1", "resolve_task_review", `{"action":"retry"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("missing task_id: want 400, got %d", rr.Code)
	}
	if rr := postAutomaton(t, s, "a1", "resolve_task_review", `{"task_id":"ghost","action":"retry"}`); rr.Code != http.StatusNotFound {
		t.Errorf("unknown task: want 404, got %d", rr.Code)
	}
	if rr := postAutomaton(t, s, "nope", "resolve_task_review", `{"task_id":"t-held","action":"retry"}`); rr.Code != http.StatusNotFound {
		t.Errorf("unknown automaton: want 404, got %d", rr.Code)
	}
}

// TestFailureEscalationRESTPeerWithoutCapabilityRefused: a federated peer that
// lacks CapAutonomousWrite is refused with the same 403 the other write cases
// give, and nothing changes.
func TestFailureEscalationRESTPeerWithoutCapabilityRefused(t *testing.T) {
	s, _, _ := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))
	peer := &multiserver.Entry{Name: "read-only-peer", Enabled: true, Federated: true}

	for _, tc := range []struct{ action, body string }{
		{"set_failure_escalation", `{"policy":{"max_failures":4}}`},
		{"resolve_task_review", `{"task_id":"t-held","action":"cancel"}`},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/a1/"+tc.action, bytes.NewReader([]byte(tc.body)))
		req = req.WithContext(context.WithValue(req.Context(), fedPeerKey, peer))
		rr := httptest.NewRecorder()
		s.handleAutonomousPRDs(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s from a peer without the capability: want 403, got %d: %s", tc.action, rr.Code, rr.Body.String())
		}
	}
	if task := autoTask(t, s, "a1", "t-held"); task.Status != autonomous.TaskReviewNeeded {
		t.Fatalf("a refused peer changed the held task to %q", task.Status)
	}
	if p := autoPRD(t, s, "a1"); p.FailureEscalation != nil {
		t.Fatalf("a refused peer set the override: %+v", p.FailureEscalation)
	}
}

func TestFailureEscalationRESTMethodGuard(t *testing.T) {
	s, _, _ := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))
	for _, action := range []string{"set_failure_escalation", "resolve_task_review"} {
		req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/a1/"+action, nil)
		rr := httptest.NewRecorder()
		s.handleAutonomousPRDs(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s: want 405, got %d", action, rr.Code)
		}
	}
}
