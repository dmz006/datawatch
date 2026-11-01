package server

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

// trackSessionManager registers the teardown a test owes the Manager it
// constructed. Call it right after session.NewManager succeeds, i.e.
// after (not before) the t.TempDir() the manager's store lives in, so the
// t.Cleanup LIFO order stops the flush BEFORE the TempDir RemoveAll runs.
//
// Without this, the store's deferred background writer fires up to one
// flushInterval after the test ends, into a directory t.TempDir is
// removing: "sessions store flush: acquire sessions lock ... no such file
// or directory" warnings under some tests, and the
// "TempDir RemoveAll cleanup: ... directory not empty" failure lands on
// whichever test happened to be running when the flush hit disk — not the
// one that leaked.
//
// It is also the leak guard: if Close fails to stop the background flush,
// the helper fails THIS test by name instead of letting the leak surface as
// the unattributed TempDir race on a bystander test. Manager.Close is
// idempotent, so tests that already close their manager get a no-op here.
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

