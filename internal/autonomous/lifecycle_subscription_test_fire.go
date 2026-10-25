// Test delivery for persisted lifecycle subscriptions: lets an operator
// (the PWA, REST) verify a subscription's sink end to end with a
// synthetic, clearly labelled event, without waiting for a real one.

package autonomous

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// testEventPrefix labels every test delivery's message text.
const testEventPrefix = "[test] "

// LifecycleTestDeliveryDeadline bounds one test delivery to a webhook or peer
// sink. It is an implementation choice, not a proposal value: the REST or
// comm call that fires the test waits for the answer, so a receiver that stays
// down is abandoned (no alert, as for any abandoned delivery) rather than held
// through the full retry schedule.
const LifecycleTestDeliveryDeadline = 30 * time.Second

// TestResult reports the outcome of a subscription test delivery. Status and
// Attempts are set for the webhook and peer sinks only: Attempts is the number
// of POSTs made and Status the last HTTP status (0 when none arrived).
type TestResult struct {
	OK       bool   `json:"ok"`
	Sink     string `json:"sink"`
	Error    string `json:"error,omitempty"`
	Status   int    `json:"status,omitempty"`
	Attempts int    `json:"attempts,omitempty"`
}

// FireTest delivers a synthetic prd.updated event, labelled "[test] ",
// to subscription id's sink only, through the same sink wiring the engine
// uses. It never uses the subscription's action text, never consumes a
// one-shot and bypasses coalescing. An unknown id returns an
// ErrSubscriptionNotFound error; a delivery failure returns the result
// (OK false, Error set) and the error. A sink whose delivery function is
// not wired yields a "sink not available" error.
func (e *LifecycleSubscriptionEngine) FireTest(id string) (TestResult, error) {
	sub, ok := e.store.Get(id)
	if !ok {
		return TestResult{}, fmt.Errorf("subscription %s: %w", id, ErrSubscriptionNotFound)
	}
	res := TestResult{Sink: sub.Sink.Type}
	ev := LifecycleEvent{
		ID:        newLifecycleEventID(),
		At:        e.nowFn(),
		Kind:      KindPRDUpdated,
		Level:     LevelPRD,
		PRDID:     "test",
		OldStatus: "draft",
		NewStatus: "approved",
		Test:      true,
	}
	text := testEventPrefix + DescribeLifecycleEvent(ev)

	var err error
	switch sub.Sink.Type {
	case SinkSession:
		err = e.deliverSessionOnce(sub.Sink.Target, text)
	case SinkComm:
		if e.cfg.Comm.Send == nil {
			err = errSinkUnavailable(sub.Sink.Type)
		} else {
			e.cfg.Comm.Send(text)
		}
	case SinkTopic:
		if e.cfg.PublishTopics == nil {
			err = errSinkUnavailable(sub.Sink.Type)
		} else {
			e.cfg.PublishTopics([]string{"alerts", "automaton-" + ev.PRDID}, ev.Kind, text)
		}
	case SinkAlert:
		if e.cfg.RecordAlert == nil {
			err = errSinkUnavailable(sub.Sink.Type)
		} else {
			e.cfg.RecordAlert(sinkLevel(ev), ev.Kind, text)
		}
	case SinkMCPQueue:
		if e.cfg.PendingQueue == nil {
			err = errSinkUnavailable(sub.Sink.Type)
		} else {
			e.cfg.PendingQueue.Append(strings.TrimSpace(sub.Sink.Target), ev)
		}
	case SinkWebhook:
		err = e.fireWebhookTest(sub, ev, &res)
	case SinkPeer:
		err = e.firePeerTest(sub, ev, &res)
	default:
		err = errSinkUnavailable(sub.Sink.Type)
	}
	if err != nil {
		res.Error = err.Error()
		return res, err
	}
	res.OK = true
	return res, nil
}

func errSinkUnavailable(sink string) error {
	return fmt.Errorf("sink not available: %q delivery is not wired in this daemon", sink)
}

// fireWebhookTest sends the test event through the webhook sink, the same
// path a real event takes, and records the attempt count and last status in
// res. A disabled sink sends nothing and says which setting enables it.
func (e *LifecycleSubscriptionEngine) fireWebhookTest(sub Subscription, ev LifecycleEvent, res *TestResult) error {
	if e.cfg.Webhook == nil {
		return errSinkUnavailable(sub.Sink.Type)
	}
	if !e.cfg.Webhook.Enabled() {
		return fmt.Errorf("webhook delivery is disabled; set autonomous.notify.webhook.enabled to true to test this subscription")
	}
	ctx, cancel := context.WithTimeout(context.Background(), LifecycleTestDeliveryDeadline)
	defer cancel()
	rep, err := e.cfg.Webhook.DeliverReport(ctx, sub, ev)
	res.Attempts, res.Status = rep.Attempts, rep.Status
	return err
}

// firePeerTest forwards the test event to the subscription's federation peer
// through the peer forwarder, the same path a real event takes. A disabled
// forwarder sends nothing and says which setting enables it.
func (e *LifecycleSubscriptionEngine) firePeerTest(sub Subscription, ev LifecycleEvent, res *TestResult) error {
	if e.cfg.Forwarder == nil {
		return errSinkUnavailable(sub.Sink.Type)
	}
	if !e.cfg.Forwarder.Enabled() {
		return fmt.Errorf("peer forwarding is disabled; set autonomous.notify.forwarding.enabled to true to test this subscription")
	}
	ctx, cancel := context.WithTimeout(context.Background(), LifecycleTestDeliveryDeadline)
	defer cancel()
	rep, err := e.cfg.Forwarder.ForwardReport(ctx, sub, ev)
	res.Attempts, res.Status = rep.Attempts, rep.Status
	return err
}
