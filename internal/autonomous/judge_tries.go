package autonomous

import (
	"fmt"
	"time"
	"unicode/utf8"
)

// Outcomes of one judge attempt on one backend.
const (
	// JudgeOutcomeTimedOut: the judge ran and hit its time limit.
	JudgeOutcomeTimedOut = "timed_out"
	// JudgeOutcomeNoSlot: the capacity lease was not granted in time, so the
	// judge never ran on that backend.
	JudgeOutcomeNoSlot = "no_slot"
	// JudgeOutcomeCouldNotStart: the judge session never received its main
	// prompt (judge_could_not_start), so it never ran on that backend.
	JudgeOutcomeCouldNotStart = "could_not_start"
)

// JudgeAttemptRecord is one judge attempt on one backend: the backend, when
// the attempt started, how long it took and how it ended.
type JudgeAttemptRecord struct {
	Backend string `json:"backend"`
	// Started is when the attempt began (before the wait for a slot).
	Started time.Time `json:"started"`
	// Duration is how long the attempt ran on its backend, from when the
	// run started (the slot was granted) until it ended. The wait for the
	// slot is not in it; see Waiting.
	Duration time.Duration `json:"duration"`
	// Waiting is how long the attempt waited for its capacity slot, recorded
	// separately from Duration: the wait is never counted against the
	// judge's time limit, which starts when the run starts.
	Waiting time.Duration `json:"waiting"`
	Outcome string        `json:"outcome"`
	// SessionID is the judge session of the attempt, and LastOutput the tail
	// of what it had printed when the attempt timed out. Both are empty for a
	// backend that never got a slot or never started a session.
	SessionID  string `json:"session_id,omitempty"`
	LastOutput string `json:"last_output,omitempty"`
}

// JudgeLastOutputMax bounds JudgeAttemptRecord.LastOutput, in bytes.
const JudgeLastOutputMax = 4096

// TailJudgeOutput keeps the last JudgeLastOutputMax bytes of s, cut at a
// character boundary.
func TailJudgeOutput(s string) string {
	if len(s) <= JudgeLastOutputMax {
		return s
	}
	s = s[len(s)-JudgeLastOutputMax:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// JudgeTry is one timed-out attempt kept on the PRD while the judge call it
// belongs to is still running. Key names that call (its unit and guardrail).
type JudgeTry struct {
	Key string `json:"key"`
	JudgeAttemptRecord
}

// JudgeTries returns the timed-out attempts recorded on the PRD for the judge
// call key, in the order they were recorded. An unknown PRD has none.
func (m *Manager) JudgeTries(prdID, key string) []JudgeAttemptRecord {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil
	}
	var out []JudgeAttemptRecord
	for _, t := range prd.JudgeTries {
		if t.Key == key {
			out = append(out, t.JudgeAttemptRecord)
		}
	}
	return out
}

// RecordJudgeTry appends one timed-out attempt for the judge call key to the
// PRD and saves the PRD, so the record survives a daemon restart.
func (m *Manager) RecordJudgeTry(prdID, key string, rec JudgeAttemptRecord) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("judge try: PRD %s not found", prdID)
	}
	m.store.WithLock(func() {
		prd.JudgeTries = append(prd.JudgeTries, JudgeTry{Key: key, JudgeAttemptRecord: rec})
	})
	return m.savePRDOwn(prd)
}

// ClearJudgeTries removes the attempts recorded for the judge call key. It
// saves the PRD only when there was something to remove.
func (m *Manager) ClearJudgeTries(prdID, key string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil
	}
	changed := false
	m.store.WithLock(func() {
		var kept []JudgeTry
		for _, t := range prd.JudgeTries {
			if t.Key != key {
				kept = append(kept, t)
			}
		}
		if len(kept) != len(prd.JudgeTries) {
			prd.JudgeTries = kept
			changed = true
		}
	})
	if !changed {
		return nil
	}
	return m.savePRDOwn(prd)
}
