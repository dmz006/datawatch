package pi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// sampleProviderTable is a pi provider table with a second provider and a
// top-level key. A node-bound launch must carry both over unchanged.
const sampleProviderTable = `{
  "meta": { "version": 3 },
  "providers": {
    "ollama": {
      "baseUrl": "http://localhost:11434/v1",
      "api": "openai-completions",
      "apiKey": "ollama",
      "models": [{ "id": "qwen3.8:27b", "contextWindow": 65536 }]
    },
    "anthropic": {
      "api": "anthropic-messages",
      "apiKey": "sk-test",
      "models": [{ "id": "claude-test" }]
    }
  }
}
`

// useAgentEnvironment points pi's config directory at a temporary source
// directory holding the given provider table, plus the credentials and session
// entries that a node-bound launch must link. TMPDIR is a temporary directory,
// so the per-session directory and the task files land there.
func useAgentEnvironment(t *testing.T, table string) string {
	t.Helper()
	src := t.TempDir()
	writeTestFile(t, filepath.Join(src, "models.json"), table)
	writeTestFile(t, filepath.Join(src, "auth.json"), `{"ollama":{"type":"api_key","key":"test"}}`)
	if err := os.Mkdir(filepath.Join(src, "sessions"), 0o755); err != nil {
		t.Fatalf("create sessions dir: %v", err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", src)
	t.Setenv("TMPDIR", t.TempDir())
	return src
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// installFakeTmux puts a tmux stand-in first on PATH. It records its argv
// instead of driving a session, so Launch never reaches a real tmux. It
// returns the file the argv is recorded in.
func installFakeTmux(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\0' \"$@\" > '" + shellEscape(argvFile) + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvFile
}

// sentCommand returns the shell command the last Launch sent through the fake tmux.
func sentCommand(t *testing.T, argvFile string) string {
	t.Helper()
	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read recorded tmux argv: %v", err)
	}
	args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	return args[len(args)-2] // send-keys -t <session> <cmd> Enter
}

// agentDirPattern matches the setting directly in front of the pi invocation,
// which is how the command limits it to the pi process.
var agentDirPattern = regexp.MustCompile(`PI_CODING_AGENT_DIR='([^']+)' pi `)

// assertNodeOverride checks a node-bound launch command: pi is pointed at a
// per-session directory under the scratch location, that directory's provider
// table binds the ollama provider to wantBase, the source's credentials are
// linked in, and the command removes the directory after pi exits.
func assertNodeOverride(t *testing.T, cmd, wantBase string) {
	t.Helper()
	m := agentDirPattern.FindStringSubmatch(cmd)
	if m == nil {
		t.Fatalf("launch command does not point pi at a node agent directory: %s", cmd)
	}
	dir := m[1]
	if !strings.HasPrefix(dir, os.TempDir()) {
		t.Errorf("agent directory %q is not under the scratch location %q", dir, os.TempDir())
	}
	raw, err := os.ReadFile(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatalf("read per-session provider table: %v", err)
	}
	var doc struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse per-session provider table: %v", err)
	}
	if got := doc.Providers["ollama"].BaseURL; got != wantBase {
		t.Errorf("ollama baseUrl = %q, want %q", got, wantBase)
	}
	if strings.Contains(cmd, "export ") {
		t.Errorf("the setting must not be exported into the shell, got: %s", cmd)
	}
	target, err := os.Readlink(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatalf("credentials must be linked from the source directory: %v", err)
	}
	if want := filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "auth.json"); target != want {
		t.Errorf("auth.json links to %q, want %q", target, want)
	}
	if !strings.Contains(cmd, "rm -rf '"+dir+"'") {
		t.Errorf("command must remove the per-session agent directory, got: %s", cmd)
	}
}

func TestPiPromptLaunchUsesNodeHost(t *testing.T) {
	useAgentEnvironment(t, sampleProviderTable)
	argvFile := installFakeTmux(t)

	b := NewPrompt("pi").(*PromptBackend)
	b.SetModel("ollama/qwen3.8:27b")
	b.SetNodeHost("node-b:11434")
	if err := b.Launch(context.Background(), "do the thing", "cs-node", t.TempDir(), ""); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	assertNodeOverride(t, sentCommand(t, argvFile), "http://node-b:11434/v1")
}

func TestPiLaunchUsesNodeHost(t *testing.T) {
	useAgentEnvironment(t, sampleProviderTable)
	argvFile := installFakeTmux(t)

	b := New("pi").(*Backend)
	b.SetSessionName("sess-node")
	b.SetModel("ollama/qwen3.8:27b")
	b.SetNodeHost("http://node-b:11434")
	if err := b.Launch(context.Background(), "", "cs-node", t.TempDir(), ""); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	cmd := sentCommand(t, argvFile)
	assertNodeOverride(t, cmd, "http://node-b:11434/v1")
	// The cleanup follows pi's status capture, so the exit status survives it.
	if !strings.Contains(cmd, "pi_status=$?; rm -rf '") {
		t.Errorf("the agent directory must be removed after pi's status is captured, got: %s", cmd)
	}
}

// scratchPathPattern matches a quoted scratch path, which differs per run.
var scratchPathPattern = regexp.MustCompile(`'[^']*datawatch-pi-[^']*'`)

func TestPiLaunchWithoutNodeHostUnchanged(t *testing.T) {
	// A source directory exists but no node is set: it must not be read or linked.
	useAgentEnvironment(t, sampleProviderTable)
	argvFile := installFakeTmux(t)
	const project, model = "/home/user/work", "ollama/qwen3.8:27b"

	b := New("pi").(*Backend)
	b.SetSessionName("sess-plain")
	b.SetModel(model)
	if err := b.Launch(context.Background(), "", "cs-plain", project, ""); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	gotInteractive := scratchPathPattern.ReplaceAllString(sentCommand(t, argvFile), "'SCRATCH'")
	wantInteractive := `cd '/home/user/work' && pi --approve --model 'ollama/qwen3.8:27b' --session-id 'sess-plain' ; pi_status=$?; echo; if [ "$pi_status" -eq 0 ]; then echo 'DATAWATCH_COMPLETE: pi session ended'; else echo "DATAWATCH_PI_FAILED: pi exited with status $pi_status"; fi; exit "$pi_status"`
	if gotInteractive != wantInteractive {
		t.Errorf("interactive command changed without a node host:\n got: %s\nwant: %s", gotInteractive, wantInteractive)
	}

	p := NewPrompt("pi").(*PromptBackend)
	p.SetModel(model)
	if err := p.Launch(context.Background(), "", "cs-plain", project, ""); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	gotPrompt := scratchPathPattern.ReplaceAllString(sentCommand(t, argvFile), "'SCRATCH'")
	wantPrompt := `cd '/home/user/work' && { pi --print --mode text --approve --model 'ollama/qwen3.8:27b' ; echo $? > 'SCRATCH'; } | tee 'SCRATCH'; read dw_status < 'SCRATCH'; [ -s 'SCRATCH' ]; dw_has_output=$?; rm -f 'SCRATCH' 'SCRATCH'; if [ "${dw_status:-1}" -ne 0 ]; then printf 'DATAWATCH_PI_FAILED: pi exited with status %s (model %s)\n' "${dw_status:-1}" 'ollama/qwen3.8:27b'; exit "${dw_status:-1}"; fi; if [ "$dw_has_output" -ne 0 ]; then echo 'DATAWATCH_PI_FAILED: pi produced no output'; exit 1; fi; echo 'DATAWATCH_COMPLETE: pi done'`
	if gotPrompt != wantPrompt {
		t.Errorf("one-shot command changed without a node host:\n got: %s\nwant: %s", gotPrompt, wantPrompt)
	}

	for _, cmd := range []string{gotInteractive, gotPrompt} {
		if strings.Contains(cmd, "PI_CODING_AGENT_DIR") || strings.Contains(cmd, "rm -rf") {
			t.Errorf("no node host: command must not touch the agent directory, got: %s", cmd)
		}
	}
	assertNoNodeAgentDirs(t)
}

func TestNodeHostOverrideFileHasOnlyNodeProvider(t *testing.T) {
	src := t.TempDir()
	writeTestFile(t, filepath.Join(src, "models.json"), sampleProviderTable)
	writeTestFile(t, filepath.Join(src, "auth.json"), `{}`)
	t.Setenv("TMPDIR", t.TempDir())

	dir, err := writeNodeHostAgentDir(src, "cs-override", "ollama", nodeBaseURL("node-b:11434"))
	if err != nil {
		t.Fatalf("writeNodeHostAgentDir: %v", err)
	}

	var want, got map[string]any
	if err := json.Unmarshal([]byte(sampleProviderTable), &want); err != nil {
		t.Fatalf("parse source table: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatalf("read override: %v", err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parse override: %v", err)
	}
	// Only the baseUrl of the provider named by the model prefix may differ.
	want["providers"].(map[string]any)["ollama"].(map[string]any)["baseUrl"] = "http://node-b:11434/v1"
	if !reflect.DeepEqual(got, want) {
		t.Errorf("override differs from source beyond the node provider's baseUrl:\n got: %s\nwant: %v", raw, want)
	}

	source, err := os.ReadFile(filepath.Join(src, "models.json"))
	if err != nil {
		t.Fatalf("re-read source: %v", err)
	}
	if string(source) != sampleProviderTable {
		t.Error("the source provider table was modified")
	}
	info, err := os.Lstat(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatalf("stat override: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("models.json must be a copy, not a link into the source")
	}
	if target, err := os.Readlink(filepath.Join(dir, "auth.json")); err != nil || target != filepath.Join(src, "auth.json") {
		t.Errorf("auth.json link = %q (%v), want a link to the source", target, err)
	}
}

func TestNodeHostMissingProviderFailsLaunch(t *testing.T) {
	useAgentEnvironment(t, sampleProviderTable)
	argvFile := installFakeTmux(t)

	// The source table has no "openrouter" entry, so a node cannot be bound to it.
	b := NewPrompt("pi").(*PromptBackend)
	b.SetModel("openrouter/some-model")
	b.SetNodeHost("node-b:11434")
	err := b.Launch(context.Background(), "do the thing", "cs-missing", t.TempDir(), "")
	if !errors.Is(err, errNodeProvider) {
		t.Fatalf("Launch with a provider missing from the source table: want errNodeProvider, got %v", err)
	}
	if _, statErr := os.Stat(argvFile); !os.IsNotExist(statErr) {
		t.Error("a launch that cannot bind its node must not send a command")
	}
	assertNoNodeAgentDirs(t)
}

// assertNoNodeAgentDirs fails if a per-session agent directory was left in the scratch location.
func assertNoNodeAgentDirs(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatalf("read scratch location: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "datawatch-pi-agent-") {
			t.Errorf("per-session agent directory left behind: %s", e.Name())
		}
	}
}
