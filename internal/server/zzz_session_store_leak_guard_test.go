package server

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

// The file name (zz_) puts this test last in the package: it runs after
// every other server test, so any session store that outlived its test —
// even one created without trackSessionManager — is still open here and
// gets named instead of surfacing later as a TempDir "directory not
// empty" cleanup failure on an innocent bystander.
func TestZZGuardNoSessionStoreOutlivesItsTest(t *testing.T) {
	if descs := session.LiveStores(); len(descs) > 0 {
		t.Fatalf("session store(s) outlived their test and their background flush may still write into deleted TempDirs: %s",
			strings.Join(descs, "; "))
	}
}
