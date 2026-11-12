package autonomous

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

// tlsPSServer starts an https server with a self-signed certificate that
// answers /api/ps with one resident model, and returns it with the SHA-256
// fingerprint of its certificate and a PEM file holding that certificate.
func tlsPSServer(t *testing.T) (srv *httptest.Server, fingerprint, caFile string) {
	t.Helper()
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ps" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3.8:27b"}]}`))
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(srv.Certificate().Raw)
	fingerprint = hex.EncodeToString(sum[:])
	caFile = filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caFile, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return srv, fingerprint, caFile
}

// editableNodes is a node lookup whose record a test can change between reads.
type editableNodes struct {
	mu    sync.Mutex
	nodes map[string]compute.Node
}

func newEditableNodes(n compute.Node) *editableNodes {
	return &editableNodes{nodes: map[string]compute.Node{n.Name: n}}
}

func (e *editableNodes) set(n compute.Node) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nodes[n.Name] = n
}

func (e *editableNodes) lookup(name string) (*compute.Node, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	n, ok := e.nodes[name]
	if !ok {
		return nil, false
	}
	cp := n
	return &cp, true
}

func tlsHost(srv *httptest.Server) PoolHost {
	return PoolHost{Name: "gpu-box", Node: "gpu-box", Endpoint: srv.URL}
}

func TestModelFetcherTLSUntrustedCertificateNamesNodeAndFix(t *testing.T) {
	srv, _, _ := tlsPSServer(t)
	nodes := newEditableNodes(compute.Node{Name: "gpu-box", Kind: compute.KindOllama, Address: srv.URL})
	_, err := NewHTTPModelFetcherForNodes(nil, nodes.lookup).LoadedModels(context.Background(), tlsHost(srv))
	if err == nil {
		t.Fatal("an untrusted certificate was accepted")
	}
	msg := err.Error()
	for _, want := range []string{"gpu-box", "tls_ca_file", "tls_cert_fingerprint", "tls_insecure_skip_verify"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %q", msg, want)
		}
	}
	if strings.Contains(msg, srv.URL) || strings.Contains(msg, "https://") || strings.Contains(msg, "127.0.0.1:") {
		t.Errorf("message leaks the endpoint: %q", msg)
	}
}

func TestModelFetcherTLSFingerprintCAFileAndSkipVerifyRead(t *testing.T) {
	srv, fp, caFile := tlsPSServer(t)
	cases := map[string]compute.Node{
		"fingerprint": {TLSCertFingerprint: fp},
		"ca file":     {TLSCAFile: caFile},
		"skip verify": {TLSInsecureSkipVerify: true},
	}
	for name, setting := range cases {
		t.Run(name, func(t *testing.T) {
			setting.Name, setting.Kind, setting.Address = "gpu-box", compute.KindOllama, srv.URL
			nodes := newEditableNodes(setting)
			got, err := NewHTTPModelFetcherForNodes(nil, nodes.lookup).LoadedModels(context.Background(), tlsHost(srv))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0] != "qwen3.8:27b" {
				t.Fatalf("models = %v", got)
			}
		})
	}
}

func TestModelFetcherTLSWrongFingerprintFails(t *testing.T) {
	srv, _, _ := tlsPSServer(t)
	nodes := newEditableNodes(compute.Node{
		Name: "gpu-box", Kind: compute.KindOllama, Address: srv.URL,
		TLSCertFingerprint: strings.Repeat("ab", 32),
	})
	_, err := NewHTTPModelFetcherForNodes(nil, nodes.lookup).LoadedModels(context.Background(), tlsHost(srv))
	if err == nil {
		t.Fatal("a wrong fingerprint was accepted")
	}
	if strings.Contains(err.Error(), srv.URL) {
		t.Errorf("message leaks the endpoint: %v", err)
	}
}

func TestModelFetcherTLSEditingSettingsRebuildsClient(t *testing.T) {
	srv, fp, _ := tlsPSServer(t)
	nodes := newEditableNodes(compute.Node{Name: "gpu-box", Kind: compute.KindOllama, Address: srv.URL})
	f := NewHTTPModelFetcherForNodes(nil, nodes.lookup)
	host := tlsHost(srv)

	if _, err := f.LoadedModels(context.Background(), host); err == nil {
		t.Fatal("untrusted certificate accepted before the edit")
	}
	nodes.set(compute.Node{Name: "gpu-box", Kind: compute.KindOllama, Address: srv.URL, TLSCertFingerprint: fp})
	if _, err := f.LoadedModels(context.Background(), host); err != nil {
		t.Fatalf("read after adding the fingerprint: %v", err)
	}
	hf := f.(*httpModelFetcher)
	first := hf.nodeClient["gpu-box"].client
	if _, err := f.LoadedModels(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	if hf.nodeClient["gpu-box"].client != first {
		t.Error("the client was rebuilt although the settings did not change")
	}
	nodes.set(compute.Node{Name: "gpu-box", Kind: compute.KindOllama, Address: srv.URL, TLSCertFingerprint: strings.Repeat("cd", 32)})
	if _, err := f.LoadedModels(context.Background(), host); err == nil {
		t.Fatal("the old client was reused after the fingerprint changed")
	}
	if hf.nodeClient["gpu-box"].client == first {
		t.Error("the client was not rebuilt after the settings changed")
	}
}

func TestModelFetcherTLSPlainHTTPNodeStillWorks(t *testing.T) {
	srv := psServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"models":[{"name":"m1"}]}`)) })
	nodes := newEditableNodes(compute.Node{Name: "gpu-box", Kind: compute.KindOllama, Address: srv.URL, TLSInsecureSkipVerify: true})
	f := NewHTTPModelFetcherForNodes(nil, nodes.lookup)
	got, err := f.LoadedModels(context.Background(), PoolHost{Name: "gpu-box", Node: "gpu-box", Endpoint: srv.URL})
	if err != nil || len(got) != 1 {
		t.Fatalf("models = %v, err = %v", got, err)
	}
	if n := len(f.(*httpModelFetcher).nodeClient); n != 0 {
		t.Errorf("a plain http node got a cached TLS client (%d)", n)
	}
}

func TestModelFetcherTLSLocalHostKeepsDefaultClient(t *testing.T) {
	srv, _, _ := tlsPSServer(t)
	nodes := newEditableNodes(compute.Node{Name: "local", Kind: compute.KindOllama, Address: srv.URL, TLSInsecureSkipVerify: true})
	// A host with no node name is the local Ollama: the node settings of a
	// node that happens to share its name do not apply.
	_, err := NewHTTPModelFetcherForNodes(nil, nodes.lookup).LoadedModels(context.Background(), PoolHost{Name: "local", Endpoint: srv.URL})
	if err == nil {
		t.Fatal("a host with no node used a node's TLS settings")
	}
}

func TestModelFetcherTLSUnusableSettingsAreReported(t *testing.T) {
	srv, _, _ := tlsPSServer(t)
	nodes := newEditableNodes(compute.Node{Name: "gpu-box", Kind: compute.KindOllama, Address: srv.URL, TLSCAFile: filepath.Join(t.TempDir(), "missing.pem")})
	_, err := NewHTTPModelFetcherForNodes(nil, nodes.lookup).LoadedModels(context.Background(), tlsHost(srv))
	if err == nil || !strings.Contains(err.Error(), "gpu-box") || !strings.Contains(err.Error(), "tls_ca_file") {
		t.Fatalf("err = %v, want it to name the node and tls_ca_file", err)
	}
}

func TestPoolEvidenceTLSUntrustedHostIsUnavailableWithFixableMessage(t *testing.T) {
	srv, fp, _ := tlsPSServer(t)
	nodes := newEditableNodes(compute.Node{Name: "gpu-box", Kind: compute.KindOllama, Address: srv.URL})
	now := newClock()
	src := NewPoolEvidenceSource(&fakeSessionLister{}, &fakeHostLister{hosts: []PoolHost{tlsHost(srv)}},
		NewHTTPModelFetcherForNodes(nil, nodes.lookup), PoolEvidenceClock(now.get))

	_, err := src.Check(context.Background())
	if err == nil {
		t.Fatal("an unreadable pool host did not make the evidence unavailable")
	}
	msg := err.Error()
	if !strings.Contains(msg, "pool host gpu-box unreadable") || !strings.Contains(msg, "tls_cert_fingerprint") {
		t.Errorf("message = %q, want the node and the fix", msg)
	}
	if strings.Contains(msg, srv.URL) {
		t.Errorf("message leaks the endpoint: %q", msg)
	}

	// The fault is bounded by the 5 second cache, not kept for good.
	nodes.set(compute.Node{Name: "gpu-box", Kind: compute.KindOllama, Address: srv.URL, TLSCertFingerprint: fp})
	if _, err := src.Check(context.Background()); err == nil {
		t.Fatal("the cached reading was dropped inside the cache window")
	}
	now.advance(poolEvidenceCacheTTL)
	ev, err := src.Check(context.Background())
	if err != nil {
		t.Fatalf("still unavailable after the cache window and the fix: %v", err)
	}
	if len(ev.LoadedModels) != 1 || ev.LoadedModels[0].Host != "gpu-box" {
		t.Errorf("evidence = %+v", ev)
	}
}

type poolTLSClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *poolTLSClock { return &poolTLSClock{t: time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)} }

func (c *poolTLSClock) get() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *poolTLSClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
