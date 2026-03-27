// Package secfile provides AES-256-GCM encrypted file I/O for data stores.
// The encryption key is derived once at startup (via Argon2id in the config
// package) and passed to each store as a raw 32-byte key.  Per-operation
// writes use a fresh random nonce so the ciphertext differs on every write
// without re-running the expensive KDF.
//
// File format:
//
//	"DWDAT1\n" + base64(nonce12 + ciphertext)
package secfile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

const (
	magic    = "DWDAT1\n"
	nonceLen = 12
)

// IsEncrypted reports whether data was written by this package.
func IsEncrypted(data []byte) bool {
	return strings.HasPrefix(string(data), magic)
}

// Encrypt encrypts plaintext with AES-256-GCM using the given 32-byte key.
// Returns error if key is not exactly 32 bytes.
func Encrypt(plaintext, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secfile: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nonce, nonce, plaintext, nil) // nonce || ciphertext
	return []byte(magic + base64.StdEncoding.EncodeToString(ct) + "\n"), nil
}

// Decrypt decrypts data produced by Encrypt using the given 32-byte key.
func Decrypt(data, key []byte) ([]byte, error) {
	if !IsEncrypted(data) {
		return nil, fmt.Errorf("secfile: not an encrypted file")
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secfile: key must be 32 bytes, got %d", len(key))
	}
	encoded := strings.TrimSpace(strings.TrimPrefix(string(data), magic))
	combined, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("secfile: base64 decode: %w", err)
	}
	if len(combined) < nonceLen+1 {
		return nil, fmt.Errorf("secfile: data too short")
	}
	nonce := combined[:nonceLen]
	ct := combined[nonceLen:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("secfile: decrypt failed (wrong key?): %w", err)
	}
	return pt, nil
}

// ReadFile reads path, decrypting with key if key is non-nil.
// If the file does not exist os.ErrNotExist is returned.
func ReadFile(path string, key []byte) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if key != nil {
		if IsEncrypted(data) {
			return Decrypt(data, key)
		}
		// Plaintext file opened with a key: return as-is so callers can
		// transparently handle files written before encryption was enabled.
	}
	return data, nil
}

// WriteFile writes data to path with the given perm, encrypting with key if
// key is non-nil.
func WriteFile(path string, data []byte, perm os.FileMode, key []byte) error {
	if key != nil {
		var err error
		data, err = Encrypt(data, key)
		if err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, perm)
}
