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
