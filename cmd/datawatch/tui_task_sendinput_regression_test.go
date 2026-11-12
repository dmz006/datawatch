package main

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/llm"
	aiderbackend "github.com/dmz006/datawatch/internal/llm/backends/aider"
	"github.com/dmz006/datawatch/internal/llm/backends/goose"
	"github.com/dmz006/datawatch/internal/llm/backends/pi"
	"github.com/dmz006/datawatch/internal/session"
)

// recordingSendInput mirrors the method the poll loop calls
// (session.Manager.SendInput) and records every invocation instead of
// touching a real tmux server.
type recordingSendInput struct {
	calls []string
}

func (r *recordingSendInput) SendInput(fullID, input, source string) error {
	r.calls = append(r.calls, fullID+"|"+input+"|"+source)
	return nil
}

// promptModeStub is a minimal llm.Backend used to register a brand-new
// family name that is absent from every legacy exclusion list. Its
// SupportsInteractiveInput() == false is the whole decision input: under
// the old name-list gate this family (being unlisted) WOULD have been
// delivered via send_input, so this scenario fails against the old code
// and passes only under the capability-based gate.
type promptModeStub struct {
	name string
}

func (s *promptModeStub) Name() string { return s.name }
func (s *promptModeStub) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	return nil
}
func (s *promptModeStub) SupportsInteractiveInput() bool { return false }
func (s *promptModeStub) Version() string                { return "test-stub" }

// TestPromptModeSessionNeverReceivesSendInput pins the TUI task delivery
// gate: a one-shot session in waiting_input with a task must have send_input
// invoked only when the backend itself supports interactive input.
func TestPromptModeSessionNeverReceivesSendInput(t *testing.T) {
	// Make the gate's capability lookup deterministic and independent of
	// test ordering / other test registrations.
	llm.Register(pi.NewPrompt(""))
	llm.Register(goose.New(""))
	llm.Register(goose.NewPrompt(""))
	llm.Register(aiderbackend.New(""))
	llm.Register(&promptModeStub{name: "regress-never-send"})

	scenarios := []struct {
		family      string
		wantCalls   int
	}{
		{"regress-never-send", 0}, // unlisted prompt-mode name: capability decides
		{"pi-prompt", 0},
		{"goose-prompt", 0},
		{"aider", 0},
		{"goose", 1}, // interactive backend: still delivered
		{"claude-code", 0},
		{"opencode-acp", 0},
		{"subprocess", 0},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.family, func(t *testing.T) {
			sess := &session.Session{
				FullID:       "test-0001",
				State:        session.StateWaitingInput,
				OneShot:      true,
				Task:         "do the one-shot task",
				BackendFamily: sc.family,
			}

			fresh := &recordingSendInput{}
			// Mirrors the main.go call site: gate false ⇒ SendInput never called.
			if needsTUITaskDelivery(sess) {
				if err := fresh.SendInput(sess.FullID, sess.Task, "tui-task-delivery"); err != nil {
					t.Errorf("family %q: SendInput(%q) error = %v, want nil", sc.family, sess.Task, err)
				}
			}

			if len(fresh.calls) != sc.wantCalls {
				t.Errorf("family %q: got %d send_input calls (%v), want %d", sc.family, len(fresh.calls), fresh.calls, sc.wantCalls)
			}
		})
	}
}
