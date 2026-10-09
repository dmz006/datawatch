// GH#201 — HTTP access / WS lifecycle / auth-failure log.
//
// Separate file from the operator action log (internal/audit, BL9):
// access-log volume is much higher (every request, not just state
// changes) and individually less significant, so it gets its own
// retention and query surface rather than drowning out audit.log.
// Reuses audit.Log's type/format — same JSON-lines shape, same
// Read/QueryFilter machinery — just pointed at a second file.
//
//   GET /api/audit/access?since=&until=&actor=&action=&limit=N
//
// Never logs the Authorization header or any token/nonce value — only
// request metadata (method, path, status, remote IP, user agent) and
// the resolved principal (which branch of fedAuthMiddleware accepted
// the request, or "auth_failure" when none did).

package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/metrics"
	"github.com/dmz006/datawatch/internal/stats"
)

// SetAccessLog wires the HTTP/WS access log used by /api/audit/access.
func (s *Server) SetAccessLog(l *audit.Log) { s.accessLog = l }

// AccessLog returns the wired access log (nil if disabled).
func (s *Server) AccessLog() *audit.Log { return s.accessLog }

// PopulateAccessLogStats (GH#201, DATAWATCH-CONTEXT.md's Observability
// checklist) fills the access-log fields on a stats snapshot. Wired via
// stats.Collector.SetAccessLogStatsFunc at daemon startup.
func (s *Server) PopulateAccessLogStats(out *stats.SystemStats) {
	out.AccessLogEnabled = s.accessLog != nil && (s.cfg == nil || s.cfg.Audit.AccessLogEnabledOrDefault())
	out.AccessLogEventsTotal = atomic.LoadInt64(&s.accessLogEventCount)
	out.AccessLogAuthFailuresTotal = atomic.LoadInt64(&s.accessLogAuthFailCount)
}

// principalFromContext resolves which fedAuthMiddleware branch accepted
// this request into a short, loggable label. Never includes the token
// itself. ctx must be the request context AFTER fedAuthMiddleware ran.
func principalFromContext(ctx context.Context) string {
	if peer := peerFromContext(ctx); peer != nil {
		return "peer:" + peer.Name
	}
	if proxyPeer := scopedProxyPeerFromContext(ctx); proxyPeer != "" {
		return "proxy:" + proxyPeer
	}
	if caps := sessionCapsFromContext(ctx); caps != nil {
		return "session-scoped"
	}
	if tok := callerTokenFromContext(ctx); tok != "" {
		return "admin"
	}
	return "unauthenticated"
}

// principalKind reduces a principal string ("peer:demo-ios", "proxy:x",
// "admin", "session-scoped", "unauthenticated") to its coarse category
// for Prometheus labeling — the exact peer/proxy name must never become
// a label value, since that's unbounded cardinality (a new value per
// peer ever added, never cleaned up from the metric's label set).
func principalKind(principal string) string {
	if i := strings.IndexByte(principal, ':'); i >= 0 {
		return principal[:i]
	}
	return principal
}

// logAccess appends one http_access or auth_failure entry to the access
// log. No-op when the access log isn't wired or is disabled in config.
func (s *Server) logAccess(r *http.Request, status int, principal string) {
	if s.accessLog == nil {
		return
	}
	if s.cfg != nil && !s.cfg.Audit.AccessLogEnabledOrDefault() {
		return
	}
	action := "http_access"
	if status == http.StatusUnauthorized {
		action = "auth_failure"
	}
	metrics.AccessLogEventsTotal.WithLabelValues(action, principalKind(principal)).Inc()
	atomic.AddInt64(&s.accessLogEventCount, 1)
	if action == "auth_failure" {
		atomic.AddInt64(&s.accessLogAuthFailCount, 1)
	}
	details := map[string]any{
		"method":     r.Method,
		"path":       r.URL.Path,
		"status":     status,
		"remote_ip":  remoteIP(r),
		"user_agent": r.Header.Get("User-Agent"),
	}
	// GH#201 Phase 2 — surface a verified cross-hop origin actor (who
	// behind a forwarding peer actually initiated this), when present.
	if chain := federation.ChainFromContext(r.Context()); len(chain) > 0 {
		details["origin_actor"] = federation.OriginActor(chain)
		details["hop_chain"] = chain
	}
	if hopChainInvalidFromContext(r.Context()) {
		details["hop_chain_invalid"] = true
	}
	_ = s.accessLog.Write(audit.Entry{
		Actor:   principal,
		Action:  action,
		Details: details,
	})
}

// remoteIP strips the port from r.RemoteAddr, falling back to the raw
// value if it isn't in host:port form (e.g. a unix socket).
func remoteIP(r *http.Request) string {
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}

func (s *Server) handleAuditAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAuditRead) {
		return
	}
	if s.accessLog == nil {
		http.Error(w, "access log not enabled", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	filter := audit.QueryFilter{
		Actor:  q.Get("actor"),
		Action: q.Get("action"),
		Limit:  100,
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.Since = t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.Until = t
		}
	}
	entries, err := s.accessLog.Read(filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"count":   len(entries),
		"entries": entries,
	})
}
