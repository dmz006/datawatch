// BL110 extension — exactly one audit record per REST config-write route
// call (surface "rest"): one per PUT /api/config and PUT /api/cost/rates,
// with the caller session id, the changed keys, secret-like values
// redacted, the denied reason on failure, and no record for admin callers.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
	"github.com/dmz006/datawatch/internal/session"
)

// ctxWithPeer is the server package's own federation-peer context key —
// the caller REST audit path must resolve through the same key the live
// middleware uses, not the MCP one.
func ctxWithPeer(name string, caps ...string) context.Context {
	return context.WithValue(context.Background(), fedPeerKey, &multiserver.Entry{Name: name, Capabilities: caps})
}

func readBL110Audit(t *testing.T, path string) []federation.ConfigAuditRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []federation.ConfigAuditRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec federation.ConfigAuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad audit line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func newBL110ConfigServer(t *testing.T, auditPath string) *Server {
	t.Helper()
	return &Server{
		cfg:         config.DefaultConfig(),
		cfgPath:     filepath.Join(t.TempDir(), "config.yaml"),
		configAudit: federation.NewConfigAuditor(auditPath, true),
	}
}

func TestRestConfigAudit_SessionCaller_OneRecordAllowed(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	s := newBL110ConfigServer(t, auditPath)
	body := `{"dashboard.console.enabled":true,"gitlab.token":"glpat-SECRET123"}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewBufferString(body))
	req = req.WithContext(ctxWithPeer("peer-1"))
	w := httptest.NewRecorder()
	s.auditConfigPut("/api/config", s.handlePutConfig)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT /api/config = %d: %s", w.Code, w.Body.String())
	}
	recs := readBL110Audit(t, auditPath)
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 audit record, got %d", len(recs))
	}
	rec := recs[0]
	if rec.Surface != "rest" {
		t.Errorf("surface = %q, want rest", rec.Surface)
	}
	if rec.Tool != "PUT /api/config" {
		t.Errorf("route = %q, want PUT /api/config", rec.Tool)
	}
	if rec.Caller != "peer:peer-1" {
		t.Errorf("caller = %q, want peer:peer-1", rec.Caller)
	}
	if !rec.Allowed {
		t.Errorf("want allowed=true")
	}
	if _, ok := rec.Values["dashboard.console.enabled"]; !ok {
		t.Errorf("changed key missing from record: %+v", rec.Values)
	}
	if rec.Values["gitlab.token"] == "glpat-SECRET123" {
		t.Fatalf("secret value not redacted: %+v", rec.Values)
	}
}

func TestRestConfigAudit_AdminCaller_NoRecord(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	s := newBL110ConfigServer(t, auditPath)
	body := `{"dashboard.console.enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.auditConfigPut("/api/config", s.handlePutConfig)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT /api/config = %d: %s", w.Code, w.Body.String())
	}
	if recs := readBL110Audit(t, auditPath); len(recs) != 0 {
		t.Fatalf("admin caller must not produce an audit record: %+v", recs)
	}
}

func TestRestConfigAudit_Denied_RecordsReason(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	s := newBL110ConfigServer(t, auditPath)
	// scope_lint with an unknown mode rejects the whole PUT (400).
	body := `{"autonomous.scope_lint":"bogus"}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewBufferString(body))
	req = req.WithContext(ctxWithPeer("peer-deny"))
	w := httptest.NewRecorder()
	s.auditConfigPut("/api/config", s.handlePutConfig)(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	recs := readBL110Audit(t, auditPath)
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 audit record for the denied PUT, got %d", len(recs))
	}
	if recs[0].Allowed {
		t.Fatalf("denied PUT must be recorded with allowed=false: %+v", recs[0])
	}
	if !strings.Contains(recs[0].Reason, "scope_lint") {
		t.Errorf("denied reason does not mention the rejected key: %q", recs[0].Reason)
	}
	if recs[0].Caller != "peer:peer-deny" {
		t.Errorf("caller = %q, want peer:peer-deny", recs[0].Caller)
	}
}

func TestRestConfigAudit_CostRates_OneRecordAllowed(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	s := newBL110ConfigServer(t, auditPath)
	manager, err := session.NewManager("h", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	manager.WithFakeTmux()
	trackSessionManager(t, manager)
	s.manager = manager
	body := `{"rates":{"claude":{"in_per_k":1.5,"out_per_k":2}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/cost/rates", bytes.NewBufferString(body))
	req = req.WithContext(ctxWithPeer("peer-cost", "config:write"))
	w := httptest.NewRecorder()
	s.handleCostRates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT /api/cost/rates = %d: %s", w.Code, w.Body.String())
	}
	recs := readBL110Audit(t, auditPath)
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 audit record, got %d", len(recs))
	}
	if recs[0].Surface != "rest" || recs[0].Tool != "PUT /api/cost/rates" {
		t.Errorf("surface/route = %q/%q, want rest/PUT /api/cost/rates", recs[0].Surface, recs[0].Tool)
	}
	if !recs[0].Allowed || recs[0].Caller != "peer:peer-cost" {
		t.Errorf("allowed/caller = %v/%q", recs[0].Allowed, recs[0].Caller)
	}
}

// TestRestConfigAudit_CostRates_RecordsIndividualRateEntries (BL110 fix)
// — a PUT /api/cost/rates names the individual rate entries that were
// written (one per backend), not a lumped "rates" key.
func TestRestConfigAudit_CostRates_RecordsIndividualRateEntries(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	s := newBL110ConfigServer(t, auditPath)
	manager, err := session.NewManager("h", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	manager.WithFakeTmux()
	trackSessionManager(t, manager)
	s.manager = manager
	body := `{"rates":{"claude":{"in_per_k":1.5,"out_per_k":2},"openai":{"in_per_k":3,"out_per_k":4}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/cost/rates", bytes.NewBufferString(body))
	req = req.WithContext(ctxWithPeer("peer-cost2", "config:write"))
	w := httptest.NewRecorder()
	s.handleCostRates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT /api/cost/rates = %d: %s", w.Code, w.Body.String())
	}
	recs := readBL110Audit(t, auditPath)
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 audit record, got %d", len(recs))
	}
	if _, lumped := recs[0].Values["rates"]; lumped {
		t.Errorf("lumped \"rates\" key must not appear in the record: %+v", recs[0].Values)
	}
	if v, ok := recs[0].Values["rates.claude"]; !ok || v == `""` {
		t.Errorf("rates.claude entry missing from record: %+v", recs[0].Values)
	}
	if v, ok := recs[0].Values["rates.openai"]; !ok || v == `""` {
		t.Errorf("rates.openai entry missing from record: %+v", recs[0].Values)
	}
}

// TestConfigureWriteAttribution (BL110 fix) — the /api/test/message
// layer resolves the calling credential to the comm configure path with
// the SAME gate the REST config-write routes use:
//
//   - session token with config:write → allowed, names the session
//   - session token without it        → denied, names the session + cap
//   - peer with config:write          → allowed, names the peer
//   - peer without it                 → denied, names the peer + cap
//   - admin (no scoped credential)    → zero attribution (operator)
func TestConfigureWriteAttribution(t *testing.T) {
	s := &Server{}
	st, err := auth.NewSessionTokenStore("")
	if err != nil {
		t.Fatalf("NewSessionTokenStore: %v", err)
	}
	s.sessionTokens = st
	denyTok, err := st.Mint("s-no-cap", []string{"session-default"})
	if err != nil {
		t.Fatalf("mint deny token: %v", err)
	}
	allowTok, err := st.Mint("s-write-cap", []string{"session-default", "config:write"})
	if err != nil {
		t.Fatalf("mint allow token: %v", err)
	}

	ctxSessDeny := context.WithValue(context.Background(), sessionCapsKey, []string{"session-default"})
	ctxSessDeny = context.WithValue(ctxSessDeny, callerTokenKey, denyTok)
	ctxSessAllow := context.WithValue(context.Background(), sessionCapsKey, []string{"session-default", "config:write"})
	ctxSessAllow = context.WithValue(ctxSessAllow, callerTokenKey, allowTok)

	t.Run("session_token_without_config_write", func(t *testing.T) {
		at := s.configureWriteAttribution(httptest.NewRequest(http.MethodPost, "/api/test/message", nil).WithContext(ctxSessDeny))
		if at.ConfigWriteAllowed {
			t.Fatalf("session without config:write must be denied: %+v", at)
		}
		if at.Caller != "session:s-no-cap" {
			t.Errorf("caller = %q, want session:s-no-cap", at.Caller)
		}
		if !strings.Contains(at.DenyReason, federation.CapConfigWrite) {
			t.Errorf("deny reason must name the missing capability, got %q", at.DenyReason)
		}
	})
	t.Run("session_token_with_config_write", func(t *testing.T) {
		at := s.configureWriteAttribution(httptest.NewRequest(http.MethodPost, "/api/test/message", nil).WithContext(ctxSessAllow))
		if !at.ConfigWriteAllowed {
			t.Fatalf("session with config:write must be allowed: %+v", at)
		}
		if at.Caller != "session:s-write-cap" {
			t.Errorf("caller = %q, want session:s-write-cap", at.Caller)
		}
	})
	t.Run("peer_with_config_write", func(t *testing.T) {
		at := s.configureWriteAttribution(httptest.NewRequest(http.MethodPost, "/api/test/message", nil).WithContext(ctxWithPeer("peer-allow", "config:write")))
		if !at.ConfigWriteAllowed {
			t.Fatalf("peer with config:write must be allowed: %+v", at)
		}
		if at.Caller != "peer:peer-allow" {
			t.Errorf("caller = %q, want peer:peer-allow", at.Caller)
		}
	})
	t.Run("peer_without_config_write", func(t *testing.T) {
		at := s.configureWriteAttribution(httptest.NewRequest(http.MethodPost, "/api/test/message", nil).WithContext(ctxWithPeer("peer-deny", "chat:send")))
		if at.ConfigWriteAllowed {
			t.Fatalf("peer without config:write must be denied: %+v", at)
		}
		if at.Caller != "peer:peer-deny" {
			t.Errorf("caller = %q, want peer:peer-deny", at.Caller)
		}
		if !strings.Contains(at.DenyReason, federation.CapConfigWrite) {
			t.Errorf("deny reason must name the missing capability, got %q", at.DenyReason)
		}
	})
	t.Run("admin_call_is_operator_not_a_session_write", func(t *testing.T) {
		at := s.configureWriteAttribution(httptest.NewRequest(http.MethodPost, "/api/test/message", nil))
		if at.Caller != "" || at.ConfigWriteAllowed || at.DenyReason != "" {
			t.Fatalf("admin call must be zero attribution (operator), got %+v", at)
		}
	})
}

// The recorder never lets a wrapped handler's body go out as markup: nosniff
// always, and a plain-text type when the handler set none or an HTML one.
func TestConfigPutRecorderHardensResponse(t *testing.T) {
	cases := map[string]struct{ set, want string }{
		"unset":     {"", "text/plain; charset=utf-8"},
		"html":      {"text/html; charset=utf-8", "text/plain; charset=utf-8"},
		"json kept": {"application/json", "application/json"},
	}
	for name, c := range cases {
		for _, viaHeader := range []bool{true, false} {
			w := httptest.NewRecorder()
			if c.set != "" {
				w.Header().Set("Content-Type", c.set)
			}
			rec := &configPutRecorder{ResponseWriter: w, status: 200}
			if viaHeader {
				rec.WriteHeader(http.StatusBadRequest)
			}
			if _, err := rec.Write([]byte("<script>alert(1)</script>")); err != nil {
				t.Fatal(err)
			}
			if got := w.Header().Get("Content-Type"); got != c.want {
				t.Errorf("%s (header first=%v): Content-Type = %q, want %q", name, viaHeader, got, c.want)
			}
			if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("%s: X-Content-Type-Options = %q, want nosniff", name, got)
			}
			if !strings.Contains(rec.body.String(), "alert(1)") {
				t.Errorf("%s: recorder lost the body", name)
			}
		}
	}
}
