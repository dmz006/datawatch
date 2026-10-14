// Regression proof that StartOptions.ClaudeEffort (claude-code's real
// reasoning `--effort` enum) reaches the backend via SetEffort, and that
// it is independent of the BL41 thoroughness hint StartOptions.Effort.
// The two were previously confused: autonomous tasks only ever set the
// BL41 field, so no --effort reached the claude CLI.

package session

import (
	"context"
	"testing"
)

// effortStubBackend is a minimal llm.Backend that records SetEffort calls.
type effortStubBackend struct {
	effortCalls []string
}

func (b *effortStubBackend) Name() string                   { return "effort-stub" }
func (b *effortStubBackend) SupportsInteractiveInput() bool { return true }
func (b *effortStubBackend) Version() string                { return "" }
func (b *effortStubBackend) Launch(_ context.Context, _, _, _, _ string) error {
	return nil
}
func (b *effortStubBackend) SetEffort(e string) { b.effortCalls = append(b.effortCalls, e) }

// noEffortStubBackend is an llm.Backend WITHOUT SetEffort (stands in for
// non-claude backends like opencode / ollama).
type noEffortStubBackend struct{}

func (noEffortStubBackend) Name() string                   { return "no-effort-stub" }
func (noEffortStubBackend) SupportsInteractiveInput() bool { return true }
func (noEffortStubBackend) Version() string                { return "" }
func (noEffortStubBackend) Launch(_ context.Context, _, _, _, _ string) error {
	return nil
}

func startWithEffortOpts(t *testing.T, opt *StartOptions) *Session {
	t.Helper()
	mgr, fake := newTestManagerWithFake(t)
	opt.LaunchFn = func(_ context.Context, _, tmuxSession, _, _ string) error {
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	}
	sess, err := mgr.Start(context.Background(), "do the thing", "", t.TempDir(), opt)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	saved, ok := mgr.GetSession(sess.FullID)
	if !ok {
		t.Fatalf("session %q not saved", sess.FullID)
	}
	return saved
}

func TestClaudeEffort_ForwardedIndependentOfBL41Effort(t *testing.T) {
	stub := &effortStubBackend{}
	saved := startWithEffortOpts(t, &StartOptions{
		ClaudeEffort: "low",
		Effort:       "quick",
		BackendObj:   stub,
	})
	if len(stub.effortCalls) != 1 || stub.effortCalls[0] != "low" {
		t.Errorf("SetEffort calls = %v, want [low]", stub.effortCalls)
	}
	if saved.Effort != "quick" {
		t.Errorf("Session.Effort = %q, want %q (BL41 field must be independent of ClaudeEffort)", saved.Effort, "quick")
	}
}

func TestClaudeEffort_EmptyDoesNotCallSetEffort(t *testing.T) {
	stub := &effortStubBackend{}
	startWithEffortOpts(t, &StartOptions{
		Effort:     "thorough",
		BackendObj: stub,
	})
	if len(stub.effortCalls) != 0 {
		t.Errorf("SetEffort must not be called when ClaudeEffort is empty (even with BL41 Effort set); got %v", stub.effortCalls)
	}
}

func TestClaudeEffort_BackendWithoutSetEffortIsNoOp(t *testing.T) {
	saved := startWithEffortOpts(t, &StartOptions{
		ClaudeEffort: "high",
		BackendObj:   noEffortStubBackend{},
	})
	if saved == nil {
		t.Fatal("expected a saved session")
	}
}
