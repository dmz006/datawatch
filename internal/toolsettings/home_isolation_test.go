package toolsettings

import (
	"os"
	"testing"

	"github.com/dmz006/datawatch/internal/testhome"
)

// TestMain gives the package its own HOME, temp folder and tmux folder.
func TestMain(m *testing.M) {
	os.Exit(testhome.RunPrivateTmp(m))
}
