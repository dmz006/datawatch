package shell

import (
	"sync"
	"time"
)

// CommandRecord describes one command the daemon started in a shell
// session. ExitCode is nil until the daemon's session record learns it.
type CommandRecord struct {
	Session  string
	Command  string
	ExitCode *int
	Start    time.Time
}

var (
	recorderMu sync.RWMutex
	recorderFn func(CommandRecord)
)

// SetRecorder registers the trace recorder hook; nil clears it. The hook
// runs on its own goroutine so it can never block a launch.
func SetRecorder(fn func(CommandRecord)) {
	recorderMu.Lock()
	recorderFn = fn
	recorderMu.Unlock()
}

func emitRecord(rec CommandRecord) {
	recorderMu.RLock()
	fn := recorderFn
	recorderMu.RUnlock()
	if fn == nil {
		return
	}
	go func() {
		defer func() { _ = recover() }()
		fn(rec)
	}()
}
