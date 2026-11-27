package delivery

import (
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/deploy"
	"github.com/dmz006/datawatch/internal/session"
)

// AutomatonFacts reads the facts the rules need from Automaton records. The
// records must be copies the caller owns. Templates are left out.
func AutomatonFacts(prds []*autonomous.PRD, live func(sessionID string) bool, bugIDs func(*autonomous.PRD) []string) []AutomatonFact {
	isLive := func(id string) bool { return id != "" && live(id) }
	out := make([]AutomatonFact, 0, len(prds))
	for _, p := range prds {
		if p == nil || p.IsTemplate {
			continue
		}
		status := string(autonomous.NormalizePRDStatus(p.Status))
		f := AutomatonFact{
			ID: p.ID, Status: status, Since: p.UpdatedAt, CreatedAt: p.CreatedAt,
			HasBlockedReason: strings.TrimSpace(p.BlockedReason) != "",
			HasWait:          strings.TrimSpace(p.WaitReason) != "",
			LiveSession:      isLive(p.PlannerSessionID),
			OnMain: p.MergedAt != nil || strings.TrimSpace(p.MergeCommit) != "" || strings.TrimSpace(p.MergedInRelease) != "" ||
				(p.Landing != nil && p.Landing.State == autonomous.LandingLanded),
			BugDropped:     p.ClosedOutcome == autonomous.OutcomeDropped || (p.Landing != nil && p.Landing.State == autonomous.LandingDropped),
			LandingPending: p.Landing != nil && p.Landing.State == autonomous.LandingPending,
		}
		if p.Hold != nil {
			f.HeldReason = strings.TrimSpace(p.Hold.Reason) != ""
			if p.Hold.Until != nil {
				f.HeldUntil = *p.Hold.Until
			}
		}
		if kind, ok := autonomous.NormalizePRDKind(p.Kind); ok && kind == autonomous.PRDKindBug {
			f.IsBug = true
			f.BugIDs = bugIDs(p)
		}
		for _, st := range p.Story {
			for _, t := range st.Tasks {
				taskLive := isLive(t.SessionID) || isLive(t.VerifierSessionID)
				f.LiveSession = f.LiveSession || taskLive
				f.Tasks = append(f.Tasks, TaskFact{
					ID: t.ID, Status: string(t.Status), Since: t.UpdatedAt,
					LiveSession: taskLive, HasWait: strings.TrimSpace(t.WaitReason) != "",
				})
			}
		}
		out = append(out, f)
	}
	return out
}

// ScheduleTargets answers what the schedule rules ask about sessions.
type ScheduleTargets interface {
	// Reachable reports whether the schedule has a session to be typed into.
	Reachable(sc session.ScheduledCommand) bool
}

// SessionCandidates reports sessions of automated work that wait for input.
func SessionCandidates(sessions []session.Session) []Candidate {
	var out []Candidate
	for _, s := range sessions {
		automated := s.PRDID != "" || s.AutomatonID != "" || s.TaskID != "" || s.ScheduleName != "" || s.LaunchRole != ""
		if s.State != session.StateWaitingInput || !automated {
			continue
		}
		out = append(out, withStart(Candidate{Kind: KindSession, ID: s.FullID, State: string(s.State),
			Since: s.UpdatedAt, Limit: LimitSessionWait, Reason: "waiting_input", WaitsOn: WaitsOnOperator, Invariant: InvRunning}))
	}
	return out
}

// finishedAutomaton reports an Automaton whose answers can no longer matter.
func finishedAutomaton(s autonomous.PRDStatus) bool {
	return automatonRules[string(autonomous.NormalizePRDStatus(s))].finished
}

// AnswerCandidates reports answers and one-time messages that did not arrive.
func AnswerCandidates(prds []*autonomous.PRD, subs []autonomous.Subscription, sessionLive func(string) bool) []Candidate {
	var out []Candidate
	// undelivered is one line datawatch still owes a session.
	undelivered := func(kind Kind, id, state string, since time.Time) {
		c := Candidate{Kind: kind, ID: id, State: "waiting", Since: since, Limit: LimitAnswerWait,
			Reason: "not_delivered", WaitsOn: WaitsOnOperator}
		if state == autonomous.DecisionReviewNeeded {
			c = Candidate{Kind: kind, ID: id, State: state, Since: since, Problem: state,
				Invariant: InvFailed, Sticky: true, WaitsOn: WaitsOnOperator}
		}
		out = append(out, c)
	}
	for _, p := range prds {
		if p == nil || p.IsTemplate || finishedAutomaton(p.Status) {
			continue
		}
		for _, st := range p.Story {
			for _, t := range st.Tasks {
				if d := t.DecisionResume; d != nil {
					switch d.Delivery {
					case autonomous.DecisionWaiting, autonomous.DecisionSending, autonomous.DecisionNotTyped, autonomous.DecisionReviewNeeded:
						undelivered(KindAnswer, d.QuestionID, d.Delivery, orElse(d.ResumedAt, t.UpdatedAt, p.UpdatedAt))
					}
				}
				for _, o := range t.OwedDecisions {
					undelivered(KindAnswer, o.QuestionID, autonomous.DecisionWaiting, orElse(t.UpdatedAt, p.UpdatedAt))
				}
			}
		}
	}
	for _, s := range subs {
		d := s.Delivery
		if d == nil {
			continue
		}
		if d.State != autonomous.DecisionReviewNeeded && !sessionLive(d.Target) {
			out = append(out, Candidate{Kind: KindMessage, ID: s.ID, State: "no_holder", Since: d.FiredAt,
				Limit: LimitAnswerWait, Reason: "no_holder", Invariant: InvNoHolder, WaitsOn: WaitsOnOperator})
			continue
		}
		undelivered(KindMessage, s.ID, d.State, d.FiredAt)
	}
	return out
}

// orElse is the first time that is set.
func orElse(times ...time.Time) time.Time {
	for _, t := range times {
		if !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

// QuestionCandidates reports open questions.
func QuestionCandidates(qs []autonomous.Question) []Candidate {
	var out []Candidate
	for _, q := range qs {
		if q.State != "" && q.State != autonomous.QuestionOpen {
			continue
		}
		var since time.Time
		if q.CreatedAt != nil {
			since = *q.CreatedAt
		}
		out = append(out, withStart(Candidate{Kind: KindQuestion, ID: q.ID, State: "open", Since: since,
			Limit: LimitWaiting, Reason: "awaiting_answer", WaitsOn: WaitsOnOperator}))
	}
	return out
}

// UnlandedCandidates reports unfinished branches that need a decision.
func UnlandedCandidates(bs []autonomous.UnlandedBranch) []Candidate {
	var out []Candidate
	for _, b := range bs {
		if b.Recommendation != autonomous.UnlandedNeedsDecision {
			continue
		}
		out = append(out, withStart(Candidate{Kind: KindUnlanded, ID: b.AutomatonID, State: b.Recommendation,
			Since: b.LastActivity, Limit: LimitWaiting, Reason: "not_on_main", WaitsOn: WaitsOnOperator}))
	}
	return out
}

// PendingItemCandidates reports registry items nothing was attached to.
func PendingItemCandidates(items []*autonomous.PendingItem) []Candidate {
	var out []Candidate
	for _, it := range items {
		if it == nil || it.State != autonomous.PendingItemWaiting {
			continue
		}
		out = append(out, withStart(Candidate{Kind: KindPendingItem, ID: it.ID, State: string(it.State),
			Since: it.CreatedAt, Limit: LimitPendingItem, Reason: "nothing_attached", WaitsOn: WaitsOnOperator}))
	}
	return out
}

// ClosedWorkCandidates reports the closed-work check's findings.
func ClosedWorkCandidates(findings []autonomous.OutcomeFinding, at time.Time) []Candidate {
	var out []Candidate
	for _, f := range findings {
		id := f.Automaton
		if id == "" {
			id = "daemon"
		}
		out = append(out, Candidate{Kind: KindClosedWork, ID: id, State: f.Kind, Since: at,
			Problem: f.Kind, Invariant: InvOverdue, WaitsOn: WaitsOnOperator})
	}
	return out
}

// DeployCandidates reports a deferred deploy.
func DeployCandidates(st deploy.State, found bool) []Candidate {
	if !found || st.Phase != deploy.PhaseDeferred {
		return nil
	}
	return []Candidate{{Kind: KindDeploy, ID: "host", State: "deferred", Since: st.RequestedAt,
		Problem: "deploy_deferred", Invariant: InvOverdue, WaitsOn: WaitsOnOperator}}
}
