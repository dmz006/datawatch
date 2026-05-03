// v6.0.7 (BL218) — channel session-start hygiene tests.
// Covers: SHA-256 staleness check in EnsureExtracted, SweepUserScopeMCPConfig.

package channel

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestEnsureExtracted_SHA256_Rewrite verifies that EnsureExtracted rewrites
// channel.js when the on-disk file has the same size but different content
// (the old size-only check would have missed this).
func TestEnsureExtracted_SHA256_Rewrite(t *testing.T) {
	t.Setenv("DATAWATCH_NODE_BIN", "/bin/sh")
	dir := t.TempDir()

	// First extraction — should succeed.
	dst, err := EnsureExtracted(dir)
	if err != nil {
		t.Fatalf("initial EnsureExtracted: %v", err)
	}

	// Tamper: overwrite with a same-length but different content.
	orig, _ := os.ReadFile(dst)
	tampered := make([]byte, len(orig))
	copy(tampered, orig)
	for i := range tampered {
		tampered[i] ^= 0x01 // flip every bit — same length, different content
	}
	if err := os.WriteFile(dst, tampered, 0644); err != nil {
		t.Fatalf("write tampered: %v", err)
	}

	// Second extraction must restore canonical content.
	if _, err := EnsureExtracted(dir); err != nil {
		t.Fatalf("second EnsureExtracted: %v", err)
	}
	restored, _ := os.ReadFile(dst)
	want := sha256.Sum256(channelJS)
	got := sha256.Sum256(restored)
	if got != want {
		t.Errorf("channel.js not restored after tampering: hash mismatch")
	}
}

// TestEnsureExtracted_NoRewrite_WhenCorrect verifies that EnsureExtracted does
// NOT rewrite channel.js when it already has the correct content.
func TestEnsureExtracted_NoRewrite_WhenCorrect(t *testing.T) {
	t.Setenv("DATAWATCH_NODE_BIN", "/bin/sh")
	dir := t.TempDir()

	// First extraction.
	dst, err := EnsureExtracted(dir)
	if err != nil {
		t.Fatalf("initial EnsureExtracted: %v", err)
	}
	info1, _ := os.Stat(dst)
	mtime1 := info1.ModTime()

	// Second extraction — file should NOT be touched.
	if _, err := EnsureExtracted(dir); err != nil {
		t.Fatalf("second EnsureExtracted: %v", err)
	}
	info2, _ := os.Stat(dst)
	if info2.ModTime() != mtime1 {
		t.Errorf("channel.js was rewritten unnecessarily (mtime changed)")
	}
}

// TestSweepUserScopeMCPConfig_WritesWhenMissing verifies that the sweep creates
// ~/.mcp.json when it does not exist yet.
func TestSweepUserScopeMCPConfig_WritesWhenMissing(t *testing.T) {
	fakeNode(t)
	// Redirect $HOME so we don't touch the real ~/.mcp.json.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	updated, err := SweepUserScopeMCPConfig("/path/to/channel.js", map[string]string{"K": "V"})
	if err != nil {
		t.Fatalf("SweepUserScopeMCPConfig: %v", err)
	}
	if !updated {
		t.Error("expected updated=true when ~/.mcp.json did not exist")
	}

	raw, err := os.ReadFile(filepath.Join(fakeHome, ".mcp.json"))
	if err != nil {
		t.Fatalf("read ~/.mcp.json: %v", err)
	}
	var cfg MCPProjectConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse: %v\n%s", err, raw)
	}
	dw, ok := cfg.MCPServers["datawatch"]
	if !ok {
		t.Fatalf("datawatch entry missing in ~/.mcp.json: %+v", cfg)
	}
	if len(dw.Args) != 1 || dw.Args[0] != "/path/to/channel.js" {
		t.Errorf("args wrong: %+v", dw.Args)
	}
	if dw.Env["K"] != "V" {
		t.Errorf("env not propagated: %+v", dw.Env)
	}
}

// TestSweepUserScopeMCPConfig_IdempotentWhenCurrent verifies that a second sweep
// does not rewrite the file when the entry is already correct.
func TestSweepUserScopeMCPConfig_IdempotentWhenCurrent(t *testing.T) {
	fakeNode(t)
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// First sweep writes the file.
	if _, err := SweepUserScopeMCPConfig("/path/to/channel.js", map[string]string{}); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	info1, _ := os.Stat(filepath.Join(fakeHome, ".mcp.json"))

	// Second sweep — should be a no-op.
	updated, err := SweepUserScopeMCPConfig("/path/to/channel.js", map[string]string{})
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if updated {
		t.Error("expected updated=false when entry already correct")
	}
	info2, _ := os.Stat(filepath.Join(fakeHome, ".mcp.json"))
	if info2.ModTime() != info1.ModTime() {
		t.Errorf("~/.mcp.json was rewritten unnecessarily")
	}
}

// TestSweepUserScopeMCPConfig_PreservesOtherEntries verifies that operator-added
// entries in ~/.mcp.json are not clobbered.
func TestSweepUserScopeMCPConfig_PreservesOtherEntries(t *testing.T) {
	fakeNode(t)
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Pre-populate ~/.mcp.json with an operator entry.
	pre := MCPProjectConfig{MCPServers: map[string]MCPServerSpec{
		"my-other-server": {Command: "/usr/bin/my-mcp"},
	}}
	raw, _ := json.Marshal(pre)
	_ = os.WriteFile(filepath.Join(fakeHome, ".mcp.json"), raw, 0644)

	if _, err := SweepUserScopeMCPConfig("/path/to/channel.js", nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	out, _ := os.ReadFile(filepath.Join(fakeHome, ".mcp.json"))
	var cfg MCPProjectConfig
	_ = json.Unmarshal(out, &cfg)
	if _, ok := cfg.MCPServers["my-other-server"]; !ok {
		t.Errorf("operator's entry was clobbered: %+v", cfg)
	}
	if _, ok := cfg.MCPServers["datawatch"]; !ok {
		t.Errorf("datawatch entry missing after sweep: %+v", cfg)
	}
}

// TestSweepUserScopeMCPConfig_UpdatesStaleJSEntry verifies that a sweep replaces
// a stale JS-pointing entry with the correct node+channel.js spec.
func TestSweepUserScopeMCPConfig_UpdatesStaleJSEntry(t *testing.T) {
	fakeNode(t)
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Pre-populate with a stale entry pointing at an old path.
	stale := MCPProjectConfig{MCPServers: map[string]MCPServerSpec{
		"datawatch": {Command: "/old/node", Args: []string{"/old/channel.js"}},
	}}
	raw, _ := json.Marshal(stale)
	_ = os.WriteFile(filepath.Join(fakeHome, ".mcp.json"), raw, 0644)

	updated, err := SweepUserScopeMCPConfig("/new/channel.js", nil)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !updated {
		t.Error("expected updated=true for stale JS entry")
	}

	out, _ := os.ReadFile(filepath.Join(fakeHome, ".mcp.json"))
	var cfg MCPProjectConfig
	_ = json.Unmarshal(out, &cfg)
	dw := cfg.MCPServers["datawatch"]
	if len(dw.Args) != 1 || dw.Args[0] != "/new/channel.js" {
		t.Errorf("stale entry not updated: args=%+v", dw.Args)
	}
}
