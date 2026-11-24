package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

func credServer(t *testing.T) (*Server, *auth.SessionTokenStore, *audit.Log) {
	t.Helper()
	srv := newLLMServer(t)
	srv.cfg = &config.Config{}
	store, err := auth.NewSessionTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv.SetSessionTokenStore(store)
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.SetAuditLog(al)
	return srv, store, al
}

// withToken returns r as the middleware would present it for a request that
// authenticated with a session-scoped token.
func withToken(r *http.Request, store *auth.SessionTokenStore, tok string) *http.Request {
	caps, _ := store.CapsForToken(tok)
	ctx := context.WithValue(r.Context(), sessionCapsKey, caps)
	ctx = context.WithValue(ctx, callerTokenKey, tok)
	return r.WithContext(ctx)
}

// ledgerAdmit wires a real ledger the way the daemon does for a start: the
// class comes from the context the handler passes.
func ledgerAdmit(srv *Server, led *capacity.Ledger, classes *[]capacity.Class) {
	srv.SetCapacityAdmit(func(ctx context.Context, _, _, holder string, _ int) (string, error) {
		class := capacity.ClassFrom(ctx)
		*classes = append(*classes, class)
		return "", led.Acquire(ctx, capacity.Request{Holder: holder, Class: class, Interactive: class == capacity.ClassOperator, Pools: []string{"llm:claude-code"}}, 0, func() bool { return false }, func(string) {})
	})
}

func startReq(t *testing.T) *http.Request {
	body, _ := json.Marshal(map[string]any{"task": "hello", "project_dir": t.TempDir(), "one_shot": true})
	return httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
}

func supervisorToken(t *testing.T, store *auth.SessionTokenStore, name string) string {
	t.Helper()
	tok, err := store.Mint(supervisorCredentialOwner(name), []string{federation.CapSessionsWrite, federation.CapCapacitySupervisorStart})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// A REST start with the supervisor credential is supervisor class and uses the
// buffer above the cap; the same start without it is an operator start.
func TestSupervisorCredential_StartIsSupervisorClassAndUsesBuffer(t *testing.T) {
	srv, store, _ := credServer(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("llm:claude-code", 1)
	led.SetReserve("llm:claude-code", 1)
	_ = led.Acquire(context.Background(), capacity.Request{Holder: "worker-1", Pools: []string{"llm:claude-code"}}, time.Second, nil, nil)
	var classes []capacity.Class
	ledgerAdmit(srv, led, &classes)
	tok := supervisorToken(t, store, "detector")

	w := httptest.NewRecorder()
	srv.handleStartSession(w, withToken(startReq(t), store, tok))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	cleanupStartedSession(t, srv, w)
	if len(classes) != 1 || classes[0] != capacity.ClassSupervisor {
		t.Fatalf("classes = %v, want [supervisor]", classes)
	}
	var sup *capacity.Lease
	for _, ls := range led.Snapshot().Leases {
		if ls.Class == capacity.ClassSupervisor {
			ls := ls
			sup = &ls
		}
	}
	if sup == nil || len(sup.Overage) != 0 {
		t.Fatalf("supervisor lease = %+v, want one inside the buffer, no overage", sup)
	}

	// No credential: an operator start.
	classes = nil
	w = httptest.NewRecorder()
	srv.handleStartSession(w, startReq(t))
	cleanupStartedSession(t, srv, w)
	if len(classes) != 1 || classes[0] != capacity.ClassOperator {
		t.Fatalf("classes = %v, want [operator] without the credential", classes)
	}
}

// With the buffer used up the supervisor start waits and is refused after its
// wait; it never goes beyond the buffer, and starts nothing.
func TestSupervisorCredential_BufferExhaustedWaitsNeverExceeds(t *testing.T) {
	srv, store, _ := credServer(t)
	srv.cfg.Session.CapacityWaitSeconds = 1
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("llm:claude-code", 1)
	led.SetReserve("llm:claude-code", 1)
	_ = led.Acquire(context.Background(), capacity.Request{Holder: "worker-1", Pools: []string{"llm:claude-code"}}, time.Second, nil, nil)
	_ = led.Acquire(context.Background(), capacity.Request{Holder: "sup-1", Class: capacity.ClassSupervisor, Pools: []string{"llm:claude-code"}}, time.Second, nil, nil)
	var classes []capacity.Class
	ledgerAdmit(srv, led, &classes)
	tok := supervisorToken(t, store, "detector")
	before := len(srv.manager.ListSessions())
	w := httptest.NewRecorder()
	srv.handleStartSession(w, withToken(startReq(t), store, tok))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503 after the wait: %s", w.Code, w.Body.String())
	}
	if len(srv.manager.ListSessions()) != before {
		t.Fatal("no session may start when the buffer is exhausted")
	}
	for _, p := range led.Snapshot().Pools {
		if p.Name == "llm:claude-code" && p.Held > 2 {
			t.Fatalf("pool held %d, supervisors must never exceed limit plus buffer (2)", p.Held)
		}
	}
}

// A session token with the capability but not minted as a supervisor
// credential, a forged header and a credential with the setting off are all
// operator starts.
func TestSupervisorCredential_NothingElseGrantsTheClass(t *testing.T) {
	srv, store, _ := credServer(t)
	caps := []string{federation.CapSessionsWrite, federation.CapCapacitySupervisorStart}
	selfTok, _ := store.Mint("sess-abc", caps)
	r := withToken(startReq(t), store, selfTok)
	if c, _ := srv.startAdmissionClass(r); c != capacity.ClassOperator {
		t.Fatalf("a session token claiming the capability got %q, want operator", c)
	}
	r = startReq(t)
	r.Header.Set(InternalLaunchHeader, "supervisor:forged")
	if c, _ := srv.startAdmissionClass(r); c != capacity.ClassOperator {
		t.Fatalf("forged header got %q, want operator", c)
	}
	tok := supervisorToken(t, store, "detector")
	if c, name := srv.startAdmissionClass(withToken(startReq(t), store, tok)); c != capacity.ClassSupervisor || name != "detector" {
		t.Fatalf("credential got %q / %q, want supervisor / detector", c, name)
	}
	off := false
	srv.cfg.Autonomous.CapacitySupervisorCredentialEnabled = &off
	if c, _ := srv.startAdmissionClass(withToken(startReq(t), store, tok)); c != capacity.ClassOperator {
		t.Fatalf("credential with the setting off got %q, want operator", c)
	}
}

// Every credentialed start is audited with the credential name.
func TestSupervisorCredential_StartIsAudited(t *testing.T) {
	srv, store, al := credServer(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("llm:claude-code", 1)
	led.SetReserve("llm:claude-code", 1)
	var classes []capacity.Class
	ledgerAdmit(srv, led, &classes)
	tok := supervisorToken(t, store, "detector")
	w := httptest.NewRecorder()
	srv.handleStartSession(w, withToken(startReq(t), store, tok))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	cleanupStartedSession(t, srv, w)
	got, err := al.Read(audit.QueryFilter{Action: audit.ActionSupervisorCredentialStart})
	if err != nil || len(got) != 1 {
		t.Fatalf("audit entries = %v, err %v, want 1", got, err)
	}
	if got[0].Actor != "supervisor-credential:detector" || got[0].Details["credential"] != "detector" {
		t.Fatalf("entry = %+v, want the credential name as actor and detail", got[0])
	}
}

func credReq(method, path, body string, store *auth.SessionTokenStore, tok string) *http.Request {
	r := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	if tok != "" {
		r = withToken(r, store, tok)
	}
	return r
}

// An operator mints, lists (names only) and revokes the credential; a revoked
// credential no longer resolves.
func TestSupervisorCredential_MintListRevoke(t *testing.T) {
	srv, store, al := credServer(t)
	w := httptest.NewRecorder()
	srv.handleSupervisorCredential(w, credReq(http.MethodPost, "/api/capacity/supervisor-credential", `{"name":"detector"}`, store, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("mint status %d: %s", w.Code, w.Body.String())
	}
	var minted struct{ Name, Token string }
	if err := json.Unmarshal(w.Body.Bytes(), &minted); err != nil || minted.Token == "" || minted.Name != "detector" {
		t.Fatalf("mint body = %s (%v)", w.Body.String(), err)
	}
	if caps, ok := store.CapsForToken(minted.Token); !ok || len(caps) != 2 {
		t.Fatalf("caps = %v ok %v, want sessions:write and the supervisor capability only", caps, ok)
	}
	w = httptest.NewRecorder()
	srv.handleSupervisorCredential(w, credReq(http.MethodGet, "/api/capacity/supervisor-credential", "", store, ""))
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"detector"`)) || bytes.Contains(w.Body.Bytes(), []byte(minted.Token)) {
		t.Fatalf("list %d %s: must name the credential and never show the token", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	srv.handleSupervisorCredential(w, credReq(http.MethodDelete, "/api/capacity/supervisor-credential?name=detector", "", store, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("revoke status %d: %s", w.Code, w.Body.String())
	}
	if _, ok := store.CapsForToken(minted.Token); ok {
		t.Fatal("a revoked credential must stop resolving")
	}
	for _, a := range []string{audit.ActionSupervisorCredentialMint, audit.ActionSupervisorCredentialRevoke} {
		if got, _ := al.Read(audit.QueryFilter{Action: a}); len(got) != 1 {
			t.Fatalf("audit %s entries = %d, want 1", a, len(got))
		}
	}
}

// A session credential and a federation peer cannot mint, list or revoke.
func TestSupervisorCredential_OnlyTheOperatorMints(t *testing.T) {
	srv, store, _ := credServer(t)
	sessTok, _ := store.Mint("sess-abc", []string{federation.CapSessionsWrite, federation.CapConfigWrite, federation.CapCapacitySupervisorStart})
	for _, m := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		w := httptest.NewRecorder()
		srv.handleSupervisorCredential(w, credReq(m, "/api/capacity/supervisor-credential?name=x", `{"name":"x"}`, store, sessTok))
		if w.Code != http.StatusForbidden {
			t.Errorf("session token %s: status %d, want 403", m, w.Code)
		}
		w = httptest.NewRecorder()
		r := credReq(m, "/api/capacity/supervisor-credential?name=x", `{"name":"x"}`, store, "")
		r = r.WithContext(context.WithValue(r.Context(), fedPeerKey, &multiserver.Entry{Name: "peer-a"}))
		srv.handleSupervisorCredential(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("peer %s: status %d, want 403", m, w.Code)
		}
	}
	w := httptest.NewRecorder()
	srv.handleSupervisorCredential(w, credReq(http.MethodPost, "/api/capacity/supervisor-credential", `{"name":"bad name!"}`, store, ""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a bad name: status %d, want 400", w.Code)
	}
}
