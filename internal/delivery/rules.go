package delivery

import "time"

// Limits are how long unfinished work may stay as it is before it is listed.
const (
	LimitPlanning     = 2 * time.Hour
	LimitWaiting      = 24 * time.Hour
	LimitDraft        = 7 * 24 * time.Hour
	LimitHeldMax      = 30 * 24 * time.Hour
	LimitBug          = 7 * 24 * time.Hour
	LimitPendingItem  = 7 * 24 * time.Hour
	LimitSessionWait  = time.Hour
	LimitScheduleWait = 24 * time.Hour
	LimitAnswerWait   = time.Hour
)

// TaskFact is what the rules need of one task.
type TaskFact struct {
	ID          string
	Status      string
	Since       time.Time
	LiveSession bool
	HasWait     bool
}

// AutomatonFact is what the rules need of one Automaton.
type AutomatonFact struct {
	ID               string
	Status           string
	Since            time.Time
	CreatedAt        time.Time
	HasBlockedReason bool
	HasWait          bool
	HeldReason       bool
	HeldUntil        time.Time
	LiveSession      bool
	Tasks            []TaskFact
	// IsBug marks a bug or a bug batch. BugIDs are the bugs filed into a
	// batch; empty means the Automaton is itself the bug.
	IsBug      bool
	BugIDs     []string
	OnMain     bool
	BugDropped bool
	// LandingPending is completed work whose branch is not on main.
	LandingPending bool
}

// WaitsOnPlanner is the WaitsOn value for work waiting on planning.
const WaitsOnPlanner = "planner"

// statusRule is how one unfinished status is checked. A finished status has
// finished true and nothing else.
type statusRule struct {
	finished bool
	limit    time.Duration
	reason   string
	waitsOn  string
}

// automatonRules has every Automaton status. Held and running have rules of
// their own (heldCandidate, runningCandidates). A status missing here is
// listed as one without a rule, and fails the status test.
var automatonRules = map[string]statusRule{
	"draft":           {limit: LimitDraft, reason: "awaiting_planning", waitsOn: WaitsOnOperator},
	"planning":        {limit: LimitPlanning, reason: "planning_in_progress", waitsOn: WaitsOnPlanner},
	"decomposing":     {limit: LimitPlanning, reason: "planning_in_progress", waitsOn: WaitsOnPlanner},
	"needs_review":    {limit: LimitWaiting, reason: "awaiting_review", waitsOn: WaitsOnOperator},
	"approved":        {limit: LimitWaiting, reason: "approved_not_started", waitsOn: WaitsOnOperator},
	"revisions_asked": {limit: LimitWaiting, reason: "awaiting_revision", waitsOn: WaitsOnPlanner},
	"blocked":         {limit: LimitWaiting, reason: "blocked", waitsOn: WaitsOnOperator},
	"failed":          {limit: LimitWaiting, reason: "failed", waitsOn: WaitsOnOperator},
	"paused":          {limit: LimitWaiting, reason: "paused", waitsOn: WaitsOnOperator},
	"held":            {limit: LimitHeldMax, reason: "held", waitsOn: WaitsOnOperator},
	"running":         {limit: LimitWaiting, reason: "recorded_wait", waitsOn: "worker"},
	"active":          {limit: LimitWaiting, reason: "recorded_wait", waitsOn: "worker"},
	"completed":       {finished: true},
	"rejected":        {finished: true},
	"cancelled":       {finished: true},
	"archived":        {finished: true},
}

// How a task status is checked.
const (
	taskCovered  = iota // its Automaton's rule covers it
	taskFinished        // nothing left to do
	taskRunning         // needs a live session or a recorded wait
	taskWaiting         // needs a recorded wait reason
	taskStuck           // someone must act: limited
)

var taskRules = map[string]int{
	"pending": taskCovered, "queued": taskCovered,
	"in_progress": taskRunning, "running_tests": taskRunning, "verifying": taskRunning,
	"waiting_capacity": taskWaiting, "waiting_decision": taskWaiting,
	"failed": taskStuck, "blocked": taskStuck, "review_needed": taskStuck, "awaiting_approval": taskStuck,
	"completed": taskFinished, "cancelled": taskFinished,
}

// StatusRuled reports whether the Automaton status has a rule.
func StatusRuled(status string) bool {
	_, ok := automatonRules[status]
	return ok
}

// TaskStatusRuled reports whether the task status has a rule.
func TaskStatusRuled(status string) bool {
	_, ok := taskRules[status]
	return ok
}

// parked are the batch statuses in which nothing is working on its bugs.
var parked = map[string]bool{
	"draft": true, "held": true, "archived": true, "cancelled": true, "rejected": true, "completed": true,
}

// AutomatonCandidates applies the status rules to Automata, their tasks, the
// bugs filed in them and completed work that is not on main.
func AutomatonCandidates(facts []AutomatonFact, now time.Time, interval time.Duration) []Candidate {
	var out []Candidate
	for _, f := range facts {
		out = append(out, bugCandidates(f)...)
		if f.Status == "completed" && f.LandingPending {
			out = append(out, withStart(Candidate{Kind: KindUnlanded, ID: f.ID, State: "landing_pending",
				Since: f.Since, Limit: LimitWaiting, Reason: "not_on_main", WaitsOn: WaitsOnOperator}))
		}
		rule, ok := automatonRules[f.Status]
		if !ok {
			out = append(out, Candidate{Kind: KindAutomaton, ID: f.ID, State: f.Status, Since: f.Since,
				Problem: "status_has_no_rule", Invariant: InvDeadlines, WaitsOn: WaitsOnOperator})
			continue
		}
		switch {
		case rule.finished:
			continue
		case f.Status == "held":
			// A held Automaton and its tasks wait by the operator's decision.
			if c, listed := heldCandidate(f, now); listed {
				out = append(out, c)
			}
			continue
		case f.Status == "running" || f.Status == "active":
			out = append(out, runningCandidates(f, interval)...)
		default:
			c := Candidate{Kind: KindAutomaton, ID: f.ID, State: f.Status, Since: f.Since,
				Limit: rule.limit, Reason: rule.reason, WaitsOn: rule.waitsOn}
			if f.Status == "blocked" && !f.HasBlockedReason {
				c.Problem, c.Invariant = "no_reason", InvDeadlines
			}
			out = append(out, withStart(c))
		}
		out = append(out, taskCandidates(f, interval)...)
	}
	return out
}

// withStart marks a candidate whose record does not say when it began.
func withStart(c Candidate) Candidate {
	if c.Since.IsZero() && c.Problem == "" {
		c.Problem, c.Invariant = "no_start_time", InvDeadlines
	}
	return c
}

// heldCandidate lists a held Automaton unless it has a reason and a review
// date that is in the future and at most 30 days away.
func heldCandidate(f AutomatonFact, now time.Time) (Candidate, bool) {
	c := Candidate{Kind: KindAutomaton, ID: f.ID, State: f.Status, Since: f.Since, WaitsOn: WaitsOnOperator, Invariant: InvDeadlines}
	switch {
	case !f.HeldReason:
		c.Problem = "held_no_reason"
	case f.HeldUntil.IsZero():
		c.Problem = "held_no_review_date"
	case !now.Before(f.HeldUntil):
		c.Problem, c.TimeDriven, c.Invariant = "held_review_due", true, InvOverdue
	case f.HeldUntil.Sub(now) > LimitHeldMax:
		c.Problem = "held_review_too_far"
	default:
		return Candidate{}, false
	}
	return c, true
}

func runningCandidates(f AutomatonFact, interval time.Duration) []Candidate {
	if f.LiveSession {
		return nil
	}
	waiting := f.HasWait
	for _, t := range f.Tasks {
		if t.LiveSession {
			return nil
		}
		if k := taskRules[t.Status]; (k == taskWaiting || k == taskRunning) && t.HasWait {
			waiting = true
		}
	}
	c := Candidate{Kind: KindAutomaton, ID: f.ID, State: f.Status, Since: f.Since, Invariant: InvRunning, WaitsOn: "worker"}
	if waiting {
		c.Limit, c.Reason = LimitWaiting, "recorded_wait"
	} else {
		c.Limit, c.Reason = 2*interval, "no_live_session"
	}
	return []Candidate{withStart(c)}
}

func taskCandidates(f AutomatonFact, interval time.Duration) []Candidate {
	running := f.Status == "running" || f.Status == "active"
	var out []Candidate
	for _, t := range f.Tasks {
		c := Candidate{Kind: KindTask, ID: f.ID + "/" + t.ID, State: t.Status, Since: t.Since, WaitsOn: WaitsOnOperator}
		kind, ok := taskRules[t.Status]
		switch {
		case !ok:
			c.Problem, c.Invariant = "status_has_no_rule", InvDeadlines
		case kind == taskStuck:
			c.Limit, c.Reason = LimitWaiting, t.Status
		case !running:
			// An Automaton that is not running is listed itself.
			continue
		case kind == taskRunning && !t.LiveSession:
			c.Invariant, c.WaitsOn = InvRunning, "worker"
			if t.HasWait {
				c.Limit, c.Reason = LimitWaiting, "recorded_wait"
			} else {
				c.Limit, c.Reason = 2*interval, "no_live_session"
			}
		case kind == taskWaiting:
			c.Invariant, c.WaitsOn = InvRunning, "worker"
			if t.HasWait {
				c.Limit, c.Reason = LimitWaiting, "recorded_wait"
			} else {
				c.Problem = "wait_without_reason"
			}
		default:
			continue
		}
		out = append(out, withStart(c))
	}
	return out
}

// bugCandidates keeps a bug open until its work is on main or the operator
// dropped it. A held, draft, archived or cancelled batch never closes it.
func bugCandidates(f AutomatonFact) []Candidate {
	if !f.IsBug || f.BugDropped || !parked[f.Status] {
		return nil
	}
	if (f.Status == "completed" || f.Status == "archived") && f.OnMain {
		return nil
	}
	ids := make([]string, 0, len(f.BugIDs))
	for _, id := range f.BugIDs {
		ids = append(ids, f.ID+"/"+id)
	}
	if len(ids) == 0 {
		ids = []string{f.ID}
	}
	since := f.CreatedAt
	if since.IsZero() {
		since = f.Since
	}
	out := make([]Candidate, 0, len(ids))
	for _, id := range ids {
		out = append(out, withStart(Candidate{Kind: KindBug, ID: id, State: "open", Since: since,
			Limit: LimitBug, Reason: "parked_" + f.Status, WaitsOn: WaitsOnOperator}))
	}
	return out
}
