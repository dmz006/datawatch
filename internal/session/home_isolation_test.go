package session

import (
	"os"
	"testing"

	"github.com/dmz006/datawatch/internal/testhome"
)

// TestMain points HOME at a throwaway directory for the whole package.
// Session starts with an empty project_dir resolve to os.UserHomeDir(), and
// the pre-launch hooks write CLAUDE.md / .mcp.json into it — without this,
// every go test run overwrote the operator's real ~/CLAUDE.md (B108).
// See internal/testhome for the cleanup and module-cache handling.
//
// realHome keeps the HOME this process started with, for tests that run the
// go toolchain as a child: it must not inherit the throwaway HOME, or its
// module cache (read-only files) lands inside it and the cleanup cannot
// remove it.
var realHome string

func TestMain(m *testing.M) {
	realHome = os.Getenv("HOME")
	// Most tests read sessions.json straight after a Save; run them
	// write-through. Tests of the deferred writer pass an explicit interval.
	DefaultFlushInterval = 0
	os.Exit(testhome.Run(m))
}
