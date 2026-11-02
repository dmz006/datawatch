package proxy

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func TestNewPool(t *testing.T) {
	servers := []config.RemoteServerConfig{
		{Name: "prod", URL: "http://localhost:9999", Enabled: true},
		{Name: "disabled", URL: "http://localhost:9998", Enabled: false},
	}
	p := NewPool(servers, DefaultPoolConfig())
	if p == nil {
		t.Fatal("expected non-nil pool")
	}
	// Only enabled servers should have clients
	if p.Client("prod") == nil {
		t.Error("expected client for enabled server 'prod'")
	}
	if p.Client("disabled") != nil {
		t.Error("expected no client for disabled server")
	}
	if p.Client("nonexistent") != nil {
		t.Error("expected no client for unknown server")
	}
}

func TestPool_CircuitBreaker(t *testing.T) {
	servers := []config.RemoteServerConfig{
		{Name: "test", URL: "http://localhost:9999", Enabled: true},
	}
	cfg := DefaultPoolConfig()
	cfg.CircuitBreakerThreshold = 2
	cfg.CircuitBreakerReset = 100 * time.Millisecond
	p := NewPool(servers, cfg)

	// Initially healthy
	if !p.IsHealthy("test") {
		t.Error("server should start healthy")
	}

	// One failure — still healthy
	p.RecordFailure("test", errTest)
	if !p.IsHealthy("test") {
		t.Error("one failure should not trip breaker (threshold=2)")
	}

	// Second failure — breaker trips
	p.RecordFailure("test", errTest)
	if p.IsHealthy("test") {
		t.Error("two failures should trip breaker")
	}

	// Health snapshot shows breaker open
	health := p.Health()
	if len(health) != 1 {
		t.Fatalf("expected 1 health entry, got %d", len(health))
	}
	if health[0].Healthy {
		t.Error("health should show unhealthy")
	}
	if !health[0].BreakerOpen {
		t.Error("health should show breaker open")
	}

	// Wait for breaker reset
	time.Sleep(150 * time.Millisecond)
	// After reset time, IsHealthy should allow probing (half-open)
	// But Healthy is still false until RecordSuccess
	health2 := p.Health()
	if health2[0].BreakerOpen {
		t.Error("breaker should be half-open after reset time")
	}

	// Record success — full recovery
	p.RecordSuccess("test")
	if !p.IsHealthy("test") {
		t.Error("server should be healthy after RecordSuccess")
	}
}

func TestPool_RecordSuccess_ClearsErrors(t *testing.T) {
	servers := []config.RemoteServerConfig{
		{Name: "s1", URL: "http://localhost:9999", Enabled: true},
	}
	p := NewPool(servers, DefaultPoolConfig())
	p.RecordFailure("s1", errTest)
	p.RecordSuccess("s1")

	health := p.Health()
	if health[0].ConsecFails != 0 {
		t.Errorf("ConsecFails = %d, want 0 after success", health[0].ConsecFails)
	}
	if health[0].LastError != "" {
		t.Errorf("LastError = %q, want empty after success", health[0].LastError)
	}
}

var errTest = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test error" }
