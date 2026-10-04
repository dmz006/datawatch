// BL394 security review (docs/plans/2026-10-03-bl394-security-findings-
// review.md §6) — handleRemotePWA used to serve a proxied federation
// peer's own PWA under the LOCAL daemon's own origin (/remote/{name}/...).
// The PWA authenticates every API call with a Bearer token read straight
// out of localStorage ("cs_token"), not a cookie, so a malicious or
// compromised peer's proxied JS could read the real local admin token
// directly -- full local-daemon takeover, not just "ride the session."
//
// The fix is a second, independent origin (this file): one extra TLS
// listener, bound to its own port on the same host/cert, serving ONLY
// /remote/ and /api/proxy/ (the routes a proxied peer's rewritten JS
// actually calls), with its own CSP whose frame-ancestors names the real
// main origin instead of 'self'. A different origin gets its own,
// naturally separate localStorage -- there is no real token to steal from
// inside it regardless of what the proxied JS does.
//
// Deliberately a second PORT today, not a per-peer subdomain: a subdomain
// needs real DNS/ingress the operator may not have, while a port works on
// any deployment with zero extra infra. Every caller that needs "the
// origin a proxied peer's content should use" goes through
// proxySandboxPortFor(peerName) -- today a one-line port lookup that
// ignores peerName, but the one seam a future per-peer-subdomain scheme
// would need to change, not something scattered across call sites.

package server

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// proxySandboxPortFor returns the port a proxied peer's content (and its
// matching /api/proxy/ callback traffic) should be served from. peerName
// is unused today -- every peer shares the one configured port -- but is
// part of the signature so a later per-peer-subdomain scheme has exactly
// one function to change, not every call site.
func (s *Server) proxySandboxPortFor(peerName string) int {
	_ = peerName
	if s.cfg == nil {
		return 0
	}
	return s.cfg.Server.ProxySandboxPort
}

// effectiveMainPort returns the port the operator's browser is actually
// viewing the main dashboard on -- cfg.TLSPort in dual-interface mode,
// cfg.Port otherwise (TLS replacing the main port, or plain HTTP).
//
// A plain function (not a *HTTPServer method): the main mux's /remote/
// redirect handler is built inside New(), before any *HTTPServer exists
// yet, from the same *config.ServerConfig local variable Start() later
// reads via s.cfg -- so every helper in this file that New()'s closures
// need takes cfg explicitly instead.
func effectiveMainPort(cfg *config.ServerConfig) int {
	if cfg.TLSEnabled && cfg.TLSPort > 0 {
		return cfg.TLSPort
	}
	return cfg.Port
}

func cspScheme(cfg *config.ServerConfig) string {
	if cfg.TLSEnabled {
		return "https"
	}
	return "http"
}

// counterpartOrigin builds scheme://hostname:port for the SAME hostname
// the incoming request r arrived on, substituting port. Used both
// directions: the main origin's /remote/ handler uses it (with the
// sandbox port) to redirect to the sandbox origin, and the sandbox
// origin's CSP middleware uses it (with the main port) to compute
// frame-ancestors -- so whichever hostname the operator used to reach
// either origin (localhost, a LAN IP, a Tailscale name), the other side
// resolves to the same hostname, just the other port.
func counterpartOrigin(cfg *config.ServerConfig, r *http.Request, port int) string {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return fmt.Sprintf("%s://%s", cspScheme(cfg), net.JoinHostPort(host, fmt.Sprintf("%d", port)))
}

// redirectToProxySandbox sends any request under /remote/ on the MAIN
// origin to the equivalent path on the sandbox origin, preserving the
// path and query string exactly. /remote/ is deliberately NOT served
// from the main mux at all -- this is the only thing the main origin
// does with that prefix, so an old bookmark/link still ends up in the
// right (isolated) place instead of 404ing.
func redirectToProxySandbox(cfg *config.ServerConfig, w http.ResponseWriter, r *http.Request) {
	port := cfg.ProxySandboxPort
	if port <= 0 {
		http.Error(w, "remote PWA proxy is disabled (server.proxy_sandbox_port <= 0)", http.StatusServiceUnavailable)
		return
	}
	target := counterpartOrigin(cfg, r, port) + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// securityResponseHeaders are the headers securityHeadersMiddleware /
// sandboxSecurityHeadersMiddleware set for this daemon's own origin(s).
// handleRemotePWA strips these from whatever the proxied remote peer's
// response carries (see proxy.go) so the proxy's own policy is never
// silently combined with -- or overridden by -- the remote's.
var securityResponseHeaders = map[string]bool{
	"content-security-policy":      true,
	"x-frame-options":              true,
	"x-content-type-options":       true,
	"referrer-policy":              true,
	"cross-origin-opener-policy":   true,
	"cross-origin-embedder-policy": true,
	"cross-origin-resource-policy": true,
	"permissions-policy":           true,
}

func isSecurityResponseHeader(name string) bool {
	return securityResponseHeaders[strings.ToLower(name)]
}

// buildSandboxCSP mirrors buildCSP's directives but with frame-ancestors
// naming the real main origin instead of 'self' -- the sandbox origin
// must only ever be embeddable by the daemon's own main dashboard, never
// by itself or anyone else. script-src/style-src still need
// 'unsafe-inline': the proxied content is literally another datawatch
// instance's app.js, with the exact same inline-event-handler shape as
// this one (see buildCSP's own comment) -- that's not a weaker policy
// than the main origin's, just the same documented trade-off applied to
// content that, structurally, can no longer steal the real token even if
// one of its inline handlers is attacker-controlled.
func buildSandboxCSP(mainOrigin string) string {
	scriptSrc := "'self' 'unsafe-inline' https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://unpkg.com"
	return "default-src 'self'; " +
		"script-src " + scriptSrc + "; " +
		"style-src 'self' 'unsafe-inline' https://unpkg.com; " +
		"connect-src 'self'; " +
		"img-src 'self' data: blob:; " +
		"font-src 'self' data:; " +
		"worker-src 'self' blob:; " +
		"base-uri 'self'; " +
		"form-action 'self'; " +
		"object-src 'none'; " +
		"frame-ancestors " + mainOrigin
}

// sandboxSecurityHeadersMiddleware is securityHeadersMiddleware's
// counterpart for the sandbox origin -- same hardening headers, but CSP's
// frame-ancestors is computed per request from the incoming Host header
// (see counterpartOrigin) rather than a single fixed string, so it's
// correct regardless of which hostname (localhost, a LAN IP, a Tailscale
// name, ...) the operator happens to be using to reach the daemon.
//
// X-Frame-Options is deliberately omitted here: its only same-origin-or-
// deny vocabulary can't express "embeddable by this other specific
// origin," and setting SAMEORIGIN would make a legacy browser (one that
// doesn't understand CSP3's frame-ancestors at all) wrongly block the
// main dashboard's own, legitimate framing. Modern browsers already
// enforce frame-ancestors from CSP alone.
func (s *HTTPServer) sandboxSecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mainOrigin := counterpartOrigin(s.cfg, r, effectiveMainPort(s.cfg))
		h := w.Header()
		h.Set("Content-Security-Policy", buildSandboxCSP(mainOrigin))
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin-allow-popups")
		h.Set("Cross-Origin-Embedder-Policy", "unsafe-none")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), accelerometer=(), gyroscope=(), magnetometer=()")
		next.ServeHTTP(w, r)
	})
}

// buildProxySandboxMux returns the narrow handler for the sandbox
// listener: ONLY /remote/ (serve a proxied peer's PWA) and /api/proxy/
// (its matching REST+WS callback traffic, including the WS relay --
// handleProxy dispatches that internally by path suffix). Deliberately
// does not reuse the main mux's full apiMux: nothing else needs to be
// reachable from this origin, and keeping the surface this small is
// itself part of the fix, not just the CSP/origin separation.
//
// /api/proxy/llm/ and /api/proxy/comm/ (unrelated features sharing the
// /api/proxy/ prefix on the main mux) are deliberately NOT replicated
// here -- rewritePWAContent never produces those shapes, only
// /api/proxy/{serverName}/... and its /ws suffix, both of which
// handleProxy itself already handles. (A federation peer literally named
// "llm"/"comm"/"agent" would be ambiguous here the same way it already
// is on the main mux -- a pre-existing edge case, not a new one.)
func (s *HTTPServer) buildProxySandboxMux() http.Handler {
	authed := http.NewServeMux()
	authed.Handle("/remote/", s.api.fedAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trimmed := strings.TrimPrefix(r.URL.Path, "/remote/")
		if !strings.Contains(trimmed, "/") {
			s.api.handleRemotePWARedirect(w, r)
			return
		}
		s.api.handleRemotePWA(w, r)
	})))
	authed.Handle("/api/proxy/", s.api.fedAuthMiddleware(http.HandlerFunc(s.api.handleProxy)))

	mux := http.NewServeMux()
	mux.Handle("/", authed)
	// /locales/ -- deliberately unauthenticated, matching the main
	// origin's own behavior (static i18n strings, served directly from
	// the same embedded FS, never gated by fedAuthMiddleware there
	// either). Not proxied to the remote peer: translations aren't
	// peer-specific, and there's no peer name to proxy by anyway -- the
	// proxied app.js's own fetch('/locales/'+lang+'.json') is a runtime-
	// built string rewritePWAContent never touches (unlike the static
	// href/src assets it does rewrite), so it always resolves relative
	// to the sandbox origin itself, with no peer segment in the path.
	mux.Handle("/locales/", cacheControlMiddleware(http.FileServer(http.FS(s.webSub))))
	return s.sandboxSecurityHeadersMiddleware(mux)
}

// startProxySandboxListener binds the sandbox origin's own TLS (or plain
// HTTP) listener on each of hosts, sharing tlsCfg (same cert as the main
// listener -- a cert is bound to a hostname, not a port, so reusing it
// for a second port on the same host is correct) but serving
// buildProxySandboxMux() instead of the main handler.
//
// A bind failure here (e.g. the port is already in use) is deliberately
// NON-fatal to the daemon as a whole, but fails CLOSED for this feature:
// logged loudly, and /remote/ on the main origin only ever redirects to
// this port (see redirectToProxySandbox) -- it never falls back to
// serving proxied content from the main origin itself.
func (s *HTTPServer) startProxySandboxListener(tlsCfg *tls.Config, hosts []string, errCh chan error) {
	port := s.api.proxySandboxPortFor("")
	if port <= 0 {
		fmt.Println("proxy sandbox listener disabled (server.proxy_sandbox_port <= 0) — /remote/ (viewing a federation peer's PWA) is unavailable")
		return
	}
	handler := s.buildProxySandboxMux()
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		addr := joinHostPort(host, port)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			fmt.Printf("proxy sandbox listener: could not bind %s: %v — /remote/ (viewing a federation peer's PWA) is unavailable\n", addr, err)
			continue
		}
		srv := &http.Server{
			Addr:              addr,
			Handler:           handler,
			TLSConfig:         tlsCfg,
			ReadTimeout:       15 * time.Second,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      0, // 0 = no timeout for the proxied WS relay
			IdleTimeout:       60 * time.Second,
		}
		if tlsCfg != nil {
			go func(l net.Listener, a string) { errCh <- srv.ServeTLS(l, "", "") }(listener, addr)
			fmt.Printf("proxy sandbox listener on https://%s (serves /remote/ + /api/proxy/ under a separate origin from the main dashboard)\n", addr)
		} else {
			go func(l net.Listener, a string) { errCh <- srv.Serve(l) }(listener, addr)
			fmt.Printf("proxy sandbox listener on http://%s (serves /remote/ + /api/proxy/ under a separate origin from the main dashboard)\n", addr)
		}
	}
}
