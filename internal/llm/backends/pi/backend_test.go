package pi

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/llm"
)

// ---- PromptBackend (one-shot, --print) ----

func TestBuildLaunchCmd_NoModel(t *testing.T) {
	cmd := buildLaunchCmd("pi", "", "", "do the thing", "/home/dmz/work", "cs-abc")
	if !strings.Contains(cmd, "cd '/home/dmz/work'") {
		t.Errorf("missing cd into project dir, got: %s", cmd)
	}
	if !strings.Contains(cmd, "pi --print --mode text --approve") {
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
	cmd := buildLaunchCmd("pi", "ollama/qwen3.8:27b", "", "task text", "/proj", "cs-abc")
	if !strings.Contains(cmd, "--model 'ollama/qwen3.8:27b'") {
		t.Errorf("expected --model flag with the set model, got: %s", cmd)
	}
}

// TestBuildLaunchCmd_TaskGoesThroughScratchFile confirms the task text is
// delivered via a scratch file ($(cat '...')), not typed inline — typing a
// multi-KB task (the session manager wraps every task with checkpoint/
// completion framing) inline via tmux send-keys submits any embedded
// newline as a literal Enter keypress mid-command. Confirmed live
// 2026-10-16 ("These: command not found" mid-paste) before this fix.
func TestBuildLaunchCmd_TaskGoesThroughScratchFile(t *testing.T) {
	cmd := buildLaunchCmd("pi", "", "", "it's a test\nwith a newline", "/proj", "cs-abc")
	if !strings.Contains(cmd, `$(cat '`) {
		t.Errorf("expected task text delivered via a scratch file, got: %s", cmd)
	}
	if strings.Contains(cmd, "with a newline") {
		t.Errorf("task text must not appear inline in the command, got: %s", cmd)
	}
}

func TestBuildLaunchCmd_EscapesSingleQuotesInProjectDir(t *testing.T) {
	cmd := buildLaunchCmd("pi", "", "", "task", "/proj's/dir", "cs-abc")
	if !strings.Contains(cmd, `/proj'\''s/dir`) {
		t.Errorf("expected single quote in project dir to be shell-escaped, got: %s", cmd)
	}
}

func TestBuildLaunchCmd_EscapesSingleQuotesInModel(t *testing.T) {
	cmd := buildLaunchCmd("pi", "weird'model", "", "task", "/proj", "cs-abc")
	if !strings.Contains(cmd, `weird'\''model`) {
		t.Errorf("expected single quote in model to be shell-escaped, got: %s", cmd)
	}
}

func TestBuildLaunchCmd_EmptyTaskOmitsArg(t *testing.T) {
	cmd := buildLaunchCmd("pi", "", "", "", "/proj", "cs-abc")
	if strings.Contains(cmd, "$(cat") {
		t.Errorf("empty task must not reference a task file, got: %s", cmd)
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

// TestBuildLaunchCmd_ModelPassesThroughVerbatim pins the model string the
// verifier selection carries to the pi --model flag: no provider-prefix
// rewriting, no substitution with a default. A regression here is how a
// pi-prompt verifier ended up launching with an unrelated ollama model.
func TestBuildLaunchCmd_ModelPassesThroughVerbatim(t *testing.T) {
	const model = "ollama-datawatch/qwen3.8-64k:27b"
	cmd := buildLaunchCmd("pi", model, "", "some task", "work", "ses")
	want := "--model '" + model + "' "
	if !strings.Contains(cmd, want) {
		t.Errorf("expected exactly %q in command, got: %s", want, cmd)
	}
	if n := strings.Count(cmd, "--model"); n != 1 {
		t.Errorf("expected exactly one --model flag, got %d in: %s", n, cmd)
	}
}

// TestBuildLaunchCmd_EmptyModelOmitsModelFlag: an empty model means pi
// uses its own configured default — the intended manager fallback.
func TestBuildLaunchCmd_EmptyModelOmitsModelFlag(t *testing.T) {
	cmd := buildLaunchCmd("pi", "", "", "some task", "work", "ses")
	if strings.Contains(cmd, "--model") {
		t.Errorf("empty model must not add any --model flag, got: %s", cmd)
	}
}

// TestPromptBackend_LaunchCmdSendsSetModelToTmux drives the real Launch path
// with a fake tmux on PATH that records its argv, proving SetModel feeds
// the command Launch sends — and that a second SetModel call wins.
func TestPromptBackend_LaunchCmdSendsSetModelToTmux(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	fakeTmux := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nprintf '%s\\0' \"$@\" > '" + shellEscape(argvFile) + "'\n"
	if err := os.WriteFile(fakeTmux, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	launch := func(b *PromptBackend) string {
		t.Helper()
		if err := b.Launch(context.Background(), "", "cs-test", dir, ""); err != nil {
			t.Fatalf("Launch: %v", err)
		}
		raw, err := os.ReadFile(argvFile)
		if err != nil {
			t.Fatalf("read recorded tmux argv: %v", err)
		}
		args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
		return args[len(args)-2] // send-keys ... <cmd> Enter
	}

	b := NewPrompt("pi").(*PromptBackend)
	b.SetModel("prov/first:64k")
	if cmd := launch(b); !strings.Contains(cmd, "--model 'prov/first:64k'") {
		t.Errorf("Launch after SetModel(first) must carry the model, got: %s", cmd)
	}

	b.SetModel("prov/second:64k")
	if cmd := launch(b); !strings.Contains(cmd, "--model 'prov/second:64k'") || strings.Contains(cmd, "prov/first") {
		t.Errorf("second SetModel must win on the next Launch, got: %s", cmd)
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
	cmd := buildInteractiveCmd("pi", "", "", "", "do the thing", "/home/dmz/work", "cs-abc")
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
	cmd := buildInteractiveCmd("pi", "ollama/qwen3.8:27b", "", "johnnyjohnny-c905", "task text", "/proj", "cs-abc")
	if !strings.Contains(cmd, "--session-id 'johnnyjohnny-c905'") {
		t.Errorf("expected --session-id with the given session name, got: %s", cmd)
	}
	if !strings.Contains(cmd, "--model 'ollama/qwen3.8:27b'") {
		t.Errorf("expected --model flag with the set model, got: %s", cmd)
	}
}

func TestBuildInteractiveCmd_EmptyTaskOmitsArg(t *testing.T) {
	cmd := buildInteractiveCmd("pi", "", "", "", "", "/proj", "cs-abc")
	if strings.Contains(cmd, "$(cat") {
		t.Errorf("empty task must not reference a task file, got: %s", cmd)
	}
}

// TestBuildInteractiveCmd_ThinkingLevelInCmd — same guarantee in
// interactive mode: the flag appears exactly once when set, and is absent
// when empty.
func TestBuildInteractiveCmd_ThinkingLevelInCmd(t *testing.T) {
	cmd := buildInteractiveCmd("pi", "", "minimal", "", "task", "/proj", "cs-abc")
	if !strings.Contains(cmd, "--thinking minimal ") {
		t.Errorf("expected --thinking minimal in interactive command, got: %s", cmd)
	}
	if n := strings.Count(cmd, "--thinking"); n != 1 {
		t.Errorf("expected exactly one --thinking flag, got %d in: %s", n, cmd)
	}
	cmd = buildInteractiveCmd("pi", "", "", "", "task", "/proj", "cs-abc")
	if strings.Contains(cmd, "--thinking") {
		t.Errorf("empty thinking level must add no --thinking flag, got: %s", cmd)
	}
}

func TestBackend_SetThinkingLevelSatisfiesSessionManagerDuckType(t *testing.T) {
	var b llm.Backend = New("")
	if _, ok := b.(interface{ SetThinkingLevel(string) }); !ok {
		t.Error("Backend must satisfy interface{ SetThinkingLevel(string) } for the session manager's thinking-level path")
	}
	bb := New("").(*Backend)
	bb.SetThinkingLevel("low")
	if bb.thinkingLevel != "low" {
		t.Errorf("thinkingLevel after SetThinkingLevel = %q, want low", bb.thinkingLevel)
	}
	bb.SetThinkingLevel("")
	if bb.thinkingLevel != "" {
		t.Errorf("thinkingLevel after SetThinkingLevel(\"\") = %q, want empty", bb.thinkingLevel)
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

// TestLaunch_TmuxStderrSurfacesInError pins the error-visibibility
// guarantee: Launch/LaunchResume/PromptBackend.Launch all send the command
// via the shared llm.SendKeys helper, so a tmux failure carries tmux's own
// stderr text instead of a bare "exit status 1". Driven with a fake tmux on
// PATH that prints a distinct stderr message and exits nonzero — no real
// tmux server is started.
func TestLaunch_TmuxStderrSurfacesInError(t *testing.T) {
	dir := t.TempDir()
	const want = "FAKE-TMUX-STDERR-PI"
	fakeTmux := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\necho '" + want + "' >&2\nexit 3\n"
	if err := os.WriteFile(fakeTmux, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cases := []struct {
		name string
		fn   func() error
	}{
		{"Launch", func() error { return New("pi").Launch(context.Background(), "task", "cs-err", dir, "") }},
		{"LaunchResume", func() error {
			return New("pi").(*Backend).LaunchResume(context.Background(), "task", "cs-err", dir, "", "res-1")
		}},
		{"PromptLaunch", func() error { return NewPrompt("pi").Launch(context.Background(), "task", "cs-err", dir, "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil {
				t.Fatal("expected the failing fake tmux to produce an error")
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error must carry tmux stderr %q, got: %v", want, err)
			}
			// The "tmux send-keys" wrapper only exists in llm.SendKeys' error
			// path — this proves the call went through the shared helper
			// rather than a bare exec.Command("tmux", ...).
			if !strings.Contains(err.Error(), "tmux send-keys") {
				t.Errorf("error must be wrapped by the shared llm.SendKeys helper (\"tmux send-keys\"), got: %v", err)
			}
		})
	}
}

// TestLaunch_SendKeysLengthGuardApplies pins the second shared-helper
// guarantee at these call sites: commands longer than
// llm.MaxSendKeysCommand bytes are rejected up front with a clear error
// naming the limit, and nothing is exec'd at all — no fake tmux is placed
// on PATH in this test, so any stray exec would fail differently. No fake
// tmux server is ever involved.
func TestLaunch_SendKeysLengthGuardApplies(t *testing.T) {
	// Oversize via the project dir, which both the interactive and
	// one-shot command builders inline (`cd '...'`), even though the task
	// text itself may go through a scratch file.
	hugeDir := strings.Repeat("p", llm.MaxSendKeysCommand+1000)
	cases := []struct {
		name string
		fn   func() error
	}{
		{"Launch", func() error { return New("pi").Launch(context.Background(), "task", "cs-guard", hugeDir, "") }},
		{"LaunchResume", func() error {
			return New("pi").(*Backend).LaunchResume(context.Background(), "task", "cs-guard", hugeDir, "", "res-1")
		}},
		{"PromptLaunch", func() error { return NewPrompt("pi").Launch(context.Background(), "task", "cs-guard", hugeDir, "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil {
				t.Fatal("expected the shared length guard to reject the oversize command")
			}
			if !strings.Contains(err.Error(), "command too long") {
				t.Errorf("expected a \"command too long\" guard error, got: %v", err)
			}
			if !strings.Contains(err.Error(), strconv.Itoa(llm.MaxSendKeysCommand)) {
				t.Errorf("guard error must name the %d-byte limit, got: %v", llm.MaxSendKeysCommand, err)
			}
			if strings.Contains(err.Error(), "tmux send-keys") {
				t.Errorf("guard rejection happens before any exec, so no exec error should be present, got: %v", err)
			}
		})
	}
}

// Compile-time contract check: pi.Backend must satisfy llm.Backend.
var _ llm.Backend = New("")
