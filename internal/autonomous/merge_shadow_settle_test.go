package autonomous

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/mergeshadow"
)

// settleMergeShadowCompares blocks until each shipped Automation in ids has
// the comparison record the post-ship hook stores — the hook always stores
// one, a real comparison or the no-plan reason record, so the record's
// presence is the completion signal. Tests call this before they return so
// their t.TempDir() cleanup cannot race the hook's background write.
func settleMergeShadowCompares(t *testing.T, dir string, ids ...string) {
	t.Helper()
	if len(ids) == 0 {
		return
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		pending := 0
		for _, id := range ids {
			if _, err := mergeshadow.LoadComparison(dir, id); err != nil {
				pending++
			}
		}
		if pending == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("merge shadow post-ship hook: no comparison record stored for %d of %d Automation(s) within 5s", pending, len(ids))
		}
		time.Sleep(20 * time.Millisecond)
	}
}
