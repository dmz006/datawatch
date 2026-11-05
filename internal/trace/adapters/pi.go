package adapters

import (
	"io"

	"github.com/dmz006/datawatch/internal/pilog"
	"github.com/dmz006/datawatch/internal/trace"
)

// Pi parses pi session logs through internal/pilog. It serves both the pi
// and pi-prompt backends, which share one log format.
type Pi struct{ name string }

// NewPi returns the adapter for the named pi backend ("pi" or "pi-prompt").
func NewPi(name string) *Pi { return &Pi{name: name} }

// Name implements Adapter.
func (a *Pi) Name() string { return a.name }

// Coverage implements Adapter.
func (*Pi) Coverage() trace.Coverage { return trace.CoverageFull }

// Parse implements Adapter. Each assistant turn yields a model_request with
// its token usage, then one tool_call per tool call in block order. Pi logs
// carry no per-call result timing, so no duration is reported.
func (a *Pi) Parse(r io.Reader, ctx *Context) ([]trace.Event, error) {
	turns, consumed, err := pilog.ParseTimedTurns(r)
	ctx.Offset += consumed
	var out []trace.Event
	for _, t := range turns {
		at := t.At
		if at.IsZero() {
			at = ctx.now()
		}
		at = at.UTC()
		if t.TokensIn > 0 || t.TokensOut > 0 {
			out = append(out, stamp(trace.Event{
				Kind: trace.KindModelRequest, Timestamp: at,
				TokensIn: int64(t.TokensIn), TokensOut: int64(t.TokensOut),
			}, a.name, ctx))
		}
		for _, tc := range t.ToolCalls {
			out = append(out, stamp(trace.Event{
				Kind: trace.KindToolCall, Name: tc.Name,
				ArgSummary: tc.Args, Timestamp: at,
			}, a.name, ctx))
		}
	}
	return out, err
}
