package server

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"time"

	"github.com/dmz006/claude-signal/internal/config"
	"github.com/dmz006/claude-signal/internal/session"
)

//go:embed web
var webFS embed.FS

// HTTPServer wraps the HTTP server and hub
type HTTPServer struct {
	cfg     *config.ServerConfig
	hub     *Hub
	srv     *http.Server
	manager *session.Manager
	api     *Server
}

// New creates a new HTTPServer
func New(cfg *config.ServerConfig, manager *session.Manager, hostname string) *HTTPServer {
	hub := NewHub()
	api := NewServer(hub, manager, hostname, cfg.Token)

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
	apiMux.HandleFunc("/api/link/start", api.handleLinkStart)
	apiMux.HandleFunc("/api/link/stream", api.handleLinkStream)
	apiMux.HandleFunc("/api/link/status", api.handleLinkStatus)

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
		hub:     hub,
		srv:     srv,
		manager: manager,
		api:     api,
	}
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

	if s.cfg.TLSCert != "" && s.cfg.TLSKey != "" {
		go func() {
			errCh <- s.srv.ServeTLS(listener, s.cfg.TLSCert, s.cfg.TLSKey)
		}()
	} else {
		go func() {
			errCh <- s.srv.Serve(listener)
		}()
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
