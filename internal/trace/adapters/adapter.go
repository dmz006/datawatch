// Package adapters turns backend-specific session transcripts into
// normalized trace events. Each backend has an Adapter; the Registry hands
// out the adapters enabled by tracing.backends.
package adapters

import (
	"io"
	"os"
	"time"

	"github.com/dmz006/datawatch/internal/trace"
)

// FallbackNote is the explicit note attached to events that were derived
// from daemon-observed state instead of a transcript.
const FallbackNote = "detail unavailable: derived from daemon-observed state"

// Context carries the identity stamped on every event and the incremental
// parsing state between Parse calls.
type Context struct {
	SessionID   string
	TaskID      string
	StoryID     string
	AutomatonID string
	Agent       string

	// Now supplies the time for events whose source has no timestamp; nil
	// reads the wall clock. Tests inject a fixed clock.
	Now func() time.Time

	// Offset is the number of source bytes already consumed. Parse advances
	// it past every complete line it processed; a trailing partial line is
	// left for the next call. The reader passed to Parse must be positioned
	// at Offset (see ParseFile).
	Offset int64

	// Pending holds tool calls seen without their result yet, keyed by call
	// id, so a result arriving in a later Parse call still pairs.
	Pending map[string]PendingCall
}

// PendingCall is a tool call waiting for its result.
type PendingCall struct {
	Name string
	At   time.Time
}

func (c *Context) now() time.Time {
	if c != nil && c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Context) pending() map[string]PendingCall {
	if c.Pending == nil {
		c.Pending = map[string]PendingCall{}
	}
	return c.Pending
}

// Adapter parses one backend's transcript format.
type Adapter interface {
	// Name is the backend name used in tracing.backends.
	Name() string
	// Coverage states how much of the backend's activity the adapter sees.
	Coverage() trace.Coverage
	// Parse reads transcript lines from r and returns the events in them,
	// advancing ctx.Offset. Malformed lines are skipped.
	Parse(r io.Reader, ctx *Context) ([]trace.Event, error)
}

// ParseFile opens path, seeks to ctx.Offset and parses from there. A file
// that shrank below the offset (rotated or truncated) is re-read from the
// start with the pending state cleared.
func ParseFile(a Adapter, path string, ctx *Context) ([]trace.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	if st, err := f.Stat(); err == nil && st.Size() < ctx.Offset {
		ctx.Offset = 0
		ctx.Pending = nil
	}
	if _, err := f.Seek(ctx.Offset, io.SeekStart); err != nil {
		return nil, err
	}
	return a.Parse(f, ctx)
}

// Observation is a daemon-observed session state, the fallback source when
// a transcript is missing or carries no detail.
type Observation struct {
	State string
	At    time.Time
}

// FallbackEvents builds state-change events from daemon observations for a
// backend whose transcript gave nothing. Every event carries FallbackNote.
func FallbackEvents(backend string, obs []Observation, ctx *Context) []trace.Event {
	out := make([]trace.Event, 0, len(obs))
	for _, o := range obs {
		at := o.At
		if at.IsZero() {
			at = ctx.now()
		}
		ev := stamp(trace.Event{
			Kind:       trace.KindStateChange,
			Name:       o.State,
			ArgSummary: FallbackNote,
			Timestamp:  at.UTC(),
		}, backend, ctx)
		out = append(out, ev)
	}
	return out
}

// ParseOrFallback parses the transcript and, when it yields no events,
// returns the daemon-observed fallback instead.
func ParseOrFallback(a Adapter, r io.Reader, ctx *Context, obs []Observation) ([]trace.Event, error) {
	var evs []trace.Event
	var err error
	if r != nil {
		evs, err = a.Parse(r, ctx)
	}
	if len(evs) == 0 {
		return FallbackEvents(a.Name(), obs, ctx), err
	}
	return evs, err
}

func stamp(ev trace.Event, backend string, ctx *Context) trace.Event {
	ev.Backend = backend
	if ctx != nil {
		ev.SessionID, ev.TaskID, ev.StoryID = ctx.SessionID, ctx.TaskID, ctx.StoryID
		ev.AutomatonID, ev.Agent = ctx.AutomatonID, ctx.Agent
	}
	return ev
}
