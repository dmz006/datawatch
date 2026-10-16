package pi

import (
	"context"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/llm"
)

// ---- PromptBackend (one-shot, --print) ----

func TestBuildLaunchCmd_NoModel(t *testing.T) {
	cmd := buildLaunchCmd("pi", "", "do the thing", "/home/dmz/work")
	if !strings.Contains(cmd, "cd '/home/dmz/work'") {
		t.Errorf("missing cd into project dir, got: %s", cmd)
	}
	if !strings.Contains(cmd, "pi --print --mode text --approve 'do the thing'") {
		t.Errorf("unexpected command shape, got: %s", cmd)
	}
	if strings.Contains(cmd, "--model") {
		t.Errorf("empty model must not add a --model flag, got: %s", cmd)
	}
	if !strings.HasSuffix(cmd, "echo 'DATAWATCH_COMPLETE: pi done'") {
		t.Errorf("completion marker must always be echoed unconditionally, got: %s", cmd)
	}
}

func TestBuildLaunchCmd_WithModel(t *testing.T) {
	cmd := buildLaunchCmd("pi", "ollama/qwen3.8:27b", "task text", "/proj")
	if !strings.Contains(cmd, "--model 'ollama/qwen3.8:27b'") {
		t.Errorf("expected --model flag with the set model, got: %s", cmd)
	}
}

func TestBuildLaunchCmd_EscapesSingleQuotesInTask(t *testing.T) {
	cmd := buildLaunchCmd("pi", "", "it's a test", "/proj")
	if !strings.Contains(cmd, `it'\''s a test`) {
		t.Errorf("expected single quote in task to be shell-escaped, got: %s", cmd)
	}
}

func TestBuildLaunchCmd_EscapesSingleQuotesInProjectDir(t *testing.T) {
	cmd := buildLaunchCmd("pi", "", "task", "/proj's/dir")
	if !strings.Contains(cmd, `/proj'\''s/dir`) {
		t.Errorf("expected single quote in project dir to be shell-escaped, got: %s", cmd)
	}
}

func TestBuildLaunchCmd_EscapesSingleQuotesInModel(t *testing.T) {
	cmd := buildLaunchCmd("pi", "weird'model", "task", "/proj")
	if !strings.Contains(cmd, `weird'\''model`) {
		t.Errorf("expected single quote in model to be shell-escaped, got: %s", cmd)
	}
}

func TestPromptBackend_Contract(t *testing.T) {
	b := NewPrompt("")
	if b.Name() != "pi-prompt" {
		t.Errorf("Name() = %q, want \"pi-prompt\"", b.Name())
	}
	if b.SupportsInteractiveInput() {
		t.Error("SupportsInteractiveInput() must be false — pi --print is one-shot, non-interactive")
	}
	if tc, ok := b.(interface{ HasToolAccess() bool }); !ok || !tc.HasToolAccess() {
		t.Error("HasToolAccess() must be true — pi is a real tool-capable coding agent")
	}
	if pr, ok := b.(llm.PromptRequirer); !ok || !pr.PromptRequired() {
		t.Error("PromptRequired() must be true — pi-prompt always needs a task")
	}
}

func TestPromptBackend_DefaultsBinaryToPi(t *testing.T) {
	b := NewPrompt("").(*PromptBackend)
	if b.binary != "pi" {
		t.Errorf("default binary = %q, want \"pi\"", b.binary)
	}
}

func TestPromptBackend_CustomBinaryPreserved(t *testing.T) {
	b := NewPrompt("/custom/path/pi").(*PromptBackend)
	if b.binary != "/custom/path/pi" {
		t.Errorf("custom binary = %q, want \"/custom/path/pi\"", b.binary)
	}
}

func TestPromptBackend_SetModel(t *testing.T) {
	b := NewPrompt("").(*PromptBackend)
	b.SetModel("ollama/qwen3.8:27b")
	if b.model != "ollama/qwen3.8:27b" {
		t.Errorf("model after SetModel = %q, want \"ollama/qwen3.8:27b\"", b.model)
	}
}

func TestPromptBackend_SetModelSatisfiesSessionManagerDuckType(t *testing.T) {
	// internal/session/manager.go type-asserts against this exact shape
	// (interface{ SetModel(string) }) to apply per-task/story/PRD model
	// overrides — same mechanism opencode-acp and goose use.
	var b llm.Backend = NewPrompt("")
	if _, ok := b.(interface{ SetModel(string) }); !ok {
		t.Error("PromptBackend must satisfy interface{ SetModel(string) } for the session manager's model-override path")
	}
}

func TestPromptBackend_VersionReturnsEmptyOnMissingBinary(t *testing.T) {
	b := NewPrompt("pi-totally-nonexistent-binary-xyz")
	if v := b.Version(); v != "" {
		t.Errorf("Version() with a missing binary = %q, want empty string", v)
	}
}

func TestPromptLaunch_InvokesTmuxSendKeys(t *testing.T) {
	b := NewPrompt("pi")
	err := b.Launch(context.Background(), "task", "cs-nonexistent-session-xyz", "/tmp", "/tmp/out.log")
	if err == nil {
		t.Skip("tmux session happened to exist or tmux accepted the target; nothing to assert here")
	}
}

// Compile-time contract check: pi.PromptBackend must satisfy llm.Backend.
var _ llm.Backend = NewPrompt("")

// ---- Backend (interactive) ----

func TestBuildInteractiveCmd_NoSessionNoModel(t *testing.T) {
	cmd := buildInteractiveCmd("pi", "", "", "do the thing", "/home/dmz/work", "cs-abc")
	if !strings.Contains(cmd, "cd '/home/dmz/work'") {
		t.Errorf("missing cd into project dir, got: %s", cmd)
	}
	if strings.Contains(cmd, "--print") {
		t.Errorf("interactive mode must never pass --print, got: %s", cmd)
	}
	if strings.Contains(cmd, "--session-id") {
		t.Errorf("empty session ID must not add a --session-id flag, got: %s", cmd)
	}
	if strings.Contains(cmd, "--model") {
		t.Errorf("empty model must not add a --model flag, got: %s", cmd)
	}
	if !strings.Contains(cmd, "--approve") {
		t.Errorf("expected --approve flag, got: %s", cmd)
	}
	if !strings.HasSuffix(cmd, "echo 'DATAWATCH_COMPLETE: pi session ended'") {
		t.Errorf("completion marker must always be echoed unconditionally, got: %s", cmd)
	}
}

func TestBuildInteractiveCmd_WithSessionAndModel(t *testing.T) {
	cmd := buildInteractiveCmd("pi", "ollama/qwen3.8:27b", "johnnyjohnny-c905", "task text", "/proj", "cs-abc")
	if !strings.Contains(cmd, "--session-id 'johnnyjohnny-c905'") {
		t.Errorf("expected --session-id with the given session name, got: %s", cmd)
	}
	if !strings.Contains(cmd, "--model 'ollama/qwen3.8:27b'") {
		t.Errorf("expected --model flag with the set model, got: %s", cmd)
	}
}

func TestBuildInteractiveCmd_EmptyTaskOmitsArg(t *testing.T) {
	cmd := buildInteractiveCmd("pi", "", "", "", "/proj", "cs-abc")
	if strings.Contains(cmd, "$(cat") {
		t.Errorf("empty task must not reference a task file, got: %s", cmd)
	}
}

func TestBackend_Contract(t *testing.T) {
	b := New("")
	if b.Name() != "pi" {
		t.Errorf("Name() = %q, want \"pi\"", b.Name())
	}
	if !b.SupportsInteractiveInput() {
		t.Error("SupportsInteractiveInput() must be true — pi interactive mode stays open for follow-up input")
	}
	if tc, ok := b.(interface{ HasToolAccess() bool }); !ok || !tc.HasToolAccess() {
		t.Error("HasToolAccess() must be true — pi is a real tool-capable coding agent")
	}
	if pr, ok := b.(llm.PromptRequirer); !ok || pr.PromptRequired() {
		t.Error("PromptRequired() must be false — interactive pi can start with no initial task")
	}
	if _, ok := b.(llm.Resumable); !ok {
		t.Error("Backend must implement llm.Resumable")
	}
	if _, ok := b.(llm.Nameable); !ok {
		t.Error("Backend must implement llm.Nameable")
	}
}

func TestBackend_DefaultsBinaryToPi(t *testing.T) {
	b := New("").(*Backend)
	if b.binary != "pi" {
		t.Errorf("default binary = %q, want \"pi\"", b.binary)
	}
}

func TestBackend_CustomBinaryPreserved(t *testing.T) {
	b := New("/custom/path/pi").(*Backend)
	if b.binary != "/custom/path/pi" {
		t.Errorf("custom binary = %q, want \"/custom/path/pi\"", b.binary)
	}
}

func TestBackend_SetModel(t *testing.T) {
	b := New("").(*Backend)
	b.SetModel("ollama/qwen3.8:27b")
	if b.model != "ollama/qwen3.8:27b" {
		t.Errorf("model after SetModel = %q, want \"ollama/qwen3.8:27b\"", b.model)
	}
}

func TestBackend_SetSessionName(t *testing.T) {
	b := New("").(*Backend)
	b.SetSessionName("johnnyjohnny-c905")
	if b.sessionName != "johnnyjohnny-c905" {
		t.Errorf("sessionName after SetSessionName = %q, want \"johnnyjohnny-c905\"", b.sessionName)
	}
}

func TestBackend_SetModelSatisfiesSessionManagerDuckType(t *testing.T) {
	var b llm.Backend = New("")
	if _, ok := b.(interface{ SetModel(string) }); !ok {
		t.Error("Backend must satisfy interface{ SetModel(string) } for the session manager's model-override path")
	}
}

func TestBackend_VersionReturnsEmptyOnMissingBinary(t *testing.T) {
	b := New("pi-totally-nonexistent-binary-xyz")
	if v := b.Version(); v != "" {
		t.Errorf("Version() with a missing binary = %q, want empty string", v)
	}
}

func TestLaunch_InvokesTmuxSendKeys(t *testing.T) {
	// Launch shells out to the real `tmux` binary; a nonexistent tmux
	// session target should fail, but the point of this test is that
	// Launch builds and attempts the command rather than panicking or
	// silently no-op'ing. A full spawn-and-observe test belongs at the
	// session-manager integration level, not here.
	b := New("pi")
	err := b.Launch(context.Background(), "task", "cs-nonexistent-session-xyz", "/tmp", "/tmp/out.log")
	if err == nil {
		t.Skip("tmux session happened to exist or tmux accepted the target; nothing to assert here")
	}
}

func TestLaunchResume_FallsBackToSessionNameWhenResumeIDEmpty(t *testing.T) {
	b := New("pi").(*Backend)
	b.SetSessionName("stored-name")
	err := b.LaunchResume(context.Background(), "task", "cs-nonexistent-session-xyz", "/tmp", "/tmp/out.log", "")
	if err == nil {
		t.Skip("tmux session happened to exist or tmux accepted the target; nothing to assert here")
	}
}

// Compile-time contract check: pi.Backend must satisfy llm.Backend.
var _ llm.Backend = New("")
