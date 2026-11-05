package adapters

import (
	"encoding/json"
	"io"
	"time"

	"github.com/dmz006/datawatch/internal/trace"
)

// Gemini parses the Gemini CLI's file telemetry output (JSON lines, one
// log record per line with an "event.name" attribute). Coverage is partial:
// tool calls and API responses are recorded, but permission prompts and
// in-pane activity are not.
type Gemini struct{}

// NewGemini returns the gemini adapter.
func NewGemini() *Gemini { return &Gemini{} }

// Name implements Adapter.
func (*Gemini) Name() string { return "gemini" }

// Coverage implements Adapter.
func (*Gemini) Coverage() trace.Coverage { return trace.CoveragePartial }

type geminiRecord struct {
	Timestamp  string `json:"timestamp"`
	Attributes struct {
		Event      string          `json:"event.name"`
		Function   string          `json:"function_name"`
		Args       json.RawMessage `json:"function_args"`
		DurationMS int64           `json:"duration_ms"`
		Success    *bool           `json:"success"`
		Model      string          `json:"model"`
		InputTok   int64           `json:"input_token_count"`
		OutputTok  int64           `json:"output_token_count"`
	} `json:"attributes"`
}

// Parse implements Adapter. gemini_cli.tool_call records become a tool_call
// plus a tool_result (outcome from success, duration from duration_ms);
// gemini_cli.api_response records become model_request events.
func (a *Gemini) Parse(r io.Reader, ctx *Context) ([]trace.Event, error) {
	var out []trace.Event
	err := scanLines(r, ctx, func(line []byte) {
		var rec geminiRecord
		if json.Unmarshal(line, &rec) != nil {
			return
		}
		at := ctx.now()
		if t, err := time.Parse(time.RFC3339Nano, rec.Timestamp); err == nil {
			at = t.UTC()
		}
		at2 := rec.Attributes
		switch at2.Event {
		case "gemini_cli.tool_call":
			out = append(out,
				stamp(trace.Event{Kind: trace.KindToolCall, Name: at2.Function,
					ArgSummary: compact(at2.Args), Timestamp: at}, a.Name(), ctx),
				stamp(trace.Event{Kind: trace.KindToolResult, Name: at2.Function,
					Outcome: outcome(at2.Success), DurationMS: at2.DurationMS, Timestamp: at}, a.Name(), ctx))
		case "gemini_cli.api_response":
			out = append(out, stamp(trace.Event{
				Kind: trace.KindModelRequest, Name: at2.Model, Timestamp: at,
				TokensIn: at2.InputTok, TokensOut: at2.OutputTok,
			}, a.Name(), ctx))
		}
	})
	return out, err
}

func outcome(success *bool) string {
	if success != nil && !*success {
		return "error"
	}
	return "ok"
}
