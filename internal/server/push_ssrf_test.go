package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func TestValidatePushEndpoint_RejectsLoopbackAlways(t *testing.T) {
	err := validatePushEndpoint("https://127.0.0.1:9999/push", config.PushConfig{})
	if err == nil {
		t.Fatal("expected loopback endpoint to be rejected")
	}
}

func TestValidatePushEndpoint_RejectsCloudMetadataAlways(t *testing.T) {
	// 169.254.169.254 doesn't resolve via a hostname lookup in this test
	// (no DNS record), so validate the IP classifier directly -- the real
	// blocking happens at dial time either way (pushHTTPClient), this just
	// confirms the classifier itself flags it.
	if reason := blockedIPReason(net.ParseIP("169.254.169.254")); reason == "" {
		t.Fatal("expected cloud metadata address to be classified as blocked")
	}
}

func TestValidatePushEndpoint_RejectsHTTPByDefault(t *testing.T) {
	err := validatePushEndpoint("http://push.example.com/endpoint", config.PushConfig{})
	if err == nil {
		t.Fatal("expected http:// to be rejected when AllowInsecureEndpoints is false")
	}
	if !strings.Contains(err.Error(), "allow_insecure_endpoints") {
		t.Fatalf("error should mention the config flag that would allow this, got: %v", err)
	}
}

func TestValidatePushEndpoint_AllowsHTTPWhenOptedIn(t *testing.T) {
	err := validatePushEndpoint("http://push.example.com/endpoint", config.PushConfig{AllowInsecureEndpoints: true})
	if err != nil {
		t.Fatalf("expected http:// to be allowed with AllowInsecureEndpoints=true, got: %v", err)
	}
}

func TestValidatePushEndpoint_PrivateRangeAllowedByDefault(t *testing.T) {
	// This is the exact case datawatch-app flagged: self-hosted ntfy/Gotify
	// on a LAN or Tailscale address must keep working with default config.
	if reason := privateIPReason(net.ParseIP("192.168.1.50")); reason == "" {
		t.Fatal("expected 192.168.1.50 to be classified as private")
	}
	// But validatePushEndpoint must NOT reject it when BlockPrivateEndpoints is false.
	err := validatePushEndpoint("https://192.168.1.50:8080/UP", config.PushConfig{})
	if err != nil {
		t.Fatalf("private-range endpoint must be allowed by default, got: %v", err)
	}
}

func TestValidatePushEndpoint_TailscaleCGNATAllowedByDefault(t *testing.T) {
	if !isTailscaleCGNAT(net.ParseIP("100.100.1.2")) {
		t.Fatal("expected 100.100.1.2 to be classified as Tailscale CGNAT")
	}
	err := validatePushEndpoint("https://100.100.1.2/UP", config.PushConfig{})
	if err != nil {
		t.Fatalf("Tailscale CGNAT endpoint must be allowed by default, got: %v", err)
	}
}

func TestValidatePushEndpoint_PrivateRangeBlockedWhenOptedIn(t *testing.T) {
	err := validatePushEndpoint("https://192.168.1.50:8080/UP", config.PushConfig{BlockPrivateEndpoints: true})
	if err == nil {
		t.Fatal("expected private-range endpoint to be rejected when BlockPrivateEndpoints is true")
	}
}

func TestIsSSEMarkerRegistration(t *testing.T) {
	if isSSEMarkerRegistration(pushRegistration{Endpoint: "https://x/UP"}) {
		t.Fatal("a registration with no client_id must NOT be classified as an SSE marker")
	}
	if !isSSEMarkerRegistration(pushRegistration{ClientID: "abc", Endpoint: "https://self/api/push/alerts"}) {
		t.Fatal("a registration with client_id set must be classified as an SSE marker")
	}
}

// TestPublishToEndpoint_SkipsSSEMarkers confirms the fan-out loop in
// publishToTopic never dials an SSE-marker registration -- that's the
// actual security property (nothing about validatePushEndpoint applies to
// it, because it's never reached at all).
func TestPublishToEndpoint_SkipsSSEMarkers(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()

	globalPushHub.mu.Lock()
	saved := globalPushHub.registered
	globalPushHub.registered = []pushRegistration{
		{ID: "sse", ClientID: "mobile-1", Endpoint: srv.URL}, // marker -- must never be dialed
	}
	globalPushHub.mu.Unlock()
	savedClientFn := newPushHTTPClient
	newPushHTTPClient = func(config.PushConfig) *http.Client { return &http.Client{Timeout: 2 * time.Second} }
	defer func() {
		globalPushHub.mu.Lock()
		globalPushHub.registered = saved
		globalPushHub.mu.Unlock()
		newPushHTTPClient = savedClientFn
	}()

	PublishToTopic("alerts", PushEvent{Message: "m"})
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("SSE-marker registration was dialed %d time(s), want 0", got)
	}
}

// TestPushHTTPClient_RefusesLoopbackAtDialTime is the real security
// boundary test: even if a bad endpoint somehow got past registration-time
// validation (e.g. DNS rebinding), the dial-time Control hook must still
// refuse it.
func TestPushHTTPClient_RefusesLoopbackAtDialTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request must never reach the handler -- dial should have been refused")
	}))
	defer srv.Close()

	client := pushHTTPClient(config.PushConfig{})
	req, err := http.NewRequest(http.MethodPost, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close() //nolint:errcheck
		t.Fatal("expected dial to loopback to be refused, got a successful response")
	}
}
