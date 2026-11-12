package ollama

import (
	"sync"
	"time"
)

// RequestRecord describes one daemon-observed HTTP chat request, for the
// trace recorder. It carries no prompt or response text.
type RequestRecord struct {
	Session    string
	Model      string
	TokensIn   int
	TokensOut  int
	Start      time.Time
	Duration   time.Duration
	StatusCode int   // 0 when no response was received
	Err        error // transport or HTTP failure, nil on success
}

var (
	recorderMu sync.RWMutex
	recorderFn func(RequestRecord)
	recordNow  = time.Now
)

// SetRecorder registers the trace recorder hook; nil clears it. The hook is
// invoked on its own goroutine so it can never block a chat turn.
func SetRecorder(fn func(RequestRecord)) {
	recorderMu.Lock()
	recorderFn = fn
	recorderMu.Unlock()
}

// emitRecord delivers rec to the recorder without blocking the caller.
func emitRecord(rec RequestRecord, now time.Time) {
	recorderMu.RLock()
	fn := recorderFn
	recorderMu.RUnlock()
	if fn == nil {
		return
	}
	rec.Duration = now.Sub(rec.Start)
	go func() {
		defer func() { _ = recover() }()
		fn(rec)
	}()
}
