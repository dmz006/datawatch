package server

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// TestWorkerProfileScheduledSpawn_SupervisorResolvesToolsAndRewritesLauncher:
// the server-level scheduled launch for a supervisor spawn resolves the
// datawatch-tools profile for session kind supervisor, and running the launch
// function it returns rewrites the launcher files (worker-mcp.json and the
// starting context) in the session directory.
func TestWorkerProfileScheduledSpawn_SupervisorResolvesToolsAndRewritesLauncher(t *testing.T) {
	cfg := &config.Config{}
	cfg.Autonomous.WorkerProfileDefaults.Kind = map[string]string{"supervisor": "datawatch-tools"}
	s := &Server{cfg: cfg}
	leanRegisterClaudeCode(t)

	ds := session.DeferredSession{
		LaunchRole: "supervisor",
		OneShot:    true,
		Backend:    "claude-code",
		Name:       "sched-" + time.Now().Format("150405.000"),
	}
	opts := &session.StartOptions{}
	reason := s.scheduledSpawnProfile(ds, opts)
	if reason == nil {
		t.Fatal("scheduledSpawnProfile returned a nil reason pointer")
	}
	if opts.WorkerProfile.Name != "datawatch-tools" {
		t.Fatalf("resolved profile = %q (fallback %q), want datawatch-tools", opts.WorkerProfile.Name, *reason)
	}
	if opts.LaunchFn == nil {
		t.Fatal("scheduled supervisor spawn got no profile-applying launch function")
	}

	d := t.TempDir()
	mcpJSON := `{"mcpServers":{"datawatch":{"command":"datawatch","args":["mcp"]},"other":{"command":"other"}}}` + "\n"
	if err := os.WriteFile(filepath.Join(d, ".mcp.json"), []byte(mcpJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	// A launcher file left in the project root by an older daemon is not
	// touched (launch files now live in a private per-session folder).
	stale := filepath.Join(d, "worker-mcp.json")
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	argvFile := leanFakeTmux(t)
	if err := opts.LaunchFn(context.Background(), "supervisor pass", "cs-sched-sup", d, filepath.Join(d, "log")); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if *reason != "" {
		t.Fatalf("fallback reason = %q, want empty", *reason)
	}

	cmd := leanRecordedLaunchCmd(t, argvFile)
	m := regexp.MustCompile(`--mcp-config '([^']+)'`).FindStringSubmatch(cmd)
	if m == nil {
		t.Fatalf("launch command has no --mcp-config path:\n%s", cmd)
	}
	if filepath.Dir(m[1]) == d {
		t.Fatalf("worker-mcp.json written into the project directory: %s", m[1])
	}
	raw, err := os.ReadFile(m[1])
	if err != nil {
		t.Fatalf("worker-mcp.json: %v", err)
	}
	if !strings.Contains(string(raw), "datawatch") {
		t.Fatalf("worker-mcp.json lacks the datawatch server:\n%s", raw)
	}
	if old, _ := os.ReadFile(stale); string(old) != "stale" {
		t.Fatalf("a launcher file in the project root was rewritten: %q", old)
	}
	if strings.Contains(string(raw), "other") {
		t.Fatalf("worker-mcp.json kept a server outside the profile:\n%s", raw)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(m[1]), "worker-starting-context.md")); err != nil {
		t.Fatalf("starting context file not written next to the config: %v", err)
	}
	if !strings.Contains(cmd, "--strict-mcp-config") || !strings.Contains(cmd, "worker-mcp.json") {
		t.Fatalf("launch command lacks the profile pin:\n%s", cmd)
	}
}
