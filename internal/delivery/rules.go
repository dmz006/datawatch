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

// StatusRuled reports whether the Automaton status has a rule.
func StatusRuled(status string) bool { return false }

// TaskStatusRuled reports whether the task status has a rule.
func TaskStatusRuled(status string) bool { return false }

// AutomatonCandidates applies the status rules.
func AutomatonCandidates(facts []AutomatonFact, now time.Time, interval time.Duration) []Candidate {
	return nil
}
