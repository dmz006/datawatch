package apns

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// testAPNsKeyPEM generates a fresh ECDSA P-256 key, PKCS#8-encoded —
// the same format Apple issues .p8 Auth Keys in.
func testAPNsKeyPEM(t *testing.T) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal PKCS8: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), key
}

func TestParseAPNsKey_PKCS8(t *testing.T) {
	keyPEM, _ := testAPNsKeyPEM(t)
	key, err := parseAPNsKey(keyPEM)
	if err != nil {
		t.Fatalf("parseAPNsKey: %v", err)
	}
	if key == nil {
		t.Fatal("expected a non-nil key")
	}
}

func TestParseAPNsKey_InvalidPEM(t *testing.T) {
	if _, err := parseAPNsKey([]byte("not a pem file")); err == nil {
		t.Fatal("expected an error for invalid PEM")
	}
}

// TestProviderToken_WellFormedAndVerifiable is the core correctness test:
// the signed JWT must actually verify against the key's own public half,
// using the raw (r||s) signature format JWS requires — not just be
// well-formed JSON with SOME bytes attached.
func TestProviderToken_WellFormedAndVerifiable(t *testing.T) {
	keyPEM, originalKey := testAPNsKeyPEM(t)
	parsed, err := parseAPNsKey(keyPEM)
	if err != nil {
		t.Fatalf("parseAPNsKey: %v", err)
	}
	d := &Dispatcher{cfg: config.APNsConfig{KeyID: "ABC1234567", TeamID: "TEAM123456"}, key: parsed}

	token, err := d.providerToken()
	if err != nil {
		t.Fatalf("providerToken: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected a 3-part JWT, got %d parts", len(parts))
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if header.Alg != "ES256" {
		t.Errorf("alg = %q, want ES256", header.Alg)
	}
	if header.Kid != "ABC1234567" {
		t.Errorf("kid = %q, want ABC1234567", header.Kid)
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.Iss != "TEAM123456" {
		t.Errorf("iss = %q, want TEAM123456", claims.Iss)
	}
	if claims.Iat == 0 {
		t.Error("expected a non-zero iat")
	}

	// Verify the signature against the key's own public half — confirms
	// this is a real, independently-verifiable ES256 JWS (raw r||s, not
	// ASN.1 DER), not just well-formed JSON with arbitrary bytes bolted on.
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if len(sigBytes) != 64 {
		t.Fatalf("expected a 64-byte raw r||s signature (P-256), got %d bytes", len(sigBytes))
	}
	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:])

	signingInput := parts[0] + "." + parts[1]
	hash := sha256.Sum256([]byte(signingInput))
	if !ecdsa.Verify(&originalKey.PublicKey, hash[:], r, s) {
		t.Fatal("signature does not verify against the key's own public half")
	}
}

func TestProviderToken_Cached(t *testing.T) {
	keyPEM, _ := testAPNsKeyPEM(t)
	parsed, err := parseAPNsKey(keyPEM)
	if err != nil {
		t.Fatalf("parseAPNsKey: %v", err)
	}
	d := &Dispatcher{cfg: config.APNsConfig{KeyID: "ABC1234567", TeamID: "TEAM123456"}, key: parsed}

	t1, err := d.providerToken()
	if err != nil {
		t.Fatalf("providerToken: %v", err)
	}
	t2, err := d.providerToken()
	if err != nil {
		t.Fatalf("providerToken (second call): %v", err)
	}
	if t1 != t2 {
		t.Error("expected the cached token to be reused within tokenTTL, got two different tokens")
	}
}

func TestErrAPNs_Unregistered(t *testing.T) {
	e := &ErrAPNs{StatusCode: http.StatusGone, Reason: "Unregistered"}
	if !e.Unregistered() {
		t.Error("expected Unregistered() true for 410/Unregistered")
	}

	e2 := &ErrAPNs{StatusCode: http.StatusTooManyRequests, Reason: "TooManyRequests"}
	if e2.Unregistered() {
		t.Error("expected Unregistered() false for 429/TooManyRequests")
	}
}

func TestNewDispatcher_RequiresCoreFields(t *testing.T) {
	if _, err := NewDispatcher(config.APNsConfig{}, nil); err == nil {
		t.Fatal("expected an error when key_id/team_id/bundle_id are all empty")
	}
}

func TestNewDispatcher_RequiresAKeySource(t *testing.T) {
	cfg := config.APNsConfig{KeyID: "ABC1234567", TeamID: "TEAM123456", BundleID: "com.example.datawatch"}
	if _, err := NewDispatcher(cfg, nil); err == nil {
		t.Fatal("expected an error when neither key_secret nor key_path is set")
	}
}

func TestNewDispatcher_LoadsFromKeyPath(t *testing.T) {
	keyPEM, _ := testAPNsKeyPEM(t)
	dir := t.TempDir()
	keyPath := dir + "/AuthKey.p8"
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	cfg := config.APNsConfig{
		KeyID: "ABC1234567", TeamID: "TEAM123456", BundleID: "com.example.datawatch",
		KeyPath: keyPath,
	}
	d, err := NewDispatcher(cfg, nil)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	if d.baseURL != prodBaseURL {
		t.Errorf("baseURL = %q, want prod by default", d.baseURL)
	}
}

func TestNewDispatcher_SandboxURL(t *testing.T) {
	keyPEM, _ := testAPNsKeyPEM(t)
	dir := t.TempDir()
	keyPath := dir + "/AuthKey.p8"
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	cfg := config.APNsConfig{
		KeyID: "ABC1234567", TeamID: "TEAM123456", BundleID: "com.example.datawatch",
		KeyPath: keyPath, Sandbox: true,
	}
	d, err := NewDispatcher(cfg, nil)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	if d.baseURL != sandboxBaseURL {
		t.Errorf("baseURL = %q, want sandbox", d.baseURL)
	}
}

// GH#183 — baseURLFor must resolve from the per-device environment
// when one is known, not just the dispatcher's own configured default.
// Without this, a device registered under one environment would 400
// BadDeviceToken against the daemon's global push.apns.sandbox setting
// whenever it disagreed with the device's own build.
func TestBaseURLFor(t *testing.T) {
	d := &Dispatcher{baseURL: prodBaseURL} // configured default: production
	cases := []struct {
		environment string
		want        string
	}{
		{"development", sandboxBaseURL},
		{"production", prodBaseURL},
		{"", prodBaseURL},      // unknown/unset -> dispatcher's configured default
		{"bogus", prodBaseURL}, // unrecognized value -> same safe fallback
	}
	for _, c := range cases {
		if got := d.baseURLFor(c.environment); got != c.want {
			t.Errorf("baseURLFor(%q) = %q, want %q", c.environment, got, c.want)
		}
	}

	// Same cases again with a sandbox-configured dispatcher, to confirm
	// the per-device override wins over EITHER configured default, not
	// just production's.
	dSandbox := &Dispatcher{baseURL: sandboxBaseURL}
	if got := dSandbox.baseURLFor("production"); got != prodBaseURL {
		t.Errorf("a device explicitly registered as production must still reach the production host even when the dispatcher default is sandbox, got %q", got)
	}
	if got := dSandbox.baseURLFor(""); got != sandboxBaseURL {
		t.Errorf("baseURLFor(\"\") = %q, want the dispatcher's own sandbox default", got)
	}
}
