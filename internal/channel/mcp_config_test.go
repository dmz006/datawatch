// BL109 — generic per-session .mcp.json writer tests.

package channel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fakeNode points NodePath at a known executable so tests don't need
// node installed. /bin/sh is universally present on the test hosts.
func fakeNode(t *testing.T) {
	t.Helper()
	prev := os.Getenv("DATAWATCH_NODE_BIN")
	t.Setenv("DATAWATCH_NODE_BIN", "/bin/sh")
	t.Cleanup(func() { _ = os.Setenv("DATAWATCH_NODE_BIN", prev) })
}

func TestWriteProjectMCPConfig_Empty(t *testing.T) {
	fakeNode(t)
	dir := t.TempDir()
	if err := WriteProjectMCPConfig(dir, "/path/to/channel.js", map[string]string{"K": "V"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg MCPProjectConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	dw, ok := cfg.MCPServers["datawatch"]
	if !ok {
		t.Fatalf("datawatch entry missing: %+v", cfg)
	}
	if len(dw.Args) != 1 || dw.Args[0] != "/path/to/channel.js" {
		t.Errorf("args wrong: %+v", dw.Args)
	}
	if dw.Env["K"] != "V" {
		t.Errorf("env not propagated: %+v", dw.Env)
	}
}

func TestWriteProjectMCPConfig_PreservesOtherEntries(t *testing.T) {
	fakeNode(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")

	// Operator pre-adds their own entry.
	pre := MCPProjectConfig{MCPServers: map[string]MCPServerSpec{
		"my-other-server": {Command: "/bin/whoami"},
	}}
	raw, _ := json.Marshal(pre)
	_ = os.WriteFile(path, raw, 0644)

	if err := WriteProjectMCPConfig(dir, "/path/to/channel.js", nil); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(path)
	var cfg MCPProjectConfig
	_ = json.Unmarshal(out, &cfg)
	if _, ok := cfg.MCPServers["datawatch"]; !ok {
		t.Errorf("datawatch entry missing")
	}
	if _, ok := cfg.MCPServers["my-other-server"]; !ok {
		t.Errorf("operator's entry was clobbered: %+v", cfg)
	}
}

func TestWriteProjectMCPConfig_EmptyDir_NoOp(t *testing.T) {
	if err := WriteProjectMCPConfig("", "/x", nil); err != nil {
		t.Errorf("empty projectDir should be a no-op, got %v", err)
	}
}

func TestWriteProjectMCPConfig_MissingChannelJS_Errors(t *testing.T) {
	if err := WriteProjectMCPConfig(t.TempDir(), "", nil); err == nil {
		t.Error("expected error for empty channel.js path")
	}
}

func TestWriteProjectMCPConfig_Idempotent(t *testing.T) {
	fakeNode(t)
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := WriteProjectMCPConfig(dir, "/path/to/channel.js", map[string]string{"K": "V"}); err != nil {
			t.Fatal(err)
		}
	}
	out, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	var cfg MCPProjectConfig
	_ = json.Unmarshal(out, &cfg)
	if len(cfg.MCPServers) != 1 {
		t.Errorf("repeated writes left %d entries; want 1", len(cfg.MCPServers))
	}
}

// BL318 — WriteInstanceMCPConfig writes to dataDir/.mcp.json, never $HOME.
func TestWriteInstanceMCPConfig_WritesToDataDir(t *testing.T) {
	fakeNode(t)
	dataDir := t.TempDir()
	updated, err := WriteInstanceMCPConfig(dataDir, "/path/to/channel.js", map[string]string{"K": "V"})
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Error("expected updated=true on first write")
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, ".mcp.json"))
	if err != nil {
		t.Fatal("expected .mcp.json in dataDir:", err)
	}
	var cfg MCPProjectConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := cfg.MCPServers["datawatch"]; !ok {
		t.Fatalf("datawatch entry missing: %+v", cfg)
	}
}

func TestWriteInstanceMCPConfig_Idempotent(t *testing.T) {
	fakeNode(t)
	dataDir := t.TempDir()
	if _, err := WriteInstanceMCPConfig(dataDir, "/path/to/channel.js", nil); err != nil {
		t.Fatal(err)
	}
	updated, err := WriteInstanceMCPConfig(dataDir, "/path/to/channel.js", nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Error("expected updated=false on repeated write with same content")
	}
}

func TestWriteInstanceMCPConfig_DoesNotWriteToHome(t *testing.T) {
	fakeNode(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	homeMCP := filepath.Join(home, ".mcp.json")
	// Note any pre-existing file so we can detect modification.
	var preStat os.FileInfo
	preStat, _ = os.Stat(homeMCP)

	dataDir := t.TempDir()
	if _, err := WriteInstanceMCPConfig(dataDir, "/path/to/channel.js", nil); err != nil {
		t.Fatal(err)
	}

	postStat, _ := os.Stat(homeMCP)
	// If neither exists, we're good. If both exist, mod times must match.
	if preStat == nil && postStat != nil {
		t.Error("WriteInstanceMCPConfig created ~/.mcp.json — must only write to dataDir")
	} else if preStat != nil && postStat != nil && preStat.ModTime() != postStat.ModTime() {
		t.Error("WriteInstanceMCPConfig modified ~/.mcp.json — must only write to dataDir")
	}
}
