// CLI surfaces for --tls-cert-fingerprint: the flag lands in the JSON
// body on server/peer add+update exactly when set, mutual exclusion with
// --tls-skip-verify fails before any HTTP call, and an update that
// doesn't pass the flag doesn't send it (no accidental reset).

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const pinCLITestHex = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// pinCLIDaemon records every request body it receives and answers 200.
func pinCLIDaemon(t *testing.T, bodies *[]map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		*bodies = append(*bodies, body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"name":"p"}`))
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	return srv
}

func TestCLIServerAdd_TLSCertFingerprintInBody(t *testing.T) {
	bodies := []map[string]any{}
	pinCLIDaemon(t, &bodies)

	// With the flag: present in the body.
	c := newServerCmd()
	c.SetArgs([]string{"add", "--name", "p", "--url", "http://x", "--tls-cert-fingerprint", pinCLITestHex})
	if err := runRunE(t, c); err != nil {
		t.Fatalf("add with pin: %v", err)
	}
	if got := bodies[0]["tls_cert_fingerprint"]; got != pinCLITestHex {
		t.Errorf("body tls_cert_fingerprint = %v, want %s", got, pinCLITestHex)
	}

	// Without the flag: key must be absent (not an empty string).
	c2 := newServerCmd()
	c2.SetArgs([]string{"add", "--name", "p2", "--url", "http://x"})
	if err := runRunE(t, c2); err != nil {
		t.Fatalf("add without pin: %v", err)
	}
	if _, present := bodies[1]["tls_cert_fingerprint"]; present {
		t.Errorf("tls_cert_fingerprint must be absent without --tls-cert-fingerprint: %v", bodies[1])
	}
}

func TestCLIServerAdd_PinAndSkipVerifyRejected(t *testing.T) {
	bodies := []map[string]any{}
	pinCLIDaemon(t, &bodies)

	c := newServerCmd()
	c.SetArgs([]string{"add", "--name", "p", "--url", "http://x", "--tls-cert-fingerprint", pinCLITestHex, "--tls-skip-verify"})
	err := runRunE(t, c)
	if err == nil {
		t.Fatalf("want error for pin + --tls-skip-verify")
	}
	for _, want := range []string{"--tls-cert-fingerprint", "--tls-skip-verify"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	if len(bodies) != 0 {
		t.Errorf("mutual exclusion must fail before any HTTP call; sent %d", len(bodies))
	}
}

func TestCLIServerUpdate_TLSFingerprintOnlyWhenSet(t *testing.T) {
	bodies := []map[string]any{}
	pinCLIDaemon(t, &bodies)

	// Flag set: body carries it (and is PUT to /api/servers/<name>).
	c := newServerCmd()
	c.SetArgs([]string{"update", "p", "--tls-cert-fingerprint", pinCLITestHex})
	if err := runRunE(t, c); err != nil {
		t.Fatalf("update with pin: %v", err)
	}
	if got := bodies[0]["tls_cert_fingerprint"]; got != pinCLITestHex {
		t.Errorf("body tls_cert_fingerprint = %v, want %s", got, pinCLITestHex)
	}

	// Flag unset: key must be absent so the server doesn't reset it.
	c2 := newServerCmd()
	c2.SetArgs([]string{"update", "p", "--label", "ops"})
	if err := runRunE(t, c2); err != nil {
		t.Fatalf("update without pin: %v", err)
	}
	if _, present := bodies[1]["tls_cert_fingerprint"]; present {
		t.Errorf("tls_cert_fingerprint must be absent when the flag is unset: %v", bodies[1])
	}
}

func TestCLIFedPeerAdd_TLSCertFingerprintInBody(t *testing.T) {
	bodies := []map[string]any{}
	pinCLIDaemon(t, &bodies)

	c := newFederationCmd()
	c.SetArgs([]string{"peer", "add", "--name", "f", "--url", "http://x", "--tls-cert-fingerprint", pinCLITestHex})
	if err := runRunE(t, c); err != nil {
		t.Fatalf("fed add with pin: %v", err)
	}
	if got := bodies[0]["tls_cert_fingerprint"]; got != pinCLITestHex {
		t.Errorf("body tls_cert_fingerprint = %v, want %s", got, pinCLITestHex)
	}

	// Without the flag: absent.
	c2 := newFederationCmd()
	c2.SetArgs([]string{"peer", "add", "--name", "f2", "--url", "http://x"})
	if err := runRunE(t, c2); err != nil {
		t.Fatalf("fed add without pin: %v", err)
	}
	if _, present := bodies[1]["tls_cert_fingerprint"]; present {
		t.Errorf("must be absent without the flag: %v", bodies[1])
	}
}

func TestCLIFedPeerAdd_PinAndSkipVerifyRejected(t *testing.T) {
	bodies := []map[string]any{}
	pinCLIDaemon(t, &bodies)

	c := newFederationCmd()
	c.SetArgs([]string{"peer", "add", "--name", "f", "--url", "http://x", "--tls-cert-fingerprint", pinCLITestHex, "--tls-skip-verify"})
	err := runRunE(t, c)
	if err == nil {
		t.Fatalf("want error for pin + --tls-skip-verify")
	}
	for _, want := range []string{"--tls-cert-fingerprint", "--tls-skip-verify"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	if len(bodies) != 0 {
		t.Errorf("mutual exclusion must fail before any HTTP call; sent %d", len(bodies))
	}
}

func TestCLIFedPeerUpdate_TLSFingerprintOnlyWhenSet(t *testing.T) {
	bodies := []map[string]any{}
	pinCLIDaemon(t, &bodies)

	c := newFederationCmd()
	c.SetArgs([]string{"peer", "update", "f", "--tls-cert-fingerprint", pinCLITestHex})
	if err := runRunE(t, c); err != nil {
		t.Fatalf("fed update with pin: %v", err)
	}
	if got := bodies[0]["tls_cert_fingerprint"]; got != pinCLITestHex {
		t.Errorf("body tls_cert_fingerprint = %v, want %s", got, pinCLITestHex)
	}

	c2 := newFederationCmd()
	c2.SetArgs([]string{"peer", "update", "f", "--url", "http://y"})
	if err := runRunE(t, c2); err != nil {
		t.Fatalf("fed update without pin: %v", err)
	}
	if _, present := bodies[1]["tls_cert_fingerprint"]; present {
		t.Errorf("must be absent when the flag is unset: %v", bodies[1])
	}
}
