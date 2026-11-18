package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// claudeModelStubBackend models the claude-code backend from the manager's
// point of view: it reports the claude-code family name, accepts SetModel,
// and its Launch assembles the same --model CLI flag the real backend does
// (internal/llm/claudecode), recording the command so tests can assert the
// exact flag that would reach the CLI.
type claudeModelStubBackend struct {
	mu         sync.Mutex
	model      string
	modelCalls []string
	commands   []string
}

func (b *claudeModelStubBackend) Name() string { return "claude-code" }

func (b *claudeModelStubBackend) SetModel(model string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.model = model
	b.modelCalls = append(b.modelCalls, model)
}

func (b *claudeModelStubBackend) Launch(_ context.Context, task, tmuxSession, projectDir, _ string) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "cd %s && claude --add-dir '%s' --name dw-test", projectDir, projectDir)
	b.mu.Lock()
	model := b.model
	b.mu.Unlock()
	if model != "" {
		fmt.Fprintf(&sb, " --model '%s'", model)
	}
	fmt.Fprintf(&sb, " %s", task)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.commands = append(b.commands, sb.String())
	return nil
}

func (b *claudeModelStubBackend) SupportsInteractiveInput() bool { return true }
func (b *claudeModelStubBackend) Version() string                { return "stub" }

func (b *claudeModelStubBackend) modelCallCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.modelCalls)
}

func (b *claudeModelStubBackend) lastCommand() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.commands) == 0 {
		return ""
	}
	return b.commands[len(b.commands)-1]
}

// TestClaudeExplicitModelLaunchRefusal pins the manager backstop: a
// claude-code launch with no explicit model is refused up front (no CLI
// launch, no session record, no daemon fallback to the account default),
// while a launch that names a model succeeds and the persisted session
// record carries exactly the model passed to the CLI as --model.
func TestClaudeExplicitModelLaunchRefusal(t *testing.T) {
	t.Run("refused when no model is set", func(t *testing.T) {
		mgr, fake := newTestManagerWithFake(t)
		stub := &claudeModelStubBackend{}

		sess, err := mgr.Start(context.Background(), "model-less task", "g1", t.TempDir(), &StartOptions{
			LaunchFn:   stub.Launch,
			BackendObj: stub,
		})
		if sess != nil {
			t.Fatalf("Start without a model must return no session; got %+v", sess)
		}
		if err == nil {
			t.Fatal("Start without a model must be refused; got nil error")
		}
		// Plain refusal: names the account default it refuses to fall back
		// to and suggests a model.
		for _, want := range []string{"refused", "account default", "sonnet", "haiku"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal error must mention %q; got: %v", want, err)
			}
		}

		// No CLI launch happened: the backend was never called and no tmux
		// traffic at all (no pane, no send-keys).
		if stub.modelCallCount() != 0 {
			t.Errorf("refused launch must not call SetModel; got %d calls", stub.modelCallCount())
		}
		if got := stub.lastCommand(); got != "" {
			t.Errorf("refused launch must not invoke the CLI; captured: %q", got)
		}
		if n := len(fake.Calls); n != 0 {
			t.Errorf("refused launch must not touch tmux; got %d calls: %+v", n, fake.Calls)
		}
		// And no session record was persisted.
		if got := mgr.ListSessions(); len(got) != 0 {
			t.Errorf("refused launch must not persist a session record; got %d", len(got))
		}
	})

	t.Run("allowed when a model is named", func(t *testing.T) {
		mgr, _ := newTestManagerWithFake(t)
		stub := &claudeModelStubBackend{}

		sess, err := mgr.Start(context.Background(), "modelled task", "g1", t.TempDir(), &StartOptions{
			LaunchFn:   stub.Launch,
			BackendObj: stub,
			Model:      "sonnet",
		})
		if err != nil {
			t.Fatalf("Start with Model=sonnet: %v", err)
		}
		if sess == nil {
			t.Fatal("Start with Model=sonnet returned no session")
		}

		// The backend got exactly the model the CLI received.
		if n := stub.modelCallCount(); n != 1 {
			t.Fatalf("SetModel call count = %d; want 1", n)
		}
		cmd := stub.lastCommand()
		if cmd == "" {
			t.Fatal("expected one captured CLI command")
		}
		if !strings.Contains(cmd, "--model 'sonnet'") {
			t.Errorf("captured CLI command must contain --model 'sonnet'; got: %s", cmd)
		}

		// The persisted session record matches the --model value.
		if sess.Model != "sonnet" {
			t.Errorf("session record Model = %q; want \"sonnet\" (must equal the CLI --model)", sess.Model)
		}
	})
}
