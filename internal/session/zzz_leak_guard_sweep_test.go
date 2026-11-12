package session

import "testing"

// TestZZLeakGuardNoStoreOutlivesItsTest is the last test in the package
// (zz_ prefix): any Store the package's tests opened and returned without
// Close is still registered here. Such a store owns a background flush
// writer that will keep hitting deleted t.TempDir directories ("TempDir
// RemoveAll ... directory not empty" on a bystander test, "sessions store
// flush: acquire sessions lock ... no such file or directory" warnings).
// Close the store in the test that created it; the message names the
// leaking test from the creation call stack.
func TestZZLeakGuardNoStoreOutlivesItsTest(t *testing.T) {
	live := LiveStores()
	if len(live) == 0 {
		return
	}
	t.Fatalf("session store(s) outlived their test and their background flush may still write into deleted TempDirs; Close() each store (or Manager) before returning — e.g. t.Cleanup: %s", live)
}
