package session

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points HOME at a throwaway directory for the whole package.
// Session starts with an empty project_dir resolve to os.UserHomeDir(), and
// the pre-launch hooks write CLAUDE.md / .mcp.json into it — without this,
// every go test run overwrote the operator's real ~/CLAUDE.md (B108).
//
// realHome keeps the HOME this process started with, for tests that run the
// go toolchain as a child: it must not inherit the throwaway HOME, or its
// module cache (read-only files) lands inside it and the cleanup below cannot
// remove it.
var realHome string

func TestMain(m *testing.M) {
	realHome = os.Getenv("HOME")
	home, err := os.MkdirTemp("", "dw-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "home isolation:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(home) //nolint:errcheck
	os.Setenv("HOME", home)  //nolint:errcheck
	code := m.Run()
	os.RemoveAll(home) //nolint:errcheck
	os.Exit(code)
}
