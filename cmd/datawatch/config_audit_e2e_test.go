// BL110 followup (verifier issue 4) — daemon-level end-to-end proof that
// `datawatch start` wires ONE shared config-write audit sink into every
// surface, so a config write by a session lands exactly ONE record no
// matter which surface it arrived on:
//
//	- REST  PUT /api/config          → surface "rest"
//	- comm  configure <key>=<value>  → surface "comm" (the loopback PUT the
//	                                  comm path issues is admin, so the
//	                                  REST point skips it — no double record)
//
// and that operator (admin) writes produce no record at all. Uses the
// real plumbing from runStart: configAuditSinkFor's default path, the
// real HTTPServer on a real port, and the real daemon comm-configure
// closure (loopback REST PUT).

package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/messaging"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
)

// e2eMsgBackend is a no-op messaging.Backend for the test router; the
// test-message path captures its own sends, so nothing needs recording
// here.
type e2eMsgBackend struct{}

func (e2eMsgBackend) Name() string           { return "e2ecap" }
func (e2eMsgBackend) Send(_, _ string) error { return nil }
func (b e2eMsgBackend) Subscribe(ctx context.Context, _ func(messaging.Message)) error {
	<-ctx.Done()
	return nil
}
func (e2eMsgBackend) Link(_ string, _ func(string)) error { return nil }
func (e2eMsgBackend) SelfID() string                      { return "e2e" }
func (e2eMsgBackend) Close() error                        { return nil }

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func readAuditFile(t *testing.T, path string) []federation.ConfigAuditRecord {
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

func TestConfigAudit_Daemon_EndToEnd_SharedSinkAcrossSurfaces(t *testing.T) {
	dataDir := t.TempDir()
	port := freePort(t)

	cfg := config.DefaultConfig()
	cfg.DataDir = dataDir
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = port
	cfg.Server.Token = "admintok" // token-protected daemon: admin presents it, sessions get scoped tokens

	auditPath := filepath.Join(dataDir, "audit", "config-self-modify.jsonl")

	manager, err := session.NewManager("h", dataDir, "", time.Hour)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	sink := configAuditSinkFor(cfg)
	if got := cfg.MCP.SelfConfigAuditPath; got != auditPath {
		t.Fatalf("daemon default audit path = %q, want %q", got, auditPath)
	}

	s := server.New(&cfg.Server, cfg, filepath.Join(dataDir, "config.yaml"), dataDir, manager, "h", nil)
	s.SetConfigAuditor(sink) // BL110 — rest point, shared sink

	st, err := auth.NewSessionTokenStore("")
	if err != nil {
		t.Fatalf("NewSessionTokenStore: %v", err)
	}
	s.SetSessionTokenStore(st)
	denyTok, err := st.Mint("sess-deny", []string{"sessions:list"})
	if err != nil {
		t.Fatalf("mint deny token: %v", err)
	}
	allowTok, err := st.Mint("sess-allow", []string{"sessions:list", federation.CapConfigWrite})
	if err != nil {
		t.Fatalf("mint allow token: %v", err)
	}

	// The daemon's exact comm-configure plumbing (main.go runStart):
	// real Router + the loopback REST PUT closure + the shared sink.
	r := router.NewRouter("h", "grp", e2eMsgBackend{}, nil, 0, nil)
	r.SetWebPort(port)
	r.SetWebToken(cfg.Server.Token)
	r.SetConfigAuditor(sink)
	r.SetConfigureFunc(func(key, value string) error {
		apiURL := loopbackBaseURL(cfg) + "/api/config"
		body := configureRequestBody(key, value)
		req, err := http.NewRequest(http.MethodPut, apiURL, strings.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.Server.Token != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode != 200 {
			if msg, _ := io.ReadAll(resp.Body); strings.TrimSpace(string(msg)) != "" {
				return &auditE2EErr{strings.TrimSpace(string(msg))}
			}
			return &auditE2EErr{resp.Status}
		}
		return nil
	})
	// The daemon's /api/test/message wiring (main.go): REST attribution →
	// router ConfigureAttribution.
	s.SetTestMessageHandler(func(text string, at server.TestMessageAttribution) []string {
		return r.HandleAttributedTestMessage(text, router.ConfigureAttribution{
			Caller:             at.Caller,
			ConfigWriteAllowed: at.ConfigWriteAllowed,
			DenyReason:         at.DenyReason,
		})
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = s.Start(ctx) }()

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(base + "/api/health")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not come up: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	put := func(tok, body string) int {
		req, _ := http.NewRequest(http.MethodPut, base+"/api/config", strings.NewReader(body))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		resp.Body.Close()              //nolint:errcheck
		return resp.StatusCode
	}
	testMsg := func(tok, text string) (int, []string) {
		body, _ := json.Marshal(map[string]string{"text": text})
		req, _ := http.NewRequest(http.MethodPost, base+"/api/test/message", strings.NewReader(string(body)))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Responses []string `json:"responses"`
		}
		data, _ := io.ReadAll(resp.Body) //nolint:errcheck
		resp.Body.Close()                //nolint:errcheck
		_ = json.Unmarshal(data, &out)
		return resp.StatusCode, out.Responses
	}

	if code := put(cfg.Server.Token, `{"dashboard.console.enabled":true}`); code != 200 {
		t.Fatalf("admin PUT /api/config = %d, want 200", code)
	}
	if recs := readAuditFile(t, auditPath); len(recs) != 0 {
		t.Fatalf("operator (admin) write must not be audited, got %d record(s): %+v", len(recs), recs)
	}

	if code := put(denyTok, `{"dashboard.console.enabled":false}`); code != 403 {
		t.Fatalf("session PUT /api/config without config:write = %d, want 403", code)
	}
	recs := readAuditFile(t, auditPath)
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 record after the denied REST write, got %d: %+v", len(recs), recs)
	}
	rec := recs[0]
	if rec.Surface != "rest" || rec.Caller != "session:sess-deny" || rec.Allowed {
		t.Errorf("denied REST record = %+v, want surface=rest caller=session:sess-deny allowed=false", rec)
	}
	if !strings.Contains(strings.ToLower(rec.Reason), federation.CapConfigWrite) {
		t.Errorf("denial reason must name the missing capability: %q", rec.Reason)
	}
	if v, ok := rec.Values["dashboard.console.enabled"]; !ok || v == `""` {
		t.Errorf("changed key missing from record: %+v", rec.Values)
	}

	// comm surface, no capability: refused, once, surface "comm" — and the
	// admin loopback PUT this path issues must NOT add a second record.
	code, responses := testMsg(denyTok, "configure dashboard.console.enabled=true")
	if code != 200 {
		t.Fatalf("POST /api/test/message = %d, want 200", code)
	}
	joined := strings.Join(responses, " ")
	if !strings.Contains(strings.ToLower(joined), "config:write") && !strings.Contains(strings.ToLower(joined), "capabilit") {
		t.Errorf("denied comm configure should report the missing capability, got: %q", joined)
	}
	recs = readAuditFile(t, auditPath)
	if len(recs) != 2 {
		t.Fatalf("expected exactly 2 records (no double recording of the admin loopback PUT), got %d: %+v", len(recs), recs)
	}
	rec = recs[1]
	if rec.Surface != "comm" || rec.Caller != "session:sess-deny" || rec.Allowed {
		t.Errorf("denied comm record = %+v, want surface=comm caller=session:sess-deny allowed=false", rec)
	}
	if !strings.Contains(strings.ToLower(rec.Reason), federation.CapConfigWrite) {
		t.Errorf("comm denial reason must name the missing capability: %q", rec.Reason)
	}
	if v, ok := rec.Values["dashboard.console.enabled"]; !ok || v == `""` {
		t.Errorf("changed key missing from the comm record: %+v", rec.Values)
	}

	// Same session flow, WITH the capability: allowed writes, still one
	// record per surface.
	if code := put(allowTok, `{"dashboard.console.enabled":false}`); code != 200 {
		t.Fatalf("session PUT /api/config with config:write = %d, want 200", code)
	}
	code, _ = testMsg(allowTok, "configure dashboard.console.enabled=true")
	if code != 200 {
		t.Fatalf("POST /api/test/message (allowed) = %d, want 200", code)
	}
	recs = readAuditFile(t, auditPath)
	if len(recs) != 4 {
		t.Fatalf("expected exactly 4 records total, got %d: %+v", len(recs), recs)
	}
	if recs[2].Surface != "rest" || recs[2].Caller != "session:sess-allow" || !recs[2].Allowed {
		t.Errorf("allowed REST record = %+v, want surface=rest caller=session:sess-allow allowed=true", recs[2])
	}
	if recs[3].Surface != "comm" || recs[3].Caller != "session:sess-allow" || !recs[3].Allowed {
		t.Errorf("allowed comm record = %+v, want surface=comm caller=session:sess-allow allowed=true", recs[3])
	}
}

// auditE2EErr is a small error for the test-only closure.
type auditE2EErr struct{ s string }

func (e *auditE2EErr) Error() string { return e.s }
