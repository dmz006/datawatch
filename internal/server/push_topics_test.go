package server

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func TestPublishToTopics_RegisteredEndpointGetsOneDelivery(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()

	globalPushHub.mu.Lock()
	saved := globalPushHub.registered
	globalPushHub.registered = []pushRegistration{{ID: "t", Endpoint: srv.URL}}
	globalPushHub.mu.Unlock()
	// BL394 -- httptest.NewServer binds to loopback, which the real
	// SSRF-guarded client (pushHTTPClient) now always refuses to dial.
	// That's correct for production; swap in a plain client here since
	// this test is about fanout delivery, not the SSRF guard itself
	// (see push_ssrf_test.go for that).
	savedClientFn := newPushHTTPClient
	newPushHTTPClient = func(config.PushConfig) *http.Client { return &http.Client{Timeout: 2 * time.Second} }
	defer func() {
		globalPushHub.mu.Lock()
		globalPushHub.registered = saved
		globalPushHub.mu.Unlock()
		newPushHTTPClient = savedClientFn
	}()

	PublishToTopics([]string{"session-x", "alerts"}, PushEvent{Message: "m"})
	time.Sleep(200 * time.Millisecond)
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("endpoint deliveries = %d, want 1", got)
	}
}
