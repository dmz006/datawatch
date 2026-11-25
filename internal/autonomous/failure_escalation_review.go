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
	"log"
	"strings"
	"time"
)

// Actions an operator can take on a held task.
const (
	reviewActionRetry  = "retry"
	reviewActionCancel = "cancel"
	reviewActionAccept = "accept"
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
	// readOnlyChanged and readOnly carry the read-only mark; reportPath is the
	// report path to set, empty when unchanged.
	readOnlyChanged bool
	readOnly        bool
	reportPath      string
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
	// The next attempt is admitted afresh from the task's current worker, never
	// from the pair the failed attempt was admitted with.
	t.AdmittedBackend, t.AdmittedModel = "", ""
	t.AdmittedTaskBackend, t.AdmittedTaskModel = "", ""
	if r.filesChanged {
		t.FilesPlanned = r.files
	}
	if r.readOnlyChanged {
		t.ReadOnly = r.readOnly
		if !r.readOnly {
			// A task that writes files cannot keep a report path.
			t.ReportPath = ""
		}
	}
	if r.reportPath != "" {
		t.ReportPath = r.reportPath
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
	if r.readOnlyChanged {
		if r.readOnly {
			b.WriteString("The operator marked this task read-only: it must not change any file")
			if r.reportPath != "" {
				b.WriteString(" except its report file " + r.reportPath)
			}
			b.WriteString(".\n")
		} else {
			b.WriteString("The operator cleared the read-only mark: this task may change files.\n")
		}
	} else if r.reportPath != "" {
		b.WriteString("The operator set the report file of this read-only task to " + r.reportPath + ".\n")
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
	if r.readOnlyChanged {
		changes = append(changes, fmt.Sprintf("read_only=%t", r.readOnly))
	}
	if r.reportPath != "" {
		changes = append(changes, "report_path="+r.reportPath)
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
// Action "retry" requeues the task to pending, with optional changes: a
// replaced spec; a worker (task-level backend, model and effort, checked the
// way SetTaskLLM checks them); a narrowed scope (the planned files, linted
// the way the pre-spawn check lints them); a read-only mark; and a report path
// (checked with validateReportPath, and only for a task that is read-only after
// the change, which must also end with no planned files). Every change is checked before
// anything is written, so a refused change leaves the task held. Action
// "cancel" cancels the task through CancelTask. Action "accept" completes a
// task whose last worker session has a completion marker and whose commit,
// named files and own check hold up (see acceptHeldTask). Every action reopens a blocked
// Automaton so the next Run rolls it up.
//
// It returns ErrTaskNotInReview, wrapped, unless the task is held in
// TaskReviewNeeded.
func (m *Manager) ResolveTaskReview(prdID, taskID string, req ReviewResolution, actor string) (*PRD, error) {
	switch req.Action {
	case reviewActionRetry, reviewActionCancel, reviewActionAccept:
	default:
		return nil, fmt.Errorf("review action %q is not valid; use %q, %q or %q", req.Action, reviewActionRetry, reviewActionCancel, reviewActionAccept)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if !isOperatorActor(actor) {
		if why := m.closedReason(prd); why != "" {
			return nil, fmt.Errorf("prd %q is closed (%s): only the operator resolves a task of a closed Automaton", prdID, why)
		}
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
	if req.Action == reviewActionAccept {
		return m.acceptHeldTask(prdID, taskID, actor, req.Note)
	}
	if req.Action == reviewActionCancel {
		return m.cancelHeldTask(prdID, taskID, actor, req.Note)
	}
	var capHeld bool
	m.store.WithLock(func() { capHeld = task.AttemptCapHeld })
	if capHeld {
		return nil, fmt.Errorf("task %q is held at the attempt cap and a retry does not start it; "+
			"only the operator's force reset (reset_task with force) clears the attempt count, or cancel the task", taskID)
	}
	if err := m.refuseHeld(prd); err != nil {
		return nil, err
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
	if req.ReadOnly != nil {
		r.readOnlyChanged, r.readOnly = true, *req.ReadOnly
	}
	readOnlyAfter := task.ReadOnly
	if r.readOnlyChanged {
		readOnlyAfter = r.readOnly
	}
	if req.ReportPath != "" {
		if err := validateReportPath(req.ReportPath); err != nil {
			return nil, err
		}
		if !readOnlyAfter {
			return nil, fmt.Errorf("report_path %q requires a read_only task; task %q is not read_only", req.ReportPath, task.ID)
		}
		r.reportPath = req.ReportPath
	}
	if readOnlyAfter && len(files) > 0 && (r.readOnlyChanged || r.filesChanged) {
		return nil, fmt.Errorf("a read_only task must not have planned files; task %q would end read_only with %d planned files (clear its files first, or do not mark it read_only)", task.ID, len(files))
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
// the Automaton (reopenBlockedPRD, manager.go).
func (m *Manager) cancelHeldTask(prdID, taskID, actor, note string) (*PRD, error) {
	reason := "cancelled after operator review"
	if note != "" {
		reason += ": " + note
	}
	if _, err := m.CancelTask(prdID, taskID, actor, reason); err != nil {
		return nil, err
	}
	m.reopenBlockedPRD(prdID, taskID, actor)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// heldTaskHasCompletionMarker reports whether the last worker session of a held
// task finished with a completion marker in its recorded output.
func (m *Manager) heldTaskHasCompletionMarker(t *Task) bool {
	var sid string
	m.store.WithLock(func() {
		if a := lastTaskAttempt(t); a != nil {
			sid = a.SessionID
		}
		if sid == "" {
			sid = t.SessionID
		}
	})
	if sid == "" {
		return false
	}
	m.mu.Lock()
	fn := m.sessionOutputFn
	m.mu.Unlock()
	if fn == nil {
		return false
	}
	out, ok := fn(sid)
	return ok && outputHasCompletionMarker(out)
}

// acceptHeldTask completes a held task with its stored result or commit. It
// refuses a task whose last worker session has no completion marker, or whose
// evidence does not hold up (checkAcceptEvidence): the marker alone never
// completes a task. There is no override. It reuses
// the verified-completion path (completeVerifyingTaskFromVerdict), withdraws
// the held question as retry does, records a decision and an audit record
// naming the actor, and reopens a blocked Automaton as cancel does.
func (m *Manager) acceptHeldTask(prdID, taskID, actor, note string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	t, _ := findTaskInPRD(prd, taskID)
	if t == nil {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	if !m.heldTaskHasCompletionMarker(t) {
		return nil, fmt.Errorf("task %q cannot be accepted: its last worker session has no completion marker; retry or cancel it instead", taskID)
	}
	var session string
	var attempts int
	m.store.WithLock(func() { session, attempts = t.SessionID, len(t.Attempts) })
	proof, err := m.checkAcceptEvidence(prd, t)
	if err != nil {
		return nil, fmt.Errorf("task %q cannot be accepted: %w; retry or cancel it instead", taskID, err)
	}
	// The task's check can run for minutes, so the task is read again. It is
	// claimed in the same lock, so a decision taken meanwhile is not undone.
	if prd, ok = m.store.GetPRD(prdID); !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if t, _ = findTaskInPRD(prd, taskID); t == nil {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	var prev TaskStatus
	var sha string
	var changed bool
	m.store.WithLock(func() {
		if changed = t.Status != TaskReviewNeeded || t.SessionID != session || len(t.Attempts) != attempts; changed {
			return
		}
		prev, t.Status = t.Status, TaskCompleted
		if n := len(t.AttemptCommitSHAs); n > 0 {
			sha = t.AttemptCommitSHAs[n-1]
		}
	})
	if changed {
		return nil, fmt.Errorf("task %q changed while it was being checked: %w", taskID, ErrTaskNotInReview)
	}
	if proof.commit != "" {
		sha = proof.commit
	}
	ev := m.completeVerifyingTaskFromVerdict(nil, "accepted by "+actor, prev, t, nil)
	m.store.WithLock(func() {
		t.ReviewSummary = nil
		if t.FailureReason != nil && (t.FailureReason.Source == failureEscalationHoldSource ||
			t.FailureReason.Source == infraRetryHoldSource) {
			t.FailureReason = nil
		}
		if t.PostVerifySHA == "" {
			t.PostVerifySHA = sha
		}
		clearInfraRetryCount(t)
	})
	if err := m.store.SaveTask(t); err != nil {
		return nil, err
	}
	dnote := fmt.Sprintf("task=%s result accepted by %s after operator review", taskID, actor)
	if proof.warning != "" {
		dnote += " (warning: " + proof.warning + ")"
	}
	if note != "" {
		dnote += ": " + note
	}
	now := time.Now()
	if cur, ok := m.store.GetPRD(prdID); ok {
		m.store.WithLock(func() {
			cur.Decisions = append(cur.Decisions, Decision{At: now, Kind: reviewDecisionKind, Actor: reviewDecisionActor, Note: dnote})
			cur.UpdatedAt = now
		})
		if err := m.store.SavePRD(cur); err != nil {
			log.Printf("[autonomous] prd=%s record accept decision: %v", prdID, err)
		}
		dec := lastDecision(cur, reviewDecisionKind)
		m.emitPendingTaskEvents(cur, []pendingTaskEvent{ev}, dec)
	}
	m.withdrawInfraHoldQuestions(prdID, taskID, actor, "task accepted by "+actor+" without answering the question")
	m.mu.Lock()
	auditFn := m.auditFn
	m.mu.Unlock()
	if auditFn != nil {
		auditFn("autonomous_review_accept", map[string]any{
			"prd_id": prdID, "task_id": taskID, "actor": actor, "commit": sha, "note": note,
			"warning": proof.warning,
		})
	}
	m.reopenBlockedPRDWithNote(prdID, dnote)
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
