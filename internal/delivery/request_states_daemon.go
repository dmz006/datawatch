// What only datawatch does: the periodic check, a holder that ended, watched
// conditions and stubs. Owner: part A.

package delivery

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// endHolder applies a holder's end. Callers hold s.opMu.
func (s *Requests) endHolder(ctx context.Context, sessionID string) error {
	err := s.update(func(w *work) error {
		for _, id := range w.ids() {
			if st := w.peek(id); st != nil && holds(&st.index, sessionID) {
				w.loseHolder(id, true)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, b := range s.Bindings() {
		if b.SessionID == sessionID && s.opts.Roles != nil {
			return s.opts.Roles.Ended(ctx, s, sessionID)
		}
	}
	return nil
}

// HolderEnded implements RequestDaemon.
func (s *Requests) HolderEnded(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := s.endHolder(ctx, sessionID); err != nil {
		return err
	}
	return s.assign(ctx)
}

// endedHolders finds sessions that ended while nobody told the store.
func (s *Requests) endedHolders() []string {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			if info, ok := s.session(id); !ok || !info.Live {
				ids = append(ids, id)
			}
		}
	}
	for _, r := range s.Requests(RequestFilter{}) {
		if !r.State.Closed() && r.State != StateReported && r.Holder != nil {
			add(r.Holder.SessionID)
		}
	}
	for _, b := range s.Bindings() {
		add(b.SessionID)
	}
	return ids
}

type holdNow struct {
	holder string
	cause  string
}

// age adds the time seen since the last check to every open request, stops the
// clocks of held ones, and ends what ran out of time.
func (s *Requests) age(holdsNow map[string]holdNow) (guarded int, err error) {
	err = s.update(func(w *work) error {
		last := s.meta.LastTick
		limit := 2 * s.opts.Interval
		var expiring []string
		for _, id := range w.ids() {
			cur := &w.peek(id).index
			if cur.State.Closed() {
				continue
			}
			// Observed time: a suspend or a clock jump adds at most two
			// intervals, and a backward jump takes nothing away.
			since := cur.Created
			if last.After(since) {
				since = last
			}
			step := min(max(w.now.Sub(since), 0), limit)
			r := &w.edit(id).index
			add := int64(step / time.Second)
			r.AgeSeconds += add
			if r.State == StateReported {
				continue
			}
			h, held := holdsNow[id]
			if held && (r.Holder == nil) == (h.holder == "") {
				if r.HeldSince == nil {
					at := w.now
					r.HeldSince = &at
				}
				r.HeldCause = codeOr(h.cause)
				r.HeldSeconds += add
				continue
			}
			r.HeldSince, r.HeldCause = nil, ""
			run := r.AgeSeconds - r.HeldSeconds
			if r.State == StateWaiting && r.Holder != nil && r.ReasonCode == "" && run >= r.TakeBySeconds {
				r.ReasonCode = WhyTakeByPassed
			}
			if run < r.DueSeconds {
				continue
			}
			r.Overdue = true
			if r.State == StateWaiting || r.State == StateDelivered {
				expiring = append(expiring, id)
			}
		}
		wasGuarded := s.meta.Guarded
		guard := len(expiring) > maxExpiringPerPass
		if guard {
			if !wasGuarded {
				guarded = len(expiring)
			}
		} else {
			for _, id := range expiring {
				if st := w.edit(id); !st.index.State.Closed() {
					w.close(&st.index, StateExpired, WhyDuePassed, "datawatch")
				}
			}
		}
		now := w.now
		w.t.meta = append(w.t.meta, func(m *storeMeta) { m.LastTick, m.Guarded = now, guard })
		return nil
	})
	return guarded, err
}

type checkResult struct {
	before  Request
	verdict Verdict
	rework  bool
}

// check runs the "done when" lists of what is reported, and of what closes on
// receipt. It is the only place a request becomes done.
func (s *Requests) check(ctx context.Context) error {
	if s.opts.Verifier == nil {
		return nil
	}
	var results []checkResult
	for _, r := range s.Requests(RequestFilter{}) {
		r := r
		if !checkable(&r) {
			continue
		}
		v, err := s.opts.Verifier.Check(ctx, r)
		if err != nil {
			continue // it stays as it is
		}
		res := checkResult{before: r, verdict: v}
		if !v.Closes && v.NotMet && r.State == StateReported {
			if res.rework, err = s.opts.Verifier.Rework(ctx, r); err != nil {
				continue
			}
		}
		results = append(results, res)
	}
	if len(results) == 0 {
		return nil
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	err := s.update(func(w *work) error {
		for _, res := range results {
			cur := w.peek(res.before.ID)
			if cur == nil || cur.index.State != res.before.State || !cur.index.StateAt.Equal(res.before.StateAt) {
				continue // it moved while the list was read
			}
			w.judge(res)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.assign(ctx)
}

const statusDoneAccepted, statusDelivered = "done_accepted", "delivered"

// judge applies one reading of a list.
func (w *work) judge(res checkResult) {
	v := res.verdict
	cur := &w.peek(res.before.ID).index
	rule, _ := RuleFor(cur.Kind)
	counts := !rule.DeliveryOnly || (cur.Holder != nil && PolicyFor(cur.Holder.Family).Delivered(cur.Delivery))
	// Work never closes on receipt.
	receiptOnly := !rule.DeliveryOnly && v.Status == statusDelivered
	switch {
	case v.Closes && counts && !receiptOnly && CanMove(rule, cur.State, StateDone, true):
		r := &w.edit(res.before.ID).index
		r.Verdict = cleanVerdict(v, w.now)
		switch {
		case v.Status == statusDoneAccepted:
			r.ProofAs = ProofAcceptedUnproven
		case rule.DeliveryOnly:
			r.ProofAs = ProofDelivered
		default:
			r.ProofAs = ProofDone
		}
		w.close(r, StateDone, "", "datawatch")
		r.Overdue = false
	case !v.Closes && v.NotMet && cur.State == StateReported:
		r := &w.edit(res.before.ID).index
		r.Verdict = cleanVerdict(v, w.now)
		if res.rework {
			w.setState(r, StateAccepted, WhyProofFailed)
			r.Report, r.ReviewBy = nil, nil
			return
		}
		w.close(r, StateFailed, WhyProofFailed, "datawatch")
	default:
		next := cleanVerdict(v, w.now)
		next.Closes = false
		if old := cur.Verdict; old != nil && old.Status == next.Status && old.NotMet == next.NotMet &&
			old.Reason == next.Reason && !old.Closes {
			return
		}
		w.edit(res.before.ID).index.Verdict = next
	}
}

// Tick implements RequestDaemon.
func (s *Requests) Tick(ctx context.Context) error {
	s.tickMu.Lock()
	defer s.tickMu.Unlock()
	s.mu.Lock()
	faults := append([]storeFault(nil), s.faults...)
	first := !s.alarmed
	if len(faults) > 0 {
		s.alarmed = true
	}
	s.mu.Unlock()
	if len(faults) > 0 {
		for _, f := range faults {
			if !first {
				break
			}
			s.record(Candidate{Kind: KindAlarm, ID: "requests:" + f.name, State: f.what, Invariant: f.invariant,
				Problem: alarmReason, Sticky: true, WaitsOn: WaitsOnOperator})
		}
		return ErrRecords
	}
	if err := s.Healthy(); err != nil {
		return err // nothing is expired or typed while nothing can be saved
	}

	guarded, err := func() (int, error) {
		s.opMu.Lock()
		defer s.opMu.Unlock()
		for _, id := range s.endedHolders() {
			if err := s.endHolder(ctx, id); err != nil {
				return 0, err
			}
		}
		s.retryDelivered(ctx)
		if err := s.assign(ctx); err != nil {
			return 0, err
		}
		holdsNow := map[string]holdNow{}
		if s.opts.Holds != nil {
			for _, r := range s.Requests(RequestFilter{}) {
				if r.State.Closed() || r.State == StateReported {
					continue
				}
				id := ""
				if r.Holder != nil {
					id = r.Holder.SessionID
				}
				if cause, held := s.opts.Holds.Held(id, r.Scope); held {
					holdsNow[r.ID] = holdNow{holder: id, cause: cause}
				}
			}
		}
		n, err := s.age(holdsNow)
		if err != nil {
			return 0, err
		}
		s.startHolders(ctx, holdsNow)
		return n, s.assign(ctx)
	}()
	if err != nil {
		return err
	}
	if guarded > 0 {
		s.record(Candidate{Kind: KindAlarm, ID: "requests_mass_expiry", State: alarmState, Invariant: InvOverdue,
			Problem: alarmReason, Count: guarded, Sticky: true, WaitsOn: WaitsOnOperator})
	}
	var errs []error
	if err := s.check(ctx); err != nil {
		errs = append(errs, err)
	}
	// Reminders only while the store can record them.
	if err := s.Healthy(); err != nil {
		return errors.Join(append(errs, err)...)
	}
	if s.opts.Notifier != nil {
		if err := s.opts.Notifier.Notify(ctx, s, s.opts.Now()); err != nil {
			errs = append(errs, err)
		}
	}
	if err := s.Healthy(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// retryDelivered repeats a hello's proof that the role keeper could not save.
func (s *Requests) retryDelivered(ctx context.Context) {
	if s.opts.Roles == nil {
		return
	}
	unproven := map[string]bool{}
	for _, b := range s.Bindings() {
		if b.CanTakeAt == nil && b.HelloID != "" {
			unproven[b.HelloID] = true
		}
	}
	for _, r := range s.Requests(RequestFilter{Kind: ReqHello}) {
		if unproven[r.ID] && r.Holder != nil && PolicyFor(r.Holder.Family).Delivered(r.Delivery) {
			_ = s.opts.Roles.Delivered(ctx, s, r)
		}
	}
}

// startHolders asks for the project's defined supervisor where a request waits
// and nobody holds the role. Job 4 wires no starter.
func (s *Requests) startHolders(ctx context.Context, held map[string]holdNow) {
	if s.opts.Starter == nil {
		return
	}
	asked := map[string]bool{}
	for _, r := range s.Requests(RequestFilter{State: StateWaiting}) {
		if _, h := held[r.ID]; h || r.Holder != nil || r.ReasonCode != WhyNoHolder || r.Role == RoleSession {
			continue
		}
		key := string(r.Role) + "@" + string(r.Scope.Kind) + ":" + r.Scope.ID
		if !asked[key] {
			asked[key] = true
			_, _ = s.opts.Starter.StartHolder(ctx, r.Role, r.Scope)
		}
	}
}

// Observe implements RequestDaemon. The condition and the request it makes are
// saved in one write.
func (s *Requests) Observe(ctx context.Context, cond Condition) (RequestView, bool, error) {
	if cond.ID == "" {
		return RequestView{}, false, inputErr("a condition needs an ID")
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.mu.Lock()
	old, seen := s.meta.Conditions[cond.ID]
	s.mu.Unlock()
	next := old
	fire := false
	if cond.Value != "" {
		next.Value = KeyHash(cond.Value)
		// The first value seen is the baseline.
		fire = seen && old.Value != next.Value
	} else {
		next.Holds = cond.Holds
		fire = cond.Holds && !old.Holds
	}
	save := func(w *work) {
		n := next
		w.t.meta = append(w.t.meta, func(m *storeMeta) { m.Conditions[cond.ID] = n })
	}
	if !fire {
		if seen && next == old {
			return RequestView{}, false, nil
		}
		return RequestView{}, false, s.update(func(w *work) error { save(w); return nil })
	}
	next.N++
	if cond.Request == nil {
		return RequestView{}, false, inputErr("a condition needs a request")
	}
	in := cond.Request(next.N)
	in.Key = "cond:" + cond.ID + ":" + strconv.Itoa(next.N)
	rule, err := s.checkCreate(&in)
	if err != nil {
		return RequestView{}, false, err
	}
	st, err := s.build(Caller{Kind: CallerDatawatch, Surface: SurfaceDaemon}, in, rule)
	if err != nil {
		return RequestView{}, false, err
	}
	id := st.index.ID
	if err := s.insert(ctx, st, save); err != nil {
		return RequestView{}, false, err
	}
	_ = s.assign(ctx)
	return s.viewOf(id), true, nil
}

// Has implements RequestDaemon.
func (s *Requests) Has(_ context.Context, key string) (bool, error) {
	_, found, err := s.byKey(key)
	return found, err
}

// leftTheList reports a closed request that the operator's list no longer
// needs.
func leftTheList(r *Request) bool {
	return r.State == StateDone || (r.State.Closed() && r.SettledAt != nil)
}

// SourceGone implements RequestDaemon.
func (s *Requests) SourceGone(_ context.Context, ref SourceRef) error {
	if ref.ID == "" {
		return nil
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.update(func(w *work) error {
		for _, id := range w.ids() {
			if r := &w.peek(id).index; r.Source == ref && leftTheList(r) {
				w.t.reqs[id] = nil
			}
		}
		return nil
	})
}

// prune leaves only the stub of a request that has left the list: the content
// goes and the key still counts. Nothing calls it by itself: how long closed
// content is kept is not decided.
func (s *Requests) prune(id string) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.update(func(w *work) error {
		cur := w.peek(id)
		if cur == nil {
			return ErrRequestNotFound
		}
		if !leftTheList(&cur.index) {
			return ErrRequestState
		}
		st := w.edit(id)
		st.content, st.index.Pruned = nil, true
		return nil
	})
}
