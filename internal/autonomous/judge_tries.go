package autonomous

import (
	"fmt"
	"time"
)

// Outcomes of one judge attempt on one backend.
const (
	// JudgeOutcomeTimedOut: the judge ran and hit its time limit.
	JudgeOutcomeTimedOut = "timed_out"
	// JudgeOutcomeNoSlot: the capacity lease was not granted in time, so the
	// judge never ran on that backend.
	JudgeOutcomeNoSlot = "no_slot"
)

// JudgeAttemptRecord is one judge attempt on one backend: the backend, when
// the attempt started, how long it took and how it ended.
type JudgeAttemptRecord struct {
	Backend  string        `json:"backend"`
	Started  time.Time     `json:"started"`
	Duration time.Duration `json:"duration"`
	Outcome  string        `json:"outcome"`
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
	prd.JudgeTries = append(prd.JudgeTries, JudgeTry{Key: key, JudgeAttemptRecord: rec})
	return m.savePRDOwn(prd)
}

// ClearJudgeTries removes the attempts recorded for the judge call key. It
// saves the PRD only when there was something to remove.
func (m *Manager) ClearJudgeTries(prdID, key string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil
	}
	var kept []JudgeTry
	for _, t := range prd.JudgeTries {
		if t.Key != key {
			kept = append(kept, t)
		}
	}
	if len(kept) == len(prd.JudgeTries) {
		return nil
	}
	prd.JudgeTries = kept
	return m.savePRDOwn(prd)
}
