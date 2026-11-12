package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/trace"
)

// SessionTraceRecorder receives trace events of one session. The session
// ingestor (session.TraceIngestor) satisfies it. RecordEvent must not
// block; the hooks below only observe and never alter an authorization
// decision or a response.
type SessionTraceRecorder interface {
	RecordEvent(sessionID string, ev trace.Event)
}

// SetTraceRecorder wires the recorder for capability denials, permission
// relay decisions and calls made with a session identity. nil detaches.
func (s *Server) SetTraceRecorder(r SessionTraceRecorder) { s.traceRecorder = r }

// traceSessionID returns the session whose scoped credential authenticated
// ctx, or "" for admin, federation-peer and anonymous callers.
func (s *Server) traceSessionID(ctx context.Context) string {
	if s == nil || s.traceRecorder == nil || s.sessionTokens == nil {
		return ""
	}
	tok := callerTokenFromContext(ctx)
	if tok == "" || sessionCapsFromContext(ctx) == nil {
		return ""
	}
	return s.sessionTokens.SessionIDForToken(tok)
}

func (s *Server) traceRecord(ctx context.Context, ev trace.Event) {
	if id := s.traceSessionID(ctx); id != "" {
		s.traceRecorder.RecordEvent(id, ev)
	}
}

// traceCapabilityDenied records a federation capability denial of a
// session's own credential.
func (s *Server) traceCapabilityDenied(ctx context.Context, required string) {
	s.traceRecord(ctx, trace.Event{Kind: trace.KindCapabilityCheck, Name: required, Outcome: "denied"})
}

// traceRESTCall records a REST call made with a session credential.
func (s *Server) traceRESTCall(r *http.Request, sessionID string) {
	if s == nil || s.traceRecorder == nil || sessionID == "" {
		return
	}
	s.traceRecorder.RecordEvent(sessionID, trace.Event{
		Kind: trace.KindToolCall, Name: "rest:" + r.Method + " " + r.URL.Path,
	})
}

// traceMCPCall records an MCP tool call made with a session credential.
func (s *Server) traceMCPCall(ctx context.Context, tool string) {
	s.traceRecord(ctx, trace.Event{Kind: trace.KindToolCall, Name: "mcp:" + tool})
}

// tracePermissionRelay records a permission relay notification of a
// session's channel bridge.
func (s *Server) tracePermissionRelay(ctx context.Context, subtype, requestID string) {
	if !strings.Contains(strings.ToLower(subtype), "permission") {
		return
	}
	s.traceRecord(ctx, trace.Event{Kind: trace.KindPermissionDecision, Name: subtype,
		ArgSummary: "request_id=" + requestID, Outcome: "relayed"})
}
