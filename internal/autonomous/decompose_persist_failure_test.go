// Decompose persistence failure — a decompose that produced, validated,
// and stored a valid plan, then failed its final SavePRD, must land in an
// explicit retryable state (PRDDraft + recorded infra failure) with the
// output artifact kept under its .failed name, and a retry after the
// persistence error clears must succeed into needs_review with stories
// stored — proving the "decompose-persist" give-up branch is distinct from
// a content/parse rejection and is operator-retryable.
package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const persistFailureValidPlan = `{"title":"Ship it","stories":[{"title":"A","tasks":[{"title":"T","spec":"do x"}]}]}`

func TestDecompose_PersistFailureIsRetryableAndRetrySucceeds(t *testing.T) {
	const forced = "SavePRD: forced failure (test hook)"
	// Both entry points share the give-up tail (failDecomposeAttempt with
	// source "decompose-persist") and the planning-mark save, so drive
	// through each and assert the same explicit-state contract.
	entries := []struct {
		name string
		exec func(m *Manager, id string) (*PRD, error)
	}{
		{name: "Decompose", exec: func(m *Manager, id string) (*PRD, error) { return m.Decompose(id) }},
		{name: "decomposeStreamingCore", exec: func(m *Manager, id string) (*PRD, error) { return m.decomposeStreamingCore(id, nil) }},
	}

	for _, tc := range entries {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			fn := func(req DecomposeRequest) (string, error) {
				return persistFailureValidPlan, nil
			}
			m, err := NewManager(dataDir, DefaultConfig(), fn)
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			prd, err := m.CreatePRD("spec", "/p", "", "", "")
			if err != nil {
				t.Fatalf("CreatePRD: %v", err)
			}
			// Seed the scratch artifact exactly as production's
			// decomposeFnSession relocate does, so the give-up-tail rename
			// has a real file to keep.
			seedDecomposeArtifact(t, dataDir, prd.ID, persistFailureValidPlan)

			// Force every SavePRD to fail. The planning-mark save is
			// log-not-fatal, so the failure necessarily lands on the final
			// success-path save after the plan was parsed, validated, and
			// its stories stored.
			t.Cleanup(func() { m.store.SavePRDFailHook = nil })
			m.store.SavePRDFailHook = func(p *PRD) bool { return true }

			_, err = tc.exec(m, prd.ID)
			if err == nil {
				t.Fatal("want error from forced persistence failure, got nil")
			}
			if !strings.Contains(err.Error(), forced) {
				t.Fatalf("error %q does not contain the forced hook message %q", err, forced)
			}

			stored, ok := m.Store().GetPRD(prd.ID)
			if !ok {
				t.Fatal("PRD missing from store")
			}
			if stored.Status != PRDDraft {
				t.Fatalf("stored status = %s, want draft (retryable, not %s/%s)", stored.Status, PRDPlanning, PRDNeedsReview)
			}
			f := stored.LastDecomposeFailure
			if f == nil {
				t.Fatal("LastDecomposeFailure = nil, want recorded decompose-persist failure")
			}
			if f.Source != "decompose-persist" {
				t.Fatalf("failure source = %q, want decompose-persist", f.Source)
			}
			if f.Category != "infra" {
				t.Fatalf("failure category = %q, want infra", f.Category)
			}
			if !f.Retryable {
				t.Fatal("failure retryable = false, want true (persistence failure is operator-retryable)")
			}
			if !strings.Contains(f.Detail, forced) {
				t.Fatalf("failure detail %q does not contain the forced error text", f.Detail)
			}

			// Artifact kept under its .failed name with the exact seeded
			// content; the plain artifact is gone.
			dir := filepath.Join(dataDir, "autonomous", "scratch", prd.ID)
			if _, statErr := os.Stat(filepath.Join(dir, "decompose-output.json")); !os.IsNotExist(statErr) {
				t.Fatalf("decompose-output.json should be gone after give-up, stat err = %v", statErr)
			}
			assertFileContent(t, filepath.Join(dir, "decompose-output.json.failed"), persistFailureValidPlan)

			// Retry after the persistence error clears: same valid fn,
			// must land in needs_review with the stories actually stored.
			m.store.SavePRDFailHook = nil
			retry, err := tc.exec(m, prd.ID)
			if err != nil {
				t.Fatalf("retry Decompose: %v", err)
			}
			if retry.Status != PRDNeedsReview {
				t.Fatalf("retry status = %s, want needs_review", retry.Status)
			}
			if retry.LastDecomposeFailure != nil {
				t.Fatalf("retry LastDecomposeFailure = %+v, want nil after success", retry.LastDecomposeFailure)
			}
			stories := storedStoriesFor(m, prd.ID)
			if len(stories) < 1 {
				t.Fatalf("stored stories after retry = %d, want >= 1", len(stories))
			}
			if len(stories[0].Tasks) < 1 {
				t.Fatalf("stored tasks after retry = %d, want >= 1", len(stories[0].Tasks))
			}
		})
	}
}
