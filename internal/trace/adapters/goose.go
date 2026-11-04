package adapters

import (
	"encoding/json"
	"io"

	"github.com/dmz006/datawatch/internal/trace"
)

// Goose parses goose session logs (JSON lines, one message per line, as
// written under goose's sessions directory and by "goose session export").
// It serves the goose and goose-prompt backends. A session started without
// --name has no discoverable log; callers then fall back to daemon events.
type Goose struct{ name string }

// NewGoose returns the adapter for "goose" or "goose-prompt".
func NewGoose(name string) *Goose { return &Goose{name: name} }

// Name implements Adapter.
func (a *Goose) Name() string { return a.name }

// Coverage implements Adapter.
func (*Goose) Coverage() trace.Coverage { return trace.CoverageFull }

type gooseLine struct {
	Role    string  `json:"role"`
	Created float64 `json:"created"`
	// Metadata line: cumulative usage for the turn just finished.
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	Content      []struct {
		Type     string `json:"type"`
		ID       string `json:"id"`
		ToolCall struct {
			Status string `json:"status"`
			Value  struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"value"`
		} `json:"toolCall"`
		ToolResult struct {
			Status string `json:"status"`
		} `json:"toolResult"`
	} `json:"content"`
}

// Parse implements Adapter. toolRequest content becomes a tool_call and the
// matching toolResponse a tool_result with the timestamp difference as
// duration. A line carrying input_tokens/output_tokens becomes a
// model_request.
func (a *Goose) Parse(r io.Reader, ctx *Context) ([]trace.Event, error) {
	var out []trace.Event
	err := scanLines(r, ctx, func(line []byte) {
		var l gooseLine
		if json.Unmarshal(line, &l) != nil {
			return
		}
		at := unixTime(l.Created)
		if at.IsZero() {
			at = ctx.now()
		}
		if l.InputTokens > 0 || l.OutputTokens > 0 {
			out = append(out, stamp(trace.Event{
				Kind: trace.KindModelRequest, Timestamp: at,
				TokensIn: l.InputTokens, TokensOut: l.OutputTokens,
			}, a.name, ctx))
		}
		for _, c := range l.Content {
			switch c.Type {
			case "toolRequest":
				name := c.ToolCall.Value.Name
				ctx.pending()[c.ID] = PendingCall{Name: name, At: at}
				out = append(out, stamp(trace.Event{
					Kind: trace.KindToolCall, Name: name,
					ArgSummary: compact(c.ToolCall.Value.Arguments), Timestamp: at,
				}, a.name, ctx))
			case "toolResponse":
				ev := trace.Event{Kind: trace.KindToolResult, Outcome: "ok", Timestamp: at}
				if c.ToolResult.Status == "error" {
					ev.Outcome = "error"
				}
				if p, ok := ctx.pending()[c.ID]; ok {
					ev.Name = p.Name
					if d := at.Sub(p.At); d > 0 {
						ev.DurationMS = d.Milliseconds()
					}
					delete(ctx.Pending, c.ID)
				}
				out = append(out, stamp(ev, a.name, ctx))
			}
		}
	})
	return out, err
}
