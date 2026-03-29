// Package secfile provides XChaCha20-Poly1305 encrypted file I/O for data stores.
// The encryption key is derived once at startup (via Argon2id in the config
// package) and passed to each store as a raw 32-byte key. Per-operation
// writes use a fresh random nonce so the ciphertext differs on every write
// without re-running the expensive KDF.
//
// File format:
//
//	"DWDAT2\n" + base64(nonce24 + ciphertext)
//
// Backward compatible: reads DWDAT1 (AES-256-GCM) files transparently.
package secfile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	magic    = "DWDAT1\n" // v1 (AES-256-GCM) — read-only backward compat
	magicV2  = "DWDAT2\n" // v2 (XChaCha20-Poly1305)
	nonceLen = 24          // XChaCha20 nonce size
)

// IsEncrypted reports whether data was written by this package (v1 or v2).
func IsEncrypted(data []byte) bool {
	s := string(data)
	return strings.HasPrefix(s, magicV2) || strings.HasPrefix(s, magic)
}

// Encrypt encrypts plaintext with XChaCha20-Poly1305 using the given 32-byte key.
func Encrypt(plaintext, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secfile: key must be 32 bytes, got %d", len(key))
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := aead.Seal(nonce, nonce, plaintext, nil) // nonce || ciphertext
	return []byte(magicV2 + base64.StdEncoding.EncodeToString(ct) + "\n"), nil
}

// Decrypt decrypts data produced by Encrypt (v1 or v2) using the given 32-byte key.
func Decrypt(data, key []byte) ([]byte, error) {
	s := string(data)
	if strings.HasPrefix(s, magicV2) {
		return decryptV2(s, key)
	}
	if strings.HasPrefix(s, magic) {
		return decryptV1(s, key)
	}
	return nil, fmt.Errorf("secfile: not an encrypted file")
}

func decryptV2(s string, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secfile: key must be 32 bytes, got %d", len(key))
	}
	encoded := strings.TrimSpace(strings.TrimPrefix(s, magicV2))
	combined, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("secfile: base64 decode: %w", err)
	}
	if len(combined) < nonceLen+1 {
		return nil, fmt.Errorf("secfile: data too short")
	}
	nonce := combined[:nonceLen]
	ct := combined[nonceLen:]

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	pt, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("secfile: decrypt failed (wrong key?): %w", err)
	}
	return pt, nil
}

// decryptV1 handles legacy AES-256-GCM files for backward compatibility.
func decryptV1(s string, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secfile: key must be 32 bytes, got %d", len(key))
	}
	encoded := strings.TrimSpace(strings.TrimPrefix(s, magic))
	combined, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("secfile: base64 decode: %w", err)
	}
	v1NonceLen := 12
	if len(combined) < v1NonceLen+1 {
		return nil, fmt.Errorf("secfile: data too short")
	}
	nonce := combined[:v1NonceLen]
	ct := combined[v1NonceLen:]

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
func ReadFile(path string, key []byte) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if key != nil {
		if IsEncrypted(data) {
			return Decrypt(data, key)
		}
		// Plaintext file opened with a key: return as-is (migration support).
	}
	return data, nil
}

// WriteFile writes data to path with the given perm, encrypting with key if non-nil.
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
