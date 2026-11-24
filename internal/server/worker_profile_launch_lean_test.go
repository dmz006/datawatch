// Lean supervisor launch — server side.
//
// workerProfileLaunchFn hands a named worker profile to the resolved backend
// backend. These tests prove the guarantee the design relies on for the lean
// (datawatch-tools) profile: a named backend entry that IS the claude-code
// backend gets the profile actually applied (MCP config written, ToolSearch env
// set), a profile that CANNOT apply reports a fallback reason instead of being
// silently dropped, and a backend that is NOT the claude-code backend never
// gets a profiled launch function in the first place. No tmux is involved:
// the tmux the launch would talk to is a PATH stub that records its argv.

package server

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm"
	_ "github.com/dmz006/datawatch/internal/llm/backends/goose" // registers a real non-claude-code backend named "goose"
	"github.com/dmz006/datawatch/internal/llm/claudecode"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// leanRegisterClaudeCode makes sure the "claude-code" registry slot holds a real
// claude-code backend — sibling server tests register fakes in the same slot
// (every real backend shares that name) — and hands the slot back afterwards.
func leanRegisterClaudeCode(t *testing.T) {
	t.Helper()
	var prev llm.Backend
	if b, err := llm.Get("claude-code"); err == nil {
		prev = b
	}
	llm.Register(claudecode.New(""))
	if prev != nil {
		t.Cleanup(func() { llm.Register(prev) })
	}
}

// leanFakeTmux puts a stub named "tmux" first on PATH that records its argv
// NUL-separated into argvFile and exits; no real tmux server is ever reached.
// TMUX is emptied so nothing outside the stub can address a multiplexer either.
func leanFakeTmux(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\0' \"$@\" > " + argvFile + "\n"
	fake := filepath.Join(dir, "tmux")
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatalf("unset TMUX: %v", err)
	}
	return argvFile
}

func leanRecordedLaunchCmd(t *testing.T, argvFile string) string {
	t.Helper()
	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read recorded tmux argv: %v", err)
	}
	args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	if len(args) < 2 {
		t.Fatalf("recorded tmux argv has %d args, want at least <session, command>", len(args))
	}
	return args[len(args)-2]
}

// TestWorkerProfileLaunchFn_LeanLaunchProfileApplied: the named supervisor
// profile (datawatch-tools) against the claude-code backend applies the
// profile, reports no fallback reason, and the recorded launch command carries
// the strict MCP pin, the session MCP config, and ENABLE_TOOL_SEARCH=true.
func TestWorkerProfileLaunchFn_LeanLaunchProfileApplied(t *testing.T) {
	cfg := &config.Config{}
	s := &Server{cfg: cfg}
	leanRegisterClaudeCode(t)

	p, ok := workerprofile.Builtin("datawatch-tools")
	if !ok {
		t.Fatal("builtin lean profile datawatch-tools is missing")
	}
	if p.MCP.Mode != workerprofile.MCPDatawatch {
		t.Fatalf("lean profile MCP mode = %q, want datawatch", p.MCP.Mode)
	}

	d := t.TempDir()
	// The session's project config offers the datawatch server the profile keeps.
	mcpJSON := `{"mcpServers":{"datawatch":{"command":"datawatch","args":["mcp"]}}}` + "\n"
	if err := os.WriteFile(filepath.Join(d, ".mcp.json"), []byte(mcpJSON), 0o600); err != nil {
		t.Fatalf("write .mcp.json: %v", err)
	}

	argvFile := leanFakeTmux(t)
	reason := ""
	fn := s.workerProfileLaunchFn(&p, "claude-code", &reason)
	if fn == nil {
		t.Fatal("claude-code backend with a resolvable profile returned a nil launch function")
	}
	if err := fn(context.Background(), "", "cs-lean-launch", d, "/nonexistent/log"); err != nil {
		t.Fatalf("profiled launch: %v", err)
	}
	if reason != "" {
		t.Errorf("fallback reason = %q, want empty: the lean profile must apply", reason)
	}

	cmd := leanRecordedLaunchCmd(t, argvFile)
	for _, want := range []string{
		"--strict-mcp-config",
		"--mcp-config",
		"worker-mcp.json",
		"ENABLE_TOOL_SEARCH=true",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("recorded launch command lacks %q:\n%s", want, cmd)
		}
	}

	// The config is written to a private per-session folder named in the
	// command, never to the project directory.
	cfgPath := regexp.MustCompile(`--mcp-config '([^']+)'`).FindStringSubmatch(cmd)
	if cfgPath == nil {
		t.Fatalf("launch command has no --mcp-config path:\n%s", cmd)
	}
	raw, err := os.ReadFile(cfgPath[1])
	if err != nil {
		t.Fatalf("session MCP config not written: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(d, "worker-mcp.json")); !os.IsNotExist(statErr) {
		t.Errorf("worker-mcp.json written into the project directory %s", d)
	}
	if !strings.Contains(string(raw), "datawatch") {
		t.Errorf("worker-mcp.json does not keep the datawatch server:\n%s", raw)
	}
}

// TestWorkerProfileLaunchFn_LeanLaunchFallbackReasonReported: a profile that
// cannot be written (it names an MCP server the session does not have) still
// launches the session, but the fallback reason is reported — never a silent
// drop — and the launch command carries none of the profile's flags. A nil
// reason pointer must also be safe.
func TestWorkerProfileLaunchFn_LeanLaunchFallbackReasonReported(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	leanRegisterClaudeCode(t)

	p, ok := workerprofile.Builtin("minimal")
	if !ok {
		t.Fatal("builtin minimal profile is missing")
	}
	p.MCP = workerprofile.MCPSelection{Mode: workerprofile.MCPList, Servers: []string{"no-such-lean-server"}}

	d := t.TempDir()
	argvFile := leanFakeTmux(t)
	reason := ""
	fn := s.workerProfileLaunchFn(&p, "claude-code", &reason)
	if fn == nil {
		t.Fatal("claude-code backend returned a nil launch function; the profile could not apply, so a reason — not a nil — is expected")
	}
	if err := fn(context.Background(), "", "cs-lean-launch-fb", d, "/nonexistent/log"); err != nil {
		t.Fatalf("launch with unapplied profile: %v", err)
	}
	if reason == "" {
		t.Fatal("fallback reason is empty, want a reported reason: an unapplied profile must never be silently dropped")
	}
	if !strings.Contains(reason, "no-such-lean-server") {
		t.Errorf("fallback reason %q does not name the missing MCP server", reason)
	}

	cmd := leanRecordedLaunchCmd(t, argvFile)
	for _, banned := range []string{"--strict-mcp-config", "worker-mcp.json", "ENABLE_TOOL_SEARCH="} {
		if strings.Contains(cmd, banned) {
			t.Errorf("recorded launch command carries profile flag %q although the profile failed to apply:\n%s", banned, cmd)
		}
	}
	if _, err := os.Stat(filepath.Join(d, "worker-mcp.json")); !os.IsNotExist(err) {
		t.Errorf("worker-mcp.json exists after a failed profile apply, want absent")
	}

	// The same unapplied profile with a nil reason pointer must launch as-is.
	if err := s.workerProfileLaunchFn(&p, "claude-code", nil)(context.Background(), "", "cs-lean-launch-nil", d, "/nonexistent/log"); err != nil {
		t.Fatalf("launch with nil reason pointer: %v", err)
	}
}

// TestWorkerProfileLaunchFn_LeanLaunchNonClaudeBackendNotApplied: named backends
// that are not the claude-code backend never receive a profiled launch
// function, so their profiles cannot be applied (and dropped) halfway.
func TestWorkerProfileLaunchFn_LeanLaunchNonClaudeBackendNotApplied(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	leanRegisterClaudeCode(t)
	p, ok := workerprofile.Builtin("datawatch-tools")
	if !ok {
		t.Fatal("builtin lean profile datawatch-tools is missing")
	}

	if fn := s.workerProfileLaunchFn(&p, "goose", nil); fn != nil {
		t.Fatalf("non-claude-code backend goose got a profiled launch function; the profile would be dropped mid-launch")
	}
	if fn := s.workerProfileLaunchFn(&p, "no-such-backend", nil); fn != nil {
		t.Fatalf("unresolvable backend name got a profiled launch function")
	}
	if fn := s.workerProfileLaunchFn(nil, "claude-code", nil); fn != nil {
		t.Fatal("nil profile returned a launch function; it must return nil")
	}
}
