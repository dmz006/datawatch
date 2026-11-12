package autonomous

// Planning re-attach: when the daemon restarts while an Automaton is in
// planning and its planner session survived, the Automaton stays in planning
// and a reader (wired by the daemon through SetPlanningReattachFn) waits on the
// session's output. The helpers here are the completion side of that path:
// IngestPlannerOutput finishes the decompose from the JSON the reader collected,
// and FailPlanningReattach ends it when no output arrives.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// errReattachNoRetry is what the decompose-lint retry step returns on a
// re-attached plan. A retry needs a fresh planner session, which a reader
// cannot start, so the lint pass keeps the plan it was given.
var errReattachNoRetry = errors.New("planner retry is not available after a re-attached planner session")

// IngestPlannerOutput finishes the decompose of prdID from raw, the planner's
// output JSON, exactly as the decompose path does once a planner returns: it
// parses the plan, runs the decompose-lint pass, validates and stores the
// stories, and moves the Automaton to needs_review. The planner session record
// is cleared on every path. When the Automaton is no longer in planning (the
// operator moved it on), nothing is stored and the call returns nil.
func (m *Manager) IngestPlannerOutput(prdID, raw string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	defer m.ClearPlannerSession(prdID)
	var status PRDStatus
	m.store.WithLock(func() { status = prd.Status })
	if status != PRDPlanning {
		return nil
	}
	title, stories, err := ParsePlanning(raw)
	if err != nil {
		m.failDecomposeAttempt(prd, "decompose-parse", err)
		return err
	}
	title, raw, stories = m.decomposeLintHook(prd, title, raw, stories,
		func(string) (string, string, []Story, error) { return "", "", nil, errReattachNoRetry })
	outcome, oerr := m.validateAndStorePlan(prd, prdID, stories)
	switch outcome {
	case decomposeAttemptStored:
		_, err := m.finalizeDecomposedPlan(prd, prdID, prd.DecompositionProfile, "", raw, title, stories, nil)
		return err
	case decomposeAttemptRetry:
		// The retry budget was spent, but there is no planner to run again.
		// Fail the attempt here so the Automaton does not stay in planning.
		oerr = fmt.Errorf("decompose plan needs a retry, which a re-attached planner cannot run")
		m.failDecomposeAttempt(prd, "decompose-reattach", oerr)
	}
	return oerr
}

// FailPlanningReattach ends the decompose of prdID when its re-attached planner
// produced no usable output (timeout, failed session, or no file). The
// Automaton goes back to draft through the normal give-up path, and the planner
// session record is cleared. It does nothing unless the Automaton is in planning.
func (m *Manager) FailPlanningReattach(prdID string, cause error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	defer m.ClearPlannerSession(prdID)
	var status PRDStatus
	m.store.WithLock(func() { status = prd.Status })
	if status != PRDPlanning {
		return
	}
	if cause == nil {
		cause = errors.New("planner session ended without output")
	}
	m.failDecomposeAttempt(prd, "decompose-reattach", cause)
}

// readPlannerOutput returns the planner output file recorded on prd, or nil
// when it is missing or unreadable. A relative recorded path is read from the
// PRD's project directory, as the planner wrote it there.
func readPlannerOutput(prd *PRD, outputFile string) []byte {
	path := outputFile
	if path != "" && !filepath.IsAbs(path) && prd.ProjectDir != "" {
		path = filepath.Join(prd.ProjectDir, path)
	}
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the planner output file the daemon recorded for this PRD
	if err != nil {
		return nil
	}
	return raw
}

// sessionIn reports whether id is one of ids.
func sessionIn(ids []string, id string) bool {
	for _, s := range ids {
		if s == id {
			return true
		}
	}
	return false
}

// plannerIDMatch reports whether two session ids name the same session. The
// PRD records the short hex id (the session store's GetByShortID form) while
// the daemon's live-session list reports host-prefixed full ids, so ids match
// when equal or when the longer one ends in "-<short id>".
func plannerIDMatch(a, b string) bool {
	if a == b {
		return true
	}
	if a == "" || b == "" {
		return false
	}
	if len(a) < len(b) {
		a, b = b, a
	}
	return strings.HasSuffix(a, "-"+b)
}

// plannerSessionLive reports whether the recorded planner session id is in a
// session list, matching the short-id/full-id forms of plannerIDMatch.
func plannerSessionLive(ids []string, plannerID string) bool {
	for _, id := range ids {
		if plannerIDMatch(id, plannerID) {
			return true
		}
	}
	return false
}
