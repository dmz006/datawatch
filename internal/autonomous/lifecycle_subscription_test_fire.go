// Test delivery for persisted lifecycle subscriptions: lets an operator
// (the PWA, REST) verify a subscription's sink end to end with a
// synthetic, clearly labelled event, without waiting for a real one.

package autonomous

import (
	"fmt"
	"strings"
)

// testEventPrefix labels every test delivery's message text.
const testEventPrefix = "[test] "

// TestResult reports the outcome of a subscription test delivery.
type TestResult struct {
	OK    bool   `json:"ok"`
	Sink  string `json:"sink"`
	Error string `json:"error,omitempty"`
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
