package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// The standalone MCP process writes the same audit line as the daemon when a
// tool's input is refused.
func TestStandaloneMCPAuditsRefusedToolInput(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/true", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	f := mgr.WithFakeTmux()
	sess := &session.Session{
		ID: "ab12", FullID: "testhost-ab12", Hostname: "testhost", TmuxSession: "cs-ab12",
		State: session.StateWaitingInput, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	f.Pane["cs-ab12"] = " Do you want to proceed?\n ❯ 1. Yes\n   2. No\n"
	done := wireStandaloneInputAudit(mgr, &config.Config{DataDir: dir})
	defer done()
	// In stdio mode stdout carries the protocol: a refusal prints nothing
	// there.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	sendErr := mgr.SendToolInput(sess.FullID, "1", "mcp")
	os.Stdout = stdout
	_ = w.Close()
	printed, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if sendErr == nil {
		t.Fatal("want a refusal")
	}
	if len(printed) != 0 {
		t.Errorf("the refusal printed to stdout: %q", printed)
	}
	b, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"action":"automated_input_refused"`, `"actor":"daemon:mcp"`, `"session_id":"testhost-ab12"`, "nothing was typed"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("audit.log lacks %s: %s", want, b)
		}
	}
	main, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	_, runMCPBody, _ := strings.Cut(string(main), "\nfunc runMCP(")
	runMCPBody, _, _ = strings.Cut(runMCPBody, "\n}\n")
	if !strings.Contains(runMCPBody, "wireStandaloneInputAudit(mgr, cfg)") {
		t.Error("runMCP does not wire the input audit")
	}
}
