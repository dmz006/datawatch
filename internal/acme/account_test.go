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
	if a1.IsRegistered() {
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

	a1, err := LoadOrCreateAccount(dir, "", nil)
	if err != nil {
		t.Fatalf("LoadOrCreateAccount: %v", err)
	}
	if err := a1.SetRegistration(&registration.Resource{URI: "https://example.test/acme/acct/1"}); err != nil {
		t.Fatalf("SetRegistration: %v", err)
	}
	if !a1.IsRegistered() {
		t.Fatal("expected IsRegistered true after SetRegistration")
	}

	a2, err := LoadOrCreateAccount(dir, "", nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !a2.IsRegistered() {
		t.Fatal("reloaded account should still be registered")
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
