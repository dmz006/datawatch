package channel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbe_MissingNodeModulesEmitsHint(t *testing.T) {
	dir := t.TempDir()
	res := Probe(dir)
	if res.Ready {
		t.Fatalf("expected not ready (no node_modules) — got Ready=true; %+v", res)
	}
	if res.Hint == "" {
		t.Fatalf("expected non-empty hint when not ready, got empty")
	}
}

func TestProbe_NativeBridgePreemptsJSPath(t *testing.T) {
	dir := t.TempDir()
	// Drop a fake datawatch-channel binary into <dir>/channel/.
	chDir := filepath.Join(dir, "channel")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	binPath := filepath.Join(chDir, "datawatch-channel")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	res := Probe(dir)
	if !res.Ready {
		t.Fatalf("expected Ready=true with native bridge present, got %+v", res)
	}
	if res.Hint == "" || !contains(res.Hint, "native Go bridge") {
		t.Errorf("hint should mention native Go bridge; got %q", res.Hint)
	}
}

func TestBinaryPath_RespectsEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "my-channel")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("DATAWATCH_CHANNEL_BIN", bin)
	if got := BinaryPath(""); got != bin {
		t.Errorf("BinaryPath = %q want %q", got, bin)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestProbe_NodeModulesPresentReadyDependsOnTooling(t *testing.T) {
	dir := t.TempDir()
	nm := filepath.Join(dir, "channel", "node_modules", "@modelcontextprotocol")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	res := Probe(dir)
	if !res.NodeModules {
		t.Fatalf("expected NodeModules=true after creating dir, got false")
	}
	// Ready is true only when node + npm + node_modules all line up.
	// On CI hosts with node available this should pass; otherwise the
	// hint must still be set.
	if !res.Ready && res.Hint == "" {
		t.Fatalf("not ready but no hint: %+v", res)
	}
}
