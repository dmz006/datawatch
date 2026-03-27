package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	magicHeader   = "DWATCH1\n"
	saltLen       = 16
	nonceLen      = 12
	keyLen        = 32
	argonTime     = 1
	argonMemory   = 64 * 1024 // 64 MB
	argonThreads  = 4
)

// IsEncrypted reports whether data starts with the datawatch encryption magic header.
func IsEncrypted(data []byte) bool {
	return strings.HasPrefix(string(data), magicHeader)
}

// Encrypt encrypts plaintext with AES-256-GCM using Argon2id key derivation.
// Output format: magicHeader + base64(salt16 + nonce12 + ciphertext) + "\n"
func Encrypt(plaintext []byte, password []byte) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	key := argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, keyLen)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	combined := make([]byte, 0, saltLen+nonceLen+len(ciphertext))
	combined = append(combined, salt...)
	combined = append(combined, nonce...)
	combined = append(combined, ciphertext...)

	encoded := base64.StdEncoding.EncodeToString(combined)
	return []byte(magicHeader + encoded + "\n"), nil
}

// DeriveKey derives a 32-byte AES key from a password using Argon2id.
// salt must be exactly 16 bytes. This is the same KDF used by Encrypt/Decrypt
// so a key derived here can be used with secfile.Encrypt/Decrypt directly,
// avoiding the per-operation KDF overhead for high-frequency store writes.
func DeriveKey(password, salt []byte) []byte {
	return argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, keyLen)
}

// LoadOrGenerateSalt reads a 16-byte salt from dataDir/enc.salt, generating
// and persisting a new random salt if the file does not exist.
// The salt is not secret; it is stored in plaintext.
func LoadOrGenerateSalt(dataDir string) ([]byte, error) {
	saltPath := filepath.Join(dataDir, "enc.salt")
	if data, err := os.ReadFile(saltPath); err == nil && len(data) == saltLen {
		return data, nil
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(saltPath, salt, 0600); err != nil {
		return nil, fmt.Errorf("write salt: %w", err)
	}
	return salt, nil
}

// Decrypt decrypts data produced by Encrypt using the given password.
func Decrypt(data []byte, password []byte) ([]byte, error) {
	if !IsEncrypted(data) {
		return nil, fmt.Errorf("not an encrypted config file")
	}

	encoded := strings.TrimSpace(strings.TrimPrefix(string(data), magicHeader))
	combined, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	if len(combined) < saltLen+nonceLen+1 {
		return nil, fmt.Errorf("encrypted data too short")
	}

	salt := combined[:saltLen]
	nonce := combined[saltLen : saltLen+nonceLen]
	ciphertext := combined[saltLen+nonceLen:]

	key := argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, keyLen)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed (wrong password?): %w", err)
	}
	return plaintext, nil
}
