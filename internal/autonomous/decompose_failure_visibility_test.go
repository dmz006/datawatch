// Decompose failure visibility — a failed decompose must leave a
// LastDecomposeFailure on the PRD that survives a fresh store read and
// shows up in the JSON the REST endpoint and CLI serialize, and a later
// successful decompose must clear it so the key disappears again.
package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecompose_FailureReasonVisibleOnPRD(t *testing.T) {
	cfg := DefaultConfig()
	// Scope budget is 0 by default; an empty plan fails validation and is
	// not retried, so the first call lands on the terminal failure path.
	plan := `{"title":"x","stories":[]}`
	fn := func(req DecomposeRequest) (string, error) {
		return plan, nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	// (a) failed decompose: returned PRD and the stored PRD both carry the failure.
	// Decompose returns a nil PRD on failure, so the stored record is the
	// only place the failure can be read back from.
	if _, err := m.Decompose(prd.ID); err == nil {
		t.Fatal("Decompose: want error for an empty plan")
	}
	stored, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD: PRD not found in store")
	}
	if stored.Status != PRDDraft {
		t.Fatalf("stored status = %s, want draft", stored.Status)
	}
	if stored.LastDecomposeFailure == nil || strings.TrimSpace(stored.LastDecomposeFailure.Detail) == "" {
		t.Fatalf("stored LastDecomposeFailure = %+v, want non-empty Detail", stored.LastDecomposeFailure)
	}
	detail := stored.LastDecomposeFailure.Detail

	// (b) the REST/CLI serialization exposes the failure on the PRD JSON.
	raw, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"last_decompose_failure"`) {
		t.Fatalf("PRD JSON missing last_decompose_failure key: %s", raw)
	}
	if !strings.Contains(string(raw), detail) {
		t.Fatalf("PRD JSON missing failure detail %q: %s", detail, raw)
	}

	// (c) a retry that succeeds clears the failure and drops the key from JSON.
	plan = `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`
	retried, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("retry Decompose: %v", err)
	}
	if retried.Status != PRDNeedsReview {
		t.Fatalf("retry status = %s, want needs_review", retried.Status)
	}
	if retried.LastDecomposeFailure != nil {
		t.Fatalf("retry LastDecomposeFailure = %+v, want nil", retried.LastDecomposeFailure)
	}
	restored, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD after retry: PRD not found in store")
	}
	raw, err = json.Marshal(restored)
	if err != nil {
		t.Fatalf("json.Marshal after retry: %v", err)
	}
	if strings.Contains(string(raw), `"last_decompose_failure"`) {
		t.Fatalf("PRD JSON still has last_decompose_failure after success: %s", raw)
	}
}
