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

// configPutRecorder captures the status code and the (bounded) response
// body so a denied REST write can carry the denial reason in its audit
// record.
type configPutRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (c *configPutRecorder) WriteHeader(code int) {
	c.status = code
	c.ResponseWriter.WriteHeader(code)
}

func (c *configPutRecorder) Write(p []byte) (int, error) {
	c.body.Write(p) // best effort; the recorder is not the source of truth
	n, err := c.ResponseWriter.Write(p)
	return n, err
}
