// Uncommitted-work REST surface:
//
//   GET /api/autonomous/uncommitted-work[?automaton=<id>] — the policy and
//   scope in effect and every task and Automaton currently held on files git
//   still sees as uncommitted in an Automaton worktree.
//
// It also carries the strict write checks for the two config keys that
// PUT /api/config and PUT /api/autonomous/config apply to
// automation.uncommitted_work_policy and automation.uncommitted_work_scope:
// an absent key keeps the current value, but a present one must be one of
// hold|warn|off (policy) or task_files|worktree (scope).

package server

import (
	"fmt"
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// uncommittedWorkReader is the narrow Manager surface the uncommitted-work
// route needs; asserted like cleanupStatusReader so minimal test doubles keep
// compiling.
type uncommittedWorkReader interface {
	UncommittedWorkStatus(automatonID string) autonomous.UncommittedWorkStatus
}

// handleUncommittedWork serves GET /api/autonomous/uncommitted-work: the
// policy and scope in effect and the held list (one entry per held task or
// Automaton). The optional automaton query narrows the list to one Automaton;
// an id that matches nothing reads an empty list. Read-only: only GET is
// served, the Automata must be enabled, a federated peer needs
// autonomous:read, and the Manager must expose the status. Any other method
// is a 405.
func (s *Server) handleUncommittedWork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	ur, ok := s.autonomousMgr.(uncommittedWorkReader)
	if !ok {
		http.Error(w, "Automaton uncommitted-work status unavailable", http.StatusServiceUnavailable)
		return
	}
	st := ur.UncommittedWorkStatus(r.URL.Query().Get("automaton"))
	if st.Held == nil {
		st.Held = []autonomous.UncommittedWorkEntry{}
	}
	writeJSONOK(w, st)
}

// validateUncommittedWorkPolicy checks automation.uncommitted_work_policy:
// it must be one of hold, warn or off. Empty and unknown words are rejected
// on write, so a read back always shows the word that was written.
func validateUncommittedWorkPolicy(v string) error {
	switch v {
	case config.UncommittedWorkPolicyHold, config.UncommittedWorkPolicyWarn, config.UncommittedWorkPolicyOff:
		return nil
	}
	return fmt.Errorf("autonomous.automation.uncommitted_work_policy: must be %q, %q or %q",
		config.UncommittedWorkPolicyHold, config.UncommittedWorkPolicyWarn, config.UncommittedWorkPolicyOff)
}

// validateUncommittedWorkScope checks automation.uncommitted_work_scope: it
// must be task_files or worktree.
func validateUncommittedWorkScope(v string) error {
	switch v {
	case config.UncommittedWorkScopeTaskFiles, config.UncommittedWorkScopeWorktree:
		return nil
	}
	return fmt.Errorf("autonomous.automation.uncommitted_work_scope: must be %q or %q",
		config.UncommittedWorkScopeTaskFiles, config.UncommittedWorkScopeWorktree)
}

// validateUncommittedWorkValue is the PUT /api/config form of the two
// checks above: v is the raw JSON value of one dotted key, which must be a
// string holding an allowed word.
func validateUncommittedWorkValue(key string, v interface{}) error {
	str, ok := v.(string)
	if !ok {
		return fmt.Errorf("%s: must be a string (%s)", key, uncommittedWorkAllowed(key))
	}
	if key == "autonomous.automation.uncommitted_work_scope" {
		return validateUncommittedWorkScope(str)
	}
	return validateUncommittedWorkPolicy(str)
}

func uncommittedWorkAllowed(key string) string {
	if key == "autonomous.automation.uncommitted_work_scope" {
		return config.UncommittedWorkScopeTaskFiles + " or " + config.UncommittedWorkScopeWorktree
	}
	return config.UncommittedWorkPolicyHold + ", " + config.UncommittedWorkPolicyWarn + " or " + config.UncommittedWorkPolicyOff
}
