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
		if en.Kind == KindAlarm || en.Kind == KindClock {
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
	return e.act(sel, by, nil, func(_ *records, en *Entry, now time.Time) {
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
// refused send. A repeat of one still listed raises its count.
func (e *Engine) Record(c Candidate) error {
	now := e.opts.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.broken) > 0 {
		return ErrRecords
	}
	next, mark, markDirty := e.rec.clone(), e.mark, false
	if mark == nil {
		mark = &startMark{Version: recordsVersion, At: now, Sources: map[string]*sourceMark{}}
		markDirty = true
	}
	key := entryKey(c.Kind, c.ID, c.State)
	if isOpen(next, key) {
		next.Entries[key].Count++
	} else {
		episode := 1
		if old := next.Entries[key]; old != nil {
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
		en.Count = 1
		next.Entries[key] = en
	}
	if err := e.persistLocked(next, mark, markDirty); err != nil {
		e.cannotRecord = true
		return ErrCannotRecord
	}
	return nil
}
