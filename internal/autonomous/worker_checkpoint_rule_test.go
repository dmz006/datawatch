package autonomous

import (
	"strings"
	"testing"
)

func TestWithWorkerCheckpointRuleAppendsOnce(t *testing.T) {
	once := withWorkerCheckpointRule("task spec")
	if !strings.HasPrefix(once, "task spec") {
		t.Fatalf("spec must come first: %q", once)
	}
	if !strings.Contains(once, workerCheckpointRuleMarker) {
		t.Fatalf("marker line missing: %q", once)
	}
	if !strings.Contains(once, "CHECKPOINT.md") {
		t.Fatalf("rule must point workers at CHECKPOINT.md: %q", once)
	}
	if !strings.Contains(once, "human operator and by the supervisor") {
		t.Fatalf("rule must name operator and supervisor as the only writers: %q", once)
	}
	again := withWorkerCheckpointRule(once)
	if again != once {
		t.Fatalf("rule must not be appended twice: %q", again)
	}
	if strings.Count(once, workerCheckpointRuleMarker) != 1 {
		t.Fatalf("exactly one marker: %q", once)
	}
}

func TestWorkerCheckpointRuleOnWorkerPrompt(t *testing.T) {
	dir := injProject(t)
	injMode(t, "", 0)
	m := &Manager{cfg: Config{}}

	got := m.withRulesContextBlock("task spec", dir, "claude-code")
	if !strings.Contains(got, "AGENT.md and CONTEXT.md") {
		t.Fatalf("worker prompt must still carry the rules pointer: %q", got)
	}
	if !strings.Contains(got, workerCheckpointRuleMarker) {
		t.Fatalf("worker prompt must carry the no-checkpoints rule: %q", got)
	}

	// A prompt that already holds the rules block (a retried task) gets
	// the rule exactly once as well, never a second rules block.
	again := m.withRulesContextBlock(got, dir, "claude-code")
	if again != got {
		t.Fatalf("retried prompt must be unchanged: %q", again)
	}
	if strings.Count(got, rulesBlockMarker) != 1 {
		t.Fatalf("exactly one rules block: %q", got)
	}
	if strings.Count(got, workerCheckpointRuleMarker) != 1 {
		t.Fatalf("exactly one no-checkpoints rule: %q", got)
	}

	// Empty dir keeps the existing early return: no block, no rule.
	if got := m.withRulesContextBlock("task spec", "", "ollama"); got != "task spec" {
		t.Fatalf("empty project dir: omitted, got %q", got)
	}
}

func TestWorkerCheckpointRuleNotInPlanningBlock(t *testing.T) {
	dir := injProject(t)
	injMode(t, "", 0)
	m := &Manager{cfg: Config{}}
	got, _, _ := m.rulesContextSuffixFor(&PRD{ProjectDir: dir}, "claude-code")
	if !strings.Contains(got, "Follow them when decomposing this PRD.") {
		t.Fatalf("planning block sanity: %q", got)
	}
	if strings.Contains(got, workerCheckpointRuleMarker) {
		t.Fatalf("planning block must not carry the worker rule: %q", got)
	}
}
