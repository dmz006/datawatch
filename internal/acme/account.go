package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-acme/lego/v4/registration"

	"github.com/dmz006/datawatch/internal/secfile"
)

// accountRecord is the on-disk shape of the ACME account, DWDAT2-encrypted
// at {data_dir}/acme/account.json (see docs/encryption.md "What Gets
// Encrypted" — registered alongside servers.json/inference/llms.json).
// The private key is PEM-encoded ECDSA P-256 (RFC 8555's recommended
// curve for ACME account keys).
type accountRecord struct {
	Email         string                 `json:"email,omitempty"`
	PrivateKeyPEM string                 `json:"private_key_pem"`
	Registration  *registration.Resource `json:"registration,omitempty"`
	// RegisteredDirectoryURL is the ACME directory (staging or
	// production) that Registration is valid against. Bug found live
	// 2026-10-06: Let's Encrypt staging and production are SEPARATE
	// registries — an account registered against staging is unknown to
	// production (and vice versa), even though the same account *key*
	// works fine against either. Switching acme.endpoint without
	// tracking this caused "KeyID header contained an invalid account
	// URL" (400 malformed) on the first order against the new directory.
	RegisteredDirectoryURL string `json:"registered_directory_url,omitempty"`
}

// Account implements registration.User (lego's interface for account
// identity) backed by an encrypted on-disk record. One Account per
// datawatch instance — lego's account key is distinct from any TLS
// certificate's private key.
type Account struct {
	mu     sync.Mutex
	path   string
	encKey []byte

	email        string
	key          *ecdsa.PrivateKey
	reg          *registration.Resource
	registeredAt string // directory URL reg is valid against, see accountRecord
}

// GetEmail implements registration.User.
func (a *Account) GetEmail() string { return a.email }

// GetRegistration implements registration.User.
func (a *Account) GetRegistration() *registration.Resource { return a.reg }

// GetPrivateKey implements registration.User.
func (a *Account) GetPrivateKey() crypto.PrivateKey { return a.key }

// SetRegistration records the registration.Resource returned by a
// successful ACME account registration against directoryURL and
// persists it.
func (a *Account) SetRegistration(reg *registration.Resource, directoryURL string) error {
	a.mu.Lock()
	a.reg = reg
	a.registeredAt = directoryURL
	a.mu.Unlock()
	return a.save()
}

// IsRegisteredFor reports whether this account has already completed
// ACME registration against this EXACT directory (staging and
// production are separate registries — a registration valid against one
// is invalid against the other, see accountRecord's doc comment).
func (a *Account) IsRegisteredFor(directoryURL string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reg != nil && a.registeredAt == directoryURL
}

// LoadOrCreateAccount loads the encrypted account record at
// {data_dir}/acme/account.json, generating a fresh ECDSA P-256 key (and an
// empty record) if none exists yet. encKey is the same DWDAT2 derivation
// key used by every other encrypted store (servers.json, alerts.json,
// etc.) — nil is valid (unencrypted at rest) for a non---secure daemon,
// matching every other store's NewStore/NewStoreEncrypted split.
func LoadOrCreateAccount(dataDir string, email string, encKey []byte) (*Account, error) {
	path := filepath.Join(dataDir, "acme", "account.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create acme dir: %w", err)
	}
	a := &Account{path: path, encKey: encKey, email: email}

	data, err := secfile.ReadFile(path, encKey)
	switch {
	case err == nil:
		var rec accountRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, fmt.Errorf("parse acme account record: %w", err)
		}
		key, err := parseECDSAKey(rec.PrivateKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("parse acme account key: %w", err)
		}
		a.key = key
		a.reg = rec.Registration
		a.registeredAt = rec.RegisteredDirectoryURL
		if rec.Email != "" {
			a.email = rec.Email
		}
		return a, nil
	case os.IsNotExist(err):
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate acme account key: %w", err)
		}
		a.key = key
		if err := a.save(); err != nil {
			return nil, fmt.Errorf("persist new acme account: %w", err)
		}
		return a, nil
	default:
		return nil, fmt.Errorf("read acme account record: %w", err)
	}
}

func (a *Account) save() error {
	pemBytes, err := marshalECDSAKey(a.key)
	if err != nil {
		return err
	}
	rec := accountRecord{
		Email:                  a.email,
		PrivateKeyPEM:          string(pemBytes),
		Registration:           a.reg,
		RegisteredDirectoryURL: a.registeredAt,
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return secfile.WriteFile(a.path, data, 0600, a.encKey)
}

func marshalECDSAKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	block := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	return pem.EncodeToMemory(block), nil
}

func parseECDSAKey(pemStr string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("invalid PEM for acme account key")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}
