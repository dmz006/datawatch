// observerpeer.Client tests — ported + extended from the original
// cmd/datawatch-stats/peer_test.go when S13 hoisted the client into a
// shared package. New cases cover SetToken (the agent-as-peer flow)
// and Shape selection.

package observerpeer

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dmz006/datawatch/internal/observer"
)

type fakeParent struct {
	srv          *httptest.Server
	mintedTokens map[string]string
	registers    int32
	pushes       int32
	lastBody     atomic.Value // string — last push body
	rejectNext   atomic.Bool
}

func newFakeParent(t *testing.T) *fakeParent {
	t.Helper()
	fp := &fakeParent{mintedTokens: map[string]string{}}
	fp.srv = httptest.NewServer(http.HandlerFunc(fp.handle))
	t.Cleanup(fp.srv.Close)
	return fp
}

func (f *fakeParent) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/observer/peers":
		atomic.AddInt32(&f.registers, 1)
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		token := "t-" + body.Name + "-" + strings.Repeat("x", int(atomic.LoadInt32(&f.registers)))
		f.mintedTokens[body.Name] = token
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":  body.Name,
			"shape": "B",
			"token": token,
		})
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/observer/peers/") && strings.HasSuffix(r.URL.Path, "/stats"):
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "no bearer", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/observer/peers/"), "/stats")
		if f.rejectNext.CompareAndSwap(true, false) {
			http.Error(w, "forced 401", http.StatusUnauthorized)
			return
		}
		want, ok := f.mintedTokens[name]
		if !ok || token != want {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		atomic.AddInt32(&f.pushes, 1)
		buf, _ := io.ReadAll(r.Body)
		f.lastBody.Store(string(buf))
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func newClient(t *testing.T, fp *fakeParent, name, tokenPath string) *Client {
	t.Helper()
	c, err := New(Config{
		ParentURL: fp.srv.URL,
		Name:      name,
		Shape:     "B",
		TokenPath: tokenPath,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNew_RequiresParentAndName(t *testing.T) {
	if _, err := New(Config{Name: "x"}); err == nil {
		t.Errorf("missing parent should error")
	}
	if _, err := New(Config{ParentURL: "http://x"}); err == nil {
		t.Errorf("missing name should error")
	}
}

func TestNew_DefaultShapeIsB(t *testing.T) {
	c, err := New(Config{ParentURL: "http://x", Name: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if c.shape != "B" {
		t.Errorf("default shape = %q want B", c.shape)
	}
}

func TestNew_ExplicitShapeRespected(t *testing.T) {
	c, _ := New(Config{ParentURL: "http://x", Name: "n", Shape: "a"})
	if c.shape != "A" {
		t.Errorf("shape = %q want A (uppercased)", c.shape)
	}
}

func TestRegister_PersistsTokenWithMode0600(t *testing.T) {
	fp := newFakeParent(t)
	tokenPath := filepath.Join(t.TempDir(), "nested", "peer.token")
	c := newClient(t, fp, "ollama", tokenPath)

	if err := c.Register(context.Background(), "0.1", HostInfo()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !c.HasToken() {
		t.Fatal("token missing in memory after Register")
	}
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != fp.mintedTokens["ollama"] {
		t.Errorf("on-disk token = %q want %q", got, fp.mintedTokens["ollama"])
	}
	info, _ := os.Stat(tokenPath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v want 0600", info.Mode().Perm())
	}
}

func TestLoadToken_SkipsRegister(t *testing.T) {
	fp := newFakeParent(t)
	tokenPath := filepath.Join(t.TempDir(), "peer.token")
	if err := os.WriteFile(tokenPath, []byte("preseeded\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newClient(t, fp, "ollama", tokenPath)
	c.LoadToken()
	if !c.HasToken() {
		t.Errorf("expected token loaded from disk")
	}
	if atomic.LoadInt32(&fp.registers) != 0 {
		t.Errorf("LoadToken should not register")
	}
}

func TestSetToken_AgentFlow_SkipsRegister(t *testing.T) {
	// S13 — agent gets token via env var; no register call.
	fp := newFakeParent(t)
	c := newClient(t, fp, "agt_a1b2", "")
	c.SetToken("preminted-token")
	if !c.HasToken() {
		t.Errorf("expected token in memory")
	}
	if atomic.LoadInt32(&fp.registers) != 0 {
		t.Errorf("SetToken should not register")
	}
}

func TestPush_HappyPath(t *testing.T) {
	fp := newFakeParent(t)
	c := newClient(t, fp, "ollama", filepath.Join(t.TempDir(), "p.tok"))
	if err := c.Register(context.Background(), "0.1", nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Push(context.Background(), &observer.StatsResponse{V: 2}, "0.1", nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if atomic.LoadInt32(&fp.pushes) != 1 {
		t.Errorf("push count = %d want 1", atomic.LoadInt32(&fp.pushes))
	}
	if got, _ := fp.lastBody.Load().(string); !strings.Contains(got, `"shape":"B"`) {
		t.Errorf("push body shape = ?? body=%s", got)
	}
}

func TestPush_ShapeAFromAgentClient(t *testing.T) {
	// S13 — agent peers push with shape "A"; verify the client's
	// configured shape lands in the wire body.
	fp := newFakeParent(t)
	c, _ := New(Config{ParentURL: fp.srv.URL, Name: "agt_a1", Shape: "A"})
	c.SetToken("agent-token")
	// Pre-mint matching token in fake parent so the push isn't 401.
	fp.mintedTokens["agt_a1"] = "agent-token"

	if err := c.Push(context.Background(), &observer.StatsResponse{V: 2}, "0.1", nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	body, _ := fp.lastBody.Load().(string)
	if !strings.Contains(body, `"shape":"A"`) {
		t.Errorf("expected shape:A in push body, got %s", body)
	}
}

func TestPush_AutoReregistersOn401(t *testing.T) {
	fp := newFakeParent(t)
	c := newClient(t, fp, "ollama", filepath.Join(t.TempDir(), "p.tok"))
	if err := c.Register(context.Background(), "0.1", nil); err != nil {
		t.Fatal(err)
	}
	registersBefore := atomic.LoadInt32(&fp.registers)
	fp.rejectNext.Store(true)

	if err := c.Push(context.Background(), &observer.StatsResponse{V: 2}, "0.1", nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if atomic.LoadInt32(&fp.registers) <= registersBefore {
		t.Errorf("expected re-register after 401")
	}
	if atomic.LoadInt32(&fp.pushes) != 1 {
		t.Errorf("push should have succeeded on retry")
	}
}

func TestPush_SurfacesNon401Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/observer/peers" {
			_, _ = w.Write([]byte(`{"token":"t"}`))
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c, _ := New(Config{ParentURL: srv.URL, Name: "x"})
	if err := c.Register(context.Background(), "0", nil); err != nil {
		t.Fatal(err)
	}
	err := c.Push(context.Background(), &observer.StatsResponse{V: 2}, "0", nil)
	if err == nil {
		t.Fatal("expected error from 500")
	}
	if errors.Is(err, ErrUnauthorized) {
		t.Errorf("500 should not be reported as ErrUnauthorized: %v", err)
	}
}

func TestHostInfo_PopulatesBasicFields(t *testing.T) {
	// Stub the GPU probe so the test neither shells out to a real
	// nvidia-smi nor reads a real instance id on the dev box.
	probe := gpuUUIDProbe
	gpuUUIDProbe = func() []string { return nil }
	defer func() { gpuUUIDProbe = probe }()

	path := nodeInstanceIDPath
	nodeInstanceIDPath = func() string { return filepath.Join(t.TempDir(), "state", "node_instance_id") }
	defer func() { nodeInstanceIDPath = path }()

	hi := HostInfo()
	for _, key := range []string{"hostname", "os", "arch"} {
		if hi[key] == nil || hi[key] == "" {
			t.Errorf("HostInfo missing %s: %+v", key, hi)
		}
	}
}

// The node-instance id must persist across HostInfo calls — first
// call generates, later calls read the same value back — and live in
// a 0600 file next to the stats agent's state.
func TestHostInfoNodeInstanceIDPersisted(t *testing.T) {
	probe := gpuUUIDProbe
	gpuUUIDProbe = func() []string { return nil }
	defer func() { gpuUUIDProbe = probe }()

	same := t.TempDir()
	path := nodeInstanceIDPath
	nodeInstanceIDPath = func() string { return filepath.Join(same, "nested", "node_instance_id") }
	defer func() { nodeInstanceIDPath = path }()

	first := HostInfo()["node_instance_id"].(string)
	if len(first) != 32 {
		t.Fatalf("node_instance_id = %q want 32 hex chars (128-bit)", first)
	}
	second := HostInfo()["node_instance_id"].(string)
	if second != first {
		t.Fatalf("later HostInfo call read %q, want the same %q", second, first)
	}
	onDisk, err := os.ReadFile(filepath.Join(same, "nested", "node_instance_id"))
	if err != nil {
		t.Fatalf("read persisted id: %v", err)
	}
	if strings.TrimSpace(string(onDisk)) != first {
		t.Fatalf("on-disk id = %q want %q", strings.TrimSpace(string(onDisk)), first)
	}
	info, err := os.Stat(filepath.Join(same, "nested", "node_instance_id"))
	if err != nil {
		t.Fatalf("stat id file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("id file perm = %v want 0600", info.Mode().Perm())
	}
}

// GPU UUIDs flow from the injected probe straight into the host
// envelope.
func TestHostInfoGPUUIDsFromTool(t *testing.T) {
	probe := gpuUUIDProbe
	gpuUUIDProbe = func() []string {
		return []string{"GPU-0f88de7f-9c8b-0d4a-c6e0-ba9e5c1d0a2f", "GPU-1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"}
	}
	defer func() { gpuUUIDProbe = probe }()

	path := nodeInstanceIDPath
	nodeInstanceIDPath = func() string { return filepath.Join(t.TempDir(), "node_instance_id") }
	defer func() { nodeInstanceIDPath = path }()

	uuids, ok := HostInfo()["gpu_uuids"].([]string)
	if !ok || len(uuids) != 2 {
		t.Fatalf("gpu_uuids = %#v want the tool's two UUIDs", HostInfo()["gpu_uuids"])
	}
	if uuids[0] != "GPU-0f88de7f-9c8b-0d4a-c6e0-ba9e5c1d0a2f" || uuids[1] != "GPU-1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d" {
		t.Fatalf("gpu_uuids = %v want the injected tool output in order", uuids)
	}
}

// S14a (v4.8.0) — PushWithChain serialises the chain field into the
// request body so the receiving primary can enforce loop prevention.
func TestPushWithChain_SerializesChainField(t *testing.T) {
	fp := newFakeParent(t)
	c := newClient(t, fp, "primary-east", filepath.Join(t.TempDir(), "p.token"))
	if err := c.Register(context.Background(), "0", nil); err != nil {
		t.Fatal(err)
	}
	chain := []string{"primary-east", "primary-root"}
	if err := c.PushWithChain(context.Background(), &observer.StatsResponse{V: 2}, chain, "0", nil); err != nil {
		t.Fatalf("PushWithChain: %v", err)
	}
	body, _ := fp.lastBody.Load().(string)
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body unmarshal: %v (raw=%s)", err, body)
	}
	gotChain, ok := got["chain"].([]any)
	if !ok {
		t.Fatalf("expected chain field in push body, got: %v", got)
	}
	if len(gotChain) != 2 || gotChain[0] != "primary-east" || gotChain[1] != "primary-root" {
		t.Errorf("chain = %v want [primary-east primary-root]", gotChain)
	}
}

// PushWithChain with an empty chain omits the field entirely so
// single-hop Shape A/B/C peers stay wire-identical to v4.7.x.
func TestPushWithChain_EmptyChainOmitsField(t *testing.T) {
	fp := newFakeParent(t)
	c := newClient(t, fp, "agt", filepath.Join(t.TempDir(), "p.token"))
	if err := c.Register(context.Background(), "0", nil); err != nil {
		t.Fatal(err)
	}
	if err := c.PushWithChain(context.Background(), &observer.StatsResponse{V: 2}, nil, "0", nil); err != nil {
		t.Fatalf("PushWithChain: %v", err)
	}
	body, _ := fp.lastBody.Load().(string)
	var got map[string]any
	_ = json.Unmarshal([]byte(body), &got)
	if _, present := got["chain"]; present {
		t.Errorf("nil chain should be omitted from body, got: %v", got)
	}
}

// --- TLS: pinning + verify-by-default (self-signed fake parent) ---

func newFakeParentTLS(t *testing.T) (*fakeParent, string) {
	t.Helper()
	fp := &fakeParent{mintedTokens: map[string]string{}}
	fp.srv = httptest.NewTLSServer(http.HandlerFunc(fp.handle)) // self-signed cert
	t.Cleanup(fp.srv.Close)
	return fp, leafSHA256(t, fp.srv)
}

// leafSHA256 dials the TLS server (verification off, test-only) and
// returns the SHA-256 hex fingerprint of the leaf certificate it
// presents — the value a pin must equal.
func leafSHA256(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	conn, err := tls.Dial("tcp", u.Host, &tls.Config{InsecureSkipVerify: true}) // #nosec G402 -- test-only dial to read the cert
	if err != nil {
		t.Fatalf("tls dial: %v", err)
	}
	defer conn.Close() //nolint:errcheck
	pc := conn.ConnectionState().PeerCertificates
	if len(pc) == 0 {
		t.Fatal("no peer certificates from test TLS server")
	}
	sum := sha256.Sum256(pc[0].Raw)
	return hex.EncodeToString(sum[:])
}

func newTLSCertClient(t *testing.T, srv *httptest.Server, cfg Config) *Client {
	t.Helper()
	cfg.ParentURL = srv.URL
	if cfg.Name == "" {
		cfg.Name = "ollama"
	}
	if cfg.Shape == "" {
		cfg.Shape = "B"
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewTLS_DefaultRejectsSelfSigned(t *testing.T) {
	fp, _ := newFakeParentTLS(t)
	c := newTLSCertClient(t, fp.srv, Config{})
	err := c.Register(context.Background(), "0", nil)
	if err == nil {
		t.Fatal("expected default config (system trust store) to reject a self-signed parent")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "certificate") {
		t.Errorf("error should mention the certificate problem, got: %v", err)
	}
}

func TestNewTLS_PinnedAcceptsSelfSigned(t *testing.T) {
	fp, fpHex := newFakeParentTLS(t)
	c := newTLSCertClient(t, fp.srv, Config{PinnedSHA256: fpHex})
	if err := c.Register(context.Background(), "0", nil); err != nil {
		t.Fatalf("Register with correct pin: %v", err)
	}
	if err := c.Push(context.Background(), &observer.StatsResponse{V: 2}, "0", nil); err != nil {
		t.Fatalf("Push with correct pin: %v", err)
	}
}

func TestNewTLS_PinIgnoresCaseAndSeparators(t *testing.T) {
	fp, fpHex := newFakeParentTLS(t)
	// Uppercase colon-grouped variant — must normalise to the same pin.
	sep := make([]string, 0, 16)
	for i := 0; i < len(fpHex); i += 4 {
		sep = append(sep, strings.ToUpper(fpHex[i:i+4]))
	}
	c := newTLSCertClient(t, fp.srv, Config{PinnedSHA256: strings.Join(sep, ":")})
	if err := c.Register(context.Background(), "0", nil); err != nil {
		t.Fatalf("Register with colon+case-variant pin: %v", err)
	}
}

func TestNewTLS_WrongPinRejects(t *testing.T) {
	fp, _ := newFakeParentTLS(t)
	c := newTLSCertClient(t, fp.srv, Config{PinnedSHA256: strings.Repeat("f", 64)})
	err := c.Register(context.Background(), "0", nil)
	if err == nil {
		t.Fatal("expected wrong pin to be rejected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "fingerprint mismatch") {
		t.Errorf("error should name the fingerprint mismatch, got: %v", err)
	}
}

func TestNewTLS_InsecureAcceptsAndWarnsOnce(t *testing.T) {
	fp, _ := newFakeParentTLS(t)
	var sinkMu sync.Mutex
	var warns []string
	warn := func(s string) { sinkMu.Lock(); warns = append(warns, s); sinkMu.Unlock() }
	c := newTLSCertClient(t, fp.srv, Config{Insecure: true, Warn: warn})
	if err := c.Register(context.Background(), "0", nil); err != nil {
		t.Fatalf("Register insecure: %v", err)
	}
	if err := c.Push(context.Background(), &observer.StatsResponse{V: 2}, "0", nil); err != nil {
		t.Fatalf("Push insecure: %v", err)
	}
	if len(warns) != 1 {
		t.Fatalf("expected exactly one insecure warning, got %d: %q", len(warns), warns)
	}
	w := warns[0]
	u, _ := url.Parse(fp.srv.URL)
	if !strings.Contains(w, u.Hostname()) {
		t.Errorf("warning should name the parent host %q, got: %q", u.Hostname(), w)
	}
	if !strings.Contains(w, "SHA-256") {
		t.Errorf("warning should say how to pin instead, got: %q", w)
	}
	if strings.Contains(w, fp.mintedTokens["ollama"]) {
		t.Errorf("warning must not contain the token, got: %q", w)
	}
}

func TestNewTLS_PinWinsOverInsecure(t *testing.T) {
	fp, fpHex := newFakeParentTLS(t)
	var warns []string
	// Correct pin + Insecure: must be accepted via the pin path.
	c := newTLSCertClient(t, fp.srv, Config{Insecure: true, PinnedSHA256: fpHex, Warn: func(s string) { warns = append(warns, s) }})
	if err := c.Register(context.Background(), "0", nil); err != nil {
		t.Fatalf("Register pin+insecure: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("a pin suppresses the insecure warning, got: %q", warns)
	}
	// Wrong pin + Insecure: must still be rejected — proving the pin
	// path (not skip-verify) is the one that's active.
	c2 := newTLSCertClient(t, fp.srv, Config{Insecure: true, PinnedSHA256: strings.Repeat("0", 64)})
	if err := c2.Register(context.Background(), "0", nil); err == nil {
		t.Fatal("wrong pin must reject even with Insecure set (pin wins)")
	}
}

func TestNew_BadPinFailsClosed(t *testing.T) {
	for _, bad := range []string{"zz", "abc123", strings.Repeat("g", 64)} {
		if _, err := New(Config{ParentURL: "https://x", Name: "n", PinnedSHA256: bad}); err == nil {
			t.Errorf("New should fail closed on malformed pin %q", bad)
		}
	}
	// A well-formed pin must still construct (verified live above).
	if _, err := New(Config{ParentURL: "https://x", Name: "n", PinnedSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Errorf("well-formed pin should not fail New: %v", err)
	}
}

// parentHost feeds the warning texts and must never leak userinfo
// (credentials) from the URL.
func TestParentHost_NeverLeaksUserinfo(t *testing.T) {
	host := parentHost("https://user:super-secret-token@primary:8443")
	if host != "primary" {
		t.Errorf("parentHost = %q want primary", host)
	}
	for _, w := range []string{insecurePushWarning(host), systemStoreNotice(host)} {
		if strings.Contains(w, "super-secret-token") {
			t.Errorf("warning must not contain credentials, got: %q", w)
		}
	}
}
