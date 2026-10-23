package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestSeededLocalServerServe serves the seeded live-style fixture over plain
// HTTP on PERF_SERVE_ADDR (e.g. 127.0.0.1:18463) so browser-measurement
// drivers (scripts/test-stories/pwa, scripts/perf-measure-tabs.mjs) can
// measure real PWA tab loads against the same data set the Go-side
// endpoint measurements use (SeedPerfServer in perf_seed_test.go).
//
// It registers only the read-only GET routes the PWA renders for its tabs
// (Sessions / Automata / Automation / Observer / Settings) plus /ws and the
// embedded static PWA, and a permissive catch-all for every other /api/*
// path (the Settings and Observer tabs fan out to a long tail of registry
// endpoints whose stores are not part of this fixture). The admin token is
// deliberately unset so requests pass the auth middleware as
// unauthenticated (loopback test server only).
//
// The test blocks until the go-test process is signalled (SIGINT/SIGTERM)
// or PERF_SERVE_STOP_MS elapses, whichever comes first — it never times
// itself out on its own, so the measurement driver owns the lifetime.
func TestSeededLocalServerServe(t *testing.T) {
	addr := os.Getenv("PERF_SERVE_ADDR")
	if addr == "" {
		t.Skip("PERF_SERVE_ADDR not set")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hub := NewHub()
	go hub.Run()

	seed := SeedPerfServer(t, t.TempDir())
	seed.Server.hub = hub
	api := seed.Server

	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	// Public routes.
	mux.HandleFunc("/api/health", api.handleHealth)
	// Tab data endpoints (real fixture-backed handlers).
	mux.HandleFunc("/api/sessions", api.handleSessions)
	mux.HandleFunc("/api/sessions/stale", api.handleSessionsStale)
	mux.HandleFunc("/api/autonomous/prds", api.handleAutonomousPRDs)
	mux.HandleFunc("/api/autonomous/prds/", api.handleAutonomousPRDs)
	mux.HandleFunc("/api/autonomous/learnings", api.handleAutonomousLearnings)
	mux.HandleFunc("/api/autonomous/subscriptions", api.handleAutonomousSubscriptions)
	mux.HandleFunc("/api/autonomous/guardrails", api.handleAutonomousGuardrails)
	mux.HandleFunc("/api/autonomous/guardrail_profiles", api.handleAutonomousGuardrailProfiles)
	mux.HandleFunc("/api/autonomous/events", api.handleAutonomousEvents)
	mux.HandleFunc("/api/autonomous/templates", api.handleAutonomousTemplates)
	mux.HandleFunc("/api/autonomous/types", api.handleAutonomousTypes)
	mux.HandleFunc("/api/autonomous/tags", api.handleAutonomousTags)
	mux.HandleFunc("/api/autonomous/config", api.handleAutonomousConfig)
	mux.HandleFunc("/api/autonomous/status", api.handleAutonomousStatus)
	mux.HandleFunc("/api/automations", api.handleAutomations)
	mux.HandleFunc("/api/automations/", api.handleAutomations)
	mux.HandleFunc("/api/alerts", api.handleAlerts)
	mux.HandleFunc("/api/alerts/count", api.handleAlertsCount)
	mux.HandleFunc("/api/observer/stats", api.handleObserverStats)
	mux.HandleFunc("/api/observer/envelopes", api.handleObserverEnvelopes)
	mux.HandleFunc("/api/observer/envelopes/", api.handleObserverEnvelopes)
	mux.HandleFunc("/api/observer/config", api.handleObserverConfig)
	mux.HandleFunc("/api/config", api.handleConfig)
	mux.HandleFunc("/api/info", api.handleInfo)
	mux.HandleFunc("/api/backends", api.handleBackends)
	mux.HandleFunc("/api/usage-backoff", api.handleUsageBackoff)
	mux.HandleFunc("/api/stats", api.handleStats)
	mux.HandleFunc("/api/schedules", api.handleSchedules)
	mux.HandleFunc("/api/queue", api.handleQueue)
	mux.HandleFunc("/api/capacity", api.handleCapacity)

	// Permissive catch-all: unknown /api/* (and everything non-API below
	// it) answers with an empty, well-shaped document so a tab's fan-out
	// fetches resolve without JS errors.
	leaky := func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"ok":true,"sessions":[],"items":[],"data":[],"prds":[],"nodes":[],"llms":[],"schedules":[],"rules":[],"plugins":[],"stats":{},"count":0,"unread_count":0,"total":0,"enabled":false,"name":"","graph":[]}`)
			return
		}
		// Static PWA (index.html gets the same version substitution the
		// production mux does).
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			body, err := fs.ReadFile(webSub, "index.html")
			if err != nil {
				http.Error(w, "index read failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, strings.ReplaceAll(string(body), "%%DW_VERSION%%", Version))
			return
		}
		http.FileServer(http.FS(webSub)).ServeHTTP(w, r)
	}
	mux.HandleFunc("/", leaky)
	mux.Handle("/ws", http.HandlerFunc(api.handleWS))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	t.Logf("serving seeded fixture at http://%s", ln.Addr())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}
}
