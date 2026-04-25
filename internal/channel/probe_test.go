package channel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbe_MissingNodeModulesEmitsHint(t *testing.T) {
	dir := t.TempDir()
	// Isolate from a real datawatch-channel install on the test host —
	// otherwise Probe legitimately returns Ready=true via BinaryPath
	// PATH lookup. Point PATH at an empty dir and clear the override.
	t.Setenv("DATAWATCH_CHANNEL_BIN", "")
	t.Setenv("PATH", t.TempDir())
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

func TestLegacyJSArtifacts_FindsAndRemoves(t *testing.T) {
	dir := t.TempDir()
	chDir := filepath.Join(dir, "channel")
	if err := os.MkdirAll(filepath.Join(chDir, "node_modules", "@modelcontextprotocol"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"channel.js", "package.json", "package-lock.json"} {
		if err := os.WriteFile(filepath.Join(chDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	got := LegacyJSArtifacts(dir)
	if len(got) != 4 {
		t.Fatalf("expected 4 legacy artifacts, got %d (%v)", len(got), got)
	}
	removed := RemoveLegacyJSArtifacts(dir)
	if len(removed) != 4 {
		t.Fatalf("expected to remove 4 artifacts, got %d (%v)", len(removed), removed)
	}
	if got := LegacyJSArtifacts(dir); len(got) != 0 {
		t.Fatalf("expected 0 artifacts after cleanup, got %d (%v)", len(got), got)
	}
	// Idempotent: second cleanup is a no-op.
	if removed := RemoveLegacyJSArtifacts(dir); len(removed) != 0 {
		t.Fatalf("second cleanup should remove 0, got %d", len(removed))
	}
}

func TestLegacyJSArtifacts_EmptyDataDirReturnsNil(t *testing.T) {
	if got := LegacyJSArtifacts(""); got != nil {
		t.Fatalf("expected nil for empty dataDir, got %v", got)
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
	// Force the JS-path probe (not BinaryPath short-circuit). See
	// TestProbe_MissingNodeModulesEmitsHint for the rationale.
	t.Setenv("DATAWATCH_CHANNEL_BIN", "")
	t.Setenv("PATH", t.TempDir())
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
