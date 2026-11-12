// Model-pull certificate policy — compute helper tests. The Ollama node is a
// local httptest server (self-signed TLS or plain http); nothing leaves the
// machine and no system trust store is touched.

package compute_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

// newFakeOllamaPullServer fakes the Ollama pull and tags endpoints. tls
// selects a self-signed HTTPS server or a plain http one.
func newFakeOllamaPullServer(useTLS bool) *httptest.Server {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/pull":
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = fmt.Fprint(w, `{"status":"pulling manifest"}
{"status":"downloading","total":100,"completed":50}
{"status":"success"}
`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/tags":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"models":[{"name":"tiny:latest"}]}`)
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	})
	if useTLS {
		return httptest.NewTLSServer(h)
	}
	return httptest.NewServer(h)
}

func pullOnce(ctx context.Context, client *http.Client, addr string) (bool, error) {
	sawSuccess := false
	err := inference.PullModel(ctx, client, addr, "tiny:latest", func(p inference.PullProgress) {
		if p.Success {
			sawSuccess = true
		}
	})
	return sawSuccess, err
}

func TestVerifyTLSPullTable(t *testing.T) {
	tlsSrv := newFakeOllamaPullServer(true)
	defer tlsSrv.Close()
	plainSrv := newFakeOllamaPullServer(false)
	defer plainSrv.Close()

	cases := []struct {
		name      string
		addr      string
		verifyTLS bool
		wantErr   bool
	}{
		{"self-signed https, verify_tls off pulls", tlsSrv.URL, false, false},
		{"self-signed https, verify_tls on fails", tlsSrv.URL, true, true},
		{"plain http, verify_tls off pulls", plainSrv.URL, false, false},
		{"plain http, verify_tls on pulls", plainSrv.URL, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := &compute.Node{Name: "gpu-tls", VerifyTLS: tc.verifyTLS}
			client := compute.PullHTTPClient(node)
			defer client.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			ok, err := pullOnce(ctx, client, tc.addr)
			err = compute.WrapPullError(node, err)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("pull succeeded, want a certificate error")
				}
				msg := err.Error()
				if !strings.Contains(msg, "gpu-tls") || !strings.Contains(msg, "certificate verification") {
					t.Fatalf("error must name the node and say certificate verification: %q", msg)
				}
				return
			}
			if err != nil {
				t.Fatalf("pull failed: %v", err)
			}
			if !ok {
				t.Fatalf("no success progress report seen")
			}
		})
	}
}

func TestVerifyTLSTagsEndpointFollowsPolicy(t *testing.T) {
	srv := newFakeOllamaPullServer(true)
	defer srv.Close()

	get := func(verify bool) error {
		client := compute.PullHTTPClient(&compute.Node{Name: "gpu-tls", VerifyTLS: verify})
		defer client.CloseIdleConnections()
		resp, err := client.Get(srv.URL + "/api/tags")
		if err != nil {
			return err
		}
		return resp.Body.Close()
	}
	if err := get(false); err != nil {
		t.Fatalf("tags with verify_tls off: %v", err)
	}
	if err := get(true); err == nil {
		t.Fatalf("tags with verify_tls on against a self-signed server succeeded")
	}
}

// A certificate the caller trusts must pass with verification on. The test
// server's own certificate stands in for one the system trusts; the client is
// built from a pool holding only that certificate, so system roots are never
// consulted.
func TestVerifyTLSPullSucceedsWhenCertificateTrusted(t *testing.T) {
	srv := newFakeOllamaPullServer(true)
	defer srv.Close()

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	client := srv.Client() // already trusts only the server certificate
	tr := client.Transport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
	client = &http.Client{Transport: tr}
	defer client.CloseIdleConnections()

	node := &compute.Node{Name: "gpu-tls", VerifyTLS: true}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ok, err := pullOnce(ctx, client, srv.URL)
	if err = compute.WrapPullError(node, err); err != nil {
		t.Fatalf("trusted certificate with verification on failed: %v", err)
	}
	if !ok {
		t.Fatalf("no success progress report seen")
	}
}

func TestVerifyTLSWrapPullErrorPassThrough(t *testing.T) {
	on := &compute.Node{Name: "gpu-tls", VerifyTLS: true}
	off := &compute.Node{Name: "gpu-tls", VerifyTLS: false}

	plain := errors.New("connection refused")
	if got := compute.WrapPullError(on, plain); got != plain {
		t.Fatalf("non-certificate error changed: %v", got)
	}
	if got := compute.WrapPullError(on, nil); got != nil {
		t.Fatalf("nil error became %v", got)
	}
	if got := compute.WrapPullError(nil, plain); got != plain {
		t.Fatalf("nil node changed error: %v", got)
	}
	certErr := &url.Error{Op: "Post", URL: "https://x", Err: x509.UnknownAuthorityError{}}
	if got := compute.WrapPullError(off, certErr); got != error(certErr) {
		t.Fatalf("verify_tls off must pass a certificate error through unchanged: %v", got)
	}
	wrapped := compute.WrapPullError(on, certErr)
	if wrapped == nil || !strings.Contains(wrapped.Error(), "gpu-tls") || !errors.Is(wrapped, certErr) {
		t.Fatalf("certificate error not wrapped with the node name and cause: %v", wrapped)
	}
}
