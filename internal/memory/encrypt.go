package memory

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/chacha20poly1305"
)

// fieldEncrypt encrypts plaintext with XChaCha20-Poly1305 and returns base64.
func fieldEncrypt(plaintext, key []byte) (string, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nonce, nonce, plaintext, nil) // nonce || ciphertext
	return base64.StdEncoding.EncodeToString(ct), nil
}

// fieldDecrypt decrypts a base64-encoded ciphertext with XChaCha20-Poly1305.
func fieldDecrypt(encoded string, key []byte) ([]byte, error) {
	combined, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	nonceSize := chacha20poly1305.NonceSizeX
	if len(combined) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := combined[:nonceSize]
	ciphertext := combined[nonceSize:]
	return aead.Open(nil, nonce, ciphertext, nil)
}

// KeyManager handles memory encryption key lifecycle.
type KeyManager struct {
	keyDir string
}

// NewKeyManager creates a key manager for the given data directory.
func NewKeyManager(dataDir string) *KeyManager {
	return &KeyManager{keyDir: dataDir}
}

func (km *KeyManager) keyPath() string {
	return filepath.Join(km.keyDir, "memory.key")
}

// Generate creates a new random 32-byte key and saves it.
func (km *KeyManager) Generate() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(km.keyDir, 0700); err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(km.keyPath(), []byte(encoded+"\n"), 0600); err != nil {
		return nil, err
	}
	return key, nil
}

// Load reads the key from the keyfile. Returns nil if no keyfile exists.
func (km *KeyManager) Load() ([]byte, error) {
	data, err := os.ReadFile(km.keyPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	// Try base64 decode
	key, err := base64.StdEncoding.DecodeString(string(data[:len(data)-1])) // trim \n
	if err != nil || len(key) != 32 {
		// Try raw 32 bytes
		if len(data) >= 32 {
			return data[:32], nil
		}
		return nil, fmt.Errorf("invalid key file format")
	}
	return key, nil
}

// Fingerprint returns the first 8 hex chars of the SHA-256 of the key.
func (km *KeyManager) Fingerprint(key []byte) string {
	if len(key) == 0 {
		return ""
	}
	h := sha256.Sum256(key)
	return hex.EncodeToString(h[:4])
}

// KeyFingerprint returns the fingerprint for a given key.
func KeyFingerprint(key []byte) string {
	if len(key) == 0 {
		return ""
	}
	h := sha256.Sum256(key)
	return hex.EncodeToString(h[:4])
}

// RotateKey re-encrypts all memory content with a new key.
// Returns the number of rows re-encrypted.
func RotateKey(store *Store, oldKey, newKey []byte) (int, error) {
	if len(newKey) != 32 {
		return 0, fmt.Errorf("new key must be 32 bytes")
	}

	rows, err := store.db.Query(
		`SELECT id, content, summary FROM memories`)
	if err != nil {
		return 0, err
	}

	type row struct {
		id      int64
		content string
		summary string
	}
	var toUpdate []row
	for rows.Next() {
		var r row
		rows.Scan(&r.id, &r.content, &r.summary) //nolint:errcheck
		toUpdate = append(toUpdate, r)
	}
	rows.Close()

	// Temporarily set old key for decryption
	origKey := store.encKey
	store.encKey = oldKey

	count := 0
	for _, r := range toUpdate {
		// Decrypt with old key
		plainContent := store.decryptField(r.content)
		plainSummary := store.decryptField(r.summary)

		// Encrypt with new key
		store.encKey = newKey
		encContent := store.encryptField(plainContent)
		encSummary := store.encryptField(plainSummary)
		store.encKey = oldKey // restore for next iteration

		_, err := store.db.Exec(
			`UPDATE memories SET content = ?, summary = ? WHERE id = ?`,
			encContent, encSummary, r.id,
		)
		if err == nil {
			count++
		}
	}

	// Set new key permanently
	store.encKey = newKey

	// Restore original if rotation was called with nil
	if origKey == nil && oldKey == nil {
		store.encKey = newKey
	}

	store.walLog("key_rotate", map[string]interface{}{
		"old_fingerprint": KeyFingerprint(oldKey),
		"new_fingerprint": KeyFingerprint(newKey),
		"rows_rotated":    count,
	})

	return count, nil
}

// MigrateToEncrypted encrypts all plaintext content with the given key.
// Called when encryption is first enabled on an existing store.
func MigrateToEncrypted(store *Store, key []byte) (int, error) {
	store.encKey = key
	rows, err := store.db.Query(
		`SELECT id, content, summary FROM memories WHERE content NOT LIKE 'ENC:%'`)
	if err != nil {
		return 0, err
	}

	type row struct {
		id      int64
		content string
		summary string
	}
	var toEncrypt []row
	for rows.Next() {
		var r row
		rows.Scan(&r.id, &r.content, &r.summary) //nolint:errcheck
		toEncrypt = append(toEncrypt, r)
	}
	rows.Close()

	count := 0
	for _, r := range toEncrypt {
		enc := store.encryptField(r.content)
		encSum := store.encryptField(r.summary)
		_, err := store.db.Exec(
			`UPDATE memories SET content = ?, summary = ? WHERE id = ?`,
			enc, encSum, r.id,
		)
		if err == nil {
			count++
		}
	}

	store.walLog("encrypt_migration", map[string]interface{}{
		"fingerprint":  KeyFingerprint(key),
		"rows_migrated": count,
	})

	return count, nil
}
