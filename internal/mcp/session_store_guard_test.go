package mcp

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

// trackSessionManager owns the flush lifecycle of the Manager a test
// constructs: call it right after session.NewManager succeeds (or the
// fixture that did). The t.Cleanup LIFO order stops the store's background
// flush BEFORE the test's t.TempDir is deleted — without it the deferred
// writer fires into a deleted directory ("TempDir ... directory not empty"
// on a bystander test, plus "acquire sessions lock ... no such file or
// directory" flush warnings), and if the teardown itself fails, this test
// fails by name instead of the leak surfacing unattributed.
// Manager.Close is idempotent, so tests that already close are unaffected.
func trackSessionManager(t testing.TB, m *session.Manager) {
	t.Helper()
	st := m.Store()
	if st == nil {
		return
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Errorf("stop session store flush of %s (store %s): %v", t.Name(), st.Path(), err)
			return
		}
		// Close must stop the background flush and wait for the in-flight
		// writer. If it is still up, the teardown failed and the store will
		// only ever surface as the unattributed TempDir race on some other
		// test — say so here, in the test that owns the store.
		if st.WriterRunning() {
			t.Errorf("%s outlived its session store: the background flush survived a Close of %s. Live stores: %s",
				t.Name(), st.Path(), strings.Join(session.LiveStores(), "; "))
		}
	})
}
