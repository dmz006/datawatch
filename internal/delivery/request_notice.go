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
	"slices"
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
	id     string
	remind []Request // waiting for a take or an accept
	toType []Request // typed once, oldest first
	// older are the roles whose one line about older requests is part of this
	// reminder.
	older   []RoleBinding
	lastAny time.Time // when it was last sent anything, from saved records
}

func noticeKey(n RequestNotice) string {
	return n.SessionID + "/" + strconv.FormatInt(n.At.UnixNano(), 10)
}

// typingReadiness is a Typer that can say beforehand that it would type
// nothing now, so that nothing is saved for a try that cannot happen. content
// is true for a request's text. The guard at the send itself still decides.
type typingReadiness interface {
	Ready(ctx context.Context, sessionID string, content bool) error
}

func notReady(ctx context.Context, ty Typer, sessionID string, content bool) error {
	if r, ok := ty.(typingReadiness); ok {
		return r.Ready(ctx, sessionID, content)
	}
	return nil
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
	sent = append(sent, typedSends(b, now.Add(-time.Hour))...)
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
			if ok, _ := ReminderAllowed(set, sent, w.id, now); !ok || notReady(ctx, n.opts.Typer, w.id, false) != nil {
				continue
			}
			var saved *RequestNotice
			if saved, err = n.remind(ctx, b, w, now); saved != nil {
				sent = append(sent, *saved)
			}
		case len(w.toType) > 0:
			// A typed request counts toward both hourly limits, as a line does.
			if ok, _ := ReminderAllowed(set, sent, w.id, now); !ok ||
				!w.lastAny.IsZero() && now.Sub(w.lastAny) < set.ReminderInterval {
				continue
			}
			r := w.toType[0]
			err = typeOnce(ctx, b, n.opts.Typer, r.ID, now)
			if after, ok := b.Request(r.ID); ok && after.Delivery.TypeClaimAt != nil {
				// The claim stays whenever the keys may have arrived.
				sent = append(sent, RequestNotice{At: now, SessionID: w.id, RequestIDs: []string{r.ID}})
			}
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

// typedSends are the full requests typed since then, read from the claims saved
// on the requests before each was typed. A claim taken back because nothing was
// typed is not there.
func typedSends(b RequestBook, since time.Time) []RequestNotice {
	var out []RequestNotice
	for _, r := range b.Requests(RequestFilter{}) {
		if n, ok := typedSend(&r); ok && !n.At.Before(since) {
			out = append(out, n)
		}
	}
	return out
}

// typedSend is the send a request's typed claim stands for.
func typedSend(r *Request) (RequestNotice, bool) {
	if r.Holder == nil || r.Delivery.TypeClaimAt == nil {
		return RequestNotice{}, false
	}
	return RequestNotice{At: *r.Delivery.TypeClaimAt, SessionID: r.Holder.SessionID, RequestIDs: []string{r.ID}}, true
}

// pickupPassed reports a request whose take-by time, held time not counted, is
// over.
func pickupPassed(r *Request) bool {
	return r.ReasonCode == WhyTakeByPassed ||
		r.TakeBySeconds > 0 && r.AgeSeconds-r.HeldSeconds >= r.TakeBySeconds
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
	bindings := b.Bindings()
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
		// Nobody fetched it in time: it is listed and no longer reminded of,
		// except in the one line to a holder given the role after it was made.
		case r.Delivery.FetchedAt == nil && pickupPassed(&r):
			if x, ok := untoldHolder(bindings, &r); ok && r.Delivery.NoticeAt == nil {
				w := get(id)
				w.remind = append(w.remind, r)
				if !slices.ContainsFunc(w.older, func(o RoleBinding) bool { return o.Role == x.Role && o.Scope == x.Scope }) {
					w.older = append(w.older, x)
				}
			}
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

// untoldHolder is the binding by which r's holder was given r's role after r
// was made, if that holder has not had its one line about older requests.
func untoldHolder(bindings []RoleBinding, r *Request) (RoleBinding, bool) {
	for _, x := range bindings {
		if x.Role == r.Role && x.Scope == r.Scope && x.SessionID == r.Holder.SessionID {
			return x, x.ToldOlderAt == nil && r.Created.Before(x.GivenAt)
		}
	}
	return RoleBinding{}, false
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
	for _, x := range w.older {
		at := now
		if err := b.SetToldOlder(x, &at); err != nil {
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
		for _, x := range w.older {
			undo = append(undo, b.SetToldOlder(x, nil))
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
