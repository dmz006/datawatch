// One pass over what the sources reported: observed ages, the start mark,
// opening and closing entries, and the guards against a storm.

package delivery

import (
	"sort"
	"strconv"
	"time"
)

const (
	// maxExpiringPerPass: when more than this many would run out of time in
	// one pass, none does and one alarm is raised.
	maxExpiringPerPass = 20
	// maxNewPerPass: at most this many new entries in one pass.
	maxNewPerPass = 50
	// closedKept is how long a closed entry is remembered once its source no
	// longer reports it.
	closedKept = 90 * 24 * time.Hour
)

// Sources of the entries the engine raises itself.
const (
	sourceCheck = "check"
	sourceEvent = "event"
)

// Alarm and clock entry parts.
const (
	alarmFlood      = "flood"
	alarmMassExpiry = "mass_expiry"
	alarmState      = "held"
	reasonOverLimit = "over_limit"
)

func entryKey(k Kind, id, state string) string { return string(k) + ":" + id + ":" + state }

type violation struct {
	c      Candidate
	source string
	age    time.Duration
}

// apply folds one pass into rec and mark, and reports whether the mark
// changed.
func (e *Engine) apply(rec *records, mark *startMark, reads []sourceRead, now time.Time) (markChanged bool) {
	interval := e.opts.Interval
	first := rec.LastPass.IsZero()
	var raw, step time.Duration
	if !first {
		raw = now.Sub(rec.LastPass)
		// Age is observed time: a suspend or a clock jump adds at most two
		// intervals, and a backward jump takes nothing away.
		step = min(max(raw, 0), 2*interval)
	}
	rec.Pass++
	rec.LastPass = now

	for _, en := range rec.Entries {
		if en.ClosedAt != nil {
			continue
		}
		en.AgeSeconds += int64(step / time.Second)
		if en.Accepted != nil && !now.Before(en.Accepted.Until) {
			// The date ran out: a new occurrence, listed and told again.
			en.Accepted, en.SeenAt, en.SeenBy, en.ToldAt, en.LastSentAt, en.NextTryAt = nil, nil, "", nil, nil, nil
			en.SendFails, en.Sends, en.Backlog, en.ListedAt, en.AgeSeconds = 0, nil, false, now, 0
			en.Episode++
		}
	}

	violations := map[string]violation{}
	readOK := map[string]bool{}
	captured := map[string]bool{}
	rec.Sources = nil
	for _, r := range reads {
		rec.Sources = append(rec.Sources, SourceStatus{Name: r.name, OK: r.err == nil, Candidates: len(r.cands)})
		if r.err != nil {
			continue // its entries and ages stay as they were
		}
		readOK[r.name] = true
		sm := mark.Sources[r.name]
		// Before the first saved check, everything found is old: also when an
		// earlier first check saved the start mark and not the list.
		capturing := sm == nil || first
		if capturing {
			sm = &sourceMark{At: now}
			mark.Sources[r.name] = sm
			captured[r.name], markChanged = true, true
		}
		seen := map[string]bool{}
		for _, c := range r.cands {
			key := entryKey(c.Kind, c.ID, c.State)
			if seen[key] {
				continue
			}
			seen[key] = true
			t := rec.Tracks[key]
			if t == nil {
				var age time.Duration
				if !c.Since.IsZero() {
					age = max(now.Sub(c.Since), 0)
				}
				if !capturing {
					age = min(age, step)
				}
				t = &track{Age: age}
				rec.Tracks[key] = t
			} else {
				t.Age += step
			}
			t.Source = r.name
			listed := c.Problem != "" || (c.Limit > 0 && t.Age > c.Limit)
			if capturing {
				sm.Open = append(sm.Open, key)
				if listed {
					sm.True = append(sm.True, key)
				}
			}
			if listed {
				violations[key] = violation{c: c, source: r.name, age: t.Age}
			}
		}
		for key, t := range rec.Tracks {
			if t.Source == r.name && !seen[key] {
				delete(rec.Tracks, key)
			}
		}
	}

	e.openAndUpdate(rec, violations, captured, now)
	closeResolved(rec, violations, readOK, now)

	if !first && (raw > 2*interval || raw < -time.Minute) {
		state, gap := "jump_forward", raw
		if raw < 0 {
			state, gap = "jump_back", -raw
		}
		raise(rec, KindClock, "jump@"+strconv.FormatInt(now.Unix(), 10), state, InvOverdue, int(gap/time.Second), now)
	}
	return markChanged
}

func (e *Engine) openAndUpdate(rec *records, violations map[string]violation, captured map[string]bool, now time.Time) {
	keys := make([]string, 0, len(violations))
	for k := range violations {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var fresh, expiring []string
	backlog := map[string]bool{}
	for _, key := range keys {
		v := violations[key]
		en := rec.Entries[key]
		switch {
		case en != nil && en.ClosedAt == nil:
			update(en, v)
		case en != nil && en.Present:
			// Still true since it was closed: the same occurrence.
		case en == nil && captured[v.source]:
			backlog[key] = true
			rec.Entries[key] = newEntry(key, v, 1, true, now)
		case v.c.Problem == "" || v.c.TimeDriven:
			expiring = append(expiring, key)
		default:
			fresh = append(fresh, key)
		}
	}

	rec.Guarded = nil
	if len(expiring) > maxExpiringPerPass && !rec.Release {
		rec.Guarded = expiring
		if !isOpen(rec, entryKey(KindAlarm, alarmMassExpiry, alarmState)) {
			raise(rec, KindAlarm, alarmMassExpiry, alarmState, InvOverdue, len(expiring), now)
		}
		expiring = nil
	}
	released := rec.Release && len(expiring) > maxExpiringPerPass
	rec.Release = false

	fresh = append(fresh, expiring...)
	sort.Strings(fresh)
	allowed := maxNewPerPass
	floodKey := entryKey(KindAlarm, alarmFlood, alarmState)
	if isOpen(rec, floodKey) {
		allowed = 0
	}
	if released {
		allowed = max(allowed, len(expiring))
	}
	rec.Unrecorded = 0
	if len(fresh) > allowed {
		rec.Unrecorded = len(fresh) - allowed
		fresh = fresh[:allowed]
	}
	for _, key := range fresh {
		episode := 1
		if old := rec.Entries[key]; old != nil {
			episode = old.Episode + 1
		}
		rec.Entries[key] = newEntry(key, violations[key], episode, false, now)
	}
	if rec.Unrecorded > 0 {
		if isOpen(rec, floodKey) {
			rec.Entries[floodKey].Count = rec.Unrecorded
		} else {
			raise(rec, KindAlarm, alarmFlood, alarmState, InvOverdue, rec.Unrecorded, now)
		}
	}
}

func isOpen(rec *records, key string) bool {
	en := rec.Entries[key]
	return en != nil && en.ClosedAt == nil
}

// raise opens an entry of the engine's own: an alarm or a clock jump. It
// stays until the operator acknowledges it.
func raise(rec *records, kind Kind, id, state, rule string, count int, now time.Time) {
	key := entryKey(kind, id, state)
	episode := 1
	if old := rec.Entries[key]; old != nil {
		episode = old.Episode + 1
	}
	rec.Entries[key] = &Entry{
		Key: key, Kind: kind, ID: id, State: state, Invariant: rule, Reason: state, WaitsOn: WaitsOnOperator,
		Since: now, ReviewBy: now, Count: count, Sticky: true, Source: sourceCheck, Episode: episode, ListedAt: now,
	}
}

func newEntry(key string, v violation, episode int, backlog bool, now time.Time) *Entry {
	en := &Entry{Key: key, Kind: v.c.Kind, ID: v.c.ID, Source: v.source, Episode: episode,
		Backlog: backlog, ListedAt: now, Since: v.c.Since}
	if en.Since.IsZero() {
		en.Since = now.Add(-v.age)
	}
	en.ReviewBy = now
	if v.c.Problem == "" {
		en.ReviewBy = now.Add(v.c.Limit - v.age)
	}
	update(en, v)
	return en
}

func update(en *Entry, v violation) {
	en.State, en.Count, en.Present = v.c.State, v.c.Count, true
	en.Sticky = en.Sticky || v.c.Sticky
	en.WaitsOn = v.c.WaitsOn
	if en.WaitsOn == "" {
		en.WaitsOn = WaitsOnOperator
	}
	en.Reason = v.c.Problem
	if en.Reason == "" {
		en.Reason = v.c.Reason
	}
	if en.Reason == "" {
		en.Reason = reasonOverLimit
	}
	en.Invariant = v.c.Invariant
	if en.Invariant == "" {
		en.Invariant = InvOverdue
	}
}

// closeResolved closes the entries whose condition is gone. Failed work stays
// until the operator acts, and an unreadable source clears nothing.
func closeResolved(rec *records, violations map[string]violation, readOK map[string]bool, now time.Time) {
	for key, en := range rec.Entries {
		if _, still := violations[key]; still {
			continue
		}
		if !readOK[en.Source] {
			if en.ClosedAt != nil && en.Source == sourceCheck && now.Sub(*en.ClosedAt) > closedKept {
				delete(rec.Entries, key)
			}
			continue
		}
		en.Present = false
		switch {
		case en.ClosedAt == nil && !en.Sticky:
			at := now
			en.ClosedAt, en.ClosedAs = &at, "cleared"
		case en.ClosedAt != nil && now.Sub(*en.ClosedAt) > closedKept:
			delete(rec.Entries, key)
		}
	}
}
