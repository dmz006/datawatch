// Structured Automaton detail for GET /api/sessions/<id>/status.
//
// Autonomous task sessions get an `automaton` block built entirely from
// data the daemon already stores (session record + PRD/story/task in the
// autonomous store), so the Status tab can render it without any hook
// payload from the agent. See
// docs/plans/2026-10-14-status-tab-session-detail-gap.md (Recommendation 1).

package server

import (
	"encoding/json"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// automatonTaskSpecMaxRunes caps the task spec excerpt in the status block.
const automatonTaskSpecMaxRunes = 600

// automatonDecisionLimit caps how many PRD decisions the status block carries.
const automatonDecisionLimit = 10

// SessionAutomatonDetail is the `automaton` block on SessionStatusBoard.
// Nil (omitted) for sessions that aren't linked to an autonomous task.
type SessionAutomatonDetail struct {
	PRDID             string `json:"prd_id"`
	PRDTitle          string `json:"prd_title,omitempty"`
	PRDStatus         string `json:"prd_status,omitempty"`
	StoryID           string `json:"story_id,omitempty"`
	StoryTitle        string `json:"story_title,omitempty"`
	TaskID            string `json:"task_id,omitempty"`
	TaskTitle         string `json:"task_title,omitempty"`
	TaskSpec          string `json:"task_spec,omitempty"`
	TaskSpecTruncated bool   `json:"task_spec_truncated,omitempty"`
	TaskStatus        string `json:"task_status,omitempty"`
	WaitReason        string `json:"wait_reason,omitempty"`
	// WaitReasonReadable is the same wait reason with the holders it names
	// shown as operator-readable names ("pool <p> full (u/l), held by
	// <PRD title> / <task title> (<short id>) and …"). wait_reason keeps the
	// raw text; this is beside it, not a replacement, and is empty when the
	// reason names no holders.
	WaitReasonReadable string `json:"wait_reason_readable,omitempty"`
	// WaitCause / WaitResetsAt classify the wait (pool_full, backoff, ...)
	// and say when it ends, when known. Holds lists the pools and tree lock
	// the task holds; HoldsNote is the sentence for a parked task.
	WaitCause      string                        `json:"wait_cause,omitempty"`
	WaitResetsAt   *time.Time                    `json:"wait_resets_at,omitempty"`
	Holds          []string                      `json:"holds,omitempty"`
	HoldsNote      string                        `json:"holds_note,omitempty"`
	StartedAt      *time.Time                    `json:"started_at,omitempty"`
	CompletedAt    *time.Time                    `json:"completed_at,omitempty"`
	RetryCount     int                           `json:"retry_count,omitempty"`
	ExecutorSource string                        `json:"executor_source,omitempty"`
	Error          string                        `json:"error,omitempty"`
	Verification   *SessionAutomatonVerification `json:"verification,omitempty"`
	Git            *SessionAutomatonGit          `json:"git,omitempty"`
	Decisions      []SessionAutomatonDecision    `json:"decisions,omitempty"`
	Cost           SessionAutomatonCost          `json:"cost"`
}

// SessionAutomatonVerification mirrors the task's verifier verdict.
type SessionAutomatonVerification struct {
	OK              bool     `json:"ok"`
	Severity        string   `json:"severity,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	Issues          []string `json:"issues,omitempty"`
	VerifierBackend string   `json:"verifier_backend,omitempty"`
	VerifierModel   string   `json:"verifier_model,omitempty"`
	VerifierSource  string   `json:"verifier_source,omitempty"`
}

// SessionAutomatonGit carries the task's git/file-association state.
type SessionAutomatonGit struct {
	PreTaskSHA        string   `json:"pre_task_sha,omitempty"`
	BaseSHA           string   `json:"base_sha,omitempty"`
	AttemptCommitSHAs []string `json:"attempt_commit_shas,omitempty"`
	FilesPlanned      []string `json:"files_planned,omitempty"`
	FilesTouched      []string `json:"files_touched,omitempty"`
	ForeignFileDrift  []string `json:"foreign_file_drift,omitempty"`
}

// SessionAutomatonDecision is one entry of the PRD decision log.
type SessionAutomatonDecision struct {
	At             time.Time `json:"at"`
	Kind           string    `json:"kind"`
	Actor          string    `json:"actor,omitempty"`
	Note           string    `json:"note,omitempty"`
	VerdictOutcome string    `json:"verdict_outcome,omitempty"`
}

// SessionAutomatonCost is the session's token/cost accounting (same
// fields as /api/cost).
type SessionAutomatonCost struct {
	TokensIn   int     `json:"tokens_in"`
	TokensOut  int     `json:"tokens_out"`
	EstCostUSD float64 `json:"est_cost_usd"`
}

// automatonPRDView is the subset of autonomous.PRD the status block reads.
// GetPRD returns `any`; decoding through JSON keeps this package free of
// an import on internal/autonomous (and works with test fakes that return maps).
type automatonPRDView struct {
	ID         string                     `json:"id"`
	Title      string                     `json:"title"`
	Status     string                     `json:"status"`
	WaitReason string                     `json:"wait_reason"`
	Stories    []automatonStoryView       `json:"stories"`
	Decisions  []SessionAutomatonDecision `json:"decisions"`
}

type automatonStoryView struct {
	ID           string              `json:"id"`
	Title        string              `json:"title"`
	FilesPlanned []string            `json:"files"`
	Tasks        []automatonTaskView `json:"tasks"`
}

type automatonTaskView struct {
	ID                string                        `json:"id"`
	StoryID           string                        `json:"story_id"`
	Title             string                        `json:"title"`
	Spec              string                        `json:"spec"`
	Status            string                        `json:"status"`
	SessionID         string                        `json:"session_id"`
	WaitReason        string                        `json:"wait_reason"`
	StartedAt         *time.Time                    `json:"started_at"`
	CompletedAt       *time.Time                    `json:"completed_at"`
	Error             string                        `json:"error"`
	RetryCount        int                           `json:"retry_count"`
	ExecutorSource    string                        `json:"executor_source"`
	Verification      *SessionAutomatonVerification `json:"verification"`
	PreTaskSHA        string                        `json:"pre_task_sha"`
	BaseSHA           string                        `json:"base_sha"`
	AttemptCommitSHAs []string                      `json:"attempt_commit_shas"`
	FilesPlanned      []string                      `json:"files"`
	FilesTouched      []string                      `json:"files_touched"`
	ForeignFileDrift  []string                      `json:"foreign_file_drift"`
}

// taskBySessionFinder is the optional autonomous-manager capability used
// when a session record carries no TaskID. Kept off AutonomousAPI so test
// fakes don't all need it.
type taskBySessionFinder interface {
	FindTaskBySessionID(sessionID string) (prdID, taskID string)
}

// buildSessionAutomatonDetail resolves the session's PRD/story/task and
// returns the structured block, or nil when the session isn't an
// autonomous task session or its PRD can't be loaded.
func (s *Server) buildSessionAutomatonDetail(sess *session.Session) *SessionAutomatonDetail {
	if sess == nil || s.autonomousMgr == nil {
		return nil
	}
	prdID, taskID := sess.PRDID, sess.TaskID
	if taskID == "" {
		if f, ok := s.autonomousMgr.(taskBySessionFinder); ok {
			for _, id := range []string{sess.FullID, sess.ID} {
				if id == "" {
					continue
				}
				if p, t := f.FindTaskBySessionID(id); t != "" && (prdID == "" || p == prdID) {
					prdID, taskID = p, t
					break
				}
			}
		}
	}
	if prdID == "" {
		return nil
	}
	raw, ok := s.autonomousMgr.GetPRD(prdID)
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var prd automatonPRDView
	if err := json.Unmarshal(b, &prd); err != nil {
		return nil
	}

	d := &SessionAutomatonDetail{
		PRDID:     prdID,
		PRDTitle:  prd.Title,
		PRDStatus: prd.Status,
		StoryID:   sess.StoryID,
		TaskID:    taskID,
		Cost: SessionAutomatonCost{
			TokensIn:   sess.TokensIn,
			TokensOut:  sess.TokensOut,
			EstCostUSD: sess.EstCostUSD,
		},
	}

	story, task := findAutomatonStoryTask(&prd, sess.StoryID, taskID)
	if story != nil {
		d.StoryID = story.ID
		d.StoryTitle = story.Title
	}
	if task != nil {
		d.TaskTitle = task.Title
		d.TaskSpec, d.TaskSpecTruncated = truncateRunes(task.Spec, automatonTaskSpecMaxRunes)
		d.TaskStatus = task.Status
		d.WaitReason = task.WaitReason
		d.StartedAt = task.StartedAt
		d.CompletedAt = task.CompletedAt
		d.RetryCount = task.RetryCount
		d.ExecutorSource = task.ExecutorSource
		d.Error = task.Error
		d.Verification = task.Verification
		planned := task.FilesPlanned
		if len(planned) == 0 && story != nil {
			planned = story.FilesPlanned
		}
		if task.PreTaskSHA != "" || task.BaseSHA != "" || len(task.AttemptCommitSHAs) > 0 || len(planned) > 0 || len(task.FilesTouched) > 0 || len(task.ForeignFileDrift) > 0 {
			d.Git = &SessionAutomatonGit{
				PreTaskSHA:        task.PreTaskSHA,
				BaseSHA:           task.BaseSHA,
				AttemptCommitSHAs: task.AttemptCommitSHAs,
				FilesPlanned:      planned,
				FilesTouched:      task.FilesTouched,
				ForeignFileDrift:  task.ForeignFileDrift,
			}
		}
	}
	if taskID != "" {
		s.fillAutomatonVisibility(d, taskID)
	}
	// Planning (decompose) sessions have no linked task — surface the
	// PRD-level planner wait reason (why the decompose is queued on
	// capacity) in the same `wait_reason` slot a waiting task already uses.
	if d.WaitReason == "" {
		d.WaitReason = prd.WaitReason
	}
	if d.WaitReason != "" {
		d.WaitReasonReadable = readableWaitSentence(s.autonomousMgr, d.WaitReason)
	}
	// Decisions carry no story/task id, so the block shows the most recent
	// PRD-level entries.
	if n := len(prd.Decisions); n > 0 {
		start := n - automatonDecisionLimit
		if start < 0 {
			start = 0
		}
		d.Decisions = append([]SessionAutomatonDecision(nil), prd.Decisions[start:]...)
	}
	return d
}

// fillAutomatonVisibility adds the wait cause, its reset time and the task's
// holdings from the autonomous per-task views, when the manager supplies them.
func (s *Server) fillAutomatonVisibility(d *SessionAutomatonDetail, taskID string) {
	p, ok := s.autonomousMgr.(taskVisibilityProvider)
	if !ok {
		return
	}
	waiting, running := p.TaskVisibility()
	for _, w := range waiting {
		if w.TaskID == taskID {
			d.WaitCause = string(w.Cause)
			d.WaitResetsAt = w.ResetsAt
			if d.WaitReason == "" {
				d.WaitReason = w.Reason
			}
			break
		}
	}
	for _, h := range running {
		if h.TaskID == taskID {
			d.Holds = h.Pools
			d.HoldsNote = h.Note
			break
		}
	}
}

// findAutomatonStoryTask locates the task by id (and its parent story);
// when no task matches it falls back to the story by id.
func findAutomatonStoryTask(prd *automatonPRDView, storyID, taskID string) (*automatonStoryView, *automatonTaskView) {
	if taskID != "" {
		for si := range prd.Stories {
			for ti := range prd.Stories[si].Tasks {
				if prd.Stories[si].Tasks[ti].ID == taskID {
					return &prd.Stories[si], &prd.Stories[si].Tasks[ti]
				}
			}
		}
	}
	if storyID != "" {
		for si := range prd.Stories {
			if prd.Stories[si].ID == storyID {
				return &prd.Stories[si], nil
			}
		}
	}
	return nil, nil
}

// legacySprintFromAutomaton builds the backward-compatible `sprint` map
// that mobile clients read. Keys are stable; new keys are additive.
func legacySprintFromAutomaton(d *SessionAutomatonDetail) map[string]any {
	sprint := map[string]any{
		"sprint_id": d.PRDID,
		"title":     d.PRDTitle,
		"status":    d.PRDStatus,
		"automata":  d.PRDTitle,
	}
	if d.TaskID != "" {
		sprint["task_id"] = d.TaskID
	}
	if d.TaskTitle != "" {
		sprint["task_title"] = d.TaskTitle
	}
	if d.StoryTitle != "" {
		sprint["story_title"] = d.StoryTitle
	}
	return sprint
}
