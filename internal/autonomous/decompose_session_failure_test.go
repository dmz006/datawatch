// Decompose one-shot session no-output handling. A decompose session that
// times out, or ends without writing its plan file, is respawned once; a
// second consecutive no-output session stops the PRD (PRDFailed) without a
// third spawn. The count is persisted in prds.jsonl so it survives restarts.
package autonomous

import (
	"strings"
	"testing"
)

const decomposeSessionTestPlan = `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`

func noOutputDecomposeErr() error {
	return &OneShotNoOutputError{Msg: "claude-code one-shot decompose session ended with state failed without producing output (backend=claude-code, model=\"\")"}
}

func TestDecompose_SessionNoOutputTwice_StopsPRDWithoutThirdSpawn(t *testing.T) {
	dataDir := t.TempDir()
	calls := 0
	var promptOnRespawn string
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		if calls == 2 {
			promptOnRespawn = req.Spec
		}
		return "", noOutputDecomposeErr()
	}
	m, err := NewManager(dataDir, DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")

	_, err = m.Decompose(prd.ID)
	if err == nil {
		t.Fatal("Decompose: want the no-output error returned once the streak stops")
	}
	if !isOneShotNoOutput(err) {
		t.Fatalf("Decompose error = %v, want the one-shot no-output family", err)
	}
	if calls != 2 {
		t.Fatalf("decompose call count = %d, want 2 (first session respawned once, then stop — no third spawn)", calls)
	}
	if !strings.Contains(promptOnRespawn, "Your previous attempt failed") {
		t.Errorf("respawned session prompt missing the failure hint: %q", promptOnRespawn)
	}

	final, _ := m.Store().GetPRD(prd.ID)
	if final.Status != PRDFailed {
		t.Fatalf("status = %s, want %s", final.Status, PRDFailed)
	}
	if final.DecomposeSessionFailures != 2 {
		t.Errorf("DecomposeSessionFailures = %d, want 2", final.DecomposeSessionFailures)
	}
	if final.LastDecomposeFailure == nil {
		t.Fatal("LastDecomposeFailure is nil, want the terminal failure recorded")
	}
	if final.LastDecomposeFailure.Category != "infra" {
		t.Errorf("LastDecomposeFailure.Category = %q, want infra", final.LastDecomposeFailure.Category)
	}
	if final.LastDecomposeFailure.Retryable {
		t.Error("LastDecomposeFailure.Retryable = true, want false")
	}
	if final.LastDecomposeFailure.Source != "decompose-session" {
		t.Errorf("LastDecomposeFailure.Source = %q, want decompose-session", final.LastDecomposeFailure.Source)
	}
	if !strings.Contains(final.LastDecomposeFailure.Detail, "default model") || !strings.Contains(final.LastDecomposeFailure.Detail, "reset to draft") {
		t.Errorf("LastDecomposeFailure.Detail = %q, want it to name the model default and the reset-to-draft recovery", final.LastDecomposeFailure.Detail)
	}

	raw := []byte(persistedPRDRecords(t, dataDir))
	if !strings.Contains(string(raw), `"decompose_session_failures":2`) {
		t.Errorf("saved PRD missing decompose_session_failures:2, got: %s", raw)
	}
}

func TestDecompose_SessionNoOutputTwice_StreamingLoopAlsoStops(t *testing.T) {
	calls := 0
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		return "", noOutputDecomposeErr()
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")

	if _, err := m.decomposeStreamingCore(prd.ID, nil); err == nil {
		t.Fatal("decomposeStreamingCore: want the no-output error once the streak stops")
	}
	if calls != 2 {
		t.Fatalf("decompose call count = %d, want 2", calls)
	}
	final, _ := m.Store().GetPRD(prd.ID)
	if final.Status != PRDFailed {
		t.Fatalf("status = %s, want %s", final.Status, PRDFailed)
	}
}

func TestDecompose_SessionNoOutputCounterPersistsAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()
	fn := func(req DecomposeRequest) (string, error) { return "", noOutputDecomposeErr() }
	m, err := NewManager(dataDir, DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	// One no-output session is recorded, then the process stops mid-streak.
	p, _ := m.Store().GetPRD(prd.ID)
	if m.noteDecomposeSessionFailure(p, "claude-code", "", noOutputDecomposeErr()) {
		t.Fatal("first no-output session must not stop the PRD")
	}

	reopened, err := NewManager(dataDir, DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("reopen NewManager: %v", err)
	}
	got, ok := reopened.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing after reopen")
	}
	if got.DecomposeSessionFailures != 1 {
		t.Fatalf("DecomposeSessionFailures after restart = %d, want 1", got.DecomposeSessionFailures)
	}
}

func TestDecompose_SessionNoOutputThenResetThenRetry_NotBlocked(t *testing.T) {
	calls := 0
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		if calls <= 2 {
			return "", noOutputDecomposeErr()
		}
		return decomposeSessionTestPlan, nil
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	if _, err := m.Decompose(prd.ID); err == nil {
		t.Fatal("first Decompose: want the stopped-streak error")
	}

	if _, err := m.ResetToDraft(prd.ID, "operator"); err != nil {
		t.Fatalf("ResetToDraft: %v", err)
	}
	reset, _ := m.Store().GetPRD(prd.ID)
	if reset.DecomposeSessionFailures != 0 {
		t.Fatalf("DecomposeSessionFailures after reset = %d, want 0", reset.DecomposeSessionFailures)
	}
	if reset.LastDecomposeFailure != nil {
		t.Errorf("LastDecomposeFailure after reset = %+v, want nil (terminal decompose-session hint must not carry over)", reset.LastDecomposeFailure)
	}

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose after reset: %v (want a fresh attempt, not blocked)", err)
	}
	if calls != 3 {
		t.Fatalf("decompose call count = %d, want 3 (2 before reset, 1 fresh after)", calls)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want %s", updated.Status, PRDNeedsReview)
	}
	if updated.DecomposeSessionFailures != 0 {
		t.Errorf("DecomposeSessionFailures after success = %d, want 0", updated.DecomposeSessionFailures)
	}
}
