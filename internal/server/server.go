package server

import (
	"context"
	"crypto/tls"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/tlsutil"
)

//go:embed web
var webFS embed.FS

// HTTPServer wraps the HTTP server and hub
type HTTPServer struct {
	cfg     *config.ServerConfig
	dataDir string
	hub     *Hub
	srv     *http.Server
	manager *session.Manager
	api     *Server
}

// New creates a new HTTPServer
func New(cfg *config.ServerConfig, fullCfg *config.Config, cfgPath string, dataDir string, manager *session.Manager, hostname string, backends []string) *HTTPServer {
	hub := NewHub()
	api := NewServer(hub, manager, hostname, cfg.Token, backends, fullCfg, cfgPath)

	webSub, _ := fs.Sub(webFS, "web")

	mux := http.NewServeMux()

	// Public routes (no auth)
	mux.HandleFunc("/api/health", api.handleHealth)

	// Docs routes (no auth required, served directly)
	mux.HandleFunc("/api/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api-docs.html", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/api/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		f, err := webSub.Open("openapi.yaml")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer f.Close()
		io.Copy(w, f) //nolint:errcheck
	})

	// Authenticated API routes
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/sessions", api.handleSessions)
	apiMux.HandleFunc("/api/output", api.handleSessionOutput)
	apiMux.HandleFunc("/api/command", api.handleCommand)
	apiMux.HandleFunc("/api/info", api.handleInfo)
	apiMux.HandleFunc("/api/backends", api.handleBackends)
	apiMux.HandleFunc("/api/files", api.handleFiles)
	apiMux.HandleFunc("/api/sessions/rename", api.handleRenameSession)
	apiMux.HandleFunc("/api/sessions/kill", api.handleKillSession)
	apiMux.HandleFunc("/api/sessions/delete", api.handleDeleteSession)
	apiMux.HandleFunc("/api/sessions/start", api.handleStartSession)
	apiMux.HandleFunc("/api/link/start", api.handleLinkStart)
	apiMux.HandleFunc("/api/link/stream", api.handleLinkStream)
	apiMux.HandleFunc("/api/link/status", api.handleLinkStatus)
	apiMux.HandleFunc("/api/config", api.handleConfig)
	apiMux.HandleFunc("/api/servers", api.handleListServers)
	apiMux.HandleFunc("/api/proxy/", api.handleProxy)
	apiMux.HandleFunc("/api/schedule", api.handleSchedule)
	apiMux.HandleFunc("/api/commands", api.handleCommands)
	apiMux.HandleFunc("/api/filters", api.handleFilters)
	apiMux.HandleFunc("/api/alerts", api.handleAlerts)

	// Apply auth middleware to API routes
	mux.Handle("/api/", api.authMiddleware(apiMux))
	mux.Handle("/ws", api.authMiddleware(http.HandlerFunc(api.handleWS)))

	// Serve PWA static files
	mux.Handle("/", http.FileServer(http.FS(webSub)))

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // 0 = no timeout for WebSocket
		IdleTimeout:  60 * time.Second,
	}

	return &HTTPServer{
		cfg:     cfg,
		dataDir: dataDir,
		hub:     hub,
		srv:     srv,
		manager: manager,
		api:     api,
	}
}

// SetScheduleStore wires a schedule store into the server for /api/schedule.
func (s *HTTPServer) SetScheduleStore(store *session.ScheduleStore) {
	s.api.SetScheduleStore(store)
}

// SetCmdLibrary wires a command library into the server for /api/commands.
func (s *HTTPServer) SetCmdLibrary(lib *session.CmdLibrary) {
	s.api.cmdLib = lib
}

// SetAlertStore wires an alert store into the server for /api/alerts.
func (s *HTTPServer) SetAlertStore(store *alerts.Store) {
	s.api.alertStore = store
}

// SetFilterStore wires a filter store into the server for /api/filters.
func (s *HTTPServer) SetFilterStore(store *session.FilterStore) {
	s.api.filterStore = store
}

// NotifyAlert broadcasts a new alert to all WebSocket clients.
func (s *HTTPServer) NotifyAlert(a *alerts.Alert) {
	s.hub.BroadcastAlert(a)
}

// NotifyStateChange broadcasts a session state change to all WS clients
func (s *HTTPServer) NotifyStateChange(sess *session.Session, oldState session.State) {
	s.hub.BroadcastSessions(s.manager.ListSessions())
}

// NotifyNeedsInput broadcasts a needs-input event to all WS clients
func (s *HTTPServer) NotifyNeedsInput(sess *session.Session, prompt string) {
	s.hub.BroadcastNeedsInput(sess.FullID, prompt)
	s.hub.BroadcastSessions(s.manager.ListSessions())
}

// NotifyOutput broadcasts new output lines for a session
func (s *HTTPServer) NotifyOutput(sessionID string, lines []string) {
	s.hub.BroadcastOutput(sessionID, lines)
}

// Start begins serving. Blocks until ctx is cancelled.
func (s *HTTPServer) Start(ctx context.Context) error {
	go s.hub.Run()

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	errCh := make(chan error, 1)

	if s.cfg.TLSEnabled {
		tlsCfg, err := tlsutil.Build(tlsutil.Config{
			Enabled:      true,
			CertFile:     s.cfg.TLSCert,
			KeyFile:      s.cfg.TLSKey,
			AutoGenerate: s.cfg.TLSAutoGenerate,
			DataDir:      s.dataDir,
			Name:         "server",
		})
		if err != nil {
			return fmt.Errorf("TLS setup: %w", err)
		}
		s.srv.TLSConfig = tlsCfg
		go func() {
			errCh <- s.srv.ServeTLS(listener, "", "")
		}()
		fmt.Printf("datawatch server listening on https://%s (TLS 1.3+, post-quantum enabled)\n", addr)
	} else {
		go func() {
			errCh <- s.srv.Serve(listener)
		}()
		fmt.Printf("datawatch server listening on http://%s\n", addr)
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// Hub returns the WebSocket hub (for wiring session manager callbacks)
func (s *HTTPServer) Hub() *Hub {
	return s.hub
}

// tlsAuthMiddleware wraps an http.Handler with bearer token auth and TLS enforcement.
// It is used by the MCP SSE server to gate remote connections.
func tlsAuthMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+token {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// BuildTLSConfig is a convenience wrapper for building a *tls.Config from a ServerConfig.
func BuildTLSConfig(cfg *config.ServerConfig, dataDir string) (*tls.Config, error) {
	return tlsutil.Build(tlsutil.Config{
		Enabled:      cfg.TLSEnabled,
		CertFile:     cfg.TLSCert,
		KeyFile:      cfg.TLSKey,
		AutoGenerate: cfg.TLSAutoGenerate,
		DataDir:      dataDir,
		Name:         "server",
	})
}
