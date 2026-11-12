// BL110 extension (multi-surface audit) — the single audit point for the
// REST config-write routes, sharing the federation.ConfigAuditor sink
// with the MCP and comm surfaces.

package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
)

// auditConfigPut is the single audit point for one REST config-write
// route: it wraps the (already capability-gated) PUT handler and records
// exactly one federation.ConfigAuditRecord per call with surface "rest",
// allowed OR denied. The record carries the keys changed (dot-path
// patches), secret-like values redacted, the caller identity (session or
// peer), and the denial reason when the write failed.
//
// Admin (operator) callers are NOT audited as session writes. The loopback
// PUTs issued by the MCP config_set tool and by the comm-channel `configure`
// command are admin-authenticated, so their ONE audited record is the one
// their origin surface writes (the MCP middleware / the comm router) — a raw
// REST audit of them would double-record the same write.
func (s *Server) auditConfigPut(route string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec := &configPutRecorder{ResponseWriter: w, status: 200}
		var body []byte
		if r.Body != nil {
			// Buffer the body: the audit record needs it and the handler
			// decodes from the rewindable copy.
			body, _ = io.ReadAll(r.Body)
			_ = r.Body.Close()
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next(rec, r)
		if s.configAudit == nil {
			return
		}
		caller, ok := s.configWriteCallerFrom(r.Context())
		if !ok {
			return // admin — not audited as a session write
		}
		allowed := rec.status < 400
		ar := federation.ConfigAuditRecord{
			Surface: "rest",
			Tool:    "PUT " + route,
			Caller:  caller,
			Allowed: allowed,
		}
		ar.Keys, ar.Values = federation.ExtractConfigPatch(body)
		if !allowed {
			if reason := strings.TrimSpace(rec.body.String()); reason != "" {
				ar.Reason = reason
			} else {
				ar.Reason = "HTTP " + http.StatusText(rec.status)
			}
		}
		s.configAudit.Record(ar)
	}
}

// configWriteCallerFrom resolves the audit caller identity for a REST
// config-write, mirroring auditActor's precedence (HLLM-003): a scoped
// session credential names the owning session; a federation peer names
// itself; otherwise it is the admin token. ok=false means admin (operator) —
// not audited as a session write — or a scoped credential whose owning
// session could not be resolved (same treatment auditActor gives it:
// "operator").
func (s *Server) configWriteCallerFrom(ctx context.Context) (string, bool) {
	if sessionCapsFromContext(ctx) != nil {
		if s.sessionTokens != nil {
			if tok := callerTokenFromContext(ctx); tok != "" {
				if id := s.sessionTokens.SessionIDForToken(tok); id != "" {
					return "session:" + id, true
				}
			}
		}
		return "", false
	}
	if peer := peerFromContext(ctx); peer != nil {
		return "peer:" + peer.Name, true
	}
	return "", false
}

// TestMessageAttribution carries the calling credential's identity and
// capability decision to the comm router for session-attributed test
// injection (POST /api/test/message), mirroring router's
// ConfigureAttribution. The zero value is an operator (admin) call: no
// session credential is involved and the comm configure path is not
// audited as a session write — consistent with the REST and MCP surfaces,
// which only record scoped (session/peer) credential use.
type TestMessageAttribution struct {
	// Caller names the session or peer ("session:<id>" / "peer:<name>").
	// Empty = operator/admin.
	Caller string
	// ConfigWriteAllowed is the config:write capability decision made
	// here, from the credential this request carries.
	ConfigWriteAllowed bool
	// DenyReason is the denial carried into the comm audit record when
	// ConfigWriteAllowed is false (e.g. the exact missing capability).
	DenyReason string
}

// configureWriteAttribution resolves the calling credential of a
// /api/test/message request into the attribution the comm configure path
// needs (identity + config:write decision), using the same gate (fedCap)
// the REST config-write routes use — the same session token or peer caps,
// the same capability, the same denials. An unresolvable session
// credential still names itself ("session:unresolved") so a denied comm
// write is auditable against something.
func (s *Server) configureWriteAttribution(r *http.Request) TestMessageAttribution {
	ctx := r.Context()
	if sessionCaps := sessionCapsFromContext(ctx); sessionCaps != nil {
		caller := "session:unresolved"
		if s.sessionTokens != nil {
			if tok := callerTokenFromContext(ctx); tok != "" {
				if id := s.sessionTokens.SessionIDForToken(tok); id != "" {
					caller = "session:" + id
				}
			}
		}
		if !federation.Check(sessionCaps, federation.CapConfigWrite) {
			return TestMessageAttribution{
				Caller:     caller,
				DenyReason: "session token lacks capability: " + federation.CapConfigWrite,
			}
		}
		return TestMessageAttribution{Caller: caller, ConfigWriteAllowed: true}
	}
	if peer := peerFromContext(ctx); peer != nil {
		var custom map[string]*federation.CapabilityGroup
		if s.fedGroupStore != nil {
			custom = s.fedGroupStore.AsMap()
		}
		resolved := federation.Resolve(peer.Capabilities, custom)
		if !federation.Check(resolved, federation.CapConfigWrite) {
			return TestMessageAttribution{
				Caller:     "peer:" + peer.Name,
				DenyReason: "federation peer lacks capability: " + federation.CapConfigWrite,
			}
		}
		return TestMessageAttribution{Caller: "peer:" + peer.Name, ConfigWriteAllowed: true}
	}
	return TestMessageAttribution{} // admin/operator — not a session write
}

// configPutRecorder captures the status code and the (bounded) response
// body so a denied REST write can carry the denial reason in its audit
// record.
type configPutRecorder struct {
	http.ResponseWriter
	status   int
	body     bytes.Buffer
	hardened bool
}

func (c *configPutRecorder) WriteHeader(code int) {
	c.status = code
	c.harden()
	c.ResponseWriter.WriteHeader(code)
}

func (c *configPutRecorder) Write(p []byte) (int, error) {
	c.harden()
	c.body.Write(p) // best effort; the recorder is not the source of truth
	return c.ResponseWriter.Write(p)
}

// harden runs before the first header or body byte leaves the recorder. The
// wrapped config handlers answer with JSON or with http.Error plain text;
// this makes that guarantee hold for any response: nosniff always, and a
// non-HTML content type whenever the handler set none (or set an HTML one),
// so a body that echoes request text can never be rendered as markup.
func (c *configPutRecorder) harden() {
	if c.hardened {
		return
	}
	c.hardened = true
	h := c.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	ct := strings.ToLower(strings.TrimSpace(h.Get("Content-Type")))
	if ct == "" || strings.HasPrefix(ct, "text/html") || strings.HasPrefix(ct, "application/xhtml") {
		h.Set("Content-Type", "text/plain; charset=utf-8")
	}
}
