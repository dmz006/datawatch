package memory

import (
	"os"
	"testing"

	"github.com/dmz006/datawatch/internal/testhome"
)

// TestMain gives the package its own HOME, temp folder and tmux folder, so no
// test reads or writes outside them (scripts/check-test-isolation.sh).
func TestMain(m *testing.M) {
	os.Exit(testhome.RunPrivateTmp(m))
}
