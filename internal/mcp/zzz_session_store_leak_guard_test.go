package mcp

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

// The zz_ file name puts this test last in the package: it runs after every
// other mcp test, so any session store still open at that point is named
// instead of surfacing as a TempDir cleanup failure or a stray flush
// warning.
func TestZZGuardNoSessionStoreOutlivesItsTest(t *testing.T) {
	if descs := session.LiveStores(); len(descs) > 0 {
		t.Fatalf("session store(s) outlived their test and their background flush may still write into deleted TempDirs: %s",
			strings.Join(descs, "; "))
	}
}
