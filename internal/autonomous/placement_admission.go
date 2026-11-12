// Placement admission wires the pure compute.PlaceMissingModel planner into
// task admission: before a worker's model is loaded, the live missing_model
// policy is applied to the planner's verdict — refuse, auto-load, or ask the
// operator. The gate runs in the admission path (admitPreferring), so the
// lease is granted on the node placement approved, and the worker session
// starts there.
package autonomous

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
)

// PlacementPlan is the planner verdict for one (backend, model) pairing,
// supplied by the daemon: the node placement chose, the missing_model policy
// in effect, and the planner's refusal lines (one per checked node) when it
// chose none. It is the autonomous package's only view of placement — the
// pure planner and the registries it needs stay in compute and the daemon.
type PlacementPlan struct {
	// Relevant is false when placement has no opinion for this pairing
	// (no compute nodes at all): the gate is skipped.
	Relevant bool
	// Serving is true when the node placement chose already serves the model
	// (it is in the node's /api/tags list). Nothing is missing then, so the
	// missing_model policy has nothing to decide and the gate is skipped:
	// admission proceeds as it always has.
	Serving bool
	// Chosen is the node placement picked for the model, or "" when it
	// refused every candidate node.
	Chosen string
	// Policy is the live missing_model value: "ask", "auto_load" or
	// "refuse" (effective via ComputePlacementConfig.EffectiveMissingModel).
	Policy string
	// Refused is the planner's one line per checked node, the node's name
	// first and then what that node lacks. Empty when a node was chosen.
	Refused []string
}

// PlacementAdvisor supplies a PlacementPlan at the admission gate. The daemon
// implementation reads the live config, the inference and compute registries
// and the observer's latest payloads, then hands them to the pure compute
// planner. With no advisor wired (tests, capacity off) the gate is skipped
// and the load proceeds exactly as before.
type PlacementAdvisor interface {
	// Plan returns the planner verdict for the model the task will run.
	// backend and model are the admission pair (model as the PRD names it);
	// pinnedTo is the task's pinned node ("" when unpinned).
	Plan(backend, model, pinnedTo string) PlacementPlan
	// AskTimeout is how long an unanswered ask waits before it is refused.
	AskTimeout() time.Duration
}

// PlacementRefused is the error an admission attempt ends with when the
// missing_model policy declines the load: the policy is "refuse", the
// operator answered refuse, or the ask timed out. The task fails from here
// the way a failed start does. QuestionID is set when an operator question
// was raised before the refusal.
type PlacementRefused struct {
	Node       string
	Model      string
	Policy     string
	QuestionID string
	// Refused is the planner's line per checked node, for the alert.
	Refused []string
}

func (e *PlacementRefused) Error() string {
	what := "placement declined the load"
	switch e.Policy {
	case config.ComputePlacementRefuse:
		what = "the missing_model policy is refuse"
	case config.ComputePlacementAsk:
		what = "the operator declined, or the ask timed out"
	}
	b := fmt.Sprintf("model %s was not loaded on node %s: %s", e.Model, e.Node, what)
	if lines := strings.Join(e.Refused, " | "); lines != "" {
		b += " (placement: " + lines + ")"
	}
	if e.QuestionID != "" {
		b += " [question " + e.QuestionID + "]"
	}
	return b
}

// IsPlacementRefused reports whether err is a placement refusal, so callers
// can tell a declined placement from a failed load.
func IsPlacementRefused(err error) bool {
	var refused *PlacementRefused
	return errors.As(err, &refused)
}

// placementQuestionAsker identifies placement asks in the question store.
const placementQuestionAsker = "placement"

// placementQuestionActor is the actor that withdraws an ask that timed out.
const placementQuestionActor = "placement"

// placementOptionLoad names the "load it" option; placementOptionRefuse the
// "refuse" option.
const (
	placementOptionLoad   = "load"
	placementOptionRefuse = "refuse"
)

// placementWaitPoll is the longest the ask waits between answer checks.
var placementWaitPoll = 250 * time.Millisecond

// placementQuestionStore returns the question store the gate raises its ask
// on, or nil when the gate has no channel (no question wired).
func (m *Manager) placementQuestionStore() *QuestionStore {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.decisionStore
}

// getPlacementAdvisor returns the wired placement advisor, or nil when none
// was wired (tests, capacity off). The gate is a no-op when nil.
func (m *Manager) getPlacementAdvisor() PlacementAdvisor {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.placementAdvisor
}

// placementAskKey is the dedupe key for a placement ask: one pending ask per
// (task, model, node), so a relaunch or a second admission of the same task
// reuses the operator's answer instead of re-asking.
func placementAskKey(taskID, model, node string) string {
	sum := sha256.Sum256([]byte(taskID + "\x00" + model + "\x00" + node))
	return hex.EncodeToString(sum[:])
}

// placementTarget is where the gate decided the model goes: the node to load
// and lease on, and the policy that allowed it (for the audit record).
type placementTarget struct {
	Node   string
	Policy string
}

// placementGate applies the live missing_model policy to the planner's
// verdict. It is called from admitPreferring, before the lease, so the lease
// is granted on the node placement approved. It returns the target to load
// and lease on, or a zero target when the gate has no opinion (admission
// proceeds exactly as before, and the ledger still ranks the nodes). The
// policy only decides a model that is missing: when the chosen node already
// serves the model, or no advisor is wired, or placement is not relevant, the
// gate returns a zero target.
//
// A *PlacementRefused error means the policy declined the load. When the task
// or the context ends while the ask is open, the gate returns
// errCancelledWhileWaiting (or the context's error), never a refusal: a
// cancelled task is not a declined load and raises no alert.
func (m *Manager) placementGate(ctx context.Context, t *Task, backend, model, fallbackNode string) (placementTarget, error) {
	a := m.getPlacementAdvisor()
	if a == nil {
		return placementTarget{}, nil // no advisor: gate skipped, admission as before
	}
	plan := a.Plan(backend, model, t.ComputeNode)
	if !plan.Relevant || (plan.Serving && plan.Chosen != "") {
		return placementTarget{}, nil // nothing missing, or no opinion: admission as before
	}
	chosen := plan.Chosen
	refuse := func(policy, node, qid string) (placementTarget, error) {
		return placementTarget{}, &PlacementRefused{Node: node, Model: model, Policy: policy, QuestionID: qid, Refused: append([]string(nil), plan.Refused...)}
	}
	switch plan.Policy {
	case config.ComputePlacementRefuse:
		m.emitPlacementAudit(audit.ActionPlacementRefused, model, firstNonEmpty(chosen, fallbackNode), config.ComputePlacementRefuse, strings.Join(plan.Refused, " | "))
		return refuse(config.ComputePlacementRefuse, firstNonEmpty(chosen, fallbackNode), "")
	case config.ComputePlacementAsk:
		target := firstNonEmpty(chosen, fallbackNode)
		// An approval the operator already gave this (task, model, node)
		// stands: no re-asking.
		if m.placementAskApproved(t.ID, model, target) {
			m.emitPlacementAudit(audit.ActionPlacementApproved, model, target, config.ComputePlacementAsk, "prior approval")
			return placementTarget{Node: target, Policy: config.ComputePlacementAsk}, nil
		}
		outcome, qid := m.placementAsk(ctx, t, model, target, plan.Refused, a.AskTimeout())
		switch outcome {
		case placementApproved:
			m.emitPlacementAudit(audit.ActionPlacementApproved, model, target, config.ComputePlacementAsk, "operator approved (question "+qid+")")
			return placementTarget{Node: target, Policy: config.ComputePlacementAsk}, nil
		case placementCancelled:
			if m.taskCancelled(t.ID) {
				return placementTarget{}, errCancelledWhileWaiting
			}
			return placementTarget{}, ctx.Err()
		}
		m.emitPlacementAudit(audit.ActionPlacementDenied, model, target, config.ComputePlacementAsk, "operator declined or the ask timed out (question "+qid+")")
		return refuse(config.ComputePlacementAsk, target, qid)
	default: // auto_load
		if chosen == "" {
			m.emitPlacementAudit(audit.ActionPlacementRefused, model, fallbackNode, config.ComputePlacementAutoLoad, strings.Join(plan.Refused, " | "))
			return refuse(config.ComputePlacementAutoLoad, fallbackNode, "")
		}
		return placementTarget{Node: chosen, Policy: config.ComputePlacementAutoLoad}, nil
	}
}

// emitPlacementAudit records a placement event in the audit log with the
// four fields the audit package documents for placement events: node, model,
// policy and reason. A nil auditFn records nothing.
func (m *Manager) emitPlacementAudit(action, model, node, policy, reason string) {
	m.mu.Lock()
	fn := m.auditFn
	m.mu.Unlock()
	if fn == nil {
		return
	}
	fn(action, map[string]any{
		"node":   node,
		"model":  model,
		"policy": policy,
		"reason": reason,
	})
}

// placementAskApproved reports whether the operator already approved loading
// the model on the node for this task (a prior ask that was answered load).
func (m *Manager) placementAskApproved(taskID, model, node string) bool {
	store := m.placementQuestionStore()
	if store == nil {
		return false
	}
	key := placementAskKey(taskID, model, node)
	qs := store.List(QuestionFilter{Asker: placementQuestionAsker})
	for _, q := range qs {
		if q.TaskID != taskID || q.SourceLineSHA256 != key {
			continue
		}
		if q.State == QuestionAnswered {
			ans, ok := store.CurrentAnswer(q.ID)
			if ok && ans.ChosenOptionID == placementOptionLoad {
				return true
			}
		}
	}
	return false
}

// placementAsk raises the operator question and polls until it is answered,
// withdrawn, or the ask times out. It reports the outcome and the raised
// question's ID (once the raise succeeded), so a declined ask can be named in
// the refusal. The task shows as waiting on the
// decision the while.
//
// The ask is de-duped by (task, model, node): a second raise of the same
// (task, model, node) reuses the existing open question instead of raising
// a duplicate.
func (m *Manager) placementAsk(ctx context.Context, t *Task, model, node string, refused []string, timeout time.Duration) (outcome placementOutcome, questionID string) {
	store := m.placementQuestionStore()
	if store == nil {
		return placementDeclined, "" // no channel: the load is declined and alerted
	}
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	key := placementAskKey(t.ID, model, node)
	q := Question{
		PRDID:     t.PRDID,
		TaskID:    t.ID,
		AskedBy:   placementQuestionAsker,
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextTask,
		Text:      placementQuestionText(model, node, refused),
		Options: []QuestionOption{
			{ID: placementOptionLoad, Label: "load " + model + " on " + node},
			{ID: placementOptionRefuse, Label: "refuse to load"},
		},
		Recommendation:   &QuestionRecommendation{OptionID: placementOptionRefuse, Rationale: "Loading a model that placement refused is the operator's call; holding the node is the default."},
		Default:          "hold",
		SourceLineSHA256: key,
	}
	raised, fresh, err := store.RaiseUnlessOpenSourceLine(q)
	if err != nil {
		return placementDeclined, ""
	}
	questionID = raised.ID
	if fresh { // false: an open ask for this (task, model, node) already exists and is reused
		m.emitPlacementAudit(audit.ActionPlacementAsked, model, node, config.ComputePlacementAsk, "awaiting the operator (question "+questionID+")")
	}
	m.setWaiting(t, "model placement awaiting approval")
	defer m.clearWaiting(t)
	return m.awaitPlacementDecision(ctx, t, raised.ID, timeout), questionID
}

// placementQuestionText is the operator-facing body of the ask.
func placementQuestionText(model, node string, refused []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Automaton is trying to load %s on node %s (the model is not on it yet). Options: load it / refuse.", model, node)
	if lines := strings.Join(refused, " | "); lines != "" {
		fmt.Fprintf(&b, " Placement refused: %s", lines)
	}
	return b.String()
}

// placementOutcome is how an ask ended.
type placementOutcome int

const (
	// placementDeclined: the operator answered refuse, the ask was withdrawn or
	// expired, or it timed out (a timeout is a refusal).
	placementDeclined placementOutcome = iota
	// placementApproved: the operator answered load.
	placementApproved
	// placementCancelled: the task was cancelled or the context ended while
	// the ask was open. Not a refusal.
	placementCancelled
)

// awaitPlacementDecision polls the question store until the ask is answered,
// withdrawn, expired, the task is cancelled, or the timeout lands. On a
// timeout or a cancel it withdraws the ask itself, so the operator sees a
// closed question, not a stale one.
func (m *Manager) awaitPlacementDecision(ctx context.Context, t *Task, questionID string, timeout time.Duration) placementOutcome {
	store := m.placementQuestionStore()
	if store == nil {
		return placementDeclined
	}
	deadline := time.Now().Add(timeout)
	for {
		if m.taskCancelled(t.ID) || ctx.Err() != nil {
			_, _ = store.Withdraw(questionID, placementQuestionActor, "task ended while the placement ask was open")
			return placementCancelled
		}
		q, ok := store.Get(questionID)
		if !ok {
			// Gone (withdrawn out-of-band): treat as declined.
			return placementDeclined
		}
		switch q.State {
		case QuestionAnswered:
			if ans, ok := store.CurrentAnswer(questionID); ok && ans.ChosenOptionID == placementOptionLoad {
				return placementApproved
			}
			return placementDeclined
		case QuestionWithdrawn, QuestionExpired:
			return placementDeclined
		}
		if time.Now().After(deadline) {
			_, _ = store.Withdraw(questionID, placementQuestionActor, "placement ask timed out")
			return placementDeclined
		}
		select {
		case <-ctx.Done():
		case <-time.After(placementWaitPoll):
		}
	}
}

// raisePlacementAlert records the alert a placement decline raises. A nil
// sink records nothing.
func raisePlacementAlert(sink NodeLoadAlerts, refused *PlacementRefused) {
	if sink == nil || refused == nil {
		return
	}
	title := fmt.Sprintf("Task deferred: model %s was not loaded on node %s", refused.Model, refused.Node)
	var b strings.Builder
	fmt.Fprintf(&b, "Placement declined loading %s on %s (missing_model policy %q).", refused.Model, refused.Node, refused.Policy)
	if lines := strings.Join(refused.Refused, " | "); lines != "" {
		fmt.Fprintf(&b, " Planner: %s", lines)
	}
	b.WriteString(" The task fails from here; raise the node's memory or disk, set the model size in compute_placement, or switch missing_model to auto_load to let admission load it anyway.")
	if refused.QuestionID != "" {
		fmt.Fprintf(&b, " The operator's question is %s.", refused.QuestionID)
	}
	sink.Add(alerts.LevelWarn, title, b.String(), "")
}
