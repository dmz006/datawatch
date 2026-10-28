// Regression proof that StartOptions.ThinkingLevel (pi's --thinking
// level: off|minimal|low|medium|high) reaches the backend via
// SetThinkingLevel when set, is a no-op when empty (so the daemon's
// own pi.thinking_level config applies), and a no-op on backends that
// do not implement SetThinkingLevel (claude-code, opencode, ...).

package session

import (
	"context"
	"testing"
)

// thinkingStubBackend is a minimal llm.Backend that records
// SetThinkingLevel calls.
type thinkingStubBackend struct {
	thinkingCalls []string
}

func (b *thinkingStubBackend) Name() string                   { return "thinking-stub" }
func (b *thinkingStubBackend) SupportsInteractiveInput() bool { return true }
func (b *thinkingStubBackend) Version() string                { return "" }
func (b *thinkingStubBackend) Launch(_ context.Context, _, _, _, _ string) error {
	return nil
}
func (b *thinkingStubBackend) SetThinkingLevel(l string) {
	b.thinkingCalls = append(b.thinkingCalls, l)
}

// noThinkingStubBackend is an llm.Backend WITHOUT SetThinkingLevel
// (stands in for non-pi backends like opencode / ollama).
type noThinkingStubBackend struct{}

func (noThinkingStubBackend) Name() string                   { return "no-thinking-stub" }
func (noThinkingStubBackend) SupportsInteractiveInput() bool { return true }
func (noThinkingStubBackend) Version() string                { return "" }
func (noThinkingStubBackend) Launch(_ context.Context, _, _, _, _ string) error {
	return nil
}

func startWithThinkingOpts(t *testing.T, opt *StartOptions) {
	t.Helper()
	mgr, fake := newTestManagerWithFake(t)
	opt.LaunchFn = func(_ context.Context, _, tmuxSession, _, _ string) error {
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	}
	if _, err := mgr.Start(context.Background(), "do the thing", "", t.TempDir(), opt); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestThinkingLevel_ForwardedToBackend(t *testing.T) {
	stub := &thinkingStubBackend{}
	startWithThinkingOpts(t, &StartOptions{
		ThinkingLevel: "medium",
		BackendObj:    stub,
	})
	if len(stub.thinkingCalls) != 1 || stub.thinkingCalls[0] != "medium" {
		t.Errorf("SetThinkingLevel calls = %v, want [medium]", stub.thinkingCalls)
	}
}

func TestThinkingLevel_EmptyDoesNotCallSetThinkingLevel(t *testing.T) {
	stub := &thinkingStubBackend{}
	startWithThinkingOpts(t, &StartOptions{
		BackendObj: stub,
	})
	if len(stub.thinkingCalls) != 0 {
		t.Errorf("SetThinkingLevel must not be called when ThinkingLevel is empty (the daemon's pi.thinking_level config applies); got %v", stub.thinkingCalls)
	}
}

func TestThinkingLevel_BackendWithoutSetThinkingLevelIsNoOp(t *testing.T) {
	// Must not panic: the apply goes through a type assertion that
	// silently skips backends without the method.
	startWithThinkingOpts(t, &StartOptions{
		ThinkingLevel: "high",
		BackendObj:    noThinkingStubBackend{},
	})
}
