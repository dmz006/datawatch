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
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "dw-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "home isolation:", err)
		os.Exit(1)
	}
	os.Setenv("HOME", home) //nolint:errcheck
	code := m.Run()
	os.RemoveAll(home) //nolint:errcheck
	os.Exit(code)
}
