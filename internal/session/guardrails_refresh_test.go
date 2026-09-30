// Operator-reported: a task-execution session's project directory already
// had a CLAUDE.md written by an earlier (unrelated-in-content) session in
// the same project_dir — a decompose session. Claude Code itself noticed
// the "# Session Guardrails" header still described the decompose job and
// stopped mid-task to ask which assignment was real, because
// WriteSessionGuardrails only merged in missing memory/RTK sections when
// the file already existed and never refreshed the header for the current
// session. This covers the fix.

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteSessionGuardrails_RefreshesStaleHeader_WhenProjectFileAlreadyExists(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := t.TempDir()

	first := &Session{
		FullID:        "host-aaaa",
		Task:          "You are decomposing a feature request into a structured PRD.",
		ProjectDir:    projectDir,
		Hostname:      "host",
		BackendFamily: "claude-code",
		CreatedAt:     time.Now(),
	}
	tr1, err := NewTracker(dataDir, first)
	if err != nil {
		t.Fatalf("NewTracker (first): %v", err)
	}
	if err := tr1.WriteSessionGuardrails("", first); err != nil {
		t.Fatalf("WriteSessionGuardrails (first): %v", err)
	}

	claudeMD := filepath.Join(projectDir, "CLAUDE.md")
	before, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read CLAUDE.md after first session: %v", err)
	}
	if !strings.Contains(string(before), "host-aaaa") || !strings.Contains(string(before), "decomposing a feature request") {
		t.Fatalf("expected first session's guardrails in CLAUDE.md, got:\n%s", before)
	}

	// A second, unrelated task session reuses the same project_dir — the
	// real-world case: a decompose session followed later by a task-
	// execution session for a story the decompose produced.
	second := &Session{
		FullID:        "host-bbbb",
		Task:          "Write current-state inventory of datawatch LLM usage and hardware",
		ProjectDir:    projectDir,
		Hostname:      "host",
		BackendFamily: "claude-code",
		CreatedAt:     time.Now(),
	}
	tr2, err := NewTracker(dataDir, second)
	if err != nil {
		t.Fatalf("NewTracker (second): %v", err)
	}
	if err := tr2.WriteSessionGuardrails("", second); err != nil {
		t.Fatalf("WriteSessionGuardrails (second): %v", err)
	}

	after, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read CLAUDE.md after second session: %v", err)
	}
	afterStr := string(after)
	if strings.Contains(afterStr, "host-aaaa") || strings.Contains(afterStr, "decomposing a feature request") {
		t.Fatalf("stale first-session guardrails still present after second session wrote CLAUDE.md:\n%s", afterStr)
	}
	if !strings.Contains(afterStr, "host-bbbb") || !strings.Contains(afterStr, "Write current-state inventory") {
		t.Fatalf("expected second session's own guardrails in CLAUDE.md, got:\n%s", afterStr)
	}
}
