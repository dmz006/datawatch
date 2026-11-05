// Package observe connects the daemon-observed recorder hooks of the
// ollama, openwebui and shell backends to the trace adapters and store.
// Install registers the hooks; Close removes them.
package observe

import (
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/llm/backends/ollama"
	"github.com/dmz006/datawatch/internal/llm/backends/openwebui"
	"github.com/dmz006/datawatch/internal/llm/backends/shell"
	"github.com/dmz006/datawatch/internal/trace"
	"github.com/dmz006/datawatch/internal/trace/adapters"
)

// Sink receives finished events; *trace.Store satisfies it.
type Sink interface {
	Append(events ...trace.Event) (int, error)
}

// Observer owns the recorder registrations and the pending shell commands.
type Observer struct {
	sink     Sink
	settings trace.Settings
	resolve  func(tmuxSession string) (sessionID string)
	now      func() time.Time

	mu       sync.Mutex
	commands map[string]shell.CommandRecord // last command per tmux session
	closed   bool
}

// Install registers the three recorder hooks. resolve maps a tmux session
// name to the datawatch session id (nil keeps the tmux name). now is the
// clock for events without a timestamp (nil reads the wall clock).
func Install(sink Sink, s trace.Settings, resolve func(string) string, now func() time.Time) *Observer {
	o := &Observer{sink: sink, settings: s.WithDefaults(), resolve: resolve, now: now,
		commands: map[string]shell.CommandRecord{}}
	ollama.SetRecorder(func(r ollama.RequestRecord) {
		o.http("ollama", adapters.NewOllama(), r.Session, r.Model, r.TokensIn, r.TokensOut, r.Start, r.Duration, r.StatusCode, r.Err)
	})
	openwebui.SetRecorder(func(r openwebui.RequestRecord) {
		o.http("openwebui", adapters.NewOpenWebUI(), r.Session, r.Model, r.TokensIn, r.TokensOut, r.Start, r.Duration, r.StatusCode, r.Err)
	})
	shell.SetRecorder(o.command)
	return o
}

// Close clears the hooks and stops recording.
func (o *Observer) Close() {
	o.mu.Lock()
	o.closed = true
	o.mu.Unlock()
	ollama.SetRecorder(nil)
	openwebui.SetRecorder(nil)
	shell.SetRecorder(nil)
}

// SessionEnded reports the exit code of the command the daemon started in
// tmuxSession, taken from the daemon's own session record. It is a no-op
// for sessions that never ran a shell command.
func (o *Observer) SessionEnded(tmuxSession string, exitCode int) {
	o.mu.Lock()
	rec, ok := o.commands[tmuxSession]
	delete(o.commands, tmuxSession)
	o.mu.Unlock()
	if ok {
		shell.ReportExit(tmuxSession, rec.Command, exitCode, rec.Start)
	}
}

func (o *Observer) ctx(tmux string) *adapters.Context {
	id := tmux
	if o.resolve != nil {
		if r := o.resolve(tmux); r != "" {
			id = r
		}
	}
	return &adapters.Context{SessionID: id, Agent: "", Now: o.now}
}

func (o *Observer) active(backend string) bool {
	o.mu.Lock()
	closed := o.closed
	o.mu.Unlock()
	return !closed && o.settings.BackendEnabled(backend) && o.settings.Mode != trace.ModeOff
}

func (o *Observer) http(backend string, a *adapters.HTTPObserved, session, model string, in, out int, start time.Time, d time.Duration, status int, err error) {
	if !o.active(backend) {
		return
	}
	req := adapters.HTTPRequest{Model: model, TokensIn: int64(in), TokensOut: int64(out), Start: start,
		DurationMS: d.Milliseconds(), StatusCode: status}
	if err != nil {
		req.Error = err.Error()
	}
	_, _ = o.sink.Append(a.Events([]adapters.HTTPRequest{req}, o.ctx(session))...)
}

func (o *Observer) command(r shell.CommandRecord) {
	if !o.active("shell") {
		return
	}
	cmd := adapters.ShellCommand{Command: r.Command, ExitCode: r.ExitCode, Start: r.Start}
	if r.ExitCode == nil {
		o.mu.Lock()
		o.commands[r.Session] = r
		o.mu.Unlock()
	} else if !r.Start.IsZero() {
		cmd.DurationMS = o.now0().Sub(r.Start).Milliseconds()
	}
	evs := adapters.NewShell().Events([]adapters.ShellCommand{cmd}, o.ctx(r.Session))
	_, _ = o.sink.Append(evs...)
}

func (o *Observer) now0() time.Time {
	if o.now != nil {
		return o.now()
	}
	return time.Now()
}
