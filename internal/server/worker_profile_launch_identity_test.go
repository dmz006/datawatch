package server

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// A stale daemon entry left in the project .mcp.json (another session's
// token and id) must never become this launch's identity; operator servers
// in the same file stay selectable.
func TestWorkerProfileLaunchFn_StaleProjectDaemonEntryIsNotThisSessionsIdentity(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	leanRegisterClaudeCode(t)
	p, ok := workerprofile.Builtin("datawatch-tools")
	if !ok {
		t.Fatal("builtin lean profile datawatch-tools is missing")
	}
	d := t.TempDir()
	mcpJSON := `{"mcpServers":{"datawatch":{"command":"bridge","args":[],"env":{"DATAWATCH_API_URL":"http://127.0.0.1:1","DATAWATCH_TOKEN":"someone-elses-token","CLAUDE_SESSION_ID":"host-other"}}}}`
	if err := os.WriteFile(filepath.Join(d, ".mcp.json"), []byte(mcpJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	argvFile := leanFakeTmux(t)
	if err := s.workerProfileLaunchFn(&p, "claude-code", nil)(context.Background(), "", "cs-lean-stale", d, "/nonexistent/log"); err != nil {
		t.Fatalf("profiled launch: %v", err)
	}
	cmd := leanRecordedLaunchCmd(t, argvFile)
	m := regexp.MustCompile(`--mcp-config '([^']+)'`).FindStringSubmatch(cmd)
	if m == nil {
		t.Fatalf("no --mcp-config path:\n%s", cmd)
	}
	raw, err := os.ReadFile(m[1])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "someone-elses-token") || strings.Contains(string(raw), "host-other") {
		t.Fatalf("launch took another session's identity from the project file:\n%s", raw)
	}
}
