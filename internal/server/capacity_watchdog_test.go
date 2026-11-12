package server

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// syncBuf is a goroutine-safe log sink.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// A ledger wedged inside a lock-held callback makes /api/capacity log the
// wedge exactly once while the request is pending, then answer normally once
// the callback is released.
func TestHandleCapacity_LockWatchdog(t *testing.T) {
	var wedge atomic.Bool
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	l := capacity.New(capacity.Options{
		External: func(string) int {
			if wedge.Load() {
				entered <- struct{}{}
				<-release
			}
			return 0
		},
	})
	l.SetLimit("host", 2)

	s := &Server{capacityWatchdogThreshold: 50 * time.Millisecond}
	s.SetCapacity(l)

	buf := &syncBuf{}
	log.SetOutput(buf)
	defer log.SetOutput(os.Stderr)

	wedge.Store(true)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
		done <- rec
	}()
	<-entered

	const marker = "capacity ledger lock held for"
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(buf.String(), marker) {
		time.Sleep(10 * time.Millisecond)
	}
	// Linger past several thresholds: the report must not repeat.
	time.Sleep(200 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("handler returned while the ledger was wedged")
	default:
	}
	if n := strings.Count(buf.String(), marker); n != 1 {
		t.Fatalf("wedge logged %d times, want 1\n%s", n, buf.String())
	}

	wedge.Store(false)
	close(release)
	select {
	case rec := <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not return after the callback was released")
	}
}
