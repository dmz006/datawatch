package delivery

import (
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/deploy"
	"github.com/dmz006/datawatch/internal/session"
)

// AutomatonFacts reads the facts the rules need from Automaton records.
func AutomatonFacts(prds []*autonomous.PRD, live func(sessionID string) bool, bugIDs func(*autonomous.PRD) []string) []AutomatonFact {
	return nil
}

// ScheduleTargets answers what the schedule rules ask about sessions.
type ScheduleTargets interface {
	// Reachable reports whether the schedule has a session to be typed into.
	Reachable(sc session.ScheduledCommand) bool
}

// ScheduleCandidates reports pending schedules that cannot fire or have waited
// too long, and failed and system-cancelled ones.
func ScheduleCandidates(all []session.ScheduledCommand, targets ScheduleTargets, now time.Time, interval time.Duration) []Candidate {
	return nil
}

// SessionCandidates reports sessions of automated work that wait for input.
func SessionCandidates(sessions []session.Session) []Candidate { return nil }

// AnswerCandidates reports answers and one-time messages that did not arrive.
func AnswerCandidates(prds []*autonomous.PRD, subs []autonomous.Subscription, sessionLive func(string) bool) []Candidate {
	return nil
}

// QuestionCandidates reports open questions.
func QuestionCandidates(qs []autonomous.Question) []Candidate { return nil }

// UnlandedCandidates reports unfinished branches that need a decision.
func UnlandedCandidates(bs []autonomous.UnlandedBranch) []Candidate { return nil }

// PendingItemCandidates reports registry items nothing was attached to.
func PendingItemCandidates(items []*autonomous.PendingItem) []Candidate { return nil }

// ClosedWorkCandidates reports the closed-work check's findings.
func ClosedWorkCandidates(findings []autonomous.OutcomeFinding, at time.Time) []Candidate {
	return nil
}

// DeployCandidates reports a deferred deploy.
func DeployCandidates(st deploy.State, found bool) []Candidate { return nil }
