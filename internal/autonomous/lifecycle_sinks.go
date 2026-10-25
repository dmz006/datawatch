// Lifecycle sinks are the built-in consumers of the in-process event
// dispatcher: an alert-store write and an SSE/mobile topic push per
// matched event. The daemon (internal/server) wires them once at
// startup; this package takes plain function values as callbacks so
// it never imports internal/alerts or internal/server (server already
// imports autonomous — importing back would be a cycle). Levels are
// plain strings ("info"/"warn"/"error") for the same reason: the
// daemon maps them onto the alerts package's level type.

package autonomous

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// SinkConfig — wired once at daemon startup. Empty Kinds = nothing is
// subscribed at all (the default: zero behavior change until the
// operator enables kinds via autonomous.notify.default_events).
type SinkConfig struct {
	Kinds []string
	// RecordAlert is called per matched event. level is one of
	// "info" / "warn" / "error" (the daemon maps these onto the
	// alerts package levels, so this package does not import it).
	RecordAlert func(level, title, body string)
	// PublishTopics is called per matched event with the topics to
	// push to (the daemon forwards each to the SSE/mobile bus).
	PublishTopics func(topics []string, title, message string)
	// CommSink and CommWindow are both optional; when CommSink is nil
	// the comm fan-out is inactive. When set, matched events are NOT
	// delivered immediately via the sinks above (which would flood the
	// channel on an event burst) but accumulate per Automaton and are
	// sent as ONE coalesced message per PRD after a quiet window;
	// CommWindow <= 0 falls back to the package default (5s).
	CommSink   func(text string)
	CommWindow time.Duration
}

const (
	// lifecycleSinkMsgLimit bounds the "(msg …)" fragment of
	// DescribeLifecycleEvent: long enough to identify the event,
	// short enough to stay one line in an alert list or push payload.
	lifecycleSinkMsgLimit = 160
	// lifecycleSinkMsgSuffix is appended when the message was trimmed.
	lifecycleSinkMsgSuffix = "…"
)

// lifecycleSinkMessage picks the free-form detail worth surfacing for
// ev (the guardrail verdict summary — or its issues when the summary
// is empty, else the coinciding decision note), flattened to a single
// line and truncated. Empty when the event carries none of them.
func lifecycleSinkMessage(ev LifecycleEvent) string {
	msg := ""
	if ev.Verdict != nil {
		msg = ev.Verdict.Summary
		if msg == "" && len(ev.Verdict.Issues) > 0 {
			// A verdict without a summary still names what it tripped
			// on — surface that instead of an empty "(msg )".
			msg = "guardrails: " + strings.Join(ev.Verdict.Issues, "; ")
		}
	}
	if msg == "" && ev.Decision != nil {
		msg = ev.Decision.Note
	}
	if msg == "" {
		return ""
	}
	msg = strings.ReplaceAll(strings.ReplaceAll(msg, "\n", " "), "\r", " ")
	msg = strings.TrimSpace(msg)
	if utf8.RuneCountInString(msg) > lifecycleSinkMsgLimit {
		msg = string([]rune(msg)[:lifecycleSinkMsgLimit]) + lifecycleSinkMsgSuffix
	}
	return msg
}

// DescribeLifecycleEvent renders one human-readable line:
// "PRD <id>: <old> -> <new>" plus " (task <id>)" and
// " (msg …)" when those fields are non-empty (truncate msg to ~160
// chars). PRDID empty -> "task <TaskID>: <old> -> <new>".
func DescribeLifecycleEvent(ev LifecycleEvent) string {
	var b strings.Builder
	if ev.PRDID != "" {
		fmt.Fprintf(&b, "PRD %s: %s -> %s", ev.PRDID, ev.OldStatus, ev.NewStatus)
		if ev.TaskID != "" {
			fmt.Fprintf(&b, " (task %s)", ev.TaskID)
		}
	} else if ev.TaskID != "" {
		fmt.Fprintf(&b, "task %s: %s -> %s", ev.TaskID, ev.OldStatus, ev.NewStatus)
	} else {
		// No PRD or task id (rare — e.g. a bus-wide event): still
		// render something attributable.
		fmt.Fprintf(&b, "%s: %s -> %s", ev.Kind, ev.OldStatus, ev.NewStatus)
	}
	if msg := lifecycleSinkMessage(ev); msg != "" {
		fmt.Fprintf(&b, " (msg %s)", msg)
	}
	if ev.Forward != nil {
		// A peer-forwarded event names the daemon it originated on.
		fmt.Fprintf(&b, " (forwarded from %s)", ev.Forward.OriginHost)
	}
	return b.String()
}

// sinkLevel maps an event kind onto the alert levels the daemon knows:
// terminal failure kinds are "error", everything else the sinks fire
// on is "warn".
func sinkLevel(ev LifecycleEvent) string {
	switch {
	case strings.HasSuffix(ev.Kind, ".failed"),
		strings.HasSuffix(ev.Kind, ".blocked"),
		strings.HasSuffix(ev.Kind, ".rejected"):
		return "error"
	default:
		return "warn"
	}
}

// InstallLifecycleSinks subscribes the built-in sinks to exactly
// sc.Kinds and returns a cancel func that unsubscribes them.
// Empty/nil Kinds -> returns a no-op cancel and subscribes NOTHING
// (the dispatcher treats an empty filter as "everything", so this
// check must happen here, not in the filter). A sink config with
// kinds but no callbacks is equally a no-op: there is nothing to
// deliver to.
//
// Panics inside RecordAlert/PublishTopics do not take the daemon
// down: lifecycleSub.deliver (lifecycle_event.go) already wraps
// every subscriber callback in recover(), so the sinks rely on the
// dispatcher's isolation rather than layering a second recover here.
func InstallLifecycleSinks(m *Manager, sc SinkConfig) (cancel func()) {
	cancel = func() {}
	if m == nil || len(sc.Kinds) == 0 || (sc.RecordAlert == nil && sc.PublishTopics == nil && sc.CommSink == nil) {
		return cancel
	}
	// One shared sink for the subscription's lifetime: coalescing only
	// works if every matched event lands in the same accumulator (a
	// per-event sink would have a 1-bucket window and defeat the point).
	var comm *CommFanoutSink
	if sc.CommSink != nil {
		comm = NewCommFanoutSink(sc.CommSink, sc.CommWindow)
	}
	subCancel := m.Subscribe(LifecycleFilter{Kinds: sc.Kinds}, func(ev LifecycleEvent) {
		// Comm fan-out active: this event COALESCES instead of sending
		// immediately — it joins its Automaton's bucket and rides along
		// in the next quiet-window flush.
		if comm != nil {
			comm.Handle(ev)
			return
		}
		msg := DescribeLifecycleEvent(ev)
		if sc.RecordAlert != nil {
			sc.RecordAlert(sinkLevel(ev), ev.Kind, msg)
		}
		if sc.PublishTopics != nil {
			topics := []string{"alerts"}
			if ev.PRDID != "" {
				// PRD-scoped feed in addition to the global one, so
				// per-PRD dashboards/clients can follow just theirs.
				topics = append(topics, "automaton-"+ev.PRDID)
			}
			sc.PublishTopics(topics, ev.Kind, msg)
		}
	})
	cancel = func() {
		subCancel()
		if comm != nil {
			// Close() is idempotent and panic-free (see comm_fanout.go):
			// it stops the pending per-bucket timers (no leaked timer
			// goroutines outlive the subscription) and flushes anything
			// still accumulated so the shutdown does not drop events.
			comm.Close()
		}
	}
	return cancel
}
