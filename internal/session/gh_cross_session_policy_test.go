// Cross-Session Communication Rule (operator policy, 2026-10-09) —
// steers spawned claude-code sessions toward datawatch's own audited
// memory/discussion/reply MCP tools instead of Claude Code's native
// cross-session messaging, for anything that should be audited or
// might cross a host/container boundary. Mirrors the existing
// Memory/RTK guardrails-injection pattern.

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func newGuardrailsTestSession(dataDir, projectDir string) (*Tracker, *Session, error) {
	sess := &Session{
		FullID:        "host-cccc",
		Task:          "some task",
		ProjectDir:    projectDir,
		Hostname:      "host",
		BackendFamily: "claude-code",
		CreatedAt:     time.Now(),
	}
	tr, err := NewTracker(dataDir, sess)
	return tr, sess, err
}

func TestWriteSessionGuardrails_CrossSessionPolicy_AppendedWhenEnabled(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := t.TempDir()

	// A CLAUDE.md must already exist for the merge path to run (merge
	// only happens when the target file is already present).
	tr0, sess0, err := newGuardrailsTestSession(dataDir, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr0.WriteSessionGuardrails("", sess0, GuardrailsOptions{}); err != nil {
		t.Fatal(err)
	}

	tr1, sess1, err := newGuardrailsTestSession(dataDir, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr1.WriteSessionGuardrails("", sess1, GuardrailsOptions{CrossSessionEnabled: true}); err != nil {
		t.Fatal(err)
	}

	claudeMD := filepath.Join(projectDir, "CLAUDE.md")
	body, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "Cross-Session Communication Rule") {
		t.Fatalf("expected the policy section in CLAUDE.md, got:\n%s", s)
	}
	for _, tool := range []string{"memory_discussion_write", "memory_discussion_recall", "discussion_subscribe", "reply_to_parent", "memory_handoff"} {
		if !strings.Contains(s, tool) {
			t.Errorf("expected policy to mention %q, got:\n%s", tool, s)
		}
	}
}

func TestWriteSessionGuardrails_CrossSessionPolicy_NotDuplicatedOnSecondWrite(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := t.TempDir()

	tr0, sess0, err := newGuardrailsTestSession(dataDir, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr0.WriteSessionGuardrails("", sess0, GuardrailsOptions{CrossSessionEnabled: true}); err != nil {
		t.Fatal(err)
	}

	// A second, later session in the same project_dir — the rule
	// should already be present and not get appended a second time.
	tr1, sess1, err := newGuardrailsTestSession(dataDir, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr1.WriteSessionGuardrails("", sess1, GuardrailsOptions{CrossSessionEnabled: true}); err != nil {
		t.Fatal(err)
	}

	claudeMD := filepath.Join(projectDir, "CLAUDE.md")
	body, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(body), "Cross-Session Communication Rule"); n != 1 {
		t.Fatalf("expected exactly 1 occurrence of the policy section, got %d:\n%s", n, body)
	}
}

func TestWriteSessionGuardrails_CrossSessionPolicy_NotAddedWhenDisabled(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := t.TempDir()

	tr0, sess0, err := newGuardrailsTestSession(dataDir, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr0.WriteSessionGuardrails("", sess0, GuardrailsOptions{}); err != nil {
		t.Fatal(err)
	}

	tr1, sess1, err := newGuardrailsTestSession(dataDir, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr1.WriteSessionGuardrails("", sess1, GuardrailsOptions{CrossSessionEnabled: false}); err != nil {
		t.Fatal(err)
	}

	claudeMD := filepath.Join(projectDir, "CLAUDE.md")
	body, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "Cross-Session Communication Rule") {
		t.Fatalf("policy should not be injected when CrossSessionEnabled is false, got:\n%s", body)
	}
}

func TestCrossSessionConfig_IsEnabled_DefaultsTrue(t *testing.T) {
	var c config.CrossSessionConfig
	if !c.IsEnabled() {
		t.Error("CrossSessionConfig with nil Enabled should default to true")
	}
	off := false
	c.Enabled = &off
	if c.IsEnabled() {
		t.Error("CrossSessionConfig with Enabled=false should return false")
	}
	on := true
	c.Enabled = &on
	if !c.IsEnabled() {
		t.Error("CrossSessionConfig with Enabled=true should return true")
	}
}
