package server

import (
	"crypto/tls"
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
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
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
	// BL394 §6 iframe-embed follow-up — see handleProxy's identical check.
	// handleProxy already calls this (it dispatches here internally), so
	// this is belt-and-suspenders for any future direct registration of
	// this handler, not currently load-bearing on its own.
	if !s.checkProxyAuth(w, r, serverName) {
		return
	}

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
	if remote.TLSSkipVerify {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 -- operator-opted-in per peer, documented risk
	}
	remoteConn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot connect to remote WS: %v", err), http.StatusBadGateway)
		return
	}

	// Upgrade client connection
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		_ = remoteConn.Close()
		return
	}

	// Bidirectional relay
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = clientConn.Close()
			_ = remoteConn.Close()
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

// findServer looks up a remote server by name — checks cfg.Servers first,
// then falls back to the runtime store (BL312 S4).
func (s *Server) findServer(name string) *config.RemoteServerConfig {
	for i := range s.cfg.Servers {
		if s.cfg.Servers[i].Name == name && s.cfg.Servers[i].Enabled {
			return &s.cfg.Servers[i]
		}
	}
	if s.serverStore != nil {
		if e, ok := s.serverStore.Get(name); ok && e.Enabled {
			return &config.RemoteServerConfig{Name: e.Name, URL: e.URL, Token: e.Token, Enabled: e.Enabled, TLSSkipVerify: e.TLSSkipVerify}
		}
	}
	return nil
}

// runtimeServers returns all enabled servers from both cfg.Servers and the
// runtime store (BL312 S4). Callers get a merged, deduplicated slice.
func (s *Server) runtimeServers() []config.RemoteServerConfig {
	seen := map[string]bool{}
	out := []config.RemoteServerConfig{}
	for _, srv := range s.cfg.Servers {
		if srv.Enabled && !seen[srv.Name] {
			seen[srv.Name] = true
			out = append(out, srv)
		}
	}
	if s.serverStore != nil {
		for _, e := range s.serverStore.List() {
			if e.Enabled && !seen[e.Name] {
				seen[e.Name] = true
				out = append(out, config.RemoteServerConfig{Name: e.Name, URL: e.URL, Token: e.Token, Enabled: true, TLSSkipVerify: e.TLSSkipVerify})
			}
		}
	}
	return out
}

// handleAggregatedSessions returns sessions from all remote servers + local,
// tagged with their source server name.
func (s *Server) handleAggregatedSessions(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
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

	// Remote sessions — fetch in parallel with timeout (BL312 S4: includes runtime store)
	type fetchResult struct {
		server   string
		sessions []taggedSession
	}
	remotes := s.runtimeServers()
	ch := make(chan fetchResult, len(remotes))

	for _, sv := range remotes {
		go func(sv config.RemoteServerConfig) {
			client := multiserver.HTTPClient(sv.TLSSkipVerify, 5*time.Second)
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
			defer resp.Body.Close() //nolint:errcheck
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
	for i := 0; i < len(remotes); i++ {
		result := <-ch
		results = append(results, result.sessions...)
	}

	// BL352 — ?parent_id=<id> filters to sessions whose parent_id matches.
	if pid := r.URL.Query().Get("parent_id"); pid != "" {
		var filtered []taggedSession
		for _, ts := range results {
			if ts.ParentID == pid {
				filtered = append(filtered, ts)
			}
		}
		results = filtered
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
	// BL394 §6 iframe-embed follow-up: moved below serverName extraction
	// (same reasoning as handleProxy/checkProxyAuth) -- every request
	// under /remote/{name}/, including the proxied page's own css/js/
	// image asset loads (which fedAuthMiddleware now also accepts a
	// scoped token for, since a <link>/<script src> tag can't carry a
	// custom header), is authorized identically to the top-level page.
	if !s.checkProxyAuth(w, r, serverName) {
		return
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
		// Pre-existing bug, found live-testing the iframe-embed follow-up
		// (BL394 §6) with a real browser rather than curl (which doesn't
		// send Accept-Encoding by default, so this never surfaced):
		// forwarding the browser's own "Accept-Encoding: gzip" header
		// upstream disables Go's http.Transport's normal behavior of
		// requesting gzip itself and transparently decompressing the
		// response -- per net/http's own documented behavior, that
		// auto-decompression only happens when the CALLER never set
		// Accept-Encoding explicitly. With it forwarded, resp.Body here
		// was the raw, still-gzipped bytes, which rewritePWAContent then
		// string-rewrote as if it were plain text (silently producing
		// garbage) and served back with no Content-Encoding header at
		// all -- so the browser received raw gzip binary labeled as
		// text/javascript and failed to parse it ("Invalid or
		// unexpected token" at line 1 col 1, confirmed against a real
		// running daemon). Dropping it here lets Transport manage
		// compression itself and decompress transparently, exactly the
		// plain bytes rewritePWAContent already assumes it's getting.
		if strings.EqualFold(k, "Accept-Encoding") {
			continue
		}
		for _, v := range vals {
			proxyReq.Header.Add(k, v)
		}
	}
	if remote.Token != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+remote.Token)
	}

	client := multiserver.HTTPClient(remote.TLSSkipVerify, 30*time.Second)
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("remote PWA error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	// Copy response headers. BL394 (docs/plans/2026-10-03-bl394-security-
	// findings-review.md §6) -- also skip every security header, not just
	// Content-Length/-Encoding: the remote peer's OWN response already
	// carries its own Content-Security-Policy etc. (set by ITS OWN
	// securityHeadersMiddleware, describing ITS OWN origin), and blindly
	// forwarding it here would land a SECOND, conflicting CSP/X-Frame-
	// Options/etc. alongside the proxy's own (sandboxSecurityHeadersMiddleware's,
	// written for the proxy's actual origin) -- confirmed live while
	// testing this fix: curl showed two Content-Security-Policy headers
	// on one response, one with frame-ancestors naming the main origin
	// (correct) and one with frame-ancestors 'self' (the remote peer's
	// own, copied verbatim). Multiple CSP headers combine as an AND
	// across directives, so the stray copy could silently make the
	// *correct* policy more restrictive than intended, or outright wrong,
	// depending on what the specific remote peer happens to send.
	for k, vals := range resp.Header {
		if strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Content-Encoding") {
			continue // will be recomputed if we rewrite content
		}
		if isSecurityResponseHeader(k) {
			continue // the proxy's own middleware sets these for its own origin
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

	// BL394 §6 iframe-embed follow-up: mint a short-lived, serverName-
	// scoped proxy token and have it injected into the proxied page's own
	// localStorage, so the proxied app.js's own tokenHeader()-based fetch
	// calls (and its WS connection's ?token= query param) authenticate
	// correctly against THIS daemon's /api/proxy/ route -- the sandbox
	// origin's localStorage is otherwise genuinely empty (that's the
	// fix), so without this the proxied dashboard loads but every one of
	// its own API calls 401s. See proxy_token.go's header comment for why
	// this can't just be the real admin token.
	token := proxyTokens.mint(serverName)

	rewritten := rewritePWAContent(body, serverName, token)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(rewritten)))
	w.WriteHeader(resp.StatusCode)
	w.Write(rewritten) //nolint:errcheck
}

// rewritePWAContent rewrites URLs in HTML/JS/CSS so they route through the proxy.
// Rewrites:
//   - /api/... → /api/proxy/{server}/api/...
//   - /ws      → /api/proxy/{server}/ws
//   - Relative asset paths (href="/...", src="/...") → /remote/{server}/...
//
// scopedToken (BL394 §6 iframe-embed follow-up), if non-empty, is
// injected as a tiny inline bootstrap script right after <head> -- before
// the real app.js tag loads, later in the document -- so the proxied
// page's own code can authenticate its own API calls. Hex-encoded, so no
// escaping concerns embedding it in a JS string literal. A no-op on
// non-HTML content (no <head> to match) or when scopedToken is empty
// (e.g. mint() failed to read crypto/rand).
func rewritePWAContent(body []byte, serverName string, scopedToken string) []byte {
	content := string(body)
	proxyAPI := "/api/proxy/" + serverName
	remotePWA := "/remote/" + serverName

	if scopedToken != "" {
		bootstrap := `<head><script>try{localStorage.setItem('cs_token','` + scopedToken + `');}catch(e){}</script>`
		content = strings.Replace(content, "<head>", bootstrap, 1)
	}

	// Rewrite WS endpoint: '/ws' or "/ws" → proxied WS path
	// Match common JS patterns for WS URL construction
	content = strings.ReplaceAll(content, `'/ws'`, `'`+proxyAPI+`/ws'`)
	content = strings.ReplaceAll(content, `"/ws"`, `"`+proxyAPI+`/ws"`)

	// Rewrite fetch/XHR API calls: '/api/...' or "/api/..."
	// Avoid double-rewriting /api/proxy/ paths by replacing that sentinel first,
	// then rewriting all remaining /api/ references, then restoring the sentinel.
	const proxysentinel = "\x00PROXYSENTINEL\x00"
	content = strings.ReplaceAll(content, "/api/proxy/", proxysentinel)
	apiRe := regexp.MustCompile(`(['"])(/api/)`)
	content = apiRe.ReplaceAllString(content, `${1}`+proxyAPI+`/api/`)
	content = strings.ReplaceAll(content, proxysentinel, "/api/proxy/")

	// Rewrite absolute asset references in HTML: href="/...", src="/..."
	// Exclude /api/, /remote/, /metrics, /healthz by checking the captured suffix.
	assetRe := regexp.MustCompile(`((?:href|src|action)=["'])(/[^"']+)`)
	skipPrefixes := []string{"/api/", "/remote/", "/metrics", "/healthz"}
	// BL394 §6 iframe-embed follow-up: these rewritten URLs (css/js/image
	// requests the browser issues itself via <link>/<script src>, not a
	// JS fetch()) are under /remote/{server}/ just like the page itself --
	// also gated by fedAuthMiddleware, and a plain tag load can no more
	// carry a custom header than the top-level navigation can. Append
	// the same scoped proxy token as a query param (fedAuthMiddleware's
	// scoped-token fallback now also covers /remote/, not just
	// /api/proxy/ -- see federation_cap.go) so these loads authenticate
	// too, instead of silently 401ing and being served back as a
	// text/plain error body that the browser then (correctly) refuses to
	// parse as CSS/JS.
	content = assetRe.ReplaceAllStringFunc(content, func(m string) string {
		groups := assetRe.FindStringSubmatch(m)
		if len(groups) < 3 {
			return m
		}
		path := groups[2]
		for _, skip := range skipPrefixes {
			if strings.HasPrefix(path, skip) {
				return m
			}
		}
		return groups[1] + appendProxyToken(remotePWA+path, scopedToken)
	})

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
