package adapters

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/dmz006/datawatch/internal/trace"
)

// OpenCode parses opencode's event stream as JSON lines: the ACP server's
// SSE events (an optional "data:" prefix is accepted) or events replayed
// from the session store. It serves the opencode, opencode-acp and
// opencode-prompt backends. The ACP backend streams every event, so it
// reports full coverage; the tmux variants only have a transcript when the
// session store was exported, so they report partial coverage and the
// caller falls back to daemon events when nothing parses.
type OpenCode struct{ name string }

// NewOpenCode returns the adapter for "opencode", "opencode-acp" or
// "opencode-prompt".
func NewOpenCode(name string) *OpenCode { return &OpenCode{name: name} }

// Name implements Adapter.
func (a *OpenCode) Name() string { return a.name }

// Coverage implements Adapter.
func (a *OpenCode) Coverage() trace.Coverage {
	if a.name == "opencode-acp" {
		return trace.CoverageFull
	}
	return trace.CoveragePartial
}

type ocEvent struct {
	Type       string `json:"type"`
	Properties struct {
		Part struct {
			Type   string `json:"type"`
			CallID string `json:"callID"`
			Tool   string `json:"tool"`
			State  struct {
				Status string          `json:"status"`
				Input  json.RawMessage `json:"input"`
				Time   struct {
					Start float64 `json:"start"`
					End   float64 `json:"end"`
				} `json:"time"`
			} `json:"state"`
			Time struct {
				End float64 `json:"end"`
			} `json:"time"`
			Tokens struct {
				Input  int64 `json:"input"`
				Output int64 `json:"output"`
				Cache  struct {
					Read  int64 `json:"read"`
					Write int64 `json:"write"`
				} `json:"cache"`
			} `json:"tokens"`
		} `json:"part"`
	} `json:"properties"`
}

// Parse implements Adapter. A tool part becomes a tool_call the first time
// its call id is seen and a tool_result once it completes or errors (the
// duration comes from the part's own start/end times). A step-finish part
// becomes a model_request carrying its token usage.
func (a *OpenCode) Parse(r io.Reader, ctx *Context) ([]trace.Event, error) {
	var out []trace.Event
	err := scanLines(r, ctx, func(line []byte) {
		line = bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		var e ocEvent
		if json.Unmarshal(line, &e) != nil || e.Type != "message.part.updated" {
			return
		}
		p := e.Properties.Part
		switch p.Type {
		case "tool":
			at := unixTime(p.State.Time.Start)
			if at.IsZero() {
				at = ctx.now()
			}
			if _, seen := ctx.pending()[p.CallID]; !seen && p.CallID != "" {
				ctx.Pending[p.CallID] = PendingCall{Name: p.Tool, At: at}
				out = append(out, stamp(trace.Event{
					Kind: trace.KindToolCall, Name: p.Tool,
					ArgSummary: compact(p.State.Input), Timestamp: at,
				}, a.name, ctx))
			}
			if p.State.Status != "completed" && p.State.Status != "error" {
				return
			}
			ev := trace.Event{Kind: trace.KindToolResult, Name: p.Tool, Outcome: "ok", Timestamp: at}
			if end := unixTime(p.State.Time.End); !end.IsZero() {
				ev.Timestamp = end
				if d := end.Sub(at); d > 0 {
					ev.DurationMS = d.Milliseconds()
				}
			}
			if p.State.Status == "error" {
				ev.Outcome = "error"
			}
			delete(ctx.Pending, p.CallID)
			out = append(out, stamp(ev, a.name, ctx))
		case "step-finish":
			at := unixTime(p.Time.End)
			if at.IsZero() {
				at = ctx.now()
			}
			out = append(out, stamp(trace.Event{
				Kind: trace.KindModelRequest, Timestamp: at,
				TokensIn:  p.Tokens.Input + p.Tokens.Cache.Read + p.Tokens.Cache.Write,
				TokensOut: p.Tokens.Output,
			}, a.name, ctx))
		}
	})
	return out, err
}
