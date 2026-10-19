package main

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/backends/aider"
	"github.com/dmz006/datawatch/internal/llm/backends/goose"
	"github.com/dmz006/datawatch/internal/llm/backends/pi"
	"github.com/dmz006/datawatch/internal/session"
)

// stubBackend is a deterministic llm.Backend for testing the capability
// gate. It is registered under a name that appears in no legacy name list,
// so the "regress-capability-prompt" case exercises only the capability
// path: it MUST return false purely because SupportsInteractiveInput()
// is false.
type stubBackend struct {
	interactive bool
}

func (s *stubBackend) Name() string { return "regress-capability-prompt" }
func (s *stubBackend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	return nil
}
func (s *stubBackend) SupportsInteractiveInput() bool { return s.interactive }
func (s *stubBackend) Version() string                { return "test" }

func TestNeedsTUITaskDelivery(t *testing.T) {
	// Register the real backends for the families asserted below so the
	// test is deterministic regardless of what else in package main (or
	// test order) happens to have registered. Re-registering just
	// overwrites the registry entry; the gate only calls
	// SupportsInteractiveInput().
	llm.Register(pi.New(""))                       // name "pi"
	llm.Register(pi.NewPrompt(""))                 // name "pi-prompt"
	llm.Register(goose.New(""))                    // name "goose"
	llm.Register(goose.NewPrompt(""))              // name "goose-prompt"
	llm.Register(aider.New(""))                    // name "aider"
	llm.Register(&stubBackend{interactive: false}) // name "regress-capability-prompt"

	cases := []struct {
		name string
		sess session.Session
		want bool
	}{
		{
			name: "not waiting_input returns false",
			sess: session.Session{State: session.StateRunning, OneShot: true, Task: "do the thing", BackendFamily: "goose"},
			want: false,
		},
		{
			name: "not one-shot returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: false, Task: "do the thing", BackendFamily: "goose"},
			want: false,
		},
		{
			name: "empty task returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "", BackendFamily: "goose"},
			want: false,
		},
		{
			name: "empty backend family returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: ""},
			want: false,
		},
		{
			name: "unregistered prompt-style family with SupportsInteractiveInput false returns false (regression anchor)",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: "regress-capability-prompt"},
			want: false,
		},
		{
			name: "pi-prompt one-shot waiting_input with task returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: "pi-prompt"},
			want: false,
		},
		{
			name: "goose-prompt one-shot waiting_input with task returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: "goose-prompt"},
			want: false,
		},
		{
			name: "aider one-shot waiting_input with task returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: "aider"},
			want: false,
		},
		{
			name: "goose one-shot waiting_input with task returns true",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: "goose"},
			want: true,
		},
		{
			name: "claude-code one-shot waiting_input with task returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: "claude-code"},
			want: false,
		},
		{
			name: "opencode-acp one-shot waiting_input with task returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: "opencode-acp"},
			want: false,
		},
		{
			name: "subprocess one-shot waiting_input with task returns false",
			sess: session.Session{State: session.StateWaitingInput, OneShot: true, Task: "do the thing", BackendFamily: "subprocess"},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := tc.sess
			if got := needsTUITaskDelivery(&sess); got != tc.want {
				t.Fatalf("needsTUITaskDelivery() = %v, want %v", got, tc.want)
			}
		})
	}
}
