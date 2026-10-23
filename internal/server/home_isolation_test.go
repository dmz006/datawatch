package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/testhome"
)

// TestMain points HOME at a throwaway directory for the whole package.
// Session starts with an empty project_dir resolve to os.UserHomeDir(), and
// the pre-launch hooks write CLAUDE.md / .mcp.json into it — without this,
// every go test run overwrote the operator's real ~/CLAUDE.md (B108).
// It also isolates the multiplexer: TMUX is unset and TMUX_TMPDIR is
// pointed at a throwaway directory, so nothing in the test binary can
// reach the operator's tmux server (see internal/testhome).
func TestMain(m *testing.M) {
	os.Exit(testhome.Run(m))
}

// TestTmuxEnvIsolated proves the test binary is scrubbed of every path to
// the operator's multiplexer: no attached client (TMUX empty) and a socket
// directory under os.TempDir() that testhome created for this run.
func TestTmuxEnvIsolated(t *testing.T) {
	if got := os.Getenv("TMUX"); got != "" {
		t.Fatalf("TMUX = %q, want empty: the test binary must never be able to reach the operator's tmux multiplexer", got)
	}
	tmpdir := os.Getenv("TMUX_TMPDIR")
	if tmpdir == "" {
		t.Fatal("TMUX_TMPDIR is empty, want the throwaway socket dir testhome creates")
	}
	fi, err := os.Stat(tmpdir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("TMUX_TMPDIR %q is not a directory: %v", tmpdir, err)
	}
	under := underDir(os.TempDir(), tmpdir, t)
	if !under {
		t.Fatalf("TMUX_TMPDIR = %q, want a path under os.TempDir() %s", tmpdir, os.TempDir())
	}
	// The dir must be this run's, not some pre-existing temp path a caller
	// set before running the test.
	if !testhome.TmuxDirOwned(tmpdir) {
		t.Fatalf("TMUX_TMPDIR %q was not created by this test run (missing testhome marker)", tmpdir)
	}
}

// underDir reports whether child is inside (or equal to) parent.
func underDir(parent, child string, t *testing.T) bool {
	t.Helper()
	p, err := filepath.Abs(parent)
	if err != nil {
		return false
	}
	c, err := filepath.Abs(child)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(p, c)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
