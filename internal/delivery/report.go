// The report: the list as it stands and why the check is red.

package delivery

import (
	"sort"
	"time"
)

const (
	// toldAfter and toldWithin: an item older than toldAfter has an accepted
	// send within toldWithin, or the operator has seen it.
	toldAfter  = 15 * time.Minute
	toldWithin = 24 * time.Hour
)

// Uncovered are the paths the check does not see, so that green is not read as
// everything.
var Uncovered = []string{
	"work on other hosts",
	"orchestrator graphs",
	"discussion sync and subscribers",
	"webhooks",
	"container workers",
	"an event that was never saved (a wake that was dropped)",
	"whether a session has the current rules",
	"whether typed input was received",
	"output filter matches left untyped because the line is a prompt",
}

// Report is the current report, without running a pass.
func (e *Engine) Report() Report {
	now := e.opts.Now()
	e.mu.Lock()
	defer e.mu.Unlock()

	r := Report{ReportOnly: true, At: now.UTC(), Pass: e.rec.Pass, Items: []Entry{}, Backlog: []Entry{},
		Accepted: []Entry{}, ByRule: map[string]int{}, ByKind: map[string]int{}, Sources: []SourceStatus{},
		Channels: []string{}, NonOperatorActions: e.rec.NonOperator, Uncovered: append([]string(nil), Uncovered...)}
	red := map[string]bool{}
	notify := e.notifyOn()
	if notify && e.opts.Sender != nil {
		r.Channels = append(r.Channels, e.opts.Sender.Channels()...)
	}
	switch {
	case len(r.Channels) == 0:
		red[RedNoChannel] = true
	case e.rec.AllFailed:
		red[RedSendsFail] = true
	}
	if e.cannotRecord {
		red[RedCannotRecord] = true
	}

	if len(e.broken) > 0 {
		for _, why := range e.broken {
			red[why] = true
		}
		r.ByRule[InvUnopenable] = len(e.broken)
	} else {
		if e.mark != nil {
			at := e.mark.At
			r.StartMark = &at
		}
		e.reportEntriesLocked(&r, red, now)
	}

	for why := range red {
		r.Red = append(r.Red, why)
	}
	sort.Strings(r.Red)
	r.Result = ResultGreen
	r.Heartbeat = HeartbeatOK
	if len(r.Red) > 0 {
		r.Result, r.Heartbeat = ResultRed, HeartbeatRed
	}
	if len(e.broken) > 0 || red[RedNoChannel] || red[RedSendsFail] || red[RedCannotRecord] {
		r.Heartbeat = HeartbeatCannotTell
	}
	return r
}

func (e *Engine) reportEntriesLocked(r *Report, red map[string]bool, now time.Time) {
	for _, en := range e.rec.pointers() {
		if en.ClosedAt != nil {
			continue
		}
		c := *en
		c.Sends = append([]SendResult(nil), en.Sends...)
		if en.Accepted != nil && now.Before(en.Accepted.Until) {
			r.Accepted = append(r.Accepted, c)
			continue
		}
		if en.Backlog {
			r.Backlog = append(r.Backlog, c)
		} else {
			r.Items = append(r.Items, c)
		}
		r.ByRule[en.Invariant]++
		r.ByKind[string(en.Kind)]++
		old := time.Duration(en.AgeSeconds)*time.Second >= toldAfter
		sent := en.LastSentAt != nil && now.Sub(*en.LastSentAt) <= toldWithin
		if !en.Backlog && old && !sent && en.SeenAt == nil {
			r.ByRule[InvTold]++
			red[RedNotTold] = true
		}
	}
	if len(r.Items)+len(r.Backlog) > 0 {
		red[RedItems] = true
	}
	r.Sources = append(r.Sources, e.rec.Sources...)
	for _, s := range e.rec.Sources {
		if !s.OK {
			red[RedSourceUnreadable] = true
			r.ByRule[InvUnopenable]++
		}
	}
	switch {
	case e.rec.Pass == 0:
		red[RedNeverRan] = true
	case now.Sub(e.rec.LastPass) > 2*e.opts.Interval:
		red[RedStale] = true
		r.ByRule[InvCheckRan]++
	}
	if len(e.rec.Guarded) > 0 {
		red[RedGuarded] = true
		r.Guarded = append(r.Guarded, e.rec.Guarded...)
	}
	if e.rec.Unrecorded > 0 {
		red[RedUnrecorded] = true
		r.Unrecorded = e.rec.Unrecorded
	}
}
