package claudecode

import (
	"strings"
	"testing"
)

// TestClaudeExplicitModelBackend pins the backend half of the explicit-model
// contract: after SetModel, the CLI arguments contain --model <value>; with
// no model set, the CLI arguments contain no --model at all (the backend
// never invents one). The session manager's backstop refuses such a launch
// before it reaches the CLI.
func TestClaudeExplicitModelBackend(t *testing.T) {
	// No model set: the backend passes no --model and does not default to
	// any account model on its own.
	noModel := NewWithOptions("claude", false, false).(*Backend)
	if got := noModel.Model(); got != "" {
		t.Fatalf("fresh backend Model() = %q; want \"\"", got)
	}
	got := noModel.postFlagsStr()
	if strings.Contains(got, "--model") {
		t.Errorf("no-model backend CLI args must not contain --model; got: %s", got)
	}

	// After SetModel: the CLI arguments carry exactly the explicit model.
	b := NewWithOptions("claude", false, false).(*Backend)
	b.SetModel("sonnet")
	if got := b.Model(); got != "sonnet" {
		t.Fatalf("Model() after SetModel(\"sonnet\") = %q; want \"sonnet\"", got)
	}
	got = b.postFlagsStr()
	if !strings.Contains(got, "--model 'sonnet'") {
		t.Errorf("SetModel(\"sonnet\"): CLI args must contain --model 'sonnet'; got: %s", got)
	}

	// SetModel replaces a prior value rather than appending a second flag.
	b.SetModel("haiku")
	got = b.postFlagsStr()
	if !strings.Contains(got, "--model 'haiku'") {
		t.Errorf("SetModel(\"haiku\"): CLI args must contain --model 'haiku'; got: %s", got)
	}
	if strings.Contains(got, "--model 'sonnet'") {
		t.Errorf("SetModel(\"haiku\"): prior model must not remain in CLI args; got: %s", got)
	}
}
