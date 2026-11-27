// What a surface and the operator's list see of a request: views, labels, the
// delivery check's "requests" source and the invariants. Owner: part A.

package delivery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// label is the plain reading of a request. Only proven done reads "done".
func label(r *Request) string {
	d := r.Delivery
	switch r.State {
	case StateWaiting:
		switch {
		case d.FetchedAt != nil:
			return LabelFetched
		case d.TypeClaimAt != nil || d.ContentTypedAt != nil:
			return LabelTypedUnconfirmed
		}
		return LabelWaiting
	case StateDelivered:
		if d.InModelCallAt != nil {
			return LabelInModelCall
		}
		return LabelDelivered
	case StateAccepted:
		return LabelAccepted
	case StateReported:
		return LabelNotAccepted
	case StateDone:
		switch r.ProofAs {
		case ProofDone:
			return LabelDone
		case ProofDelivered:
			return LabelDeliveredOnly
		}
		return LabelAcceptedUnproven
	case StateFailed:
		return LabelFailed
	case StateExpired:
		return LabelExpired
	}
	return LabelCancelled
}

func view(r Request) RequestView {
	rule, _ := RuleFor(r.Kind)
	return RequestView{Request: r, Label: label(&r), ReadOnly: rule.ReadOnly}
}

// Get implements RequestService.
func (s *Requests) Get(_ context.Context, c Caller, id string) (RequestView, error) {
	if err := refusePeer(c); err != nil {
		return RequestView{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.lookupLocked(id)
	if err != nil {
		return RequestView{}, err
	}
	return view(*cloneJSON(&st.index)), nil
}

// List implements RequestService. While a file is red the list is not whole,
// so it is an error and never shorter.
func (s *Requests) List(_ context.Context, c Caller, f RequestFilter) ([]RequestView, error) {
	if err := refusePeer(c); err != nil {
		return nil, err
	}
	if err := s.faultError(); err != nil {
		return nil, err
	}
	out := []RequestView{}
	for _, r := range s.Requests(f) {
		out = append(out, view(r))
	}
	return out, nil
}

// Roles implements RequestService.
func (s *Requests) Roles(_ context.Context, c Caller) ([]RoleBinding, error) {
	if err := refusePeer(c); err != nil {
		return nil, err
	}
	if err := s.faultError(); err != nil {
		return nil, err
	}
	return append([]RoleBinding{}, s.Bindings()...), nil
}

// Mine implements RequestService.
func (s *Requests) Mine(_ context.Context, c Caller) (MineReply, error) {
	if err := refusePeer(c); err != nil {
		return MineReply{}, err
	}
	if c.Kind != CallerSession || c.SessionID == "" {
		return MineReply{}, ErrNotHolder
	}
	if err := s.faultError(); err != nil {
		return MineReply{}, err
	}
	out := MineReply{SessionID: c.SessionID, Items: []RequestView{}, Roles: []RoleBinding{}}
	for _, r := range s.Requests(RequestFilter{Holder: c.SessionID}) {
		if !r.State.Closed() {
			out.Items = append(out.Items, view(r))
		}
	}
	for _, b := range s.Bindings() {
		if b.SessionID == c.SessionID {
			out.Roles = append(out.Roles, b)
		}
	}
	return out, nil
}

// faultError names the files that cannot be believed. It holds file names,
// rule numbers and codes only.
func (s *Requests) faultError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.faults) == 0 {
		return nil
	}
	parts := make([]string, 0, len(s.faults))
	for _, f := range s.faults {
		parts = append(parts, fmt.Sprintf("%s: %s %s", f.name, f.invariant, f.what))
	}
	return fmt.Errorf("%w: %s", ErrRecords, strings.Join(parts, "; "))
}

// Candidates implements RequestDaemon: the delivery check's "requests" source.
// A request leaves it when it moves on, or, once failed, expired or cancelled,
// only by reissue or drop.
func (s *Requests) Candidates(_ context.Context, now time.Time) ([]Candidate, error) {
	if err := s.faultError(); err != nil {
		return nil, err
	}
	all := s.Requests(RequestFilter{})
	var out []Candidate
	held := map[string]*Candidate{}
	for i := range all {
		r := &all[i]
		if r.State.Closed() {
			if r.State != StateDone && r.SettledAt == nil {
				why := r.ReasonCode
				if why == "" {
					why = string(r.State)
				}
				since := r.StateAt
				if r.ClosedAt != nil {
					since = *r.ClosedAt
				}
				out = append(out, Candidate{Kind: KindRequest, ID: r.ID, State: string(r.State), Invariant: InvFailed,
					Problem: why, Since: since, WaitsOn: WaitsOnOperator})
			}
			continue
		}
		if r.HeldSince != nil && r.State != StateReported {
			// One item for each cause, however many requests it holds.
			g := held[r.HeldCause]
			if g == nil {
				g = &Candidate{Kind: KindRequest, ID: "held:" + r.HeldCause, State: r.HeldCause, Invariant: InvOverdue,
					Problem: r.HeldCause, Since: *r.HeldSince, WaitsOn: WaitsOnOperator}
				held[r.HeldCause] = g
			}
			g.Count++
			if r.HeldSince.Before(g.Since) {
				g.Since = *r.HeldSince
			}
			continue
		}
		if c, ok := openProblem(r, now); ok {
			out = append(out, c)
		}
	}
	causes := make([]string, 0, len(held))
	for k := range held {
		causes = append(causes, k)
	}
	sort.Strings(causes)
	for _, k := range causes {
		out = append(out, *held[k])
	}
	return append(out, s.violations(all, now)...), nil
}

// openProblem says why an open request is listed, if it is.
func openProblem(r *Request, now time.Time) (Candidate, bool) {
	c := Candidate{Kind: KindRequest, ID: r.ID, State: string(r.State), Invariant: InvOverdue,
		Since: r.StateAt, WaitsOn: WaitsOnOperator}
	run := r.AgeSeconds - r.HeldSeconds
	switch {
	case r.Verdict != nil && (r.Verdict.Reason == WhyListsOff || r.Verdict.Reason == WhyNoList):
		c.Problem = r.Verdict.Reason
	case r.State == StateReported:
		if r.ReviewBy != nil && !now.Before(*r.ReviewBy) {
			c.Problem, c.TimeDriven = WhyWaitingOnYou, true
		}
	case r.Overdue:
		c.Problem, c.TimeDriven = WhyDuePassed, true
		if r.State == StateAccepted && r.Holder != nil {
			c.WaitsOn = "session:" + r.Holder.SessionID
		}
	case r.State == StateWaiting && r.ReasonCode == WhyTakeNotProven:
		c.Problem = WhyTakeNotProven
	case r.State == StateWaiting && run >= r.TakeBySeconds:
		c.Problem, c.TimeDriven = WhyTakeByPassed, true
		if r.Holder == nil && r.Role != RoleSession && r.After == "" {
			c.Problem, c.Invariant = WhyNoHolder, InvNoHolder
		}
	case r.State != StateAccepted && r.Delivery.AcceptedAt == nil && r.Delivery.Reminders >= MaxFetchedReminders:
		c.Problem, c.TimeDriven = WhyWaitingOnYou, true
	}
	return c, c.Problem != ""
}

// Rules a request record can break on its own. The others are listed by the
// problems above.
const (
	invDeadlines = InvDeadlines
	invProof     = "I4"
	invEnded     = "I6"
	invOneHolder = "I7"
	invKeys      = "I9"
	invCaps      = "I13"
)

var holderVerbs = map[string]bool{VerbTake: true, VerbSeen: true, VerbAccept: true, VerbReport: true, VerbDecline: true}

// violations checks the stored records against the rules, whatever wrote them.
func (s *Requests) violations(all []Request, now time.Time) []Candidate {
	var out []Candidate
	broke := func(id, rule string, since time.Time) {
		code := "broke_" + strings.ToLower(rule)
		out = append(out, Candidate{Kind: KindRequest, ID: id + ":" + rule, State: code, Invariant: rule,
			Problem: code, Since: since, WaitsOn: WaitsOnOperator})
	}
	keys := map[string]string{}
	for i := range all {
		r := &all[i]
		if !r.State.Closed() && (r.TakeBySeconds <= 0 || r.DueSeconds <= 0 || r.Created.IsZero()) {
			broke(r.ID, invDeadlines, r.StateAt)
		}
		if r.State.Closed() && r.State != StateDone && r.SettledAt != nil && r.ReissuedAs == "" &&
			NonOperator(r.SettledBy) {
			broke(r.ID, InvFailed, r.StateAt)
		}
		if r.State == StateDone && (r.Verdict == nil || !r.Verdict.Closes || r.ProofAs == "") {
			broke(r.ID, invProof, r.StateAt)
		}
		if (r.State == StateDelivered || r.State == StateAccepted) && r.Holder != nil {
			if info, ok := s.session(r.Holder.SessionID); !ok || !info.Live {
				broke(r.ID, invEnded, r.StateAt)
			}
		}
		allowed := map[string]bool{}
		for _, f := range r.FormerHolders {
			allowed["session:"+f] = true
		}
		if r.Holder != nil {
			allowed["session:"+r.Holder.SessionID] = true
		}
		for _, call := range r.Calls {
			if call.Refused == "" && holderVerbs[call.Verb] && !allowed[call.Observed] {
				broke(r.ID, invOneHolder, call.At)
				break
			}
		}
		if _, twice := keys[r.KeyHash]; twice {
			broke(r.ID, invKeys, r.Created)
		}
		keys[r.KeyHash] = r.ID
		d := r.Delivery
		if d.Reminders > MaxFetchedReminders || (d.ContentTypedAt != nil && d.TypeClaimAt == nil) {
			broke(r.ID, invCaps, r.StateAt)
		}
	}
	roles := map[string]bool{}
	for _, b := range s.Bindings() {
		k := string(b.Role) + "@" + string(b.Scope.Kind) + ":" + b.Scope.ID
		if roles[k] {
			broke("role:"+k, invOneHolder, b.GivenAt)
		}
		roles[k] = true
	}
	set := s.settings()
	perSession := map[string]int{}
	recent := s.Notices(now.Add(-time.Hour + time.Second))
	for _, n := range recent {
		perSession[n.SessionID]++
	}
	over := len(recent) > set.RemindersPerHostHour
	for _, n := range perSession {
		over = over || n > set.RemindersPerSessionHour
	}
	if over {
		broke("reminders", invCaps, now)
	}
	return out
}
