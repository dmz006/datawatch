package server

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
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
	defer func() {
		globalPushHub.mu.Lock()
		globalPushHub.registered = saved
		globalPushHub.mu.Unlock()
	}()

	PublishToTopics([]string{"session-x", "alerts"}, PushEvent{Message: "m"})
	time.Sleep(200 * time.Millisecond)
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("endpoint deliveries = %d, want 1", got)
	}
}
