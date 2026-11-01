// Held question table — the pure, model-free mapping from one held
// Automaton state to the operator question the daemon raises for it.
//
// A held question is asked when an Automaton sits in one of the five held
// states (the config package's HeldState* ids: blocked, review_held,
// revision_requested, awaiting_decision, pending_insert). Everything here is
// a fixed table: which options the question offers, which one is
// recommended and why, and what the question text says. No model call, no
// store access — the caller persists the built Question through the
// existing question store.
//
// The options map to REAL actions only, using the action names the code
// already accepts: ReviewResolution.Action's "retry"/"cancel" with an
// optional spec change or worker change (ResolveTaskReview in
// failure_escalation_review.go), ApproveInsertedMember / a rejected
// pending insert (automation_insert.go), and the Manager Resume/Approve
// paths. Every question carries the "hold" option, and "hold" is always
// the default, so no choice is ever silent.
package autonomous

import (
	"fmt"
	"strings"
)

// HeldState is one held state id — the same vocabulary the config package
// validates under autonomous.questions.held_states.
type HeldState string

// The five held state ids a held question is raised for.
const (
	HeldBlocked           HeldState = "blocked"
	HeldReviewHeld        HeldState = "review_held"
	HeldRevisionRequested HeldState = "revision_requested"
	HeldAwaitingDecision  HeldState = "awaiting_decision"
	HeldPendingInsert     HeldState = "pending_insert"
)

// HeldStates are the five held state ids in order.
var HeldStates = []HeldState{
	HeldBlocked,
	HeldReviewHeld,
	HeldRevisionRequested,
	HeldAwaitingDecision,
	HeldPendingInsert,
}

// Failure categories: the same vocabulary the executor classifies attempts
// in (TaskAttempt.FailureCategory) and the retry budget is kept in
// (TriageRetryCategories), "infra", "content", "scope" or "policy". An
// empty or unknown category is not one of these and reads as unclassified.
const (
	FailureCategoryInfra   = "infra"
	FailureCategoryContent = "content"
	FailureCategoryScope   = "scope"
	FailureCategoryPolicy  = "policy"
)

// Option ids for held questions. Stable strings the resolver and the
// operator's answer both address by. Every id maps to a real action the
// Manager already implements; "hold" keeps the Automaton where it is.
const (
	// HeldOptionHold keeps the Automaton held with no change.
	HeldOptionHold = "hold"
	// HeldOptionRetry is ReviewResolution.Action "retry" unchanged.
	HeldOptionRetry = "retry"
	// HeldOptionRetryStronger is a "retry" whose worker is one of the
	// item's available backends.
	HeldOptionRetryStronger = "retry_stronger"
	// HeldOptionChangeSpec is a "retry" carrying a replaced spec (and
	// optionally a narrowed file set).
	HeldOptionChangeSpec = "change_spec"
	// HeldOptionCancel is ReviewResolution.Action "cancel".
	HeldOptionCancel = "cancel"
	// HeldOptionApproveInsert is ApproveInsertedMember on the item's
	// pending insert.
	HeldOptionApproveInsert = "approve_insert"
	// HeldOptionRejectInsert drops the item's pending insert.
	HeldOptionRejectInsert = "reject_insert"
	// HeldOptionResume releases a blocked or revision-requested Automaton
	// through the Manager Resume/Approve paths so the next Run continues.
	HeldOptionResume = "resume"
)

// heldOptionLabels is the display label each option id carries. Labels are
// short, unique and within the question option label cap.
var heldOptionLabels = map[string]string{
	HeldOptionHold:          "Hold — keep it held, take no action yet",
	HeldOptionRetry:         "Retry the task on its current worker",
	HeldOptionRetryStronger: "Retry the task on a stronger available backend",
	HeldOptionChangeSpec:    "Change the task spec before retrying",
	HeldOptionCancel:        "Cancel the task and let the Automaton roll up",
	HeldOptionApproveInsert: "Approve the pending insert into the Automation chain",
	HeldOptionRejectInsert:  "Reject the pending insert",
	HeldOptionResume:        "Release the hold and let the Automaton continue",
}

// heldOptionTradeoffs is the cost-and-risk note each option id carries, so
// an unfamiliar operator can weigh every choice without knowing the daemon's
// failure taxonomy or the action behind it. Every option id has a note;
// they are plain text within the question tradeoffs byte cap and never
// carry internal ids.
var heldOptionTradeoffs = map[string]string{
	HeldOptionHold:          "No action runs and nothing is spent; the risk is that the work stays held until someone unblocks it later",
	HeldOptionRetry:         "Costs one more attempt of the retry budget on the same worker; it can fail the same way if the fault is in the work rather than the moment",
	HeldOptionRetryStronger: "Costs a slower, more costly worker for the same task; it can still fail if the spec or the plan, not the worker, is at fault",
	HeldOptionChangeSpec:    "Costs your time rewriting the spec before any more attempts; if the diagnosis is wrong the new spec fails too",
	HeldOptionCancel:        "Ends the task for good and lets the Automaton roll up; the work behind the goal is lost until it is replanned",
	HeldOptionApproveInsert: "Adds the pending member to the live Automation chain at once; a wrong insert keeps running in the chain until it is edited out",
	HeldOptionRejectInsert:  "The insert stays pending because the daemon has no path to drop it, so it keeps waiting until it is approved or removed by hand",
	HeldOptionResume:        "Re-admits the work into the run; if the condition that held it has not actually cleared it can be held again",
}

// heldOptionsByState is the option set each held state offers, in display
// order. "hold" is the last option on every state.
var heldOptionsByState = map[HeldState][]string{
	HeldBlocked:           {HeldOptionResume, HeldOptionRetry, HeldOptionChangeSpec, HeldOptionCancel, HeldOptionHold},
	HeldReviewHeld:        {HeldOptionRetry, HeldOptionRetryStronger, HeldOptionChangeSpec, HeldOptionCancel, HeldOptionHold},
	HeldRevisionRequested: {HeldOptionChangeSpec, HeldOptionResume, HeldOptionHold},
	HeldAwaitingDecision:  {HeldOptionResume, HeldOptionCancel, HeldOptionHold},
	HeldPendingInsert:     {HeldOptionApproveInsert, HeldOptionRejectInsert, HeldOptionHold},
}

// heldOptionFallback is the set offered on a state that is not one of the
// five: real actions only, ending in hold.
var heldOptionFallback = []string{HeldOptionRetry, HeldOptionCancel, HeldOptionHold}

// HeldItem is everything the table needs to know about one held Automaton.
// It is plain data the caller fills in from the PRD, the Automation and the
// holding condition — no store reads and no model calls happen from it.
type HeldItem struct {
	// State is one of the five HeldState ids (or, defensively, an unknown
	// one, which gets the fallback options and the hold recommendation).
	State HeldState
	// PRDID and PRDTitle name the held Automaton.
	PRDID    string
	PRDTitle string
	// TaskID and TaskTitle name the task behind the hold when there is one.
	TaskID    string
	TaskTitle string
	// AutomationID and AutomationName name the owning Automation when the
	// Automaton is one of its members.
	AutomationID   string
	AutomationName string
	// MemberID is the member id within the owning Automation when set.
	MemberID string
	// PendingID is the pending insert id, for HeldPendingInsert only.
	PendingID string
	// FailureCategory is one of FailureCategoryInfra/Content/Scope/Policy,
	// taken from the attempt's classification (TaskAttempt.FailureCategory)
	// or the matching retry budget category; empty when unclassified.
	FailureCategory string
	// Reason is the block or failure reason, already capped at its source.
	Reason string
	// AttemptsUsed is how many attempts the task has spent so far.
	AttemptsUsed int
	// AvailableBackends are the worker backends a retry could use.
	AvailableBackends []string
}

// HeldOptions returns the question options for item in display order: the
// state's real actions followed by the mandatory "hold" option. A nil item
// or an unknown state gets the fallback set. Option ids are the stable
// HeldOption* strings, labels are unique and non-empty, and every option
// carries a tradeoffs note naming its cost and its risk, so the rendered
// question stands alone for an operator who does not know the daemon.
func HeldOptions(item *HeldItem) []QuestionOption {
	ids := heldOptionFallback
	if item != nil {
		if s, ok := heldOptionsByState[item.State]; ok {
			ids = s
		}
	}
	opts := make([]QuestionOption, 0, len(ids))
	for _, id := range ids {
		opts = append(opts, QuestionOption{ID: id, Label: heldOptionLabels[id], Tradeoffs: heldOptionTradeoffs[id]})
	}
	return opts
}

// heldRecommendationTable is the fixed (state, failure category) -> option
// table. Every cell returns a real option of that state; the fallback cell
// (key "") returns hold, so an unclassified or unknown category never
// recommends an action. The rationale is one sentence.
var heldRecommendationTable = map[HeldState]map[string]heldRecommendationRow{
	HeldBlocked: {
		"":                     {HeldOptionHold, "The block is waiting on something outside this task, so keeping it held until it clears or the operator rules is the safe default."},
		FailureCategoryInfra:   {HeldOptionHold, "An infra block usually clears on its own, so the cheapest move is to keep it held."},
		FailureCategoryContent: {HeldOptionHold, "A content block needs operator input that has not arrived, so it should stay held."},
		FailureCategoryScope:   {HeldOptionHold, "A scope block is the operator's ruling to make, so the Automaton should stay held."},
		FailureCategoryPolicy:  {HeldOptionHold, "A policy block is the operator's ruling to make, so the Automaton should stay held."},
	},
	HeldReviewHeld: {
		"":                     {HeldOptionHold, "The failure is unclassified, so keeping it held for operator review is the safe default."},
		FailureCategoryInfra:   {HeldOptionRetry, "Infra failures are usually transient, so one more attempt on the same worker is the cheapest next step."},
		FailureCategoryContent: {HeldOptionChangeSpec, "Content failures point at the spec, so fixing the spec before any retry is the real fix."},
		FailureCategoryScope:   {HeldOptionChangeSpec, "Scope failures mean the planned files do not match the work, so the spec or file set needs a change first."},
		FailureCategoryPolicy:  {HeldOptionHold, "Policy failures are the operator's ruling to make, so the task should stay held."},
	},
	HeldRevisionRequested: {
		"":                     {HeldOptionChangeSpec, "A revision was requested, so changing the spec is the action that satisfies it."},
		FailureCategoryInfra:   {HeldOptionChangeSpec, "A revision was requested, so changing the spec is the action that satisfies it."},
		FailureCategoryContent: {HeldOptionChangeSpec, "A revision was requested, so changing the spec is the action that satisfies it."},
		FailureCategoryScope:   {HeldOptionChangeSpec, "A revision was requested, so changing the spec is the action that satisfies it."},
		FailureCategoryPolicy:  {HeldOptionChangeSpec, "A revision was requested, so changing the spec is the action that satisfies it."},
	},
	HeldAwaitingDecision: {
		"":                     {HeldOptionHold, "It is waiting on an operator decision, so keeping it held until the decision lands is the default."},
		FailureCategoryInfra:   {HeldOptionHold, "It is waiting on an operator decision, so keeping it held until the decision lands is the default."},
		FailureCategoryContent: {HeldOptionHold, "It is waiting on an operator decision, so keeping it held until the decision lands is the default."},
		FailureCategoryScope:   {HeldOptionHold, "It is waiting on an operator decision, so keeping it held until the decision lands is the default."},
		FailureCategoryPolicy:  {HeldOptionHold, "It is waiting on an operator decision, so keeping it held until the decision lands is the default."},
	},
	HeldPendingInsert: {
		"":                     {HeldOptionHold, "The insert is waiting on operator approval, so approve or reject it explicitly rather than let it sit."},
		FailureCategoryInfra:   {HeldOptionHold, "The insert is waiting on operator approval, so approve or reject it explicitly rather than let it sit."},
		FailureCategoryContent: {HeldOptionHold, "The insert is waiting on operator approval, so approve or reject it explicitly rather than let it sit."},
		FailureCategoryScope:   {HeldOptionHold, "The insert is waiting on operator approval, so approve or reject it explicitly rather than let it sit."},
		FailureCategoryPolicy:  {HeldOptionHold, "The insert is waiting on operator approval, so approve or reject it explicitly rather than let it sit."},
	},
}

type heldRecommendationRow struct {
	OptionID  string
	Rationale string
}

var heldRecommendationFallback = heldRecommendationRow{
	OptionID:  HeldOptionHold,
	Rationale: "This hold is not in the table, so keeping it held is the only safe default.",
}

// HeldRecommendation returns the fixed-table recommendation for item: the
// option id and a one-sentence rationale, keyed by (state, failure
// category). It never calls a model. An unknown category (empty or
// anything other than infra, content, scope or policy) falls back to hold,
// and a nil item or unknown state falls back to hold as well. The returned
// option id is always among HeldOptions(item).
func HeldRecommendation(item *HeldItem) *QuestionRecommendation {
	row := heldRecommendationFallback
	if item != nil {
		if byCategory, ok := heldRecommendationTable[item.State]; ok {
			if r, ok := byCategory[item.FailureCategory]; ok {
				row = r
			} else {
				row = byCategory[""]
			}
		}
	}
	return &QuestionRecommendation{OptionID: row.OptionID, Rationale: row.Rationale}
}

// heldStateText is the plain description of each held state, used by the
// question text. It names the Automaton, never "PRD".
var heldStateText = map[HeldState]string{
	HeldBlocked:           "blocked, waiting on a condition outside the task",
	HeldReviewHeld:        "held for operator review after failed attempts",
	HeldRevisionRequested: "held because a revision of its spec was requested",
	HeldAwaitingDecision:  "waiting on an operator decision",
	HeldPendingInsert:     "held as a pending insert waiting on operator approval",
}

// HeldQuestionText renders the plain-text question body: what is held, why,
// naming the Automaton with its title and id, owning Automation and member
// when set, the task when set, and the reason. It is capped at
// questionTextMaxBytes and never uses the word "PRD" — the held work is an
// Automaton.
func HeldQuestionText(item *HeldItem) string {
	if item == nil {
		item = &HeldItem{}
	}
	var b strings.Builder

	who := "this Automaton"
	switch {
	case item.PRDTitle != "" && item.PRDID != "":
		who = fmt.Sprintf("Automaton %q (%s)", item.PRDTitle, item.PRDID)
	case item.PRDTitle != "":
		who = fmt.Sprintf("Automaton %q", item.PRDTitle)
	case item.PRDID != "":
		who = "Automaton with id " + item.PRDID
	}
	state := heldStateText[item.State]
	if state == "" {
		state = "in held state " + string(item.State)
	}
	fmt.Fprintf(&b, "%s is %s.", who, state)

	if item.AutomationID != "" {
		a := item.AutomationID
		if item.AutomationName != "" {
			a = fmt.Sprintf("%q (%s)", item.AutomationName, a)
		}
		b.WriteString(" It is a member of Automation " + a)
		if item.MemberID != "" {
			b.WriteString(" as member " + item.MemberID)
		}
		b.WriteString(".")
	}
	if item.TaskID != "" {
		var task string
		if item.TaskTitle != "" {
			task = fmt.Sprintf("task %q (%s)", item.TaskTitle, item.TaskID)
		} else {
			task = "task " + item.TaskID
		}
		b.WriteString(" Its work is at " + task + ".")
	}
	if item.PendingID != "" {
		b.WriteString(" Pending insert id is " + item.PendingID + ".")
	}
	if reason := strings.TrimSpace(item.Reason); reason != "" {
		b.WriteString(" Why it is held: " + reason + ".")
	}
	if item.FailureCategory != "" {
		b.WriteString(" Failure category: " + item.FailureCategory + ".")
	}
	if item.AttemptsUsed > 0 {
		fmt.Fprintf(&b, " Attempts used so far: %d.", item.AttemptsUsed)
	}
	if len(item.AvailableBackends) > 0 {
		b.WriteString(" Available backends: " + strings.Join(item.AvailableBackends, ", ") + ".")
	}
	// The closing line names who decides and the enforced default, so the
	// question is complete on its own for an unfamiliar operator: the
	// decision is never left to the reader, and the default (hold, which
	// acts on nothing) is stated next to the options it overrides.
	b.WriteString(" You have the call: the default is hold, which acts on nothing and keeps the hold in place, or pick any option to act now.")
	text := b.String()
	if len(text) > questionTextMaxBytes {
		text = strings.TrimRight(text[:questionTextMaxBytes], " ,;:")
	}
	return text
}

// BuildHeldQuestion assembles the operator question for item: its options,
// its fixed-table recommendation, the plain text, and the record fields a
// held question carries — Context is task when the hold names a task (with
// both the task id and the Automaton id as links) and prd otherwise,
// Default is always hold, Label is decision, AskerKind is the daemon's
// existing system kind, Source is QuestionSourceHeld, and Blocking is true.
// The result passes ValidateNewQuestion.
func BuildHeldQuestion(item *HeldItem) Question {
	if item == nil {
		item = &HeldItem{}
	}
	q := Question{
		AskedBy:        "autonomous",
		AskerKind:      QuestionAskerSystem,
		Label:          "decision",
		Text:           HeldQuestionText(item),
		Options:        HeldOptions(item),
		Recommendation: HeldRecommendation(item),
		Default:        HeldOptionHold,
		Blocking:       true,
		Source:         QuestionSourceHeld,
	}
	switch {
	case item.TaskID != "":
		q.Context = QuestionContextTask
		q.TaskID = item.TaskID
		q.PRDID = item.PRDID
	case item.PRDID != "":
		q.Context = QuestionContextPrd
		q.PRDID = item.PRDID
	default:
		q.Context = QuestionContextNone
	}
	return q
}
