package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// injCheckpointProject builds a project dir holding AGENT.md (rules file)
// and CLAUDE.md (configured as the single context file) and installs that
// policy for this test.
func injCheckpointProject(t *testing.T) string {
	t.Helper()
	installRulesContextConfig(t, config.AutonomousRulesContextConfig{ContextFiles: []string{"CLAUDE.md"}})
	dir := t.TempDir()
	injWrite(t, dir, "AGENT.md", injRulesBody)
	injWrite(t, dir, "CLAUDE.md", injRulesBody)
	return dir
}

func TestWorkerCheckpointRuleWorkerPrompt(t *testing.T) {
	dir := injCheckpointProject(t)
	m := &Manager{cfg: Config{}}

	got := m.withRulesContextBlock("task spec", dir, "claude-code")
	if !strings.HasPrefix(got, "task spec") {
		t.Fatalf("spec must come first: %q", got)
	}
	if !strings.Contains(got, "AGENT.md") || !strings.Contains(got, "CLAUDE.md") {
		t.Fatalf("worker prompt must name the rules files: %q", got)
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
}

func TestWorkerCheckpointRuleEmptyDirUnchanged(t *testing.T) {
	m := &Manager{cfg: Config{}}
	if got := m.withRulesContextBlock("task spec", "", "ollama"); got != "task spec" {
		t.Fatalf("empty project dir: spec must be unchanged, got %q", got)
	}
}

func TestWorkerCheckpointRuleNotInPlanningBlock(t *testing.T) {
	dir := injCheckpointProject(t)
	m := &Manager{cfg: Config{}}
	got, _, _ := m.rulesContextSuffixFor(&PRD{ProjectDir: dir}, "claude-code")
	if !strings.Contains(got, "Follow them when decomposing this PRD.") {
		t.Fatalf("planning block sanity: %q", got)
	}
	if strings.Contains(got, workerCheckpointRuleMarker) {
		t.Fatalf("planning block must not carry the worker rule: %q", got)
	}
}

func TestWorkerCheckpointRuleText(t *testing.T) {
	once := withWorkerCheckpointRule("task spec")
	if !strings.Contains(once, "CHECKPOINT.md") {
		t.Fatalf("rule must point workers at CHECKPOINT.md: %q", once)
	}
	if !strings.Contains(once, "progress note, write it to CHECKPOINT.md") {
		t.Fatalf("rule must name CHECKPOINT.md as the place for a worker note: %q", once)
	}
	if !strings.Contains(once, "written only by the human operator and by the supervisor") {
		t.Fatalf("rule must say checkpoints are written by the operator and supervisor only: %q", once)
	}
	if strings.Count(once, workerCheckpointRuleMarker) != 1 {
		t.Fatalf("exactly one marker: %q", once)
	}
}

func TestWorkerCheckpointRuleRepoClaudeMd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "CLAUDE.md"))
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("no CLAUDE.md at the repository root")
		}
		t.Fatal(err)
	}
	s := string(raw)
	const head = "\n## Checkpoints"
	start := strings.Index(s, head)
	if start < 0 {
		t.Fatalf("CLAUDE.md has no %q heading: %q", "## Checkpoints", s)
	}
	section := s[start+len(head):]
	if next := strings.Index(section, "\n## "); next >= 0 {
		section = section[:next]
	}
	if !strings.Contains(section, "human operator and by the supervisor only") {
		t.Fatalf("Checkpoints section must say entries are written by the operator and supervisor only: %q", section)
	}
	if !strings.Contains(section, "Automaton task workers must not") {
		t.Fatalf("Checkpoints section must carry the no-task-worker note: %q", section)
	}
	if !strings.Contains(section, "CHECKPOINT.md") {
		t.Fatalf("Checkpoints section must point workers at CHECKPOINT.md: %q", section)
	}
}
