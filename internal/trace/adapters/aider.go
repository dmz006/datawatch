package adapters

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/dmz006/datawatch/internal/trace"
)

// Aider parses aider's analytics log (JSON lines, --analytics-log) mixed
// with lines of its chat history markdown (.aider.chat.history.md), either
// or both concatenated into one stream. Coverage is partial: the analytics
// log gives model requests with token counts, the chat history only shows
// applied edits and shell commands, and aider exposes no per-tool results.
type Aider struct{}

// NewAider returns the aider adapter.
func NewAider() *Aider { return &Aider{} }

// Name implements Adapter.
func (*Aider) Name() string { return "aider" }

// Coverage implements Adapter.
func (*Aider) Coverage() trace.Coverage { return trace.CoveragePartial }

type aiderAnalytics struct {
	Event      string  `json:"event"`
	Time       float64 `json:"time"`
	Properties struct {
		Model      string `json:"main_model"`
		Prompt     int64  `json:"prompt_tokens"`
		Completion int64  `json:"completion_tokens"`
	} `json:"properties"`
}

// Parse implements Adapter. A message_send analytics event becomes a
// model_request. Chat-history lines "> Applied edit to <file>" and
// "> Running <command>" become tool_call events named edit and run.
func (a *Aider) Parse(r io.Reader, ctx *Context) ([]trace.Event, error) {
	var out []trace.Event
	err := scanLines(r, ctx, func(line []byte) {
		s := string(line)
		if strings.HasPrefix(s, "{") {
			var e aiderAnalytics
			if json.Unmarshal(line, &e) != nil || e.Event != "message_send" {
				return
			}
			at := unixTime(e.Time)
			if at.IsZero() {
				at = ctx.now()
			}
			out = append(out, stamp(trace.Event{
				Kind: trace.KindModelRequest, Name: e.Properties.Model, Timestamp: at,
				TokensIn: e.Properties.Prompt, TokensOut: e.Properties.Completion,
			}, a.Name(), ctx))
			return
		}
		for prefix, name := range map[string]string{"> Applied edit to ": "edit", "> Running ": "run"} {
			if rest, ok := strings.CutPrefix(s, prefix); ok {
				out = append(out, stamp(trace.Event{
					Kind: trace.KindToolCall, Name: name,
					ArgSummary: strings.TrimSpace(rest), Timestamp: ctx.now(),
				}, a.Name(), ctx))
			}
		}
	})
	return out, err
}
