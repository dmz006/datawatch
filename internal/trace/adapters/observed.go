package adapters

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/trace"
)

// HTTPRequest is one daemon-observed HTTP chat request to a model server.
// It holds no prompt or response text.
type HTTPRequest struct {
	Model      string    `json:"model"`
	TokensIn   int64     `json:"tokens_in,omitempty"`
	TokensOut  int64     `json:"tokens_out,omitempty"`
	Start      time.Time `json:"start"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	StatusCode int       `json:"status_code,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// HTTPObserved serves the ollama and openwebui backends. The daemon sees
// only the HTTP requests it makes, so coverage is events-only: model
// requests with model, tokens and duration, and never tool calls.
type HTTPObserved struct{ name string }

// NewOllama returns the adapter for the ollama backend.
func NewOllama() *HTTPObserved { return &HTTPObserved{name: "ollama"} }

// NewOpenWebUI returns the adapter for the openwebui backend.
func NewOpenWebUI() *HTTPObserved { return &HTTPObserved{name: "openwebui"} }

// Name implements Adapter.
func (a *HTTPObserved) Name() string { return a.name }

// Coverage implements Adapter.
func (*HTTPObserved) Coverage() trace.Coverage { return trace.CoverageEventsOnly }

// Events converts observed requests to model_request events.
func (a *HTTPObserved) Events(reqs []HTTPRequest, ctx *Context) []trace.Event {
	out := make([]trace.Event, 0, len(reqs))
	for _, r := range reqs {
		at := r.Start
		if at.IsZero() {
			at = ctx.now()
		}
		outcome := "ok"
		if r.Error != "" || r.StatusCode >= 400 {
			outcome = "error"
		}
		summary := "daemon-observed HTTP request"
		if r.StatusCode > 0 {
			summary = fmt.Sprintf("%s: HTTP %d", summary, r.StatusCode)
		}
		out = append(out, stamp(trace.Event{
			Kind: trace.KindModelRequest, Name: r.Model, ArgSummary: summary,
			Outcome: outcome, DurationMS: r.DurationMS,
			TokensIn: r.TokensIn, TokensOut: r.TokensOut, Timestamp: at.UTC(),
		}, a.name, ctx))
	}
	return out
}

// Parse implements Adapter over a journal of HTTPRequest JSON lines.
func (a *HTTPObserved) Parse(r io.Reader, ctx *Context) ([]trace.Event, error) {
	var reqs []HTTPRequest
	err := scanLines(r, ctx, func(line []byte) {
		var q HTTPRequest
		if json.Unmarshal(line, &q) == nil && q.Model != "" {
			reqs = append(reqs, q)
		}
	})
	return a.Events(reqs, ctx), err
}

// ShellCommand is one command from the daemon's own session record.
type ShellCommand struct {
	Command    string    `json:"command"`
	ExitCode   *int      `json:"exit_code,omitempty"`
	Start      time.Time `json:"start"`
	DurationMS int64     `json:"duration_ms,omitempty"`
}

// Shell serves the shell backend from the daemon's session record:
// commands run and their exit codes, events-only (state changes).
type Shell struct{}

// NewShell returns the adapter for the shell backend.
func NewShell() *Shell { return &Shell{} }

// Name implements Adapter.
func (*Shell) Name() string { return "shell" }

// Coverage implements Adapter.
func (*Shell) Coverage() trace.Coverage { return trace.CoverageEventsOnly }

// Events converts commands to state_change events: "command" when run and,
// once an exit code is known, "command_exit" with outcome ok (0) or error.
func (a *Shell) Events(cmds []ShellCommand, ctx *Context) []trace.Event {
	var out []trace.Event
	for _, c := range cmds {
		at := c.Start
		if at.IsZero() {
			at = ctx.now()
		}
		at = at.UTC()
		out = append(out, stamp(trace.Event{
			Kind: trace.KindStateChange, Name: "command",
			ArgSummary: strings.TrimSpace(c.Command), Timestamp: at,
		}, "shell", ctx))
		if c.ExitCode == nil {
			continue
		}
		outcome := "ok"
		if *c.ExitCode != 0 {
			outcome = "error"
		}
		out = append(out, stamp(trace.Event{
			Kind: trace.KindStateChange, Name: "command_exit",
			ArgSummary: fmt.Sprintf("exit %d", *c.ExitCode), Outcome: outcome,
			DurationMS: c.DurationMS,
			Timestamp:  at.Add(time.Duration(c.DurationMS) * time.Millisecond),
		}, "shell", ctx))
	}
	return out
}

// Parse implements Adapter over a journal of ShellCommand JSON lines.
func (a *Shell) Parse(r io.Reader, ctx *Context) ([]trace.Event, error) {
	var cmds []ShellCommand
	err := scanLines(r, ctx, func(line []byte) {
		var c ShellCommand
		if json.Unmarshal(line, &c) == nil && c.Command != "" {
			cmds = append(cmds, c)
		}
	})
	return a.Events(cmds, ctx), err
}
