// v5.27.10 (BL216) — tests for BridgeKind/BridgePath accessors,
// the WriteProjectMCPConfig Go-bridge fix, and IsStaleProjectMCPConfig.

package channel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBridgeKind_DefaultsToJS(t *testing.T) {
	prev := channelBinPathForReg
	t.Cleanup(func() { channelBinPathForReg = prev })

	channelBinPathForReg = ""
	if got := BridgeKind(); got != "js" {
		t.Errorf("BridgeKind with empty hint = %q, want js", got)
	}
	if got := BridgePath(); got != "" {
		t.Errorf("BridgePath with empty hint = %q, want empty", got)
	}
}

func TestBridgeKind_GoWhenHintSet(t *testing.T) {
	prev := channelBinPathForReg
	t.Cleanup(func() { channelBinPathForReg = prev })

	SetBinaryHint("/usr/local/bin/datawatch-channel")
	if got := BridgeKind(); got != "go" {
		t.Errorf("BridgeKind after SetBinaryHint = %q, want go", got)
	}
	if got := BridgePath(); got != "/usr/local/bin/datawatch-channel" {
		t.Errorf("BridgePath = %q, want /usr/local/bin/datawatch-channel", got)
	}
}

func TestWriteProjectMCPConfig_PrefersGoBridge(t *testing.T) {
	prev := channelBinPathForReg
	t.Cleanup(func() { channelBinPathForReg = prev })

	dir := t.TempDir()
	SetBinaryHint("/opt/datawatch/datawatch-channel")
	if err := WriteProjectMCPConfig(dir, "/should/not/be/used.js", map[string]string{"K": "V"}); err != nil {
		t.Fatalf("WriteProjectMCPConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	var cfg MCPProjectConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := cfg.MCPServers["datawatch"]
	if got.Command != "/opt/datawatch/datawatch-channel" {
		t.Errorf("command = %q, want Go bridge path", got.Command)
	}
	if len(got.Args) != 0 {
		t.Errorf("args = %v, want empty (Go bridge takes no args)", got.Args)
	}
	if got.Env["K"] != "V" {
		t.Errorf("env not preserved: %v", got.Env)
	}
}

func TestIsStaleProjectMCPConfig_StaleJS(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, ".mcp.json")
	missingJS := filepath.Join(dir, "does-not-exist", "channel.js")
	body, _ := json.Marshal(MCPProjectConfig{
		MCPServers: map[string]MCPServerSpec{
			"datawatch": {Command: "/usr/bin/node", Args: []string{missingJS}},
		},
	})
	if err := os.WriteFile(mcpPath, body, 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stale, jsPath, err := IsStaleProjectMCPConfig(mcpPath)
	if err != nil {
		t.Fatalf("IsStaleProjectMCPConfig: %v", err)
	}
	if !stale {
		t.Error("expected stale=true")
	}
	if jsPath != missingJS {
		t.Errorf("jsPath = %q, want %q", jsPath, missingJS)
	}
}

func TestIsStaleProjectMCPConfig_LiveJS(t *testing.T) {
	dir := t.TempDir()
	jsFile := filepath.Join(dir, "channel.js")
	if err := os.WriteFile(jsFile, []byte("// stub"), 0644); err != nil {
		t.Fatalf("seed js: %v", err)
	}
	mcpPath := filepath.Join(dir, ".mcp.json")
	body, _ := json.Marshal(MCPProjectConfig{
		MCPServers: map[string]MCPServerSpec{
			"datawatch": {Command: "/usr/bin/node", Args: []string{jsFile}},
		},
	})
	_ = os.WriteFile(mcpPath, body, 0644)

	stale, _, err := IsStaleProjectMCPConfig(mcpPath)
	if err != nil {
		t.Fatalf("IsStaleProjectMCPConfig: %v", err)
	}
	if stale {
		t.Error("expected stale=false when channel.js exists")
	}
}

func TestIsStaleProjectMCPConfig_GoBridge(t *testing.T) {
	// Go-bridge entry has Args=[] — should never be flagged stale
	// because the JS-shape detection is what stale-checks against.
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, ".mcp.json")
	body, _ := json.Marshal(MCPProjectConfig{
		MCPServers: map[string]MCPServerSpec{
			"datawatch": {Command: "/opt/datawatch-channel", Args: []string{}},
		},
	})
	_ = os.WriteFile(mcpPath, body, 0644)

	stale, _, err := IsStaleProjectMCPConfig(mcpPath)
	if err != nil {
		t.Fatalf("IsStaleProjectMCPConfig: %v", err)
	}
	if stale {
		t.Error("expected stale=false for Go-bridge entry")
	}
}
