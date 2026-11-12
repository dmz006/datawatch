package goose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/testhome"
)

// installFakeTmux puts a PATH-resolvable fake `tmux` in dir that records
// its argv (one argument per line) to argvFile. No real tmux server is
// ever touched.
//
// Recording uses `printf '%s\n'` inside a POSIX `for` loop rather than the
// NUL-separated `printf '%s\0'` form: NUL bytes in printf formats are not
// guaranteed on every shell/busybox build on every host, while `%s\n` is
// POSIX. The command Launch sends is a single line (the task body lives in
// a scratch file, never inline), so line separation loses nothing.
func installFakeTmux(t *testing.T, dir, argvFile string) {
	t.Helper()
	fakeTmux := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > '" + shellQuote(argvFile) + "'\n"
	if err := os.WriteFile(fakeTmux, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func recordedSendKeysCmd(t *testing.T, argvFile string) string {
	t.Helper()
	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read recorded tmux argv: %v", err)
	}
	// One argument per line: tmux send-keys -t <session> <cmd> Enter
	args := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(args) < 2 || args[len(args)-2] == "" {
		t.Fatalf("fake tmux recorded %d argv element(s) (%q); want `... <cmd> Enter` with a non-empty command", len(args), string(raw))
	}
	return args[len(args)-2]
}

// TestPromptBackendLaunch_LongTaskGoesThroughScratchFile proves that the
// goose-prompt one-shot Launch with a 40 KB task (multi-KB spec inline
// would blow past tmux's ~16 KB send-keys limit) emits a short command
// containing the `$(cat '...')` expression, and that the scratch file
// holds the exact task text. The interactive goose Launch/LaunchResume
// send no task on the command line and are unaffected.
func TestPromptBackendLaunch_LongTaskGoesThroughScratchFile(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	installFakeTmux(t, dir, argvFile)
	testhome.PrivateTmp(t) // scratch task file is removed with the test's temp dir

	task := "SPEC\n" + strings.Repeat("y", 40*1024)
	b := NewPrompt("goose")
	if err := b.Launch(context.Background(), task, "cs-test", "/proj", "/tmp/out.log"); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	cmd := recordedSendKeysCmd(t, argvFile)

	if len(cmd) >= len(task) {
		t.Errorf("Launch command length %d must be far shorter than the %d-byte task", len(cmd), len(task))
	}
	i := strings.Index(cmd, "$(cat '")
	if i < 0 {
		t.Fatalf("expected task delivered via `$(cat '...')`, got: %.200s", cmd)
	}
	if strings.Contains(cmd, "yyyyyyyyyyyyyyyy") {
		t.Errorf("task text must not appear inline in the command, got: %.200s", cmd)
	}
	// Extract the scratch file path and verify it holds the full task.
	rest := cmd[i+len("$(cat '"):]
	end := strings.Index(rest, "'")
	if end < 0 {
		t.Fatalf("unterminated scratch file path in: %s", rest)
	}
	path := rest[:end]
	if !filepath.IsAbs(path) {
		t.Fatalf("scratch file path = %q, want absolute", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read scratch file: %v", err)
	}
	if string(got) != task {
		t.Errorf("scratch file contents = %d bytes, want the full %d-byte task text", len(got), len(task))
	}
	// Cleanup must be scheduled before the completion echo.
	rmIdx := strings.Index(cmd, "rm -f '"+path+"'")
	echoIdx := strings.Index(cmd, "DATAWATCH_COMPLETE")
	if rmIdx < 0 || rmIdx >= echoIdx {
		t.Errorf("rm -f cleanup must come before the DATAWATCH_COMPLETE echo, got: %.400s", cmd)
	}
	if !strings.HasSuffix(cmd, "echo 'DATAWATCH_COMPLETE: goose done'") {
		t.Errorf("completion marker must be echoed unconditionally, got: %.400s", cmd)
	}
}

// TestPromptBackendLaunch_FallsBackToInlineWhenScratchFileFails proves
// that if writing the scratch file fails (unwritable temp dir), the
// goose-prompt Launch falls back to the old inline form so the task is
// still delivered.
func TestPromptBackendLaunch_FallsBackToInlineWhenScratchFileFails(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	installFakeTmux(t, dir, argvFile)
	testhome.PrivateTmp(t) // any scratch file lands in the test's temp dir
	// Point TMPDIR at a path that doesn't exist so os.CreateTemp fails.
	t.Setenv("TMPDIR", filepath.Join(dir, "definitely-missing"))

	task := "small inline task"
	b := NewPrompt("goose")
	if err := b.Launch(context.Background(), task, "cs-test", "/proj", ""); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	cmd := recordedSendKeysCmd(t, argvFile)
	if strings.Contains(cmd, "$(cat '") {
		t.Errorf("scratch file write failed; command must not reference a cat file, got: %s", cmd)
	}
	if !strings.Contains(cmd, "--text 'small inline task'") {
		t.Errorf("expected inline fallback form in: %s", cmd)
	}
}

func TestResolveBinary_PathLookup(t *testing.T) {
	// If the binary name is on PATH, it should be returned as-is.
	got := resolveBinary("sh") // sh is always on PATH
	if got != "sh" {
		t.Errorf("resolveBinary(\"sh\") = %q, want \"sh\"", got)
	}
}

func TestResolveBinary_AbsolutePath(t *testing.T) {
	got := resolveBinary("/usr/bin/env")
	if got != "/usr/bin/env" {
		t.Errorf("resolveBinary(\"/usr/bin/env\") = %q, want \"/usr/bin/env\"", got)
	}
}

func TestResolveBinary_FallbackToLocalBin(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	const binaryName = "goose-fake-bin"
	localBin := filepath.Join(tmp, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	localGoose := filepath.Join(localBin, binaryName)
	if err := os.WriteFile(localGoose, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveBinary(binaryName)
	if got != localGoose {
		t.Errorf("resolveBinary fallback = %q, want %q", got, localGoose)
	}
}

func TestResolveBinary_NotFound(t *testing.T) {
	// Binary not on PATH and no fallback files exist — return original name.
	got := resolveBinary("goose-totally-nonexistent-xyz")
	if got != "goose-totally-nonexistent-xyz" {
		t.Errorf("resolveBinary(not found) = %q, want original name", got)
	}
}

func TestVersionNormalization(t *testing.T) {
	// Version() prefixes 'v' when the binary omits it (Goose outputs "1.43.0").
	// We test the normalization logic directly since we can't run the real binary.
	cases := []struct {
		raw  string
		want string
	}{
		{"1.43.0", "v1.43.0"},
		{"v1.43.0", "v1.43.0"},
		{"", ""},
		{"2.0.0-rc1", "v2.0.0-rc1"},
	}
	for _, tc := range cases {
		v := tc.raw
		if v != "" && !hasVPrefix(v) {
			v = "v" + v
		}
		if v != tc.want {
			t.Errorf("normalize(%q) = %q, want %q", tc.raw, v, tc.want)
		}
	}
}

func hasVPrefix(s string) bool { return len(s) > 0 && s[0] == 'v' }

func TestBackendName(t *testing.T) {
	b := New("goose")
	if b.Name() != "goose" {
		t.Errorf("Backend.Name() = %q, want \"goose\"", b.Name())
	}
}

func TestPromptBackendName(t *testing.T) {
	b := NewPrompt("goose")
	if b.Name() != "goose-prompt" {
		t.Errorf("PromptBackend.Name() = %q, want \"goose-prompt\"", b.Name())
	}
}

func TestSupportsInteractiveInput(t *testing.T) {
	b := New("goose").(*Backend)
	if !b.SupportsInteractiveInput() {
		t.Error("Backend.SupportsInteractiveInput() = false, want true")
	}
	pb := NewPrompt("goose").(*PromptBackend)
	if pb.SupportsInteractiveInput() {
		t.Error("PromptBackend.SupportsInteractiveInput() = true, want false")
	}
}

func TestPromptRequired(t *testing.T) {
	b := New("goose").(*Backend)
	if b.PromptRequired() {
		t.Error("Backend.PromptRequired() = true, want false")
	}
	pb := NewPrompt("goose").(*PromptBackend)
	if !pb.PromptRequired() {
		t.Error("PromptBackend.PromptRequired() = false, want true")
	}
}

func TestSetSessionName_SanitizesColons(t *testing.T) {
	b := New("goose").(*Backend)
	b.SetSessionName("local-node:abc123")
	if b.sessionName != "local-node-abc123" {
		t.Errorf("SetSessionName colon sanitize: got %q, want \"local-node-abc123\"", b.sessionName)
	}
}

func TestSetSessionName_SanitizesSlashes(t *testing.T) {
	b := New("goose").(*Backend)
	b.SetSessionName("my/session\\name")
	if b.sessionName != "my-session-name" {
		t.Errorf("SetSessionName slash sanitize: got %q, want \"my-session-name\"", b.sessionName)
	}
}

// --- T2/T3 tests (BL363) ---

func TestProviderKeyEnvVar(t *testing.T) {
	cases := []struct{ provider, want string }{
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"Anthropic", "ANTHROPIC_API_KEY"},
		{"openai", "OPENAI_API_KEY"},
		{"OpenAI", "OPENAI_API_KEY"},
		{"google", "GOOGLE_API_KEY"},
		{"gemini", "GOOGLE_API_KEY"},
		{"Gemini", "GOOGLE_API_KEY"},
		{"ollama", "GOOSE_API_KEY"},
		{"", "GOOSE_API_KEY"},
		{"unknown-provider", "GOOSE_API_KEY"},
	}
	for _, tc := range cases {
		got := providerKeyEnvVar(tc.provider)
		if got != tc.want {
			t.Errorf("providerKeyEnvVar(%q) = %q, want %q", tc.provider, got, tc.want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "'hello'"},
		{"with space", "'with space'"},
		{"it's fine", `'it'\''s fine'`},
		{"", "''"},
		{"a'b'c", `'a'\''b'\''c'`},
	}
	for _, tc := range cases {
		got := shellQuote(tc.in)
		if got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGooseEnvPrefix_Empty(t *testing.T) {
	b := New("goose").(*Backend)
	if got := b.gooseEnvPrefix(); got != "" {
		t.Errorf("gooseEnvPrefix() with no fields = %q, want empty", got)
	}
}

func TestGooseEnvPrefix_ProviderAndModel(t *testing.T) {
	b := New("goose").(*Backend)
	b.SetProvider("anthropic")
	b.SetModel("claude-sonnet-4-6")
	got := b.gooseEnvPrefix()
	if !contains(got, "GOOSE_PROVIDER='anthropic'") {
		t.Errorf("missing GOOSE_PROVIDER in %q", got)
	}
	if !contains(got, "GOOSE_MODEL='claude-sonnet-4-6'") {
		t.Errorf("missing GOOSE_MODEL in %q", got)
	}
	// No api key set — provider key env var must NOT appear.
	if contains(got, "ANTHROPIC_API_KEY") {
		t.Errorf("unexpected ANTHROPIC_API_KEY in %q (no key was set)", got)
	}
}

func TestGooseEnvPrefix_FullAnthropicConfig(t *testing.T) {
	b := New("goose").(*Backend)
	b.SetProvider("anthropic")
	b.SetModel("claude-opus-4-5")
	b.SetAPIKey("sk-ant-test123")
	got := b.gooseEnvPrefix()
	if !contains(got, "GOOSE_PROVIDER='anthropic'") {
		t.Errorf("missing GOOSE_PROVIDER in %q", got)
	}
	if !contains(got, "GOOSE_MODEL='claude-opus-4-5'") {
		t.Errorf("missing GOOSE_MODEL in %q", got)
	}
	if !contains(got, "ANTHROPIC_API_KEY='sk-ant-test123'") {
		t.Errorf("missing ANTHROPIC_API_KEY in %q", got)
	}
}

func TestGooseEnvPrefix_UnknownProviderUsesGooseAPIKey(t *testing.T) {
	b := New("goose").(*Backend)
	b.SetProvider("mylocal")
	b.SetAPIKey("localkey")
	got := b.gooseEnvPrefix()
	if !contains(got, "GOOSE_API_KEY='localkey'") {
		t.Errorf("missing GOOSE_API_KEY in %q", got)
	}
}

func TestGooseEnvPrefix_ChannelRequiresSessionID(t *testing.T) {
	b := New("goose").(*Backend)
	b.SetChannelEnabled(true)
	// No session ID set — MCP env vars must NOT appear.
	got := b.gooseEnvPrefix()
	if contains(got, "GOOSE_MCP__DATAWATCH") {
		t.Errorf("unexpected MCP env vars without session ID in %q", got)
	}
}

func TestGooseEnvPrefix_ChannelWithSessionID(t *testing.T) {
	b := New("goose").(*Backend)
	b.SetChannelEnabled(true)
	b.SetSessionFullID("local-node:abc123")
	got := b.gooseEnvPrefix()
	if !contains(got, "GOOSE_MCP__DATAWATCH__TYPE=stdio") {
		t.Errorf("missing MCP TYPE env var in %q", got)
	}
	if !contains(got, "GOOSE_MCP__DATAWATCH__CMD=") {
		t.Errorf("missing MCP CMD env var in %q", got)
	}
	if !contains(got, "mcp,--caller-session-id,local-node:abc123") {
		t.Errorf("missing session ID in MCP ARGS in %q", got)
	}
}

func TestSetters_Backend(t *testing.T) {
	b := New("goose").(*Backend)
	b.SetProvider("openai")
	b.SetModel("gpt-4o")
	b.SetAPIKey("sk-openai-test")
	b.SetChannelEnabled(true)
	b.SetSessionFullID("mysession:xyz")
	if b.provider != "openai" {
		t.Errorf("SetProvider: got %q, want \"openai\"", b.provider)
	}
	if b.model != "gpt-4o" {
		t.Errorf("SetModel: got %q, want \"gpt-4o\"", b.model)
	}
	if b.apiKey != "sk-openai-test" {
		t.Errorf("SetAPIKey: got %q, want \"sk-openai-test\"", b.apiKey)
	}
	if !b.channelEnabled {
		t.Error("SetChannelEnabled(true): channelEnabled = false")
	}
	if b.sessionFullID != "mysession:xyz" {
		t.Errorf("SetSessionFullID: got %q, want \"mysession:xyz\"", b.sessionFullID)
	}
}

func TestSetters_PromptBackend(t *testing.T) {
	pb := NewPrompt("goose").(*PromptBackend)
	pb.SetProvider("google")
	pb.SetModel("gemini-2.5-pro")
	pb.SetAPIKey("gkey")
	pb.SetChannelEnabled(true)
	pb.SetSessionFullID("prompt:session:99")
	if pb.provider != "google" {
		t.Errorf("SetProvider: got %q, want \"google\"", pb.provider)
	}
	if pb.model != "gemini-2.5-pro" {
		t.Errorf("SetModel: got %q, want \"gemini-2.5-pro\"", pb.model)
	}
	if pb.apiKey != "gkey" {
		t.Errorf("SetAPIKey: got %q, want \"gkey\"", pb.apiKey)
	}
	if !pb.channelEnabled {
		t.Error("SetChannelEnabled(true): channelEnabled = false")
	}
	if pb.sessionFullID != "prompt:session:99" {
		t.Errorf("SetSessionFullID: got %q, want \"prompt:session:99\"", pb.sessionFullID)
	}
}

func TestPromptBackendEnvPrefix_FullConfig(t *testing.T) {
	pb := NewPrompt("goose").(*PromptBackend)
	pb.SetProvider("openai")
	pb.SetModel("gpt-4o-mini")
	pb.SetAPIKey("sk-test")
	got := pb.gooseEnvPrefix()
	if !contains(got, "GOOSE_PROVIDER='openai'") {
		t.Errorf("missing GOOSE_PROVIDER in PromptBackend env: %q", got)
	}
	if !contains(got, "OPENAI_API_KEY='sk-test'") {
		t.Errorf("missing OPENAI_API_KEY in PromptBackend env: %q", got)
	}
}

func contains(s, sub string) bool { return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0 }
func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
