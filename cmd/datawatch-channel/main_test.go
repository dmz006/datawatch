package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// fakeParent collects the requests that the bridge would send to the
// real datawatch parent, so tests can assert on the wire contract.
type fakeParent struct {
	srv      *httptest.Server
	received []receivedRequest
}

type receivedRequest struct {
	Path   string
	Body   map[string]any
	Bearer string
}

func newFakeParent(t *testing.T) *fakeParent {
	t.Helper()
	fp := &fakeParent{}
	fp.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		fp.received = append(fp.received, receivedRequest{
			Path:   r.URL.Path,
			Body:   m,
			Bearer: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
		})
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fp.srv.Close)
	return fp
}

// helper to build a configured bridge against the fake parent.
func newTestBridge(t *testing.T, fp *fakeParent, sessionID, token string) *bridge {
	t.Helper()
	mcpSrv := server.NewMCPServer(bridgeName, bridgeVersion, server.WithToolCapabilities(true))
	b := &bridge{
		cfg: config{
			channelPort: 0,
			apiURL:      fp.srv.URL,
			token:       token,
			sessionID:   sessionID,
		},
		srv: mcpSrv,
	}
	mcpSrv.AddTool(b.replyTool(), b.handleReply)
	return b
}

func TestReplyTool_PostsToParentWithSessionID(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "ralfthewise-cdbb", "sekret")

	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "reply"
	req.Params.Arguments = map[string]any{
		"text": "hello from claude",
	}

	res, err := b.handleReply(context.Background(), req)
	if err != nil || res == nil {
		t.Fatalf("handleReply: err=%v res=%v", err, res)
	}
	if len(fp.received) != 1 {
		t.Fatalf("expected 1 parent request, got %d", len(fp.received))
	}
	got := fp.received[0]
	if got.Path != "/api/channel/reply" {
		t.Errorf("path = %q want /api/channel/reply", got.Path)
	}
	if got.Body["text"] != "hello from claude" {
		t.Errorf("text = %v", got.Body["text"])
	}
	if got.Body["session_id"] != "ralfthewise-cdbb" {
		t.Errorf("session_id = %v want ralfthewise-cdbb", got.Body["session_id"])
	}
	if got.Bearer != "sekret" {
		t.Errorf("bearer = %q want sekret", got.Bearer)
	}
}

func TestReplyTool_ExplicitSessionIDOverridesEnv(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "env-default", "")

	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "reply"
	req.Params.Arguments = map[string]any{
		"text":       "explicit",
		"session_id": "explicit-id",
	}
	if _, err := b.handleReply(context.Background(), req); err != nil {
		t.Fatalf("handleReply: %v", err)
	}
	if got := fp.received[0].Body["session_id"]; got != "explicit-id" {
		t.Errorf("session_id = %v want explicit-id", got)
	}
}

func TestReplyTool_MissingTextErrors(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "", "")

	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "reply"
	req.Params.Arguments = map[string]any{}

	res, err := b.handleReply(context.Background(), req)
	if err != nil {
		t.Fatalf("handleReply unexpected err: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected IsError result for missing text, got %+v", res)
	}
	if len(fp.received) != 0 {
		t.Errorf("parent should not be hit on bad input, got %d calls", len(fp.received))
	}
}

func TestHTTPSendForwardsAsNotification(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "", "")

	srv := httptest.NewServer(b.httpHandler())
	defer srv.Close()

	body := `{"text":"hello","source":"signal","session_id":"abc"}`
	resp, err := http.Post(srv.URL+"/send", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post /send: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d want 200", resp.StatusCode)
	}
	resp.Body.Close() //nolint:errcheck
	// We don't assert on the broadcast itself — there are no MCP clients
	// connected in unit tests — but the handler must not 500.
}

func TestHTTPSendRejectsNonPOST(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "", "")

	srv := httptest.NewServer(b.httpHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/send")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d want 405", resp.StatusCode)
	}
}

func TestHTTPHealthOK(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "", "")
	srv := httptest.NewServer(b.httpHandler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("health: err=%v code=%v", err, resp.StatusCode)
	}
}

func TestNotifyReadyIdempotent(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "id-1", "")
	b.actualPort = 7000

	if err := b.notifyReady(); err != nil {
		t.Fatalf("notifyReady #1: %v", err)
	}
	if err := b.notifyReady(); err != nil {
		t.Fatalf("notifyReady #2: %v", err)
	}
	if got := len(fp.received); got != 1 {
		t.Errorf("expected 1 ready post (idempotent), got %d", got)
	}
	if fp.received[0].Path != "/api/channel/ready" {
		t.Errorf("path = %q", fp.received[0].Path)
	}
	if fp.received[0].Body["port"].(float64) != 7000 {
		t.Errorf("port = %v want 7000", fp.received[0].Body["port"])
	}
}

// GH#174 — notifyReady's one-shot guard meant the bridge never spoke
// again after its first announce, so a daemon restart (which forgets
// every session's ChannelReady/ChannelPort) left "Waiting for MCP
// channel…" stuck forever. sendReady/readyHeartbeat bypass the guard
// and must keep re-announcing on every tick.
func TestReadyHeartbeat_KeepsReannouncing(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "id-1", "")
	b.actualPort = 7000

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.readyHeartbeat(ctx, 10*time.Millisecond)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(fp.received) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done // confirm ctx.Done() actually stops the loop

	if len(fp.received) < 3 {
		t.Fatalf("expected at least 3 re-announces within the deadline, got %d", len(fp.received))
	}
	for _, r := range fp.received {
		if r.Path != "/api/channel/ready" {
			t.Errorf("heartbeat posted to unexpected path %q", r.Path)
		}
		if r.Body["port"].(float64) != 7000 {
			t.Errorf("heartbeat port = %v, want 7000", r.Body["port"])
		}
	}
}

// Confirms the fix doesn't regress the original one-shot guard: the
// startup notifyReady() call must still only log/flag once, but the
// heartbeat (a separate path, sendReady) must bypass it.
func TestNotifyReady_DoesNotBlockSubsequentHeartbeatSends(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "id-1", "")
	b.actualPort = 7000

	if err := b.notifyReady(); err != nil {
		t.Fatalf("notifyReady: %v", err)
	}
	if err := b.sendReady(); err != nil {
		t.Fatalf("sendReady: %v", err)
	}
	if err := b.sendReady(); err != nil {
		t.Fatalf("sendReady #2: %v", err)
	}
	if got := len(fp.received); got != 3 {
		t.Errorf("expected 3 posts (1 notifyReady + 2 sendReady), got %d", got)
	}
}

func TestPostToParent_FailsOnUnreachable(t *testing.T) {
	// Server immediately closed → connection refused. Ensures the
	// helper surfaces transport errors rather than panicking.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	b := &bridge{cfg: config{apiURL: url}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := b.postToParent(ctx, "/api/channel/reply", map[string]any{"text": "x"}); err == nil {
		t.Fatal("expected error against closed server, got nil")
	}
}

// writeTestCert writes a self-signed cert for 127.0.0.1 and returns its PEM path.
func writeTestCert(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cert.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildTLSConfig_DefaultVerifies(t *testing.T) {
	tc := buildTLSConfig(config{})
	if tc.InsecureSkipVerify {
		t.Fatal("default config must verify the daemon certificate")
	}
	if tc.MinVersion < tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x, want >= TLS 1.2", tc.MinVersion)
	}
}

func TestBuildTLSConfig_CACertBecomesRoot(t *testing.T) {
	tc := buildTLSConfig(config{caCertFile: writeTestCert(t)})
	if tc.InsecureSkipVerify {
		t.Fatal("a CA cert must not disable verification")
	}
	if tc.RootCAs == nil {
		t.Fatal("RootCAs not set from the daemon certificate")
	}
}

func TestBuildTLSConfig_UnreadableCertStillVerifies(t *testing.T) {
	tc := buildTLSConfig(config{caCertFile: filepath.Join(t.TempDir(), "missing.pem")})
	if tc.InsecureSkipVerify {
		t.Fatal("a missing cert must not fall back to skipping verification")
	}
}

func TestBuildTLSConfig_SkipOnlyWithOptIn(t *testing.T) {
	tc := buildTLSConfig(config{insecureSkipVerify: true})
	if !tc.InsecureSkipVerify {
		t.Fatal("opt-in should skip verification")
	}
}

func TestLoadConfig_SkipVerifyDefaultsOff(t *testing.T) {
	t.Setenv("DATAWATCH_CHANNEL_INSECURE_SKIP_VERIFY", "")
	if loadConfig().insecureSkipVerify {
		t.Fatal("skip-verify must default to false")
	}
	t.Setenv("DATAWATCH_CHANNEL_INSECURE_SKIP_VERIFY", "true")
	if !loadConfig().insecureSkipVerify {
		t.Fatal("env opt-in not honoured")
	}
}

// The default config must talk to a self-signed TLS daemon when given its cert,
// and must refuse it without the cert.
func TestProbeDaemon_SelfSignedDaemon(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	certPath := filepath.Join(t.TempDir(), "cert.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := probeDaemon(config{apiURL: srv.URL, caCertFile: certPath}); err != nil {
		t.Fatalf("probe with daemon cert as root: %v", err)
	}
	if err := probeDaemon(config{apiURL: srv.URL}); err == nil {
		t.Fatal("probe without the daemon cert should fail verification")
	}
	if err := probeDaemon(config{apiURL: srv.URL, insecureSkipVerify: true}); err != nil {
		t.Fatalf("probe with opt-in skip: %v", err)
	}
}
