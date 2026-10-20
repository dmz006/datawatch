// POST /api/autonomous/prds/{id}/set_llm while a plan is in flight must be
// refused with 409 (Conflict), not 400 — a plan in progress is a transient
// state the client can retry after the planner finishes, not a malformed
// request. The refusal comes from the Manager's ErrPlanInProgress sentinel
// (PRD locked in the `planning` status), so this test drives the real
// Manager with a fake planner that signals start, blocks until released
// and then returns a valid plan — no sessions, tmux or real LLM services.
package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// validPlanJSON is a minimally valid plan (one story, one task) so the
// blocked planner, once released, legitimately lands the PRD in
// needs_review with intact stories.
const validPlanJSON = `{"title":"T","stories":[{"title":"A","tasks":[{"title":"T1","spec":"do x"}]}]}`

func TestSetLLMDuringPlanning(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan func(), 1)
	fn := func(req autonomous.DecomposeRequest) (string, error) {
		started <- struct{}{}
		rf := <-release
		rf()
		return validPlanJSON, nil
	}

	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := mgr.CreatePRD("Build the thing.", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	srv := &Server{}
	srv.SetAutonomousAPI(autonomous.NewAPI(mgr))

	// Start the decompose; the fake planner blocks inside it so the PRD
	// sits in `planning` while we probe set_llm.
	go func() { _, _ = mgr.Decompose(prd.ID) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("fake planner never started")
	}

	// Mid-plan: set_llm must be refused with 409 + planning text.
	req := httptest.NewRequest(http.MethodPost,
		"/api/autonomous/prds/"+prd.ID+"/set_llm",
		strings.NewReader(`{"backend":"x"}`))
	rr := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("set_llm during planning: want 409, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(strings.ToLower(rr.Body.String()), "planning") {
		t.Errorf("set_llm during planning: body should mention planning, got %q", rr.Body.String())
	}

	// Release the planner: the plan must survive and land in
	// needs_review with its stories.
	release <- func() {}
	deadline := time.Now().Add(5 * time.Second)
	var done *autonomous.PRD
	for time.Now().Before(deadline) {
		if p, ok := mgr.Store().GetPRD(prd.ID); ok && p != nil && p.Status == autonomous.PRDNeedsReview {
			done = p
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if done == nil {
		p, _ := mgr.Store().GetPRD(prd.ID)
		if p != nil {
			t.Fatalf("prd did not reach needs_review; status=%s", p.Status)
		}
		t.Fatal("prd missing after planner released")
	}
	if len(done.Story) != 1 || done.Story[0].Title != "A" {
		t.Errorf("stories not intact after plan: %+v", done.Story)
	}
	if len(done.Story) == 1 && len(done.Story[0].Tasks) != 1 {
		t.Errorf("tasks not intact after plan: %+v", done.Story[0].Tasks)
	}

	// Post-plan: the same set_llm call must now be accepted (no 409).
	req2 := httptest.NewRequest(http.MethodPost,
		"/api/autonomous/prds/"+prd.ID+"/set_llm",
		strings.NewReader(`{"backend":"x"}`))
	rr2 := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("set_llm after planning: want 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}
}

// TestSetGuardrailLLMDuringPlanning pins the second ErrPlanInProgress
// site (SetPRDGuardrailLLM) at the HTTP layer: a guardrail-only set_llm
// body (guardrail_backend/guardrail_model, no legacy keys) must hit the
// SAME 409-during-planning contract as the legacy setter, persist
// nothing while refused, and be accepted once the plan lands. The
// legacy path above (TestSetLLMDuringPlanning) never exercises this
// branch, so without this test the 409 mapping on the guardrail setter
// would be verified only at Manager level, never at the HTTP layer.
func TestSetGuardrailLLMDuringPlanning(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan func(), 1)
	fn := func(req autonomous.DecomposeRequest) (string, error) {
		started <- struct{}{}
		rf := <-release
		rf()
		return validPlanJSON, nil
	}

	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := mgr.CreatePRD("Build the thing.", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	srv := &Server{}
	srv.SetAutonomousAPI(autonomous.NewAPI(mgr))

	go func() { _, _ = mgr.Decompose(prd.ID) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("fake planner never started")
	}

	// Mid-plan: a guardrail-only body must be refused with 409 + the
	// same planning wording the legacy branch uses.
	req := httptest.NewRequest(http.MethodPost,
		"/api/autonomous/prds/"+prd.ID+"/set_llm",
		strings.NewReader(`{"guardrail_backend":"x","guardrail_model":"m"}`))
	rr := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("guardrail set_llm during planning: want 409, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(strings.ToLower(rr.Body.String()), "planning") {
		t.Errorf("guardrail set_llm during planning: body should mention planning, got %q", rr.Body.String())
	}

	// The refused call must persist nothing.
	if p, ok := mgr.Store().GetPRD(prd.ID); ok && p != nil {
		if p.GuardrailBackend != "" || p.GuardrailModel != "" {
			t.Errorf("refused set_llm must not persist guardrail fields: backend=%q model=%q", p.GuardrailBackend, p.GuardrailModel)
		}
	}

	// Release the planner and wait for the plan to land.
	release <- func() {}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if p, ok := mgr.Store().GetPRD(prd.ID); ok && p != nil && p.Status == autonomous.PRDNeedsReview {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Post-plan: the same guardrail-only body must now be accepted and
	// persisted.
	req2 := httptest.NewRequest(http.MethodPost,
		"/api/autonomous/prds/"+prd.ID+"/set_llm",
		strings.NewReader(`{"guardrail_backend":"x","guardrail_model":"m"}`))
	rr2 := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("guardrail set_llm after planning: want 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}
	p, ok := mgr.Store().GetPRD(prd.ID)
	if !ok || p == nil || p.GuardrailBackend != "x" || p.GuardrailModel != "m" {
		t.Errorf("guardrail fields not persisted after planning: %+v", p)
	}
}
