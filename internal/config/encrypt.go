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
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	magicHeader  = "DWATCH1\n" // v1 (AES-256-GCM) — kept for backward compat reads
	magicV2      = "DWATCH2\n" // v2 (XChaCha20-Poly1305)
	saltLen      = 16
	keyLen       = 32
	argonTime    = 1
	argonMemory  = 64 * 1024 // 64 MB
	argonThreads = 4
)

// IsEncrypted reports whether data starts with a datawatch encryption magic header (v1 or v2).
func IsEncrypted(data []byte) bool {
	s := string(data)
	return strings.HasPrefix(s, magicV2) || strings.HasPrefix(s, magicHeader)
}

// Encrypt encrypts plaintext with XChaCha20-Poly1305 using Argon2id key derivation.
// The salt is embedded in the output for later extraction by ExtractSalt.
// Output format: DWATCH2\n + base64(salt16 + nonce24 + ciphertext) + "\n"
func Encrypt(plaintext []byte, password []byte) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	return EncryptWithSalt(plaintext, password, salt)
}

// EncryptWithSalt encrypts plaintext using a specific salt (for re-encrypting with same salt).
func EncryptWithSalt(plaintext, password, salt []byte) ([]byte, error) {
	key := argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, keyLen)

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("create XChaCha20: %w", err)
	}

	nonce := make([]byte, chacha20poly1305.NonceSizeX) // 24 bytes
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := aead.Seal(nil, nonce, plaintext, nil)

	combined := make([]byte, 0, saltLen+len(nonce)+len(ciphertext))
	combined = append(combined, salt...)
	combined = append(combined, nonce...)
	combined = append(combined, ciphertext...)

	encoded := base64.StdEncoding.EncodeToString(combined)
	return []byte(magicV2 + encoded + "\n"), nil
}

// ExtractSalt extracts the 16-byte salt from an encrypted config file without decrypting.
// This is used to derive data store keys without needing a separate salt file.
func ExtractSalt(data []byte) ([]byte, error) {
	s := string(data)
	var encoded string
	if strings.HasPrefix(s, magicV2) {
		encoded = strings.TrimSpace(strings.TrimPrefix(s, magicV2))
	} else if strings.HasPrefix(s, magicHeader) {
		encoded = strings.TrimSpace(strings.TrimPrefix(s, magicHeader))
	} else {
		return nil, fmt.Errorf("not an encrypted config file")
	}
	combined, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	if len(combined) < saltLen {
		return nil, fmt.Errorf("encrypted data too short for salt extraction")
	}
	salt := make([]byte, saltLen)
	copy(salt, combined[:saltLen])
	return salt, nil
}

// LoadOrGenerateSalt reads a 16-byte salt from dataDir/enc.salt (legacy support).
// New installations use ExtractSalt from the encrypted config instead.
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

// DeriveKey derives a 32-byte key from a password using Argon2id.
// salt must be exactly 16 bytes.
func DeriveKey(password, salt []byte) []byte {
	return argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, keyLen)
}

// Decrypt decrypts data produced by Encrypt (v1 or v2) using the given password.
func Decrypt(data []byte, password []byte) ([]byte, error) {
	s := string(data)
	if strings.HasPrefix(s, magicV2) {
		return decryptV2(s, password)
	}
	if strings.HasPrefix(s, magicHeader) {
		return decryptV1(s, password)
	}
	return nil, fmt.Errorf("not an encrypted config file")
}

func decryptV2(s string, password []byte) ([]byte, error) {
	encoded := strings.TrimSpace(strings.TrimPrefix(s, magicV2))
	combined, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	nonceSize := chacha20poly1305.NonceSizeX // 24
	if len(combined) < saltLen+nonceSize+1 {
		return nil, fmt.Errorf("encrypted data too short")
	}

	salt := combined[:saltLen]
	nonce := combined[saltLen : saltLen+nonceSize]
	ciphertext := combined[saltLen+nonceSize:]

	key := argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, keyLen)

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("create XChaCha20: %w", err)
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed (wrong password?): %w", err)
	}
	return plaintext, nil
}

// decryptV1 handles legacy AES-256-GCM encrypted configs for backward compatibility.
func decryptV1(s string, password []byte) ([]byte, error) {
	encoded := strings.TrimSpace(strings.TrimPrefix(s, magicHeader))
	combined, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	v1NonceLen := 12
	if len(combined) < saltLen+v1NonceLen+1 {
		return nil, fmt.Errorf("encrypted data too short")
	}

	salt := combined[:saltLen]
	nonce := combined[saltLen : saltLen+v1NonceLen]
	ciphertext := combined[saltLen+v1NonceLen:]

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
