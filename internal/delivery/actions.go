// The operator's actions on the list. The rule is that only the operator does
// these; who did is recorded and, until sign-in exists, not enforced.

package delivery

import "time"

func (s Selector) empty() bool {
	return len(s.Keys) == 0 && s.Kind == "" && s.State == "" && s.Reason == "" && !s.Backlog
}

func (s Selector) matches(en *Entry) bool {
	if len(s.Keys) > 0 {
		found := false
		for _, k := range s.Keys {
			found = found || k == en.Key
		}
		if !found {
			return false
		}
	}
	return (s.Kind == "" || s.Kind == en.Kind) && (s.State == "" || s.State == en.State) &&
		(s.Reason == "" || s.Reason == en.Reason) && (!s.Backlog || en.Backlog)
}

// ackOnly reports an entry the engine raised about the check itself. While one
// is open it can be what keeps other items off the list.
func ackOnly(en *Entry) bool { return en.Kind == KindAlarm || en.Kind == KindClock }

// act applies change to the open entries sel names, and saves. Nothing
// changes when check refuses or the save fails.
func (e *Engine) act(sel Selector, by string, check func(*Entry) error, change func(*records, *Entry, time.Time)) (int, error) {
	if sel.empty() {
		return 0, ErrSelector
	}
	now := e.opts.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.broken) > 0 {
		return 0, ErrRecords
	}
	next := e.rec.clone()
	var matched []*Entry
	for _, en := range next.pointers() {
		if en.ClosedAt == nil && sel.matches(en) {
			if check != nil {
				if err := check(en); err != nil {
					return 0, err
				}
			}
			matched = append(matched, en)
		}
	}
	if len(matched) == 0 {
		return 0, ErrNoMatch
	}
	for _, en := range matched {
		change(next, en, now)
	}
	if NonOperator(by) {
		next.NonOperator++
	}
	if e.mark == nil {
		return 0, ErrNoMatch
	}
	if err := e.persistLocked(next, e.mark, false); err != nil {
		e.cannotRecord = true
		return 0, ErrCannotRecord
	}
	return len(matched), nil
}

func closeEntry(en *Entry, as, by string, now time.Time) {
	at := now
	en.ClosedAt, en.ClosedAs, en.ClosedBy = &at, as, by
}

// Ack marks entries as seen. It stops their messages; they stay listed,
// except an alarm or a clock jump, which acknowledging closes.
func (e *Engine) Ack(sel Selector, by string) (int, error) {
	return e.act(sel, by, nil, func(rec *records, en *Entry, now time.Time) {
		at := now
		en.SeenAt, en.SeenBy = &at, by
		if en.Kind == KindAlarm && en.ID == alarmMassExpiry {
			rec.Release = true
		}
		if ackOnly(en) {
			closeEntry(en, "acknowledged", by, now)
			en.Present = false
		}
	})
}

// Accept lets entries wait until until for the reason code.
func (e *Engine) Accept(sel Selector, reason string, until time.Time, by string) (int, error) {
	if sel.empty() {
		return 0, ErrSelector
	}
	if err := CheckReasonCode(reason); err != nil {
		return 0, err
	}
	now := e.opts.Now()
	if !until.After(now) || until.Sub(now) > LimitHeldMax {
		return 0, ErrUntil
	}
	// One alarm among the choice refuses all of it.
	noAlarm := func(en *Entry) error {
		if ackOnly(en) {
			return ErrAlarm
		}
		return nil
	}
	return e.act(sel, by, noAlarm, func(_ *records, en *Entry, now time.Time) {
		en.Accepted = &Accept{Reason: reason, Until: until.UTC(), By: by, At: now.UTC()}
		en.NextTryAt = nil
	})
}

// Drop closes failed or backlog entries for the reason code. Work that is only
// stuck cannot be dropped off the list.
func (e *Engine) Drop(sel Selector, reason, by string) (int, error) {
	if sel.empty() {
		return 0, ErrSelector
	}
	if err := CheckReasonCode(reason); err != nil {
		return 0, err
	}
	droppable := func(en *Entry) error {
		if ackOnly(en) {
			return ErrAlarm
		}
		if !en.Sticky && !en.Backlog {
			return ErrNotDroppable
		}
		return nil
	}
	return e.act(sel, by, droppable, func(_ *records, en *Entry, now time.Time) {
		closeEntry(en, "dropped", by, now)
		en.DropReason = reason
		// Present stays set while the source still reports it, so it is not
		// listed again.
		en.Present = en.Source != sourceCheck && en.Source != sourceEvent
	})
}

// Record saves a failure that no source can be asked about later, such as a
// refused send. A repeat of one still listed raises its count. One that cannot
// be saved now is held, and saved with the next change that can be: nothing
// else knows it happened. A daemon restart loses what is still held.
func (e *Engine) Record(c Candidate) error {
	now := e.opts.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.broken) > 0 {
		e.holdLocked(c)
		return ErrRecords
	}
	next, mark, markDirty := e.rec.clone(), e.mark, false
	if mark == nil {
		mark = &startMark{Version: recordsVersion, At: now, Sources: map[string]*sourceMark{}}
		markDirty = true
	}
	e.foldHeldLocked(next, now)
	addEvent(next, c, 1, now)
	if err := e.persistLocked(next, mark, markDirty); err != nil {
		e.cannotRecord = true
		e.holdLocked(c)
		return ErrCannotRecord
	}
	e.clearHeldLocked()
	return nil
}

// addEvent lists n occurrences of a recorded failure.
func addEvent(rec *records, c Candidate, n int, now time.Time) {
	key := entryKey(c.Kind, c.ID, c.State)
	if isOpen(rec, key) {
		rec.Entries[key].Count += n
		return
	}
	episode := 1
	if old := rec.Entries[key]; old != nil {
		episode = old.Episode + 1
	}
	if c.Problem == "" {
		c.Problem = c.State
	}
	if c.Invariant == "" {
		c.Invariant = InvFailed
	}
	c.Sticky = true
	en := newEntry(key, violation{c: c, source: sourceEvent}, episode, false, now)
	en.Count = n
	rec.Entries[key] = en
}

func (e *Engine) holdLocked(c Candidate) {
	key := entryKey(c.Kind, c.ID, c.State)
	for i := range e.unsaved {
		if h := &e.unsaved[i]; entryKey(h.c.Kind, h.c.ID, h.c.State) == key {
			h.count++
			return
		}
	}
	if len(e.unsaved) >= maxUnsaved {
		e.lost++
		return
	}
	e.unsaved = append(e.unsaved, heldEvent{c: c, count: 1})
}

// foldHeldLocked adds what is held to rec. It stays held until rec is saved.
func (e *Engine) foldHeldLocked(rec *records, now time.Time) {
	for _, h := range e.unsaved {
		addEvent(rec, h.c, h.count, now)
	}
	if e.lost > 0 {
		raise(rec, KindAlarm, alarmEventsLost, alarmState, InvFailed, e.lost, now)
	}
}

func (e *Engine) clearHeldLocked() { e.unsaved, e.lost = nil, 0 }
