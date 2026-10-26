package session

import (
	"sync"

	"github.com/dmz006/datawatch/internal/llm"
)

// sharedBackendLaunch serialises the span from configuring a shared backend to
// handing it the launch. A registry backend is one object for every session
// that uses it, and its per-session settings (node host, model, session name)
// are plain fields set just before Launch reads them. Two sessions starting at
// once would overwrite each other's settings, and a pi worker would then run
// against another worker's compute node instead of the one its lease names.
var sharedBackendLaunch sync.Mutex

// lockSharedBackend takes the launch lock for a backend whose launch reads
// per-session settings from the shared object (the pi family, which takes a
// node host). It returns an unlock that is safe to call more than once, so a
// caller can release it right after the launch and still defer it for the
// early returns. Other backends get a no-op.
func lockSharedBackend(b llm.Backend) (unlock func()) {
	if _, ok := b.(interface{ SetNodeHost(string) }); !ok {
		return func() {}
	}
	sharedBackendLaunch.Lock()
	var once sync.Once
	return func() { once.Do(sharedBackendLaunch.Unlock) }
}
