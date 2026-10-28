package session

// Periodic one-line status emission for pi sessions in the session tail
// path (the `datawatch session tail` CLI and any consumer of the manager's
// output stream).
//
// Design contract: the status line is an *emission* — it is delivered to a
// consumer-supplied sink (e.g. the CLI's stdout) and is never written into
// the session's stored output log (sess.LogFile / output.log) or the
// sessions store. Completion detection (DATAWATCH_COMPLETE:) scans the
// stored output only, so the status line cannot influence it.

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/pilog"
)

// DefaultPiStatusInterval is the fixed interval between status lines when a
// caller does not configure one (60s).
const DefaultPiStatusInterval = 60 * time.Second

// PiStatusLinePrefix is the first field of every emitted status line.
const PiStatusLinePrefix = "progress:"

// IsPiBackendFamily reports whether backendFamily (a Session.BackendFamily
// value, which carries the backend name) belongs to the pi family whose own
// JSONL session log drives status formatting.
func IsPiBackendFamily(backendFamily string) bool {
	switch backendFamily {
	case "pi", "pi-prompt":
		return true
	}
	return false
}

// PiStatusLine formats the one-line status of a pi session from a pilog
// snapshot. It is a pure function: same inputs always produce the same
// string. Tool counts are listed in ascending tool-name order so the line is
// deterministic. Fields without data are omitted; idle is "unknown" when the
// log carries no timestamped event. Example:
//
//	progress: turns=5 tools=bash:3,edit:1,read:1 last=bash idle=42s
func PiStatusLine(p pilog.Progress, now time.Time) string {
	var b []byte
	b = append(b, PiStatusLinePrefix+" turns="...)
	b = append(b, fmt.Sprint(p.AssistantTurns)...)

	if len(p.ToolCalls) > 0 {
		names := make([]string, 0, len(p.ToolCalls))
		for name := range p.ToolCalls {
			names = append(names, name)
		}
		sort.Strings(names)
		b = append(b, " tools="...)
		for i, name := range names {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, name...)
			b = append(b, ':')
			b = append(b, fmt.Sprint(p.ToolCalls[name])...)
		}
	}

	if p.LastToolName != "" {
		b = append(b, " last="...)
		b = append(b, p.LastToolName...)
	}

	if p.LastEventAt.IsZero() {
		b = append(b, " idle=unknown"...)
	} else {
		d := now.Sub(p.LastEventAt)
		if d < 0 {
			d = 0
		}
		b = append(b, " idle="...)
		b = append(b, fmt.Sprint(int64(d/time.Second))...)
		b = append(b, 's')
	}
	return string(b)
}

// PiStatusLineFor returns the formatted status line when backendFamily is a
// pi family and p is a valid snapshot, "" otherwise. It is the single gate
// that keeps non-pi sessions (claude-code, opencode, ollama, ...) silent.
func PiStatusLineFor(backendFamily string, p pilog.Progress, now time.Time) string {
	if !IsPiBackendFamily(backendFamily) {
		return ""
	}
	return PiStatusLine(p, now)
}

// PiStatusTicker emits a formatted status line on a fixed interval. It never
// writes to the session's stored output — lines go to Emit only, so the
// DATAWATCH_COMPLETE: marker logic on the stored log is unaffected.
type PiStatusTicker struct {
	// Interval between emissions; <= 0 falls back to DefaultPiStatusInterval.
	Interval time.Duration
	// BackendFamily is the emitting session's Session.BackendFamily;
	// non-pi families never emit a line.
	BackendFamily string
	// Progress returns the current pilog snapshot. A nil function or an
	// error at a tick skips that tick's emission (e.g. the log file has not
	// been created yet, or a transient read failure).
	Progress func() (pilog.Progress, error)
	// Emit receives each formatted line at the consuming endpoint
	// (e.g. the CLI's stdout). Must not be used to append to the stored
	// session output log.
	Emit func(line string)
	// Now overrides the clock (tests). nil means time.Now.
	Now func() time.Time
}

// Tick computes the emission for one tick at now. It returns the formatted
// line and true when a pi snapshot was available, ("", false) when the
// session is not a pi family or no snapshot is available.
func (t *PiStatusTicker) Tick(now time.Time) (string, bool) {
	if !IsPiBackendFamily(t.BackendFamily) || t.Progress == nil {
		return "", false
	}
	p, err := t.Progress()
	if err != nil {
		return "", false
	}
	return PiStatusLine(p, now), true
}

// Start runs the immediate first tick and then emits one line per Interval
// until ctx is canceled, then returns nil.
func (t *PiStatusTicker) Start(ctx context.Context) {
	interval := t.Interval
	if interval <= 0 {
		interval = DefaultPiStatusInterval
	}
	now := t.Now
	if now == nil {
		now = time.Now
	}
	emitTick := func() {
		line, ok := t.Tick(now())
		if ok && t.Emit != nil {
			t.Emit(line)
		}
	}
	emitTick()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			emitTick()
		}
	}
}
