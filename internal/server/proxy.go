package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/gorilla/websocket"
)

// handleProxyWS relays a WebSocket connection between the client and a remote
// datawatch server. Route: /api/proxy/{serverName}/ws
func (s *Server) handleProxyWS(w http.ResponseWriter, r *http.Request) {
	// Extract server name: /api/proxy/<name>/ws
	path := strings.TrimPrefix(r.URL.Path, "/api/proxy/")
	idx := strings.Index(path, "/")
	if idx < 0 {
		http.Error(w, "missing server name", http.StatusBadRequest)
		return
	}
	serverName := path[:idx]

	remote := s.findServer(serverName)
	if remote == nil {
		http.Error(w, fmt.Sprintf("server %q not found or disabled", serverName), http.StatusNotFound)
		return
	}

	// Build remote WS URL
	remoteURL, err := url.Parse(remote.URL)
	if err != nil {
		http.Error(w, "invalid server URL", http.StatusBadRequest)
		return
	}
	scheme := "ws"
	if remoteURL.Scheme == "https" {
		scheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s/ws", scheme, remoteURL.Host)

	// Connect to remote WS
	header := http.Header{}
	if remote.Token != "" {
		header.Set("Authorization", "Bearer "+remote.Token)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	remoteConn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot connect to remote WS: %v", err), http.StatusBadGateway)
		return
	}

	// Upgrade client connection
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		remoteConn.Close()
		return
	}

	// Bidirectional relay
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			clientConn.Close()
			remoteConn.Close()
		})
	}

	// Remote → Client
	go func() {
		defer closeBoth()
		for {
			msgType, data, err := remoteConn.ReadMessage()
			if err != nil {
				return
			}
			if err := clientConn.WriteMessage(msgType, data); err != nil {
				return
			}
		}
	}()

	// Client → Remote
	defer closeBoth()
	for {
		msgType, data, err := clientConn.ReadMessage()
		if err != nil {
			return
		}
		if err := remoteConn.WriteMessage(msgType, data); err != nil {
			return
		}
	}
}

// findServer looks up a remote server by name from config.
func (s *Server) findServer(name string) *config.RemoteServerConfig {
	for i := range s.cfg.Servers {
		if s.cfg.Servers[i].Name == name && s.cfg.Servers[i].Enabled {
			return &s.cfg.Servers[i]
		}
	}
	return nil
}

// handleAggregatedSessions returns sessions from all remote servers + local,
// tagged with their source server name.
func (s *Server) handleAggregatedSessions(w http.ResponseWriter, r *http.Request) {
	type taggedSession struct {
		*session.Session
		Server string `json:"server"`
	}

	// Local sessions
	local := s.manager.ListSessions()
	results := make([]taggedSession, 0, len(local))
	for _, sess := range local {
		results = append(results, taggedSession{Session: sess, Server: "local"})
	}

	// Remote sessions — fetch in parallel with timeout
	type fetchResult struct {
		server   string
		sessions []taggedSession
	}
	ch := make(chan fetchResult, len(s.cfg.Servers))

	for _, sv := range s.cfg.Servers {
		if !sv.Enabled {
			continue
		}
		go func(sv config.RemoteServerConfig) {
			client := &http.Client{Timeout: 5 * time.Second}
			apiURL := strings.TrimRight(sv.URL, "/") + "/api/sessions"
			req, err := http.NewRequest(http.MethodGet, apiURL, nil)
			if err != nil {
				ch <- fetchResult{server: sv.Name}
				return
			}
			if sv.Token != "" {
				req.Header.Set("Authorization", "Bearer "+sv.Token)
			}
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("[proxy] %s: fetch sessions failed: %v", sv.Name, err)
				ch <- fetchResult{server: sv.Name}
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			var sessions []*session.Session
			if err := json.Unmarshal(body, &sessions); err != nil {
				log.Printf("[proxy] %s: parse sessions: %v", sv.Name, err)
				ch <- fetchResult{server: sv.Name}
				return
			}
			tagged := make([]taggedSession, 0, len(sessions))
			for _, sess := range sessions {
				tagged = append(tagged, taggedSession{Session: sess, Server: sv.Name})
			}
			ch <- fetchResult{server: sv.Name, sessions: tagged}
		}(sv)
	}

	// Collect results
	enabledCount := 0
	for _, sv := range s.cfg.Servers {
		if sv.Enabled {
			enabledCount++
		}
	}
	for i := 0; i < enabledCount; i++ {
		result := <-ch
		results = append(results, result.sessions...)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) //nolint:errcheck
}

// handleRemotePWA proxies the full PWA from a remote datawatch instance.
// Route: /remote/{serverName}/...
// All HTML, JS, and CSS content is rewritten so API calls, WS connections,
// and asset URLs route back through the proxy.
func (s *Server) handleRemotePWA(w http.ResponseWriter, r *http.Request) {
	// Extract server name and sub-path: /remote/<name>/...
	path := strings.TrimPrefix(r.URL.Path, "/remote/")
	idx := strings.Index(path, "/")
	var serverName, subPath string
	if idx < 0 {
		serverName = path
		subPath = "/"
	} else {
		serverName = path[:idx]
		subPath = path[idx:]
	}
	if subPath == "" {
		subPath = "/"
	}

	remote := s.findServer(serverName)
	if remote == nil {
		http.Error(w, fmt.Sprintf("server %q not found or disabled", serverName), http.StatusNotFound)
		return
	}

	// Build target URL
	targetURL := strings.TrimRight(remote.URL, "/") + subPath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for k, vals := range r.Header {
		for _, v := range vals {
			proxyReq.Header.Add(k, v)
		}
	}
	if remote.Token != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+remote.Token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("remote PWA error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, vals := range resp.Header {
		if strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Content-Encoding") {
			continue // will be recomputed if we rewrite content
		}
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}

	ct := resp.Header.Get("Content-Type")
	needsRewrite := strings.Contains(ct, "text/html") ||
		strings.Contains(ct, "javascript") ||
		strings.Contains(ct, "text/css")

	if !needsRewrite {
		// Binary assets (images, fonts, etc.) — pass through directly
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			w.Header().Set("Content-Length", cl)
		}
		if ce := resp.Header.Get("Content-Encoding"); ce != "" {
			w.Header().Set("Content-Encoding", ce)
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body) //nolint:errcheck
		return
	}

	// Read and rewrite content
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "read remote body: "+err.Error(), http.StatusBadGateway)
		return
	}

	rewritten := rewritePWAContent(body, serverName)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(rewritten)))
	w.WriteHeader(resp.StatusCode)
	w.Write(rewritten) //nolint:errcheck
}

// rewritePWAContent rewrites URLs in HTML/JS/CSS so they route through the proxy.
// Rewrites:
//   - /api/... → /api/proxy/{server}/api/...
//   - /ws      → /api/proxy/{server}/ws
//   - Relative asset paths (href="/...", src="/...") → /remote/{server}/...
func rewritePWAContent(body []byte, serverName string) []byte {
	content := string(body)
	proxyAPI := "/api/proxy/" + serverName
	remotePWA := "/remote/" + serverName

	// Rewrite WS endpoint: '/ws' or "/ws" → proxied WS path
	// Match common JS patterns for WS URL construction
	content = strings.ReplaceAll(content, `'/ws'`, `'`+proxyAPI+`/ws'`)
	content = strings.ReplaceAll(content, `"/ws"`, `"`+proxyAPI+`/ws"`)

	// Rewrite fetch/XHR API calls: '/api/...' or "/api/..."
	// Use regex to avoid double-rewriting /api/proxy/ paths
	apiRe := regexp.MustCompile(`(['"])(/api/)(?!proxy/)`)
	content = apiRe.ReplaceAllString(content, `${1}`+proxyAPI+`/api/`)

	// Rewrite absolute asset references in HTML: href="/...", src="/..."
	// But not /api/ (already handled) or /remote/ (avoid loops)
	assetRe := regexp.MustCompile(`((?:href|src|action)=["'])(/(?!api/|remote/|metrics|healthz))`)
	content = assetRe.ReplaceAllString(content, `${1}`+remotePWA+`${2}`)

	// Rewrite favicon and manifest references
	content = strings.ReplaceAll(content, `href="/favicon`, `href="`+remotePWA+`/favicon`)
	content = strings.ReplaceAll(content, `href="/manifest`, `href="`+remotePWA+`/manifest`)

	return []byte(content)
}

// handleRemotePWARedirect redirects /remote/{server} to /remote/{server}/ to
// ensure relative paths work correctly.
func (s *Server) handleRemotePWARedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
}

// isRemotePWAPath checks if a request is for a remote PWA sub-path.
func isRemotePWAPath(path string) bool {
	trimmed := strings.TrimPrefix(path, "/remote/")
	return trimmed != path && len(trimmed) > 0
}

