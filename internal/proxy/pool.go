// Package proxy provides remote datawatch server communication for proxy mode.
// pool.go implements HTTP client pooling, circuit breaker, and background health checks.
package proxy

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// PoolConfig controls connection pool and resilience behaviour.
type PoolConfig struct {
	// RequestTimeout per remote request (default 10s).
	RequestTimeout time.Duration
	// CircuitBreakerThreshold — consecutive failures before marking server down (default 3).
	CircuitBreakerThreshold int
	// CircuitBreakerReset — how long a tripped breaker stays open (default 30s).
	CircuitBreakerReset time.Duration
	// HealthInterval — how often to poll /healthz on remote servers (default 30s).
	HealthInterval time.Duration
}

// DefaultPoolConfig returns sensible defaults.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		RequestTimeout:          10 * time.Second,
		CircuitBreakerThreshold: 3,
		CircuitBreakerReset:     30 * time.Second,
		HealthInterval:          30 * time.Second,
	}
}

// ServerHealth tracks a single remote server's health state.
type ServerHealth struct {
	Name          string    `json:"name"`
	URL           string    `json:"url"`
	Healthy       bool      `json:"healthy"`
	LastCheck     time.Time `json:"last_check"`
	LastError     string    `json:"last_error,omitempty"`
	ConsecFails   int       `json:"consec_fails"`
	BreakerOpen   bool      `json:"breaker_open"`
	BreakerUntil  time.Time `json:"breaker_until,omitempty"`
}

// Pool manages HTTP clients and circuit breakers for remote servers.
type Pool struct {
	cfg     PoolConfig
	servers []config.RemoteServerConfig

	mu      sync.RWMutex
	health  map[string]*ServerHealth // name → health
	clients map[string]*http.Client  // name → persistent client

	stopCh chan struct{}
	stopOnce sync.Once
}

// NewPool creates a connection pool with health tracking for each server.
func NewPool(servers []config.RemoteServerConfig, cfg PoolConfig) *Pool {
	p := &Pool{
		cfg:     cfg,
		servers: servers,
		health:  make(map[string]*ServerHealth, len(servers)),
		clients: make(map[string]*http.Client, len(servers)),
		stopCh:  make(chan struct{}),
	}
	for _, sv := range servers {
		if !sv.Enabled {
			continue
		}
		p.health[sv.Name] = &ServerHealth{
			Name:    sv.Name,
			URL:     sv.URL,
			Healthy: true, // optimistic start
		}
		p.clients[sv.Name] = &http.Client{
			Timeout: cfg.RequestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 5,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	}
	return p
}

// Start begins background health checking. Call Stop() to shut down.
func (p *Pool) Start() {
	go p.healthLoop()
}

// Stop terminates the background health checker.
func (p *Pool) Stop() {
	p.stopOnce.Do(func() { close(p.stopCh) })
}

// Client returns the persistent HTTP client for a server.
// Returns nil if the server is unknown or disabled.
func (p *Pool) Client(serverName string) *http.Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.clients[serverName]
}

// IsHealthy returns whether a server is reachable and the breaker is closed.
func (p *Pool) IsHealthy(serverName string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	h, ok := p.health[serverName]
	if !ok {
		return false
	}
	if h.BreakerOpen && time.Now().Before(h.BreakerUntil) {
		return false
	}
	return h.Healthy
}

// RecordSuccess records a successful request to a server, closing the breaker if open.
func (p *Pool) RecordSuccess(serverName string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	h, ok := p.health[serverName]
	if !ok {
		return
	}
	h.ConsecFails = 0
	h.Healthy = true
	h.BreakerOpen = false
	h.LastError = ""
}

// RecordFailure records a failed request. If consecutive failures exceed the
// threshold, the circuit breaker trips and the server is marked unhealthy.
func (p *Pool) RecordFailure(serverName string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	h, ok := p.health[serverName]
	if !ok {
		return
	}
	h.ConsecFails++
	h.LastError = err.Error()
	if h.ConsecFails >= p.cfg.CircuitBreakerThreshold {
		h.Healthy = false
		h.BreakerOpen = true
		h.BreakerUntil = time.Now().Add(p.cfg.CircuitBreakerReset)
		log.Printf("[proxy-pool] circuit breaker OPEN for %s after %d failures: %v",
			serverName, h.ConsecFails, err)
	}
}

// Health returns a snapshot of all server health states.
func (p *Pool) Health() []ServerHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]ServerHealth, 0, len(p.health))
	for _, h := range p.health {
		snap := *h
		// If breaker expired, report as half-open (next request is a probe)
		if snap.BreakerOpen && time.Now().After(snap.BreakerUntil) {
			snap.BreakerOpen = false
		}
		result = append(result, snap)
	}
	return result
}

// healthLoop periodically checks /healthz on each remote server.
func (p *Pool) healthLoop() {
	ticker := time.NewTicker(p.cfg.HealthInterval)
	defer ticker.Stop()

	// Run an initial check immediately
	p.checkAll()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.checkAll()
		}
	}
}

func (p *Pool) checkAll() {
	for _, sv := range p.servers {
		if !sv.Enabled {
			continue
		}
		go p.checkServer(sv)
	}
}

func (p *Pool) checkServer(sv config.RemoteServerConfig) {
	client := p.Client(sv.Name)
	if client == nil {
		return
	}

	healthURL := fmt.Sprintf("%s/healthz", strings.TrimRight(sv.URL, "/"))
	req, err := http.NewRequest(http.MethodGet, healthURL, nil)
	if err != nil {
		p.RecordFailure(sv.Name, err)
		return
	}
	if sv.Token != "" {
		req.Header.Set("Authorization", "Bearer "+sv.Token)
	}

	resp, err := client.Do(req)
	if err != nil {
		p.RecordFailure(sv.Name, err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		wasDown := !p.IsHealthy(sv.Name)
		p.RecordSuccess(sv.Name)
		if wasDown {
			log.Printf("[proxy-pool] server %s recovered (healthz OK)", sv.Name)
		}
	} else {
		p.RecordFailure(sv.Name, fmt.Errorf("healthz returned %d", resp.StatusCode))
	}

	p.mu.Lock()
	if h, ok := p.health[sv.Name]; ok {
		h.LastCheck = time.Now()
	}
	p.mu.Unlock()
}
