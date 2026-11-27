// Reminders: one content-free line per session saying how many requests wait.
// (notify.go is the other direction, the messages to the operator.) Owner: part
// C.
//
// A worker whose program asks for its own requests (pi) is never typed into. A
// worker with no proven take is not reminded: its request is typed once
// instead (TypeOnce). Nothing is typed before it is saved, and nothing while
// the session or the request is held.

package delivery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"
)

// NotifierOptions configure the reminders.
type NotifierOptions struct {
	Typer    Typer
	Holds    Holds
	Sessions SessionBook
	Roles    RoleKeeper
	// Verifier is told of a request's first reminder or typed input.
	Verifier Verifier
	Settings func() RequestSettings
}

type notifier struct {
	opts NotifierOptions
	mu   sync.Mutex
	// refused names the saved reminders that typed nothing, because the
	// session was at a prompt. They do not count. After a restart this is
	// empty, so they count again until they are an hour old: the limits only
	// ever get stricter.
	refused map[string]time.Time
}

// NewNotifier returns the reminder sender.
func NewNotifier(opts NotifierOptions) Notifier {
	return &notifier{opts: opts, refused: map[string]time.Time{}}
}

// sessionWork is what one session could be sent in this pass.
type sessionWork struct {
	id      string
	remind  []Request // waiting for a take or an accept
	toType  []Request // typed once, oldest first
	lastAny time.Time // when it was last sent anything, from saved records
}

func noticeKey(n RequestNotice) string {
	return n.SessionID + "/" + strconv.FormatInt(n.At.UnixNano(), 10)
}

// reminderLine is the whole of a reminder. It holds a number only.
func reminderLine(n int) string {
	return fmt.Sprintf("datawatch: %d request(s) wait for you. Call request_mine, then request_take for each, "+
		"and request_accept for what you will do.", n)
}

// Notify sends what is allowed now: at most one line or one typed request to
// each session.
func (n *notifier) Notify(ctx context.Context, b RequestBook, now time.Time) error {
	if n.opts.Typer == nil || n.opts.Sessions == nil || n.opts.Settings == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	set := n.opts.Settings()

	var sent []RequestNotice
	for _, x := range b.Notices(now.Add(-time.Hour)) {
		if _, refused := n.refused[noticeKey(x)]; !refused {
			sent = append(sent, x)
		}
	}
	for key, at := range n.refused {
		if now.Sub(at) > time.Hour {
			delete(n.refused, key)
		}
	}

	var errs []error
	for _, w := range n.work(b, sent) {
		var err error
		switch {
		case len(w.remind) > 0:
			if ok, _ := ReminderAllowed(set, sent, w.id, now); !ok {
				continue
			}
			var saved *RequestNotice
			if saved, err = n.remind(ctx, b, w, now); saved != nil {
				sent = append(sent, *saved)
			}
		case len(w.toType) > 0:
			if !w.lastAny.IsZero() && now.Sub(w.lastAny) < set.ReminderInterval {
				continue
			}
			r := w.toType[0]
			err = typeOnce(ctx, b, n.opts.Typer, r.ID, now)
			if err == nil || !errors.Is(err, ErrPromptRefused) && !errors.Is(err, ErrCannotRecord) {
				n.started(ctx, r)
			}
		}
		if errors.Is(err, ErrCannotRecord) {
			// Nothing more is typed while nothing can be saved.
			return errors.Join(append(errs, err)...)
		}
		if err != nil && !errors.Is(err, ErrPromptRefused) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// work groups what could be sent by session, the longest without a send
// first, so the computer's limit does not always go to the same sessions.
func (n *notifier) work(b RequestBook, sent []RequestNotice) []*sessionWork {
	all := b.Requests(RequestFilter{})
	sort.SliceStable(all, func(i, j int) bool { return all[i].Created.Before(all[j].Created) })
	by := map[string]*sessionWork{}
	get := func(id string) *sessionWork {
		if by[id] == nil {
			by[id] = &sessionWork{id: id}
		}
		return by[id]
	}
	latest := func(w *sessionWork, at *time.Time) {
		if at != nil && at.After(w.lastAny) {
			w.lastAny = *at
		}
	}
	for _, r := range all {
		if r.Holder == nil {
			continue
		}
		id := r.Holder.SessionID
		latest(get(id), r.Delivery.TypeClaimAt)
		if r.State.Closed() {
			continue
		}
		if info, ok := n.opts.Sessions.Info(id); !ok || !info.Live {
			continue
		}
		if n.opts.Holds != nil {
			if _, held := n.opts.Holds.Held(id, r.Scope); held {
				continue
			}
		}
		p := PolicyFor(r.Holder.Family)
		switch {
		case p.PollsItself:
		case !p.Proven:
			if mayType(&r) {
				get(id).toType = append(get(id).toType, r)
			}
		case r.State != StateWaiting && r.State != StateDelivered:
		case r.Delivery.FetchedAt != nil && r.Delivery.Reminders >= MaxFetchedReminders:
		default:
			get(id).remind = append(get(id).remind, r)
		}
	}
	for _, x := range sent {
		if w := by[x.SessionID]; w != nil && x.At.After(w.lastAny) {
			w.lastAny = x.At
		}
	}
	out := make([]*sessionWork, 0, len(by))
	for _, w := range by {
		if len(w.remind)+len(w.toType) > 0 {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].lastAny.Equal(out[j].lastAny) {
			return out[i].lastAny.Before(out[j].lastAny)
		}
		return out[i].id < out[j].id
	})
	return out
}

// remind saves one reminder, then types its line. It returns the saved notice
// when it counts.
func (n *notifier) remind(ctx context.Context, b RequestBook, w *sessionWork, now time.Time) (*RequestNotice, error) {
	note := RequestNotice{At: now, SessionID: w.id}
	for _, r := range w.remind {
		note.RequestIDs = append(note.RequestIDs, r.ID)
	}
	if err := b.AddNotice(note); err != nil {
		return nil, err
	}
	for _, r := range w.remind {
		if _, err := b.Change(r.ID, func(r *Request) error {
			at := now
			r.Delivery.NoticeAt = &at
			if r.Delivery.FetchedAt != nil {
				r.Delivery.Reminders++
			}
			return nil
		}); err != nil {
			return &note, err
		}
	}
	err := n.opts.Typer.Notice(ctx, w.id, reminderLine(len(w.remind)))
	if errors.Is(err, ErrPromptRefused) {
		// Nothing was typed, so the try does not count.
		n.refused[noticeKey(note)] = now
		var undo []error
		for _, before := range w.remind {
			before := before
			_, cerr := b.Change(before.ID, func(r *Request) error {
				r.Delivery.NoticeAt = before.Delivery.NoticeAt
				if r.Delivery.FetchedAt != nil && r.Delivery.Reminders > 0 {
					r.Delivery.Reminders--
				}
				return nil
			})
			undo = append(undo, cerr)
		}
		return nil, errors.Join(undo...)
	}
	for _, r := range w.remind {
		if r.Delivery.NoticeAt == nil {
			n.started(ctx, r)
		}
	}
	return &note, err
}

// started tells the "done when" list of a request's first send. A failure
// changes nothing here: the list's start record only feeds its own check.
func (n *notifier) started(ctx context.Context, r Request) {
	if n.opts.Verifier != nil {
		_ = n.opts.Verifier.Started(ctx, r.ID, r.Kind)
	}
}
