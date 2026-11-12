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
	// TaskFile is the absolute path of the task file when the task was
	// passed to the script by file. It is empty when the task was passed
	// inline.
	TaskFile string
}

var (
	recorderMu sync.RWMutex
	recorderFn func(CommandRecord)
	recordNow  = time.Now
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

// ReportExit tells the recorder that a command the daemon started in a
// session finished with the given exit code, taken from the daemon's own
// session record. It is non-blocking and a no-op without a recorder.
func ReportExit(session, command string, code int, start time.Time) {
	c := code
	emitRecord(CommandRecord{Session: session, Command: command, ExitCode: &c, Start: start})
}
