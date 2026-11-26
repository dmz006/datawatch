// One-time messages to a session. A one-shot subscription with a session sink
// fires once, and its message must not be lost:
//
//   - The message is typed into the session. It is not sent over the channel
//     bridge: a bridge reply does not show the session got it. Typing keeps
//     the session's prompt guard, so a prompt on screen is never answered.
//   - It is saved on the subscription before any key is sent, and the
//     subscription is used up only when typing worked.
//   - When nothing was typed it waits: the operator is told once and it is
//     tried again, 30 seconds later and then less often, until it is typed or
//     the subscription is deleted. It is kept across a daemon restart, and
//     past the subscription's expiry.
//   - A send that may have reached the session, or one the daemon did not see
//     finish, is never repeated: it is listed as review_needed with an alert,
//     and the subscription stays until it is deleted.
//
// While a subscription holds a message it has already fired: later events are
// ignored. Alerts and logs carry ids and reason codes, never the text.

package autonomous

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// OneTimeDelivery is the message of a one-time session subscription that has
// fired and is not typed yet. Only datawatch writes it.
type OneTimeDelivery struct {
	// State is waiting, sending, not_typed or review_needed, with the meaning
	// the decision delivery states have.
	State string `json:"state"`
	// Reason is a reason code for not_typed and review_needed.
	Reason string `json:"reason,omitempty"`
	// Target is the session the message is for, and Text the message.
	Target  string    `json:"target"`
	Text    string    `json:"text"`
	EventID string    `json:"event_id,omitempty"`
	FiredAt time.Time `json:"fired_at"`
	// Notified is set once the operator was told the message waits.
	Notified bool `json:"notified,omitempty"`
}

// deliverySessionUnavailable is the reason code for a message not typed for a
// reason other than a prompt: the session is not found or takes no input.
const deliverySessionUnavailable = "session_unavailable"

// deliveryNotSaved is the reason code in the notice when the message's state
// cannot be saved, so nothing may be typed.
const deliveryNotSaved = "not_saved"

const (
	oneShotWaitingTitle = "Subscription delivery not typed yet"
	oneShotReviewTitle  = "Subscription delivery: review needed"
)

// oneShotState is the engine's in-memory side of the one-time messages.
type oneShotState struct {
	mu     sync.Mutex
	busy   map[string]bool
	timers map[string]func() bool
	waits  map[string]time.Duration
	// unsaved holds a fired message the store could not save yet. Nothing of
	// it is typed until it is saved.
	unsaved map[string]OneTimeDelivery
	// untyped marks a message still saved as sending although nothing was
	// typed, because that result could not be saved.
	untyped map[string]bool
	// told marks a waiting message the operator was told about, also when
	// that could not be saved.
	told   map[string]bool
	closed bool
}

type oneShotOutcome int

const (
	oneShotDone  oneShotOutcome = iota // typed, listed, or nothing to do
	oneShotRetry                       // nothing was typed: try again
)

func isOneTimeSession(sub Subscription) bool {
	return sub.OneShot && sub.Sink.Type == SinkSession
}

// fireOneShot handles the event a one-time session subscription fires on.
func (e *LifecycleSubscriptionEngine) fireOneShot(sub Subscription, target, text, eventID string) {
	rec := OneTimeDelivery{State: DecisionWaiting, Target: target, Text: text, EventID: eventID, FiredAt: e.nowFn().UTC()}
	e.one.mu.Lock()
	_, pending := e.one.unsaved[sub.ID]
	e.one.mu.Unlock()
	if pending {
		return
	}
	claimed, err := e.claimOneShot(sub.ID, rec)
	if err != nil {
		rec.Notified = true
		e.one.mu.Lock()
		if e.one.unsaved == nil {
			e.one.unsaved = map[string]OneTimeDelivery{}
		}
		e.one.unsaved[sub.ID] = rec
		e.one.mu.Unlock()
		log.Printf("[autonomous] lifecycle sub %s: one-time message not saved, nothing typed: %v", sub.ID, err)
		e.oneShotAlert("warn", oneShotWaitingTitle, fmt.Sprintf(
			"subscription=%s session=%s. The message could not be saved, so nothing was typed. datawatch holds it in memory and tries again; a daemon restart before a save works would lose it.", sub.ID, target))
		e.scheduleOneShotRetry(sub.ID)
		return
	}
	if !claimed {
		return // already fired, deleted, or no longer a one-time session message
	}
	// Two attempts at once, as any session delivery gets, then it waits.
	if e.attemptOneShot(sub.ID) == oneShotRetry && e.attemptOneShot(sub.ID) == oneShotRetry {
		e.announceOneShot(sub.ID)
		e.scheduleOneShotRetry(sub.ID)
	}
}

func (e *LifecycleSubscriptionEngine) claimOneShot(id string, rec OneTimeDelivery) (bool, error) {
	return e.store.setDelivery(id, func(cur Subscription) (*OneTimeDelivery, bool) {
		if cur.Delivery != nil || !isOneTimeSession(cur) {
			return nil, false
		}
		next := rec
		return &next, true
	})
}

// moveOneShot changes the stored message if it is in one of the from states.
func (e *LifecycleSubscriptionEngine) moveOneShot(id string, from []string, change func(*OneTimeDelivery)) (OneTimeDelivery, bool, error) {
	var out OneTimeDelivery
	ok, err := e.store.setDelivery(id, func(cur Subscription) (*OneTimeDelivery, bool) {
		if cur.Delivery == nil {
			return nil, false
		}
		for _, s := range from {
			if cur.Delivery.State == s {
				next := *cur.Delivery
				change(&next)
				out = next
				return &next, true
			}
		}
		return nil, false
	})
	return out, ok, err
}

var oneShotUntyped = []string{DecisionWaiting, DecisionNotTyped}

// attemptOneShot types the waiting message once. One attempt per subscription
// runs at a time.
func (e *LifecycleSubscriptionEngine) attemptOneShot(id string) oneShotOutcome {
	e.one.mu.Lock()
	if e.one.closed || e.one.busy[id] {
		e.one.mu.Unlock()
		return oneShotDone
	}
	if e.one.busy == nil {
		e.one.busy = map[string]bool{}
	}
	e.one.busy[id] = true
	held, unsaved := e.one.unsaved[id]
	e.one.mu.Unlock()
	defer func() {
		e.one.mu.Lock()
		delete(e.one.busy, id)
		e.one.mu.Unlock()
	}()

	if unsaved {
		if _, err := e.claimOneShot(id, held); err != nil {
			return oneShotRetry
		}
		e.one.mu.Lock()
		delete(e.one.unsaved, id)
		e.one.mu.Unlock()
	}
	// Saved as sending before any key: with no save nothing is typed.
	from := oneShotUntyped
	e.one.mu.Lock()
	if e.one.untyped[id] {
		from = append([]string{DecisionSending}, from...)
	}
	e.one.mu.Unlock()
	var before string // the reason of the last attempt that typed nothing
	msg, ok, err := e.moveOneShot(id, from, func(d *OneTimeDelivery) {
		before, d.State, d.Reason = d.Reason, DecisionSending, ""
	})
	if err != nil {
		log.Printf("[autonomous] lifecycle sub %s: one-time message not typed: it could not be saved: %v", id, err)
		return oneShotRetry
	}
	if !ok {
		return oneShotDone
	}
	e.markOneShotUntyped(id, false)

	sendErr := e.typeIntoSession(msg.Target, msg.Text)
	switch {
	case sendErr == nil:
		e.finishOneShot(id)
		return oneShotDone
	case session.TextMayBeInSession(sendErr):
		log.Printf("[autonomous] lifecycle sub %s: send to session %q failed after keys were sent: %v", id, msg.Target, sendErr)
		e.reviewOneShot(id, msg.Target, deliverySendError)
		return oneShotDone
	}
	// Every other SendInput error is from before the first key.
	reason := deliverySessionUnavailable
	if session.RefusedBeforeTyping(sendErr) {
		reason = deliveryPromptOnScreen
	}
	if before != reason { // a repeat of the same refusal is not logged again
		log.Printf("[autonomous] lifecycle sub %s: nothing typed into session %q: %v", id, msg.Target, sendErr)
	}
	if _, _, err := e.moveOneShot(id, []string{DecisionSending}, func(d *OneTimeDelivery) {
		d.State, d.Reason = DecisionNotTyped, reason
	}); err != nil {
		// The saved state stays "sending", which a restart reads as unsure.
		// In this process nothing was typed, so it is tried again.
		e.markOneShotUntyped(id, true)
		log.Printf("[autonomous] lifecycle sub %s: the send result could not be saved: %v", id, err)
	}
	return oneShotRetry
}

func (e *LifecycleSubscriptionEngine) markOneShotUntyped(id string, untyped bool) {
	e.one.mu.Lock()
	defer e.one.mu.Unlock()
	if !untyped {
		delete(e.one.untyped, id)
		return
	}
	if e.one.untyped == nil {
		e.one.untyped = map[string]bool{}
	}
	e.one.untyped[id] = true
}

// finishOneShot uses the subscription up after its message was typed. One
// edited into something else meanwhile is kept, with nothing waiting.
func (e *LifecycleSubscriptionEngine) finishOneShot(id string) {
	e.stopOneShotTimer(id)
	cur, ok := e.store.Get(id)
	if !ok {
		return
	}
	var err error
	if isOneTimeSession(cur) {
		err = e.store.Delete(id)
	} else {
		_, err = e.store.setDelivery(id, func(Subscription) (*OneTimeDelivery, bool) { return nil, true })
	}
	if err != nil && !errors.Is(err, ErrSubscriptionNotFound) {
		// It stays "sending": never typed again, and listed after a restart.
		log.Printf("[autonomous] lifecycle sub %s: one-shot removal: %v", id, err)
	}
}

// reviewOneShot ends a message that may or may not have arrived.
func (e *LifecycleSubscriptionEngine) reviewOneShot(id, target, reason string) {
	_, ok, err := e.moveOneShot(id, []string{DecisionSending}, func(d *OneTimeDelivery) {
		d.State, d.Reason = DecisionReviewNeeded, reason
	})
	if err != nil {
		log.Printf("[autonomous] lifecycle sub %s: review state could not be saved: %v", id, err)
	} else if !ok {
		return
	}
	e.oneShotAlert("error", oneShotReviewTitle, fmt.Sprintf(
		"review needed: subscription=%s session=%s reason=%s. datawatch cannot tell that the session got this one-time message and does not send it again. Check the session; delete the subscription when done.", id, target, reason))
}

// announceOneShot tells the operator, once, that a message waits. It is
// called only after an attempt that typed nothing. The warning does not
// depend on the note of it being saved.
func (e *LifecycleSubscriptionEngine) announceOneShot(id string) {
	cur, ok := e.store.Get(id)
	if !ok || cur.Delivery == nil || cur.Delivery.Notified || cur.Delivery.State == DecisionReviewNeeded {
		return
	}
	e.one.mu.Lock()
	told := e.one.told[id]
	if e.one.told == nil {
		e.one.told = map[string]bool{}
	}
	e.one.told[id] = true
	e.one.mu.Unlock()
	if told {
		return
	}
	msg := *cur.Delivery
	if _, err := e.store.setDelivery(id, func(c Subscription) (*OneTimeDelivery, bool) {
		if c.Delivery == nil {
			return nil, false
		}
		next := *c.Delivery
		next.Notified = true
		return &next, true
	}); err != nil {
		log.Printf("[autonomous] lifecycle sub %s: the waiting notice could not be saved: %v", id, err)
	}
	reason := msg.Reason
	if reason == "" {
		reason = deliveryNotSaved
	}
	e.oneShotAlert("warn", oneShotWaitingTitle, fmt.Sprintf(
		"subscription=%s session=%s reason=%s. Nothing was typed. datawatch keeps the message and tries again until it can type it into this session. Delete the subscription to cancel it.", id, msg.Target, reason))
}

func (e *LifecycleSubscriptionEngine) oneShotAlert(level, title, body string) {
	if e.m != nil {
		e.m.deliveryAlert(level, title, body)
	}
}

// typeIntoSession types text into the session, never over its channel bridge.
func (e *LifecycleSubscriptionEngine) typeIntoSession(target, text string) error {
	if e.cfg.FindSession == nil {
		return fmt.Errorf("no session lookup configured")
	}
	fullID, _, ok := e.cfg.FindSession(target)
	if !ok {
		return fmt.Errorf("session %q not found", target)
	}
	if e.cfg.SendInput == nil {
		return fmt.Errorf("no way to type into session %q: SendInput is not configured", target)
	}
	return e.cfg.SendInput(fullID, text, sessionPushSourceTag)
}

// scheduleOneShotRetry arms one retry timer for the subscription, after
// wakeRetryFirst and then twice as long each time, up to wakeRetryMax.
func (e *LifecycleSubscriptionEngine) scheduleOneShotRetry(id string) {
	e.one.mu.Lock()
	defer e.one.mu.Unlock()
	if e.one.closed || e.one.timers[id] != nil {
		return
	}
	if e.one.timers == nil {
		e.one.timers, e.one.waits = map[string]func() bool{}, map[string]time.Duration{}
	}
	wait := e.one.waits[id]
	switch {
	case wait == 0:
		wait = wakeRetryFirst
	case wait*2 < wakeRetryMax:
		wait *= 2
	default:
		wait = wakeRetryMax
	}
	e.one.waits[id] = wait
	e.one.timers[id] = e.afterFn(wait, func() { e.retryOneShot(id) })
}

func (e *LifecycleSubscriptionEngine) retryOneShot(id string) {
	e.one.mu.Lock()
	delete(e.one.timers, id)
	e.one.mu.Unlock()
	if e.attemptOneShot(id) == oneShotRetry {
		e.announceOneShot(id)
		e.scheduleOneShotRetry(id)
	}
}

func (e *LifecycleSubscriptionEngine) stopOneShotTimer(id string) {
	e.one.mu.Lock()
	defer e.one.mu.Unlock()
	if stop := e.one.timers[id]; stop != nil {
		stop()
	}
	delete(e.one.timers, id)
	delete(e.one.waits, id)
}

// forgetOneShot drops what is held in memory for a deleted subscription.
func (e *LifecycleSubscriptionEngine) forgetOneShot(id string) {
	e.stopOneShotTimer(id)
	e.one.mu.Lock()
	delete(e.one.unsaved, id)
	delete(e.one.untyped, id)
	delete(e.one.told, id)
	e.one.mu.Unlock()
}

// resumeOneShots picks up the messages an earlier daemon run left. One saved
// as sending may have been typed; the others never were.
func (e *LifecycleSubscriptionEngine) resumeOneShots() {
	for _, sub := range e.store.List("") {
		if sub.Delivery == nil {
			continue
		}
		switch sub.Delivery.State {
		case DecisionSending:
			e.reviewOneShot(sub.ID, sub.Delivery.Target, deliveryInterrupted)
		case DecisionWaiting, DecisionNotTyped:
			e.scheduleOneShotRetry(sub.ID)
		}
	}
}

// closeOneShots stops every retry. The saved messages stay for the next run.
func (e *LifecycleSubscriptionEngine) closeOneShots() {
	e.one.mu.Lock()
	defer e.one.mu.Unlock()
	e.one.closed = true
	for _, stop := range e.one.timers {
		stop()
	}
	e.one.timers = nil
}
