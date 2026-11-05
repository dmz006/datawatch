package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/trace"
)

type fakeTraceRecorder struct {
	mu  sync.Mutex
	got map[string][]trace.Event
}

func (f *fakeTraceRecorder) RecordEvent(id string, ev trace.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.got == nil {
		f.got = map[string][]trace.Event{}
	}
	f.got[id] = append(f.got[id], ev)
}

func TestTraceDenialAndMCPCallRecordedWithoutChangingOutcome(t *testing.T) {
	s, _ := newMCPCapTestServer(t)
	tok, err := s.sessionTokens.Mint("sess-1", federation.Resolve([]string{"session-default"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	// Baseline without a recorder: status codes the guard produces.
	denied := callMCP(s, http.MethodPost, "/api/mcp/call", tok, `{"tool":"secret_set","args":{}}`)
	allowed := callMCP(s, http.MethodPost, "/api/mcp/call", tok, `{"tool":"get_my_session_id","args":{}}`)

	rec := &fakeTraceRecorder{}
	s.SetTraceRecorder(rec)
	denied2 := callMCP(s, http.MethodPost, "/api/mcp/call", tok, `{"tool":"secret_set","args":{}}`)
	allowed2 := callMCP(s, http.MethodPost, "/api/mcp/call", tok, `{"tool":"get_my_session_id","args":{}}`)
	if denied2.Code != denied.Code || allowed2.Code != allowed.Code || denied2.Code != http.StatusForbidden {
		t.Fatalf("recorder changed guard outcome: denied %d/%d allowed %d/%d", denied.Code, denied2.Code, allowed.Code, allowed2.Code)
	}
	if denied2.Body.String() != denied.Body.String() {
		t.Errorf("denial body changed: %q vs %q", denied.Body.String(), denied2.Body.String())
	}
	evs := rec.got["sess-1"]
	var sawDenial, sawMCP bool
	for _, ev := range evs {
		switch {
		case ev.Kind == trace.KindCapabilityCheck && ev.Outcome == "denied" && ev.Name != "":
			sawDenial = true
		case ev.Kind == trace.KindToolCall && ev.Name == "mcp:get_my_session_id":
			sawMCP = true
		}
	}
	if !sawDenial || !sawMCP {
		t.Errorf("denial=%v mcp=%v events=%+v", sawDenial, sawMCP, evs)
	}
}

func TestTraceIgnoresAdminCallers(t *testing.T) {
	s, _ := newMCPCapTestServer(t)
	rec := &fakeTraceRecorder{}
	s.SetTraceRecorder(rec)
	rr := callMCP(s, http.MethodPost, "/api/mcp/call", "admin-token", `{"tool":"secret_set","args":{}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("admin call: %d", rr.Code)
	}
	if len(rec.got) != 0 {
		t.Errorf("admin call recorded as a session event: %+v", rec.got)
	}
}

func TestTraceRESTCallWithSessionIdentity(t *testing.T) {
	s, _ := newMCPCapTestServer(t)
	tok, _ := s.sessionTokens.Mint("sess-2", federation.Resolve([]string{"session-default"}, nil))
	rec := &fakeTraceRecorder{}
	s.SetTraceRecorder(rec)
	h := s.fedAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	req, _ := http.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status %d", rr.Code)
	}
	evs := rec.got["sess-2"]
	if len(evs) != 1 || evs[0].Name != "rest:GET /api/sessions" || evs[0].Kind != trace.KindToolCall {
		t.Errorf("events: %+v", evs)
	}
}

func TestTracePermissionRelay(t *testing.T) {
	s, _ := newMCPCapTestServer(t)
	rec := &fakeTraceRecorder{}
	s.SetTraceRecorder(rec)
	s.tracePermissionRelay(ctxWithSession(t, s, "sess-3"), "permission_request", "r-9")
	s.tracePermissionRelay(ctxWithSession(t, s, "sess-3"), "info", "r-10") // not a permission event
	evs := rec.got["sess-3"]
	if len(evs) != 1 || evs[0].Kind != trace.KindPermissionDecision || !strings.Contains(evs[0].ArgSummary, "r-9") {
		t.Errorf("events: %+v", evs)
	}
}

func ctxWithSession(t *testing.T, s *Server, id string) context.Context {
	t.Helper()
	caps := federation.Resolve([]string{"session-default"}, nil)
	tok, err := s.sessionTokens.Mint(id, caps)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), sessionCapsKey, caps)
	return context.WithValue(ctx, callerTokenKey, tok)
}
