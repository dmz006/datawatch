package observe

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/llm/backends/shell"
	"github.com/dmz006/datawatch/internal/trace"
	"github.com/dmz006/datawatch/internal/trace/adapters"
)

func newOllama() *adapters.HTTPObserved { return adapters.NewOllama() }

type fakeSink struct {
	mu  sync.Mutex
	evs []trace.Event
	ch  chan struct{}
}

func newSink() *fakeSink { return &fakeSink{ch: make(chan struct{}, 16)} }

func (f *fakeSink) Append(e ...trace.Event) (int, error) {
	f.mu.Lock()
	f.evs = append(f.evs, e...)
	f.mu.Unlock()
	f.ch <- struct{}{}
	return len(e), nil
}

func (f *fakeSink) wait(t *testing.T) []trace.Event {
	t.Helper()
	select {
	case <-f.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no events recorded")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]trace.Event(nil), f.evs...)
}

var fixed = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

func TestShellCommandAndExit(t *testing.T) {
	s := newSink()
	o := Install(s, trace.Settings{}, func(string) string { return "host-ab12" }, fixed)
	defer o.Close()
	start := fixed().Add(-2 * time.Second)
	shell.ReportExit("tmux-1", "make test", 0, start) // no pending: still emitted via hook
	evs := s.wait(t)
	if len(evs) != 2 || evs[1].Name != "command_exit" || evs[1].Outcome != "ok" || evs[0].SessionID != "host-ab12" {
		t.Fatalf("unexpected events: %+v", evs)
	}
	if evs[1].DurationMS != 2000 {
		t.Errorf("duration = %d", evs[1].DurationMS)
	}
}

func TestSessionEndedUsesPendingCommand(t *testing.T) {
	s := newSink()
	o := Install(s, trace.Settings{}, nil, fixed)
	defer o.Close()
	o.mu.Lock()
	o.commands["t1"] = shell.CommandRecord{Session: "t1", Command: "false", Start: fixed().Add(-time.Second)}
	o.mu.Unlock()
	o.SessionEnded("t1", 1)
	evs := s.wait(t)
	last := evs[len(evs)-1]
	if last.Name != "command_exit" || last.Outcome != "error" || last.SessionID != "t1" {
		t.Fatalf("bad exit event: %+v", last)
	}
	o.SessionEnded("t1", 1) // second call is a no-op
}

func TestDisabledBackendAndClose(t *testing.T) {
	s := newSink()
	o := Install(s, trace.Settings{Backends: map[string]bool{"shell": false}}, nil, fixed)
	shell.ReportExit("t", "ls", 0, fixed())
	time.Sleep(50 * time.Millisecond)
	if len(s.evs) != 0 {
		t.Fatal("disabled backend recorded")
	}
	o.Close()
}

func TestHTTPErrorOutcome(t *testing.T) {
	s := newSink()
	o := Install(s, trace.Settings{}, nil, fixed)
	defer o.Close()
	o.http("ollama", newOllama(), "t", "qwen", 3, 4, fixed(), time.Second, 500, errors.New("boom"))
	evs := s.wait(t)
	if evs[0].Outcome != "error" || evs[0].TokensIn != 3 || evs[0].Name != "qwen" || evs[0].DurationMS != 1000 {
		t.Fatalf("bad event: %+v", evs[0])
	}
}
