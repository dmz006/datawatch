package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
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
