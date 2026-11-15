// Operator-visible tls_cert_fingerprint surfaces: REST create/update
// accept, normalize and validate the pin; the one-line
// non-verification warning appears in the response body; and the
// warn-level alert fires at most once per peer per daemon run through
// TLSPeerWarnTracker.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/server/multiserver"
)

const tlsPinTestHex = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// tlsEmitLog is a concurrency-safe fake alert sink for
// TLSPeerWarnTracker.
type tlsEmitLog struct {
	mu    sync.Mutex
	calls []string // "peerName|warning"
}

func (l *tlsEmitLog) record(peerName, warning string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, peerName+"|"+warning)
}

func (l *tlsEmitLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

func tlsPinServer(t *testing.T) (*Server, *multiserver.Store, *tlsEmitLog) {
	t.Helper()
	st, err := multiserver.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("multiserver.NewStore: %v", err)
	}
	elog := &tlsEmitLog{}
	s := &Server{serverStore: st}
	s.SetTLSPeerWarnTracker(NewTLSPeerWarnTracker(elog.record))
	return s, st, elog
}

func tlsPinDo(t *testing.T, s *Server, method, target, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rr := httptest.NewRecorder()
	if strings.HasPrefix(target, "/api/federation/peers") {
		s.handleFederationPeers(rr, req)
	} else {
		s.handleBL312Servers(rr, req)
	}
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	return rr, out
}

func TestServerAdd_TLSPinValidNormalized(t *testing.T) {
	s, st, elog := tlsPinServer(t)
	body := `{"name":"pinned","url":"http://127.0.0.1:19999","tls_cert_fingerprint":"` +
		strings.ToUpper(tlsPinTestHex[:32]) + ":" + tlsPinTestHex[32:] + `"}`
	rr, out := tlsPinDo(t, s, http.MethodPost, "/api/servers", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/servers: %d %s, want 200", rr.Code, rr.Body.String())
	}
	if out["name"] != "pinned" || out["ok"] != true {
		t.Errorf("response = %v, want name=pinned ok=true", out)
	}
	if _, has := out["warnings"]; has {
		t.Errorf("verified/pinned peer must not carry warnings: %v", out)
	}
	e, ok := st.Get("pinned")
	if !ok {
		t.Fatal("peer not stored")
	}
	if e.TLSCertFingerprint != tlsPinTestHex {
		t.Errorf("stored pin = %q, want normalized %q", e.TLSCertFingerprint, tlsPinTestHex)
	}
	if len(elog.list()) != 0 {
		t.Errorf("pinned peer must not fire the warn alert; sink = %v", elog.list())
	}
}

func TestServerAdd_TLSPinMalformedRejected(t *testing.T) {
	s, st, elog := tlsPinServer(t)
	rr, _ := tlsPinDo(t, s, http.MethodPost, "/api/servers",
		`{"name":"badpin","url":"http://127.0.0.1:19999","tls_cert_fingerprint":"nothex123"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("POST with 9-char pin: %d %s, want 400", rr.Code, rr.Body.String())
	}
	if _, ok := st.Get("badpin"); ok {
		t.Errorf("rejected peer must not be stored")
	}
	if len(elog.list()) != 0 {
		t.Errorf("rejected add must not emit a warning; sink = %v", elog.list())
	}
}

func TestServerAdd_TLSPinWithSkipVerifyRejected(t *testing.T) {
	s, st, _ := tlsPinServer(t)
	rr, _ := tlsPinDo(t, s, http.MethodPost, "/api/servers",
		`{"name":"both","url":"http://127.0.0.1:19999","tls_skip_verify":true,"tls_cert_fingerprint":"`+tlsPinTestHex+`"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("pin + tls_skip_verify: %d %s, want 400", rr.Code, rr.Body.String())
	}
	if _, ok := st.Get("both"); ok {
		t.Errorf("rejected peer must not be stored")
	}
}

func TestServerAdd_SkipVerifyWarnsInBodyAndAlert(t *testing.T) {
	s, _, elog := tlsPinServer(t)
	rr, out := tlsPinDo(t, s, http.MethodPost, "/api/servers",
		`{"name":"skip1","url":"http://127.0.0.1:19999","tls_skip_verify":true}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST skip-verify peer: %d %s, want 200", rr.Code, rr.Body.String())
	}
	warns, _ := out["warnings"].([]any)
	if len(warns) != 1 {
		t.Fatalf("warnings = %v, want exactly one entry", out["warnings"])
	}
	w, _ := warns[0].(string)
	for _, want := range []string{"skip1:", "certificate is not verified", "tls_cert_fingerprint"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning %q missing %q", w, want)
		}
	}
	calls := elog.list()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "skip1|") {
		t.Errorf("warn alert sink = %v, want exactly one emission for skip1", calls)
	}
}

func TestServerAdd_VerifiedNoWarningNoAlert(t *testing.T) {
	s, _, elog := tlsPinServer(t)
	rr, out := tlsPinDo(t, s, http.MethodPost, "/api/servers",
		`{"name":"plain","url":"http://127.0.0.1:19999"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST verified peer: %d %s, want 200", rr.Code, rr.Body.String())
	}
	if _, has := out["warnings"]; has {
		t.Errorf("CA-verified peer must not carry warnings: %v", out)
	}
	if len(elog.list()) != 0 {
		t.Errorf("verified peer must not fire the warn alert; sink = %v", elog.list())
	}
}

func TestServerUpdate_TLSPinMalformedAndBothRejected(t *testing.T) {
	s, st, _ := tlsPinServer(t)
	if rr, _ := tlsPinDo(t, s, http.MethodPost, "/api/servers", `{"name":"u1","url":"http://127.0.0.1:19999"}`); rr.Code != http.StatusOK {
		t.Fatalf("seed add: %d %s", rr.Code, rr.Body.String())
	}
	rr, _ := tlsPinDo(t, s, http.MethodPut, "/api/servers/u1",
		`{"name":"u1","url":"http://127.0.0.1:19999","tls_cert_fingerprint":"zz"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT 2-char pin: %d %s, want 400", rr.Code, rr.Body.String())
	}
	rr, _ = tlsPinDo(t, s, http.MethodPut, "/api/servers/u1",
		`{"name":"u1","url":"http://127.0.0.1:19999","tls_skip_verify":true,"tls_cert_fingerprint":"`+tlsPinTestHex+`"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT pin + tls_skip_verify: %d %s, want 400", rr.Code, rr.Body.String())
	}
	e, ok := st.Get("u1")
	if !ok {
		t.Fatal("peer missing after rejections")
	}
	if e.TLSCertFingerprint != "" {
		t.Errorf("stored pin after rejections = %q, want unchanged (empty)", e.TLSCertFingerprint)
	}
}

func TestServerUpdate_SkipVerifyWarnsAndAlertDedupes(t *testing.T) {
	s, _, elog := tlsPinServer(t)
	rr, out := tlsPinDo(t, s, http.MethodPost, "/api/servers",
		`{"name":"dup","url":"http://127.0.0.1:19999","tls_skip_verify":true}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("add skip-verify: %d %s", rr.Code, rr.Body.String())
	}
	if _, has := out["warnings"]; !has {
		t.Fatalf("add response missing warnings: %v", out)
	}
	// A later update of the same still-unverified peer warns in the body
	// again but must NOT re-raise the alert (once per peer per daemon run).
	rr, out = tlsPinDo(t, s, http.MethodPut, "/api/servers/dup",
		`{"name":"dup","url":"http://127.0.0.1:19999","tls_skip_verify":true,"label":"self-signed"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("update skip-verify: %d %s", rr.Code, rr.Body.String())
	}
	if _, has := out["warnings"]; !has {
		t.Errorf("update response of unverified peer missing warnings: %v", out)
	}
	calls := elog.list()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "dup|") {
		t.Errorf("warn alert sink = %v, want exactly one emission for dup", calls)
	}
}

func TestServerUpdate_PinningRemovesWarning(t *testing.T) {
	s, st, elog := tlsPinServer(t)
	if rr, _ := tlsPinDo(t, s, http.MethodPost, "/api/servers",
		`{"name":"pinme","url":"http://127.0.0.1:19999","tls_skip_verify":true}`); rr.Code != http.StatusOK {
		t.Fatalf("add skip-verify: %d %s", rr.Code, rr.Body.String())
	}
	rr, out := tlsPinDo(t, s, http.MethodPut, "/api/servers/pinme",
		`{"name":"pinme","url":"http://127.0.0.1:19999","tls_cert_fingerprint":"`+tlsPinTestHex+`"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("add pin: %d %s", rr.Code, rr.Body.String())
	}
	if _, has := out["warnings"]; has {
		t.Errorf("pinned update must not warn: %v", out)
	}
	e, ok := st.Get("pinme")
	if !ok {
		t.Fatal("peer missing")
	}
	if e.TLSSkipVerify || e.TLSCertFingerprint != tlsPinTestHex {
		t.Errorf("entry = skip=%v pin=%q, want skip=false pin=%q", e.TLSSkipVerify, e.TLSCertFingerprint, tlsPinTestHex)
	}
	if len(elog.list()) != 1 {
		t.Errorf("sink = %v, want the single earlier emission", elog.list())
	}
}

func TestFedPeerAdd_TLSPinValidRejectedVariants(t *testing.T) {
	s, st, elog := tlsPinServer(t)
	// Valid pin — normalized, no warning, no alert.
	pretty := strings.ToUpper(tlsPinTestHex[:32]) + ": " + tlsPinTestHex[32:]
	rr, out := tlsPinDo(t, s, http.MethodPost, "/api/federation/peers",
		`{"name":"fp1","url":"http://127.0.0.1:19999","tls_cert_fingerprint":"`+pretty+`"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("federation peer add (pin): %d %s, want 201", rr.Code, rr.Body.String())
	}
	if out["name"] != "fp1" || out["tls_cert_fingerprint"] != tlsPinTestHex {
		t.Errorf("response = %v, want name=fp1 with normalized pin", out)
	}
	if _, has := out["warnings"]; has {
		t.Errorf("pinned federation peer must not warn: %v", out)
	}
	if len(elog.list()) != 0 {
		t.Errorf("pinned add must not fire the alert; sink = %v", elog.list())
	}

	// Malformed pin — 400, not stored.
	rr, _ = tlsPinDo(t, s, http.MethodPost, "/api/federation/peers",
		`{"name":"fp2","url":"http://127.0.0.1:19999","tls_cert_fingerprint":"abc"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("federation peer add (bad pin): %d %s, want 400", rr.Code, rr.Body.String())
	}
	if _, ok := st.Get("fp2"); ok {
		t.Errorf("rejected peer must not be stored")
	}

	// Pin + tls_skip_verify — 400, not stored.
	rr, _ = tlsPinDo(t, s, http.MethodPost, "/api/federation/peers",
		`{"name":"fp3","url":"http://127.0.0.1:19999","tls_skip_verify":true,"tls_cert_fingerprint":"`+tlsPinTestHex+`"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("federation peer add (pin+skip): %d %s, want 400", rr.Code, rr.Body.String())
	}
	if _, ok := st.Get("fp3"); ok {
		t.Errorf("rejected peer must not be stored")
	}
}

func TestFedPeerAdd_SkipVerifyWarns(t *testing.T) {
	s, _, elog := tlsPinServer(t)
	rr, out := tlsPinDo(t, s, http.MethodPost, "/api/federation/peers",
		`{"name":"fskip","url":"http://127.0.0.1:19999","token":"secret","tls_skip_verify":true}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("federation peer add (skip): %d %s, want 201", rr.Code, rr.Body.String())
	}
	warns, _ := out["warnings"].([]any)
	if len(warns) != 1 || !strings.Contains(warns[0].(string), "certificate is not verified") {
		t.Errorf("warnings = %v, want one non-verification warning", out["warnings"])
	}
	// The token must never be echoed in the create response.
	if tok, present := out["token"]; present && tok != "" {
		t.Errorf("create response must redact the token; got %v", tok)
	}
	if tp, _ := out["token_prefix"].(string); tp != "secr" {
		t.Errorf("token_prefix = %v, want first 4 chars \"secr\" of the redacted prefix", out["token_prefix"])
	}
	if got := elog.list(); len(got) != 1 || !strings.HasPrefix(got[0], "fskip|") {
		t.Errorf("sink = %v, want one emission for fskip", got)
	}
}

func TestFedPeerUpdate_SkipVerifyWarnsAndDedupes(t *testing.T) {
	s, _, elog := tlsPinServer(t)
	if rr, _ := tlsPinDo(t, s, http.MethodPost, "/api/federation/peers",
		`{"name":"fup","url":"http://127.0.0.1:19999"}`); rr.Code != http.StatusCreated {
		t.Fatalf("seed add: %d %s", rr.Code, rr.Body.String())
	}
	rr, out := tlsPinDo(t, s, http.MethodPut, "/api/federation/peers/fup", `{"tls_skip_verify":true}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("update to skip-verify: %d %s, want 200", rr.Code, rr.Body.String())
	}
	if _, has := out["warnings"]; !has {
		t.Errorf("update response of unverified peer missing warnings: %v", out)
	}
	rr, out = tlsPinDo(t, s, http.MethodPut, "/api/federation/peers/fup",
		`{"label":"re-confirmed"}`) // partial update keeps tls_skip_verify
	if rr.Code != http.StatusOK {
		t.Fatalf("partial update: %d %s, want 200", rr.Code, rr.Body.String())
	}
	if _, has := out["warnings"]; !has {
		t.Errorf("still-unverified peer must warn again in the body: %v", out)
	}
	calls := elog.list()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "fup|") {
		t.Errorf("sink = %v, want exactly one emission for fup", calls)
	}
}

func TestTLSPeerWarnTracker_DedupeAndNilSafety(t *testing.T) {
	elog := &tlsEmitLog{}
	tr := NewTLSPeerWarnTracker(elog.record)

	unverified := &multiserver.Entry{Name: "a", URL: "http://x", TLSSkipVerify: true}
	verified := &multiserver.Entry{Name: "a", URL: "http://x"}
	pinnedOK := &multiserver.Entry{Name: "b", URL: "http://x", TLSCertFingerprint: tlsPinTestHex}
	other := &multiserver.Entry{Name: "c", URL: "http://x", TLSSkipVerify: true}

	tr.Notify(nil)        // nil entry — must not panic or emit
	tr.Notify(verified)   // CA-verified — silent
	tr.Notify(pinnedOK)   // pinned — silent
	tr.Notify(unverified) // first sighting — emit
	tr.Notify(unverified) // repeat — deduped
	tr.Notify(other)      // different peer — emit

	calls := elog.list()
	if len(calls) != 2 {
		t.Fatalf("sink = %v, want exactly 2 emissions (a, then c)", calls)
	}
	if !strings.HasPrefix(calls[0], "a|") || !strings.HasPrefix(calls[1], "c|") {
		t.Errorf("sink = %v, want emissions in order [a, c]", calls)
	}
}
