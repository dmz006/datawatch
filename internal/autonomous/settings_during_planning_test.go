// While a plan is in flight (PRD in the `planning` status) the operator
// must not be able to mutate that PRD's LLM settings: the plan was
// computed against the settings that were in effect when it started, so
// changing them mid-flight would leave the finished plan detached from its
// LLM provenance. Once the plan finishes (needs_review) the same calls must
// succeed again. These tests drive that contract with a fake planner that
// blocks while claiming to be mid-plan — no sessions, tmux or services.
package autonomous

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// planValidJSON is valid plan output with one story and one task, so the
// blocked planner produces a plan that legitimately lands the PRD in
// needs_review with intact stories.
const planValidJSON = `{"title":"T","stories":[{"title":"A","tasks":[{"title":"T1","spec":"do x"}]}]}`

// planningManager builds a Manager whose fake planner signals `started`
// and then blocks on `<-release` before returning planValidJSON. Calling
// the returned release func unblocks the planner so Decompose completes.
func planningManager(t *testing.T, started chan<- struct{}, release chan func()) (*Manager, string) {
	t.Helper()
	fn := func(req DecomposeRequest) (string, error) {
		started <- struct{}{}
		releaseFn := <-release
		releaseFn()
		return planValidJSON, nil
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	return m, prd.ID
}

// waitForStatusPlanning polls the store for prdID to reach want, bounded by the
// timeout so a stuck planner can never hang the test.
func waitForStatusPlanning(t *testing.T, m *Manager, prdID string, want PRDStatus, timeout time.Duration) *PRD {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		prd, _ := m.store.GetPRD(prdID)
		if prd != nil && prd.Status == want {
			return prd
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for prd %s to reach %s", prdID, want)
	return nil
}

func TestSettingsDuringPlanning_RefusedAndPlanKept(t *testing.T) {
	started := make(chan struct{})
	release := make(chan func())

	m, prdID := planningManager(t, started, release)

	backendBefore := ""
	if prd, _ := m.store.GetPRD(prdID); prd != nil {
		backendBefore = prd.Backend
	}

	doneCh := make(chan *PRD, 1)
	errCh := make(chan error, 1)
	go func() {
		got, err := m.Decompose(prdID)
		if err != nil {
			errCh <- err
		}
		doneCh <- got
	}()

	// Wait for the planner to have claimed the `planning` status (bounded).
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for planner to start")
	}

	prd, _ := m.store.GetPRD(prdID)
	if prd == nil {
		t.Fatal("PRD missing from store during planning")
	}
	if prd.Status != PRDPlanning {
		t.Fatalf("status during planning = %s, want %s", prd.Status, PRDPlanning)
	}

	backendPtr := "fake-backend"

	// SetPRDLLM must be refused while planning, and must not touch Backend.
	if _, _, err := m.SetPRDLLM(prdID, &backendPtr, nil, nil, nil, nil, "op"); err == nil {
		t.Fatal("SetPRDLLM: want error during planning, got nil")
	} else {
		if !errors.Is(err, ErrPlanInProgress) {
			t.Fatalf("SetPRDLLM error = %v, want errors.Is(err, ErrPlanInProgress)", err)
		}
		if !strings.Contains(err.Error(), "planning") {
			t.Fatalf("SetPRDLLM error %q does not mention planning", err)
		}
	}

	// Same for the guardrail LLM override.
	if _, err := m.SetPRDGuardrailLLM(prdID, &backendPtr, nil, "op"); err == nil {
		t.Fatal("SetPRDGuardrailLLM: want error during planning, got nil")
	} else {
		if !errors.Is(err, ErrPlanInProgress) {
			t.Fatalf("SetPRDGuardrailLLM error = %v, want errors.Is(err, ErrPlanInProgress)", err)
		}
		if !strings.Contains(err.Error(), "planning") {
			t.Fatalf("SetPRDGuardrailLLM error %q does not mention planning", err)
		}
	}

	if prd, _ := m.store.GetPRD(prdID); prd != nil && prd.Backend != backendBefore {
		t.Fatalf("Backend mutated during planning: got %q want %q", prd.Backend, backendBefore)
	}

	// Release the planner and let Decompose finish.
	release <- func() {}
	var updated *PRD
	select {
	case err := <-errCh:
		t.Fatalf("Decompose: %v", err)
	case updated = <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Decompose to finish")
	}
	_ = waitForStatusPlanning(t, m, prdID, PRDNeedsReview, 2*time.Second)

	stored, _ := m.store.GetPRD(prdID)
	if stored == nil {
		t.Fatal("PRD missing after planning")
	}
	if stored.Status != PRDNeedsReview {
		t.Fatalf("status after planning = %s, want %s", stored.Status, PRDNeedsReview)
	}
	stories := storedStoriesFor(m, prdID)
	if len(stories) != 1 || len(stories[0].Tasks) != 1 {
		t.Fatalf("stored stories/tasks = %d story / %d task, want 1/1", len(stories), 0)
	}
	if stored.Backend != backendBefore {
		t.Fatalf("Backend changed after planning: got %q want %q", stored.Backend, backendBefore)
	}
	_ = updated
}

func TestSettingsDuringPlanning_AllowedAfterPlanFinishes(t *testing.T) {
	started := make(chan struct{})
	release := make(chan func())

	m, prdID := planningManager(t, started, release)

	doneCh := make(chan struct{})
	go func() {
		if _, err := m.Decompose(prdID); err != nil {
			t.Errorf("Decompose: %v", err)
		}
		doneCh <- struct{}{}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for planner to start")
	}

	waitForStatusPlanning(t, m, prdID, PRDPlanning, 2*time.Second)
	release <- func() {}
	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Decompose to finish")
	}

	waitForStatusPlanning(t, m, prdID, PRDNeedsReview, 2*time.Second)

	// Plan is done: the same override now succeeds.
	backendPtr := "fake-backend"
	got, _, err := m.SetPRDLLM(prdID, &backendPtr, nil, nil, nil, nil, "op")
	if err != nil {
		t.Fatalf("SetPRDLLM after plan finished: %v", err)
	}
	if got != nil && got.Backend != "fake-backend" {
		t.Fatalf("Backend after successful SetPRDLLM = %q, want %q", got.Backend, "fake-backend")
	}
	if prd, _ := m.store.GetPRD(prdID); prd != nil && prd.Backend != "fake-backend" {
		t.Fatalf("stored Backend after successful SetPRDLLM = %q, want %q", prd.Backend, "fake-backend")
	}
}
