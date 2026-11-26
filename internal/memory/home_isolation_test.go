package memory

import (
	"os"
	"testing"

	"github.com/dmz006/datawatch/internal/testhome"
	"github.com/dmz006/datawatch/internal/toolsettings"
)

// TestMain gives the package its own HOME, temp folder and tmux folder, so no
// test reads or writes outside them (scripts/check-test-isolation.sh). The hook
// installer also refuses the account's own home, which HOME does not move: here
// that is the test's HOME too, and no CLAUDE_CONFIG_DIR is inherited.
func TestMain(m *testing.M) {
	toolsettings.AccountHome = func() string { return os.Getenv("HOME") }
	if err := os.Unsetenv("CLAUDE_CONFIG_DIR"); err != nil {
		panic(err)
	}
	os.Exit(testhome.RunPrivateTmp(m))
}
