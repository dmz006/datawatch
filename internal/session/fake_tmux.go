// BL89 — in-memory fake tmux for tests.
//
// FakeTmux satisfies TmuxAPI without shelling out, so router / server
// handler tests can construct a real session.Manager and exercise
// session lifecycle without a tmux server. Records every call it
// receives for assertions.

package session

import (
	"fmt"
	"sync"
	"time"
)

// FakeTmux is a test-only TmuxAPI that records calls in memory.
type FakeTmux struct {
	mu       sync.Mutex
	sessions map[string]bool
	Calls    []FakeTmuxCall

	// FailNext holds errors to return from subsequent calls. Pop from
	// the front; when empty, calls succeed. Used to assert error paths.
	FailNext []error

	// Pane simulates visible pane content returned by CapturePane*.
	Pane map[string]string
}

// FakeTmuxCall records one method invocation.
type FakeTmuxCall struct {
	Op      string
	Session string
	Args    []string
}

// NewFakeTmux returns a fresh in-memory fake.
func NewFakeTmux() *FakeTmux {
	return &FakeTmux{
		sessions: map[string]bool{},
		Pane:     map[string]string{},
	}
}

func (f *FakeTmux) record(op, session string, args ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, FakeTmuxCall{Op: op, Session: session, Args: args})
	if len(f.FailNext) > 0 {
		err := f.FailNext[0]
		f.FailNext = f.FailNext[1:]
		return err
	}
	return nil
}

// Count returns the number of calls with the given op.
func (f *FakeTmux) Count(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if c.Op == op {
			n++
		}
	}
	return n
}

// LastCall returns the most recent call with the given op, or zero value.
func (f *FakeTmux) LastCall(op string) FakeTmuxCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.Calls) - 1; i >= 0; i-- {
		if f.Calls[i].Op == op {
			return f.Calls[i]
		}
	}
	return FakeTmuxCall{}
}

func (f *FakeTmux) NewSessionWithSize(name string, cols, rows int) error {
	if err := f.record("new", name, fmt.Sprintf("%dx%d", cols, rows)); err != nil {
		return err
	}
	f.mu.Lock()
	f.sessions[name] = true
	f.mu.Unlock()
	return nil
}

func (f *FakeTmux) SessionExists(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[name]
}

func (f *FakeTmux) SendKeys(session, keys string) error {
	return f.record("send-keys", session, keys, "Enter")
}

func (f *FakeTmux) SendKeysWithSettle(session, keys string, settle time.Duration) error {
	return f.record("send-keys-settle", session, keys, settle.String())
}

func (f *FakeTmux) SendKeysLiteral(session, data string) error {
	return f.record("send-keys-literal", session, data)
}

func (f *FakeTmux) ResizePane(session string, cols, rows int) error {
	return f.record("resize", session, fmt.Sprintf("%dx%d", cols, rows))
}

func (f *FakeTmux) CapturePaneVisible(session string) (string, error) {
	if err := f.record("capture-visible", session); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Pane[session], nil
}

func (f *FakeTmux) CapturePaneANSI(session string) (string, error) {
	if err := f.record("capture-ansi", session); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Pane[session], nil
}

func (f *FakeTmux) PipeOutput(session, logFile string) error {
	return f.record("pipe", session, logFile)
}

func (f *FakeTmux) KillSession(name string) error {
	f.mu.Lock()
	delete(f.sessions, name)
	f.mu.Unlock()
	return f.record("kill", name)
}

func (f *FakeTmux) SetEnvironment(session string, env map[string]string) error {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	return f.record("set-env", session, keys...)
}

// WithFakeTmux swaps in a FakeTmux on the given Manager and returns it.
// The Manager MUST NOT have running sessions at the time of swap.
// Intended for use in tests only.
func (m *Manager) WithFakeTmux() *FakeTmux {
	f := NewFakeTmux()
	m.tmux = f
	return f
}
