package pi

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/testhome"
)

// ---- PromptBackend (one-shot, --print) ----

func TestBuildLaunchCmd_NoModel(t *testing.T) {
	// Since v9.10.19 the pi-family backends no longer fall back to the
	// host's built-in default model: an empty model is a build error.
	_, err := buildLaunchCmd("pi", "", "do the thing", "/home/user/work", "cs-abc")
	if !errors.Is(err, ErrNoModel) {
		t.Fatalf("empty model must be an error wrapping ErrNoModel, got: %v", err)
	}
}

func TestBuildLaunchCmd_WhitespaceModelRefused(t *testing.T) {
	if _, err := buildLaunchCmd("pi", "   ", "task", "/proj", "cs-abc"); !errors.Is(err, ErrNoModel) {
		t.Fatalf("whitespace-only model must be an ErrNoModel error, got: %v", err)
	}
}

func TestBuildLaunchCmd_WithModel(t *testing.T) {
	testhome.PrivateTmp(t) // the scratch task file is removed with the test's temp dir
	cmd, err := buildLaunchCmd("pi", "ollama/qwen3.8:27b", "task text", "/proj", "cs-abc")
	if err != nil {
		t.Fatalf("buildLaunchCmd with a model: %v", err)
	}
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
	testhome.PrivateTmp(t) // the scratch task file is removed with the test's temp dir
	cmd, err := buildLaunchCmd("pi", "prov/model-a", "it's a test\nwith a newline", "/proj", "cs-abc")
	if err != nil {
		t.Fatalf("buildLaunchCmd: %v", err)
	}
	if !strings.Contains(cmd, `$(cat '`) {
		t.Errorf("expected task text delivered via a scratch file, got: %s", cmd)
	}
	if strings.Contains(cmd, "with a newline") {
		t.Errorf("task text must not appear inline in the command, got: %s", cmd)
	}
}

func TestBuildLaunchCmd_EscapesSingleQuotesInProjectDir(t *testing.T) {
	testhome.PrivateTmp(t) // the scratch task file is removed with the test's temp dir
	cmd, err := buildLaunchCmd("pi", "prov/model-a", "task", "/proj's/dir", "cs-abc")
	if err != nil {
		t.Fatalf("buildLaunchCmd: %v", err)
	}
	if !strings.Contains(cmd, `/proj'\''s/dir`) {
		t.Errorf("expected single quote in project dir to be shell-escaped, got: %s", cmd)
	}
}

func TestBuildLaunchCmd_EscapesSingleQuotesInModel(t *testing.T) {
	testhome.PrivateTmp(t) // the scratch task file is removed with the test's temp dir
	cmd, err := buildLaunchCmd("pi", "weird'model", "task", "/proj", "cs-abc")
	if err != nil {
		t.Fatalf("buildLaunchCmd: %v", err)
	}
	if !strings.Contains(cmd, `weird'\''model`) {
		t.Errorf("expected single quote in model to be shell-escaped, got: %s", cmd)
	}
}

func TestBuildLaunchCmd_EmptyTaskOmitsArg(t *testing.T) {
	testhome.PrivateTmp(t) // the out/status scratch files are removed with the test's temp dir
	cmd, err := buildLaunchCmd("pi", "prov/model-a", "", "/proj", "cs-abc")
	if err != nil {
		t.Fatalf("buildLaunchCmd: %v", err)
	}
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
	testhome.PrivateTmp(t) // the scratch task file is removed with the test's temp dir
	const model = "ollama-datawatch/qwen3.8-64k:27b"
	cmd, err := buildLaunchCmd("pi", model, "some task", "work", "ses")
	if err != nil {
		t.Fatalf("buildLaunchCmd: %v", err)
	}
	want := "--model '" + model + "' "
	if !strings.Contains(cmd, want) {
		t.Errorf("expected exactly %q in command, got: %s", want, cmd)
	}
	if n := strings.Count(cmd, "--model"); n != 1 {
		t.Errorf("expected exactly one --model flag, got %d in: %s", n, cmd)
	}
}

// TestBuildLaunchCmd_EmptyModelRefused: since v9.10.19 an empty model is a
// hard error — silently running on the pi harness's built-in default model
// is exactly the fallback the datawatch launch paths must not take.
func TestBuildLaunchCmd_EmptyModelRefused(t *testing.T) {
	_, err := buildLaunchCmd("pi", "", "some task", "work", "ses")
	if !errors.Is(err, ErrNoModel) {
		t.Fatalf("expected ErrNoModel for an empty model, got: %v", err)
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
	testhome.PrivateTmp(t) // the out/status scratch files of each Launch are removed with the test's temp dir

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
	b := NewPrompt("")
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
	testhome.PrivateTmp(t) // the scratch task file is removed with the test's temp dir
	b := NewPrompt("pi").(*PromptBackend)
	b.SetModel("prov/model-a")
	err := b.Launch(context.Background(), "task", "cs-nonexistent-session-xyz", "/tmp", "/tmp/out.log")
	if err == nil {
		t.Skip("tmux session happened to exist or tmux accepted the target; nothing to assert here")
	}
}

// Compile-time contract check: pi.PromptBackend must satisfy llm.Backend.
var _ llm.Backend = NewPrompt("")

// ---- Backend (interactive) ----

func TestBuildInteractiveCmd_NoSessionWithModel(t *testing.T) {
	// Since v9.10.19 model is required; an empty session ID still must not
	// add a --session-id flag.
	testhome.PrivateTmp(t) // the scratch task file is removed with the test's temp dir
	cmd, err := buildInteractiveCmd("pi", "prov/model-a", "", "do the thing", "/home/user/work", "cs-abc")
	if err != nil {
		t.Fatalf("buildInteractiveCmd with model: %v", err)
	}
	if !strings.Contains(cmd, "cd '/home/user/work'") {
		t.Errorf("missing cd into project dir, got: %s", cmd)
	}
	if strings.Contains(cmd, "--print") {
		t.Errorf("interactive mode must never pass --print, got: %s", cmd)
	}
	if strings.Contains(cmd, "--session-id") {
		t.Errorf("empty session ID must not add a --session-id flag, got: %s", cmd)
	}
	if !strings.Contains(cmd, "--model 'prov/model-a'") {
		t.Errorf("expected the --model flag, got: %s", cmd)
	}
	if !strings.Contains(cmd, "--approve") {
		t.Errorf("expected --approve flag, got: %s", cmd)
	}
	// BL468 — the completion marker is gated on pi's zero exit status; the
	// command captures pi's status, gates the marker on it, and propagates it.
	if !strings.Contains(cmd, "echo 'DATAWATCH_COMPLETE: pi session ended'") {
		t.Errorf("expected the completion marker in the command, got: %s", cmd)
	}
	if !strings.Contains(cmd, `[ "$pi_status" -eq 0 ]`) {
		t.Errorf("completion marker must be gated on pi's zero exit status, got: %s", cmd)
	}
	if !strings.HasSuffix(cmd, `exit "$pi_status"`) {
		t.Errorf("command must exit with pi's own status, got: %s", cmd)
	}
}

func TestBuildInteractiveCmd_EmptyModelRefused(t *testing.T) {
	// Since v9.10.19 an empty model is a hard error — silently running on
	// the pi harness's built-in default model is exactly the fallback the
	// datawatch launch paths must not take.
	_, err := buildInteractiveCmd("pi", "", "", "do the thing", "/home/user/work", "cs-abc")
	if !errors.Is(err, ErrNoModel) {
		t.Fatalf("empty model must be an ErrNoModel error, got: %v", err)
	}
}

func TestBuildInteractiveCmd_WithSessionAndModel(t *testing.T) {
	testhome.PrivateTmp(t) // the scratch task file is removed with the test's temp dir
	cmd, err := buildInteractiveCmd("pi", "ollama/qwen3.8:27b", "local-node-c905", "task text", "/proj", "cs-abc")
	if err != nil {
		t.Fatalf("buildInteractiveCmd: %v", err)
	}
	if !strings.Contains(cmd, "--session-id 'local-node-c905'") {
		t.Errorf("expected --session-id with the given session name, got: %s", cmd)
	}
	if !strings.Contains(cmd, "--model 'ollama/qwen3.8:27b'") {
		t.Errorf("expected --model flag with the set model, got: %s", cmd)
	}
}

func TestBuildInteractiveCmd_EmptyTaskOmitsArg(t *testing.T) {
	cmd, err := buildInteractiveCmd("pi", "prov/model-a", "", "", "/proj", "cs-abc")
	if err != nil {
		t.Fatalf("buildInteractiveCmd: %v", err)
	}
	if strings.Contains(cmd, "$(cat") {
		t.Errorf("empty task must not reference a task file, got: %s", cmd)
	}
}

// TestBuildInteractiveCmd_ThinkingLevelInCmd — same guarantee in
// interactive mode: the flag appears exactly once when set, and is absent
// when empty.
func TestBuildInteractiveCmd_ThinkingLevelInCmd(t *testing.T) {
	testhome.PrivateTmp(t) // the scratch task file is removed with the test's temp dir
	cmd, err := buildInteractiveCmdWithAgentDir("pi", "prov/model-a", "minimal", "", "task", "/proj", "cs-abc", "")
	if err != nil {
		t.Fatalf("buildInteractiveCmdWithAgentDir: %v", err)
	}
	if !strings.Contains(cmd, "--thinking minimal ") {
		t.Errorf("expected --thinking minimal in interactive command, got: %s", cmd)
	}
	if n := strings.Count(cmd, "--thinking"); n != 1 {
		t.Errorf("expected exactly one --thinking flag, got %d in: %s", n, cmd)
	}
	cmd, err = buildInteractiveCmdWithAgentDir("pi", "prov/model-a", "", "", "task", "/proj", "cs-abc", "")
	if err != nil {
		t.Fatalf("buildInteractiveCmdWithAgentDir: %v", err)
	}
	if strings.Contains(cmd, "--thinking") {
		t.Errorf("empty thinking level must add no --thinking flag, got: %s", cmd)
	}
}

func TestBackend_SetThinkingLevelSatisfiesSessionManagerDuckType(t *testing.T) {
	b := New("")
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
	b.SetSessionName("local-node-c905")
	if b.sessionName != "local-node-c905" {
		t.Errorf("sessionName after SetSessionName = %q, want \"local-node-c905\"", b.sessionName)
	}
}

func TestBackend_SetModelSatisfiesSessionManagerDuckType(t *testing.T) {
	b := New("")
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
	b := New("pi").(*Backend)
	b.SetModel("prov/model-a")
	testhome.PrivateTmp(t) // the scratch task file is removed with the test's temp dir
	err := b.Launch(context.Background(), "task", "cs-nonexistent-session-xyz", "/tmp", "/tmp/out.log")
	if err == nil {
		t.Skip("tmux session happened to exist or tmux accepted the target; nothing to assert here")
	}
}

func TestLaunchResume_FallsBackToSessionNameWhenResumeIDEmpty(t *testing.T) {
	b := New("pi").(*Backend)
	b.SetSessionName("stored-name")
	b.SetModel("prov/model-a")
	testhome.PrivateTmp(t) // the scratch task file is removed with the test's temp dir
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
	testhome.PrivateTmp(t) // scratch task files are removed with the test's temp dir
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
		{"Launch", func() error {
			b := New("pi").(*Backend)
			b.SetModel("prov/model-a")
			return b.Launch(context.Background(), "task", "cs-err", dir, "")
		}},
		{"LaunchResume", func() error {
			b := New("pi").(*Backend)
			b.SetModel("prov/model-a")
			return b.LaunchResume(context.Background(), "task", "cs-err", dir, "", "res-1")
		}},
		{"PromptLaunch", func() error {
			b := NewPrompt("pi").(*PromptBackend)
			b.SetModel("prov/model-a")
			return b.Launch(context.Background(), "task", "cs-err", dir, "")
		}},
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
	testhome.PrivateTmp(t) // scratch task files are removed with the test's temp dir
	// Oversize via the project dir, which both the interactive and
	// one-shot command builders inline (`cd '...'`), even though the task
	// text itself may go through a scratch file.
	hugeDir := strings.Repeat("p", llm.MaxSendKeysCommand+1000)
	cases := []struct {
		name string
		fn   func() error
	}{
		{"Launch", func() error {
			b := New("pi").(*Backend)
			b.SetModel("prov/model-a")
			return b.Launch(context.Background(), "task", "cs-guard", hugeDir, "")
		}},
		{"LaunchResume", func() error {
			b := New("pi").(*Backend)
			b.SetModel("prov/model-a")
			return b.LaunchResume(context.Background(), "task", "cs-guard", hugeDir, "", "res-1")
		}},
		{"PromptLaunch", func() error {
			b := NewPrompt("pi").(*PromptBackend)
			b.SetModel("prov/model-a")
			return b.Launch(context.Background(), "task", "cs-guard", hugeDir, "")
		}},
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

// runLaunchCmdWithStubPi runs the command buildLaunchCmd generates under sh,
// with a stub pi script (body is the shell code after the shebang) passed as
// the binary. No tmux is involved: the command is executed directly.
func runLaunchCmdWithStubPi(t *testing.T, stubBody, model string) (string, error) {
	t.Helper()
	// Since v9.10.19 buildLaunchCmd refuses an empty model outright; these
	// scenario tests exercise the command's failure/success reporting, so a
	// model must be supplied (the rejection path itself is covered by the
	// ErrNoModel tests above).
	if strings.TrimSpace(model) == "" {
		t.Fatal("runLaunchCmdWithStubPi: model must be non-empty")
	}
	testhome.PrivateTmp(t) // the task/out/status scratch files are removed with the test's temp dir
	dir := t.TempDir()
	stub := filepath.Join(dir, "pi-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"+stubBody), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write stub pi: %v", err)
	}
	cmd, err := buildLaunchCmd(stub, model, "do the thing", dir, "cs-loud-test")
	if err != nil {
		t.Fatalf("buildLaunchCmd: %v", err)
	}
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	return string(out), err
}

// exitCodeOf returns the process exit code from a finished exec, or 0 when
// the command exited cleanly.
func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected an exit status, got: %v", err)
	}
	return exitErr.ExitCode()
}

// TestPiLoudFailureNonZeroExitReportsFailure: pi exits 3 with no output. The
// command must report DATAWATCH_PI_FAILED, must not report success, and must
// exit non-zero with pi's own status.
func TestPiLoudFailureNonZeroExitReportsFailure(t *testing.T) {
	out, err := runLaunchCmdWithStubPi(t, "exit 3\n", "prov/model-a")
	if !strings.Contains(out, "DATAWATCH_PI_FAILED") {
		t.Errorf("expected DATAWATCH_PI_FAILED in output, got: %q", out)
	}
	if strings.Contains(out, "DATAWATCH_COMPLETE") {
		t.Errorf("failure path must not print DATAWATCH_COMPLETE, got: %q", out)
	}
	if code := exitCodeOf(t, err); code != 3 {
		t.Errorf("exit status = %d, want 3 (pi's own status)", code)
	}
}

// TestPiLoudFailureSuccessWithOutputReportsComplete: pi prints a result and
// exits 0. The success marker is printed and no failure line appears.
func TestPiLoudFailureSuccessWithOutputReportsComplete(t *testing.T) {
	out, err := runLaunchCmdWithStubPi(t, "echo 'the result'\nexit 0\n", "prov/model-a")
	if err != nil {
		t.Fatalf("expected a clean run, got: %v (output %q)", err, out)
	}
	if !strings.Contains(out, "DATAWATCH_COMPLETE: pi done") {
		t.Errorf("expected DATAWATCH_COMPLETE on a clean run with output, got: %q", out)
	}
	if strings.Contains(out, "DATAWATCH_PI_FAILED") {
		t.Errorf("clean run must not print DATAWATCH_PI_FAILED, got: %q", out)
	}
}

// TestPiLoudFailureZeroExitNoOutputReportsFailure: pi exits 0 but prints
// nothing. This is a silent failure, so the command must report that pi
// produced no output and exit non-zero without the success marker.
func TestPiLoudFailureZeroExitNoOutputReportsFailure(t *testing.T) {
	out, err := runLaunchCmdWithStubPi(t, "exit 0\n", "prov/model-a")
	if !strings.Contains(out, "DATAWATCH_PI_FAILED") || !strings.Contains(out, "no output") {
		t.Errorf("expected a DATAWATCH_PI_FAILED no-output line, got: %q", out)
	}
	if strings.Contains(out, "DATAWATCH_COMPLETE") {
		t.Errorf("no-output path must not print DATAWATCH_COMPLETE, got: %q", out)
	}
	if code := exitCodeOf(t, err); code == 0 {
		t.Errorf("no-output path must exit non-zero, got exit 0 (output %q)", out)
	}
}

// TestPiLoudFailureFailureLineNamesModel: a non-zero exit names the model in
// the failure line, so the operator can see which model failed.
func TestPiLoudFailureFailureLineNamesModel(t *testing.T) {
	const model = "ollama/qwen3.8:27b"
	out, _ := runLaunchCmdWithStubPi(t, "exit 5\n", model)
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "DATAWATCH_PI_FAILED:") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("expected a line starting DATAWATCH_PI_FAILED:, got: %q", out)
	}
	if !strings.Contains(line, model) {
		t.Errorf("failure line must name model %q, got: %q", model, line)
	}
	if !strings.Contains(line, "5") {
		t.Errorf("failure line must include the exit status, got: %q", line)
	}
}

// Compile-time contract check: pi.Backend must satisfy llm.Backend.
var _ llm.Backend = New("")

// TestBuildLaunchCmd_ThinkingLevelInCmd: the one-shot command carries pi's
// --thinking flag exactly once when a level is set and none when it is empty.
func TestBuildLaunchCmd_ThinkingLevelInCmd(t *testing.T) {
	testhome.PrivateTmp(t) // the scratch task file is removed with the test's temp dir
	cmd, err := buildLaunchCmdWithAgentDir("pi", "prov/model-a", "high", "task", "/proj", "cs-abc", "")
	if err != nil {
		t.Fatalf("buildLaunchCmdWithAgentDir: %v", err)
	}
	if !strings.Contains(cmd, "--thinking high ") {
		t.Errorf("expected --thinking high in one-shot command, got: %s", cmd)
	}
	if n := strings.Count(cmd, "--thinking"); n != 1 {
		t.Errorf("expected exactly one --thinking flag, got %d in: %s", n, cmd)
	}
	cmd, err = buildLaunchCmd("pi", "prov/model-a", "task", "/proj", "cs-abc")
	if err != nil {
		t.Fatalf("buildLaunchCmd: %v", err)
	}
	if strings.Contains(cmd, "--thinking") {
		t.Errorf("empty thinking level must add no --thinking flag, got: %s", cmd)
	}
}
