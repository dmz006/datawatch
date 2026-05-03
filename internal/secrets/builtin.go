// BL242 Phase 1 — AES-256-GCM encrypted JSON secrets store.
//
// Layout on disk:
//   ~/.datawatch/secrets.key  — 32 random bytes, 0600
//   ~/.datawatch/secrets.db   — nonce(12) + GCM ciphertext of JSON, 0600
//
// Set DATAWATCH_SECRETS_KEY=<32 bytes> to override the keyfile for
// headless / container deployments.

package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"
)

// BuiltinStore is the default AES-256-GCM encrypted secrets backend.
type BuiltinStore struct {
	mu      sync.RWMutex
	keyPath string
	dbPath  string
}

type dbFile struct {
	Secrets map[string]Secret `json:"secrets"`
}

// NewBuiltinStore opens (or creates) the built-in encrypted secrets store
// rooted at dataDir (typically ~/.datawatch).
func NewBuiltinStore(dataDir string) (*BuiltinStore, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("secrets: mkdir: %w", err)
	}
	bs := &BuiltinStore{
		keyPath: dataDir + "/secrets.key",
		dbPath:  dataDir + "/secrets.db",
	}
	if _, err := os.Stat(bs.keyPath); os.IsNotExist(err) {
		key := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("secrets: generate key: %w", err)
		}
		if err := os.WriteFile(bs.keyPath, key, 0600); err != nil {
			return nil, fmt.Errorf("secrets: write keyfile: %w", err)
		}
	}
	return bs, nil
}

func (bs *BuiltinStore) loadKey() ([]byte, error) {
	if env := os.Getenv("DATAWATCH_SECRETS_KEY"); env != "" {
		key := []byte(env)
		if len(key) != 32 {
			return nil, fmt.Errorf("secrets: DATAWATCH_SECRETS_KEY must be exactly 32 bytes (got %d)", len(key))
		}
		return key, nil
	}
	key, err := os.ReadFile(bs.keyPath)
	if err != nil {
		return nil, fmt.Errorf("secrets: read keyfile: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: keyfile must be 32 bytes (got %d)", len(key))
	}
	return key, nil
}

func (bs *BuiltinStore) decrypt(key []byte) (*dbFile, error) {
	data, err := os.ReadFile(bs.dbPath)
	if os.IsNotExist(err) {
		return &dbFile{Secrets: make(map[string]Secret)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("secrets: read db: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: gcm: %w", err)
	}
	if len(data) < gcm.NonceSize() {
		return nil, fmt.Errorf("secrets: db corrupt (too short)")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("secrets: decrypt: %w", err)
	}
	var db dbFile
	if err := json.Unmarshal(plaintext, &db); err != nil {
		return nil, fmt.Errorf("secrets: unmarshal: %w", err)
	}
	if db.Secrets == nil {
		db.Secrets = make(map[string]Secret)
	}
	return &db, nil
}

func (bs *BuiltinStore) save(key []byte, db *dbFile) error {
	plaintext, err := json.Marshal(db)
	if err != nil {
		return fmt.Errorf("secrets: marshal: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("secrets: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("secrets: gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("secrets: nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	tmp := bs.dbPath + ".tmp"
	if err := os.WriteFile(tmp, ciphertext, 0600); err != nil {
		return fmt.Errorf("secrets: write: %w", err)
	}
	return os.Rename(tmp, bs.dbPath)
}

// List returns all secrets without their values, sorted by name.
func (bs *BuiltinStore) List() ([]Secret, error) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	key, err := bs.loadKey()
	if err != nil {
		return nil, err
	}
	db, err := bs.decrypt(key)
	if err != nil {
		return nil, err
	}
	out := make([]Secret, 0, len(db.Secrets))
	for _, s := range db.Secrets {
		s.Value = ""
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get returns the named secret including its value.
func (bs *BuiltinStore) Get(name string) (Secret, error) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	key, err := bs.loadKey()
	if err != nil {
		return Secret{}, err
	}
	db, err := bs.decrypt(key)
	if err != nil {
		return Secret{}, err
	}
	s, ok := db.Secrets[name]
	if !ok {
		return Secret{}, fmt.Errorf("%w: %q", ErrSecretNotFound, name)
	}
	return s, nil
}

// Set creates or updates a secret.
func (bs *BuiltinStore) Set(name, value string, tags []string, description string) error {
	if name == "" {
		return fmt.Errorf("secrets: name required")
	}
	bs.mu.Lock()
	defer bs.mu.Unlock()
	key, err := bs.loadKey()
	if err != nil {
		return err
	}
	db, err := bs.decrypt(key)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	s := Secret{
		Name:        name,
		Value:       value,
		Tags:        tags,
		Description: description,
		Backend:     "builtin",
		UpdatedAt:   now,
	}
	if existing, exists := db.Secrets[name]; exists {
		s.CreatedAt = existing.CreatedAt
	} else {
		s.CreatedAt = now
	}
	db.Secrets[name] = s
	return bs.save(key, db)
}

// Delete removes a secret by name.
func (bs *BuiltinStore) Delete(name string) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	key, err := bs.loadKey()
	if err != nil {
		return err
	}
	db, err := bs.decrypt(key)
	if err != nil {
		return err
	}
	if _, ok := db.Secrets[name]; !ok {
		return fmt.Errorf("%w: %q", ErrSecretNotFound, name)
	}
	delete(db.Secrets, name)
	return bs.save(key, db)
}

// Exists reports whether a secret with the given name exists.
func (bs *BuiltinStore) Exists(name string) (bool, error) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	key, err := bs.loadKey()
	if err != nil {
		return false, err
	}
	db, err := bs.decrypt(key)
	if err != nil {
		return false, err
	}
	_, ok := db.Secrets[name]
	return ok, nil
}
