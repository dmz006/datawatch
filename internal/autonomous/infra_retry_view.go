// Infrastructure retry read view: the policy in effect and every task that has
// been retried, or is held, because the machinery around it failed.
//
// GET /api/autonomous/infra-retries serves InfraRetryStatus, and the
// autonomous.infra_retry block of GET /api/config serves the same
// InfraRetrySettings, so the two surfaces show the same effective values.
package autonomous

import (
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// InfraRetrySettings is the infrastructure retry policy in effect: every key
// of autonomous.infra_retry with its default filled in. BackoffSeconds is the
// list as configured (the default list when none is set); the last entry
// repeats when a task has more tries than entries.
type InfraRetrySettings struct {
	Enabled         bool     `json:"enabled"`
	MaxTries        int      `json:"max_tries"`
	BackoffSeconds  []int    `json:"backoff_seconds"`
	Categories      []string `json:"categories"`
	ExhaustedAction string   `json:"exhausted_action"`
	RetryDependents bool     `json:"retry_dependents"`
	RaiseQuestion   bool     `json:"raise_question"`
}

// InfraRetrySettingsFrom resolves the effective settings of a config block.
// The lists are copies and never nil, so they encode as [] and not null.
func InfraRetrySettingsFrom(c config.InfraRetryConfig) InfraRetrySettings {
	backoff := c.BackoffSeconds
	if backoff == nil {
		backoff = config.InfraRetryDefaultBackoff()
	}
	return InfraRetrySettings{
		Enabled:         c.EffectiveEnabled(),
		MaxTries:        c.EffectiveMaxTries(),
		BackoffSeconds:  append([]int{}, backoff...),
		Categories:      c.EffectiveCategories(),
		ExhaustedAction: c.EffectiveExhaustedAction(),
		RetryDependents: c.EffectiveRetryDependents(),
		RaiseQuestion:   c.EffectiveRaiseQuestion(),
	}
}

// InfraRetryEntry is one scheduled retry in a task's history.
type InfraRetryEntry struct {
	Number       int       `json:"number"`
	Class        string    `json:"class"`
	Reason       string    `json:"reason,omitempty"`
	At           time.Time `json:"at"`
	DelaySeconds int       `json:"delay_seconds"`
	NextAt       time.Time `json:"next_at"`
}

// InfraRetryTask is one task with infrastructure retries or held for an
// infrastructure reason, with the Automaton it belongs to.
type InfraRetryTask struct {
	AutomatonID    string            `json:"automaton_id"`
	AutomatonTitle string            `json:"automaton_title"`
	TaskID         string            `json:"task_id"`
	TaskTitle      string            `json:"task_title"`
	Status         TaskStatus        `json:"status"`
	RetryCount     int               `json:"retry_count"`
	MaxTries       int               `json:"max_tries"`
	Retries        []InfraRetryEntry `json:"retries"`
	// Held is true while the task waits for the operator because its
	// infrastructure retries ran out; HeldCause says why.
	Held      bool   `json:"held"`
	HeldCause string `json:"held_cause,omitempty"`
	// QuestionID names the operator question open for the hold, when there is
	// one.
	QuestionID string `json:"question_id,omitempty"`
}

// InfraRetryStatus is the body of GET /api/autonomous/infra-retries: the
// effective policy, then the tasks most recently retried first, at most the
// requested number of them. Count is the number of tasks listed.
type InfraRetryStatus struct {
	InfraRetrySettings
	Tasks []InfraRetryTask `json:"tasks"`
	Count int              `json:"count"`
}

// InfraRetryStatus reads the retry state of every Automaton, or of the one
// named by automatonID when it is not empty, and returns at most limit tasks
// (all of them when limit is below 1). An unknown Automaton gives no tasks.
func (a *API) InfraRetryStatus(automatonID string, limit int) InfraRetryStatus {
	cfg := a.M.infraRetryConfig()
	st := InfraRetryStatus{
		InfraRetrySettings: InfraRetrySettingsFrom(cfg),
		Tasks:              []InfraRetryTask{},
	}
	questions := a.M.openInfraHoldQuestions()
	maxTries := cfg.EffectiveMaxTries()

	prds := a.M.Store().ListPRDs()
	a.M.store.WithLock(func() {
		for _, prd := range prds {
			if automatonID != "" && prd.ID != automatonID {
				continue
			}
			for i := range prd.Story {
				for j := range prd.Story[i].Tasks {
					t := &prd.Story[i].Tasks[j]
					if row, ok := infraRetryTaskRow(prd, t, maxTries); ok {
						row.QuestionID = questions[infraHoldQuestionRef{prd.ID, t.ID}]
						st.Tasks = append(st.Tasks, row)
					}
				}
			}
		}
	})
	sort.SliceStable(st.Tasks, func(i, j int) bool {
		ti, tj := latestInfraRetryAt(st.Tasks[i]), latestInfraRetryAt(st.Tasks[j])
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		if st.Tasks[i].AutomatonID != st.Tasks[j].AutomatonID {
			return st.Tasks[i].AutomatonID < st.Tasks[j].AutomatonID
		}
		return st.Tasks[i].TaskID < st.Tasks[j].TaskID
	})
	if limit > 0 && len(st.Tasks) > limit {
		st.Tasks = st.Tasks[:limit]
	}
	st.Count = len(st.Tasks)
	return st
}

// infraRetryTaskRow builds the row for t, or reports false when t has no
// infrastructure retries and is not held for an infrastructure reason. The
// caller holds the store lock.
func infraRetryTaskRow(prd *PRD, t *Task, maxTries int) (InfraRetryTask, bool) {
	held := t.Status == TaskReviewNeeded && t.FailureReason != nil &&
		t.FailureReason.Source == infraRetryHoldSource
	if !held && t.InfraRetryCount == 0 && len(t.InfraRetries) == 0 {
		return InfraRetryTask{}, false
	}
	row := InfraRetryTask{
		AutomatonID:    prd.ID,
		AutomatonTitle: prd.Title,
		TaskID:         t.ID,
		TaskTitle:      t.Title,
		Status:         t.Status,
		RetryCount:     t.InfraRetryCount,
		MaxTries:       maxTries,
		Retries:        make([]InfraRetryEntry, 0, len(t.InfraRetries)),
		Held:           held,
	}
	if held {
		row.HeldCause = t.FailureReason.Detail
	}
	for _, r := range t.InfraRetries {
		row.Retries = append(row.Retries, InfraRetryEntry{
			Number: r.Number, Class: r.Class, Reason: r.Reason,
			At: r.At, DelaySeconds: r.DelaySeconds, NextAt: r.NextAt,
		})
	}
	return row, true
}

// latestInfraRetryAt is when the task's newest retry was decided; a task with
// no record sorts last.
func latestInfraRetryAt(t InfraRetryTask) time.Time {
	if n := len(t.Retries); n > 0 {
		return t.Retries[n-1].At
	}
	return time.Time{}
}

// infraHoldQuestionRef identifies the task an operator question is about.
type infraHoldQuestionRef struct{ prdID, taskID string }

// openInfraHoldQuestions maps each task with an open infrastructure hold
// question to that question's id. It is empty when no question store is wired.
func (m *Manager) openInfraHoldQuestions() map[infraHoldQuestionRef]string {
	out := map[infraHoldQuestionRef]string{}
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil {
		return out
	}
	for _, q := range store.List(QuestionFilter{State: QuestionOpen, Asker: infraHoldQuestionAsker, Limit: 1000}) {
		if q.PRDID == "" || q.TaskID == "" {
			continue
		}
		ref := infraHoldQuestionRef{q.PRDID, q.TaskID}
		if _, seen := out[ref]; !seen { // the list is newest first
			out[ref] = q.ID
		}
	}
	return out
}
