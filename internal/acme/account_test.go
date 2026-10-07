package acme

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-acme/lego/v4/registration"
)

func TestLoadOrCreateAccount_GeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()

	a1, err := LoadOrCreateAccount(dir, "ops@example.com", nil)
	if err != nil {
		t.Fatalf("LoadOrCreateAccount: %v", err)
	}
	if a1.GetPrivateKey() == nil {
		t.Fatal("expected a generated private key")
	}
	if a1.IsRegisteredFor("https://acme-staging-v02.api.letsencrypt.org/directory") {
		t.Fatal("new account should not be registered yet")
	}

	if _, err := os.Stat(filepath.Join(dir, "acme", "account.json")); err != nil {
		t.Fatalf("expected account.json to be written: %v", err)
	}

	// Reload — must return the SAME key, not a freshly generated one.
	a2, err := LoadOrCreateAccount(dir, "ops@example.com", nil)
	if err != nil {
		t.Fatalf("reload LoadOrCreateAccount: %v", err)
	}
	pem1, err := marshalECDSAKey(a1.key)
	if err != nil {
		t.Fatalf("marshal a1: %v", err)
	}
	pem2, err := marshalECDSAKey(a2.key)
	if err != nil {
		t.Fatalf("marshal a2: %v", err)
	}
	if string(pem1) != string(pem2) {
		t.Fatal("reloaded account key does not match the originally generated key")
	}
}

func TestAccount_SetRegistration_PersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	const dirURL = "https://example.test/directory"

	a1, err := LoadOrCreateAccount(dir, "", nil)
	if err != nil {
		t.Fatalf("LoadOrCreateAccount: %v", err)
	}
	if err := a1.SetRegistration(&registration.Resource{URI: "https://example.test/acme/acct/1"}, dirURL); err != nil {
		t.Fatalf("SetRegistration: %v", err)
	}
	if !a1.IsRegisteredFor(dirURL) {
		t.Fatal("expected IsRegisteredFor(dirURL) true after SetRegistration")
	}

	a2, err := LoadOrCreateAccount(dir, "", nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !a2.IsRegisteredFor(dirURL) {
		t.Fatal("reloaded account should still be registered for the same directory")
	}
}

// TestAccount_IsRegisteredFor_DoesNotCrossDirectories is the regression
// test for the live-test bug (2026-10-06): a registration valid against
// one ACME directory (e.g. staging) must NOT be reported as valid for a
// different directory (e.g. production) — Let's Encrypt rejects a
// mismatched account URL with a 400 malformed/invalid-account-URL error.
func TestAccount_IsRegisteredFor_DoesNotCrossDirectories(t *testing.T) {
	dir := t.TempDir()
	staging := "https://acme-staging-v02.api.letsencrypt.org/directory"
	production := "https://acme-v02.api.letsencrypt.org/directory"

	a, err := LoadOrCreateAccount(dir, "", nil)
	if err != nil {
		t.Fatalf("LoadOrCreateAccount: %v", err)
	}
	if err := a.SetRegistration(&registration.Resource{URI: "https://acme-staging-v02.api.letsencrypt.org/acme/acct/1"}, staging); err != nil {
		t.Fatalf("SetRegistration: %v", err)
	}

	if !a.IsRegisteredFor(staging) {
		t.Fatal("expected registered for staging, the directory it was actually registered against")
	}
	if a.IsRegisteredFor(production) {
		t.Fatal("a staging registration must NOT be reported as valid for production — this is exactly the bug that broke the live endpoint flip")
	}
}

// TestAccount_ClearRegistration_NilsGetRegistration is the regression
// test for the second layer of the same live-test bug: lego's client
// inspects GetRegistration() to pick embedded-JWK (new account) vs KeyID
// (existing account) signing. A stale non-nil Resource from a different
// directory made it choose KeyID and get rejected ("No embedded JWK in
// JWS header"). ClearRegistration must make GetRegistration() return nil
// again, in memory, without requiring a save.
func TestAccount_ClearRegistration_NilsGetRegistration(t *testing.T) {
	dir := t.TempDir()
	a, err := LoadOrCreateAccount(dir, "", nil)
	if err != nil {
		t.Fatalf("LoadOrCreateAccount: %v", err)
	}
	if err := a.SetRegistration(&registration.Resource{URI: "https://example.test/acme/acct/1"}, "https://staging.example/directory"); err != nil {
		t.Fatalf("SetRegistration: %v", err)
	}
	if a.GetRegistration() == nil {
		t.Fatal("test setup bug: expected a non-nil registration before ClearRegistration")
	}

	a.ClearRegistration()

	if a.GetRegistration() != nil {
		t.Fatal("expected GetRegistration() to be nil after ClearRegistration — a stale cross-directory registration would make lego sign with the wrong JWS mode")
	}
}

func TestLoadOrCreateAccount_EncryptedAtRest(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	if _, err := LoadOrCreateAccount(dir, "", key); err != nil {
		t.Fatalf("LoadOrCreateAccount with encKey: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "acme", "account.json"))
	if err != nil {
		t.Fatalf("read account.json: %v", err)
	}
	n := len(raw)
	if n > 1 && raw[0] == '{' {
		t.Fatal("account.json should be DWDAT2-encrypted at rest, found plaintext JSON")
	}

	// Loading with the WRONG key must fail, not silently decrypt garbage.
	wrongKey := make([]byte, 32)
	wrongKey[0] = 0xFF
	if _, err := LoadOrCreateAccount(dir, "", wrongKey); err == nil {
		t.Fatal("expected an error loading an encrypted account with the wrong key")
	}
}
