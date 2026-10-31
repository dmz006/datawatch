// Failure escalation — operator actions on a task held for review.
//
// A task held in TaskReviewNeeded (see failure_escalation_hold.go) waits for
// the operator. ResolveTaskReview either retries it, optionally with a changed
// spec, worker or narrowed file scope, or cancels it. SetFailureEscalation sets
// or clears the failure escalation override on an Automaton or on one task.
//
// A retry goes through resetTask, the requeue path ResetTaskWithOptions uses,
// so the operator's changes and the requeue are saved in one write. A cancel
// goes through CancelTask. Both reopen a blocked Automaton so the next Run can
// roll it up; the API layer in api.go relaunches that run.
package autonomous

import (
	"fmt"
	"strings"
	"time"
)

// Actions an operator can take on a held task.
const (
	reviewActionRetry  = "retry"
	reviewActionCancel = "cancel"
)

// Decision kinds and the actor name the operator's review decisions carry.
const (
	reviewDecisionKind       = "review_resolved"
	reviewDecisionActor      = "operator"
	failureEscalationSetKind = "failure_escalation_set"
)

// reviewFilesMax bounds the planned files of a task, as SetTaskFiles does.
const reviewFilesMax = 50

// reviewRequeue is an operator's retry of a held task. ResolveTaskReview builds
// it after checking every change, and resetTask applies it to the task in the
// same save as the requeue.
type reviewRequeue struct {
	req ReviewResolution
	// held is a copy of the report the operator reviewed, or nil.
	held          *ReviewSummary
	workerChanged bool
	filesChanged  bool
	files         []string
}

// apply makes the operator's changes to t and sets the hint the next attempt
// starts with. The previous spec and the cleared report are kept in t.History.
// It runs only after the changes were checked, so it cannot fail.
func (r *reviewRequeue) apply(t *Task, now time.Time) {
	if r.held != nil {
		t.History = append(t.History, TaskHistoryEntry{At: now, Note: "review report cleared by operator retry:\n" + r.held.Text})
	}
	t.ReviewSummary = nil
	if t.FailureReason != nil && (t.FailureReason.Source == failureEscalationHoldSource ||
		t.FailureReason.Source == infraRetryHoldSource) {
		t.FailureReason = nil
	}
	// The operator's retry starts a new incident: the task gets a full set of
	// infrastructure retries again.
	clearInfraRetryCount(t)
	if r.req.Spec != "" {
		t.History = append(t.History, TaskHistoryEntry{At: now, Note: "spec replaced by operator; previous spec:\n" + t.Spec})
		t.Spec = r.req.Spec
	}
	if r.workerChanged {
		t.Backend, t.Model, t.Effort = r.req.Backend, r.req.Model, r.req.Effort
	}
	t.OperatorWorkerSet = r.workerChanged
	if r.filesChanged {
		t.FilesPlanned = r.files
	}
	t.PriorAttemptHint = r.hint()
}

// hint is the note the next attempt starts with: the report the operator
// reviewed, what changed, and the operator's note. The worker also reads every
// recorded attempt through the escalation retry prompt.
func (r *reviewRequeue) hint() string {
	var b strings.Builder
	b.WriteString("This task was held for operator review after failed attempts, and the operator has asked for another attempt.\n")
	if r.held != nil {
		b.WriteString("Review report for the held attempts:\n")
		b.WriteString(r.held.Text)
		b.WriteString("\n")
	}
	if r.req.Spec != "" {
		b.WriteString("The operator replaced the task spec. Follow the spec below, not the earlier one.\n")
	}
	if r.filesChanged {
		b.WriteString("The operator narrowed the files this task may change to: " + strings.Join(r.files, ", ") + ".\n")
	}
	if r.req.Note != "" {
		b.WriteString("Operator note: " + r.req.Note + "\n")
	}
	return b.String()
}

// decisionNote describes the retry for the Automaton's decision log.
func (r *reviewRequeue) decisionNote(taskID, actor string) string {
	var changes []string
	if r.req.Spec != "" {
		changes = append(changes, "spec replaced")
	}
	if r.workerChanged {
		changes = append(changes, "worker set to "+r.req.Backend+"/"+r.req.Model)
	}
	if r.filesChanged {
		changes = append(changes, fmt.Sprintf("scope narrowed to %d files", len(r.files)))
	}
	what := "no changes"
	if len(changes) > 0 {
		what = strings.Join(changes, ", ")
	}
	note := fmt.Sprintf("task=%s retry after operator review requested by %s: %s", taskID, actor, what)
	if r.req.Note != "" {
		note += ": " + r.req.Note
	}
	return note
}

// ResolveTaskReview is the operator's decision on a task held for review.
//
// Action "retry" requeues the task to pending, with three optional changes: a
// replaced spec; a worker (task-level backend, model and effort, checked the
// way SetTaskLLM checks them); and a narrowed scope (the planned files, linted
// the way the pre-spawn check lints them). Every change is checked before
// anything is written, so a refused change leaves the task held. Action
// "cancel" cancels the task through CancelTask. Either action reopens a blocked
// Automaton so the next Run rolls it up.
//
// It returns ErrTaskNotInReview, wrapped, unless the task is held in
// TaskReviewNeeded.
func (m *Manager) ResolveTaskReview(prdID, taskID string, req ReviewResolution, actor string) (*PRD, error) {
	switch req.Action {
	case reviewActionRetry, reviewActionCancel:
	default:
		return nil, fmt.Errorf("review action %q is not valid; use %q or %q", req.Action, reviewActionRetry, reviewActionCancel)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	task, story := findTaskInPRD(prd, taskID)
	if task == nil {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	var status TaskStatus
	var held *ReviewSummary
	m.store.WithLock(func() {
		status = task.Status
		if task.ReviewSummary != nil {
			c := *task.ReviewSummary
			held = &c
		}
	})
	if status != TaskReviewNeeded {
		return nil, fmt.Errorf("task %q is %q: %w", taskID, normTaskStatus(status), ErrTaskNotInReview)
	}
	if req.Action == reviewActionCancel {
		return m.cancelHeldTask(prdID, taskID, actor, req.Note)
	}
	r, err := m.reviewRetryChanges(prdID, prd, task, story, req, held)
	if err != nil {
		return nil, err
	}
	out, err := m.resetTask(prdID, taskID, actor, false, false, r)
	if err == nil {
		m.withdrawInfraHoldQuestions(prdID, taskID, actor, "task retried by "+actor+" without answering the question")
	}
	return out, err
}

// reviewRetryChanges checks a retry's changes against the held task and
// returns the requeue that carries them. It writes nothing.
func (m *Manager) reviewRetryChanges(prdID string, prd *PRD, task *Task, story *Story, req ReviewResolution, held *ReviewSummary) (*reviewRequeue, error) {
	r := &reviewRequeue{req: req, held: held}
	if req.Spec != "" {
		if err := m.checkInjectionGuard("task spec edit", req.Spec); err != nil {
			return nil, err
		}
	}
	if req.Backend != "" || req.Model != "" || req.Effort != "" {
		if !validVerificationEffort(string(req.Effort)) {
			return nil, fmt.Errorf("effort %q is invalid; valid values: low, medium, high, max, quick, normal, thorough, or empty", req.Effort)
		}
		// The pair the task would run with after this change, checked the way
		// SetTaskLLM checks it.
		if err := checkResolvedLLMPair(prdID, &Task{Backend: req.Backend, Model: req.Model}, story, prd, m.AutomationForPRD(prd)); err != nil {
			return nil, err
		}
		r.workerChanged = true
	}
	files := task.FilesPlanned
	if len(req.Files) > 0 {
		files = req.Files
		if len(files) > reviewFilesMax {
			files = files[:reviewFilesMax]
		}
		r.files, r.filesChanged = files, true
	}
	if req.Spec != "" || r.filesChanged {
		spec := task.Spec
		if req.Spec != "" {
			spec = req.Spec
		}
		if problems := LintTaskScopeFor(prd, task, "task '"+task.Title+"'", spec, files); len(problems) > 0 {
			return nil, fmt.Errorf("scope: %s (narrow the files, or widen the Automaton's read or write directories)", strings.Join(problems, "; "))
		}
	}
	return r, nil
}

// cancelHeldTask cancels a held task through CancelTask, then reopens a blocked
// Automaton so the next Run can roll it up. The review decision is recorded on
// the Automaton.
func (m *Manager) cancelHeldTask(prdID, taskID, actor, note string) (*PRD, error) {
	reason := "cancelled after operator review"
	if note != "" {
		reason += ": " + note
	}
	if _, err := m.CancelTask(prdID, taskID, actor, reason); err != nil {
		return nil, err
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	now := time.Now()
	var old PRDStatus
	m.store.WithLock(func() {
		old = prd.Status
		if prd.Status == PRDBlocked {
			prd.Status = PRDRunning
			prd.BlockedReason = ""
		}
		prd.UpdatedAt = now
		prd.Decisions = append(prd.Decisions, Decision{
			At: now, Kind: reviewDecisionKind, Actor: reviewDecisionActor,
			Note: fmt.Sprintf("task=%s cancelled after operator review requested by %s", taskID, actor),
		})
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	if prd.Status != old {
		m.emitPRDStatus(prd, old, lastDecision(prd, reviewDecisionKind), nil)
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetFailureEscalation sets the failure escalation override at the Automaton
// level (taskID empty) or on one task of it. A nil policy clears that override.
// The policy is checked by validateFailureEscalationPolicy. The Automaton must
// be in a status that accepts overrides (autonomous.task_override_statuses),
// the same rule SetTaskLLM applies. It records a failure_escalation_set
// decision.
func (m *Manager) SetFailureEscalation(prdID, taskID string, policy *FailureEscalationPolicy, actor string) (*PRD, error) {
	if err := validateFailureEscalationPolicy(policy); err != nil {
		return nil, err
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	var task *Task
	level := "Automaton"
	if taskID != "" {
		task, _ = findTaskInPRD(prd, taskID)
		if task == nil {
			return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
		}
		level = "task " + taskID
	}
	stored := cloneFailureEscalationPolicy(policy)
	now := time.Now()
	m.store.WithLock(func() {
		if task != nil {
			task.FailureEscalation = stored
			task.UpdatedAt = now
		} else {
			prd.FailureEscalation = stored
		}
		prd.UpdatedAt = now
		prd.Decisions = append(prd.Decisions, Decision{
			At: now, Kind: failureEscalationSetKind, Actor: actor,
			Note: fmt.Sprintf("%s failure escalation: %s", level, describeFailureEscalation(stored)),
		})
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// cloneFailureEscalationPolicy returns a deep copy of p, or nil for nil, so a
// stored override never aliases the caller's value.
func cloneFailureEscalationPolicy(p *FailureEscalationPolicy) *FailureEscalationPolicy {
	if p == nil {
		return nil
	}
	c := *p
	if p.Enabled != nil {
		v := *p.Enabled
		c.Enabled = &v
	}
	c.Steps = cloneEscalationSteps(p.Steps)
	return &c
}

// describeFailureEscalation renders an override for the decision log. A nil
// policy reads as a cleared override.
func describeFailureEscalation(p *FailureEscalationPolicy) string {
	if p == nil {
		return "override cleared"
	}
	var parts []string
	if p.Enabled != nil {
		parts = append(parts, fmt.Sprintf("enabled=%t", *p.Enabled))
	}
	if p.MaxFailures > 0 {
		parts = append(parts, fmt.Sprintf("max_failures=%d", p.MaxFailures))
	}
	if len(p.Steps) > 0 {
		parts = append(parts, "steps="+FormatFailureEscalationSteps(p.Steps))
	}
	if len(parts) == 0 {
		return "no fields set, every field inherits"
	}
	return strings.Join(parts, " ")
}
