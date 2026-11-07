package server

import (
	"net/http"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// TestSessionLaunchTags covers the launch fields of POST /api/sessions/start
// (docs/plans/2026-11-06-automata-pages-redesign.md §12): launch_role and
// launch_kind are validated against the known vocabulary, an accepted pair
// lands in the session's system tags through the live placement-tag config,
// and a request without the fields behaves exactly as before it existed.
func TestSessionLaunchTags(t *testing.T) {
	t.Run("ValidRoleAndKindLandInSystemTags", func(t *testing.T) {
		s, _, _ := fedCapFixture(t)
		body := `{"task":"plain","project_dir":"` + t.TempDir() + `","launch_role":"verifier","launch_kind":"review"}`
		fullID := startTagsSession(t, s, body) // 200 already asserted.

		sess, ok := s.manager.GetSession(fullID)
		if !ok {
			t.Fatalf("session %s not found", fullID)
		}
		for _, want := range []string{"role:verifier", "kind:review"} {
			if !launchTagPresent(sess.SystemTags, want) {
				t.Errorf("system tags %v missing %q", sess.SystemTags, want)
			}
		}
	})

	t.Run("UnknownRoleAndKindAreRefusedWith400", func(t *testing.T) {
		s, _, _ := fedCapFixture(t)
		for _, body := range []string{
			`{"task":"plain","project_dir":"` + t.TempDir() + `","launch_role":"root"}`,
			`{"task":"plain","project_dir":"` + t.TempDir() + `","launch_role":"root","launch_kind":"decompose"}`,
			`{"task":"plain","project_dir":"` + t.TempDir() + `","launch_role":"verifier","launch_kind":"qa"}`,
		} {
			w := startTagsSessionRaw(t, s, body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("unknown launch value in %s: status = %d, want 400 (%s)", body, w.Code, w.Body.String())
			}
		}
	})

	t.Run("AbsentLaunchFieldsAddNothing", func(t *testing.T) {
		s, _, _ := fedCapFixture(t)
		body := `{"task":"plain","project_dir":"` + t.TempDir() + `"}`
		fullID := startTagsSession(t, s, body) // 200 already asserted.

		sess, ok := s.manager.GetSession(fullID)
		if !ok {
			t.Fatalf("session %s not found", fullID)
		}
		if len(sess.SystemTags) != 0 {
			t.Errorf("session system tags = %v, want empty (launch fields absent)", sess.SystemTags)
		}
	})
}

// TestSessionLaunchTagsAutomaton covers launch_automaton / launch_stage:
// the named Automaton's PRD resolves to its Automation, and both facts (plus
// the stage) land in the session's system tags.
func TestSessionLaunchTagsAutomaton(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	prd := &autonomous.PRD{ID: "prd-1", AutomationID: "auto-1"}
	s.autonomousMgr = &launchTestAutonomous{prds: map[string]*autonomous.PRD{prd.ID: prd}}

	body := `{"task":"plain","project_dir":"` + t.TempDir() + `","launch_automaton":"prd-1","launch_stage":"verify"}`
	fullID := startTagsSession(t, s, body)

	sess, ok := s.manager.GetSession(fullID)
	if !ok {
		t.Fatalf("session %s not found", fullID)
	}
	for _, want := range []string{"automaton:prd-1", "automation:auto-1", "stage:verify"} {
		if !launchTagPresent(sess.SystemTags, want) {
			t.Errorf("system tags %v missing %q", sess.SystemTags, want)
		}
	}
}

// launchTestAutonomous stands in for the autonomous manager in the launch
// tests: everything is the real *autonomous.API (never called — only
// GetPRD is on the launch path) except the PRD lookup, which serves the
// map. Embedding a nil *autonomous.API is fine because the overridden
// method is the only one the Server invokes.
type launchTestAutonomous struct {
	*autonomous.API
	prds map[string]*autonomous.PRD
}

func (f *launchTestAutonomous) GetPRD(id string) (any, bool) {
	prd, ok := f.prds[id]
	return prd, ok
}

// launchTagPresent reports whether tag is in xs.
func launchTagPresent(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
