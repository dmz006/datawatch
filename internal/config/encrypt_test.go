package config

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

func TestEncryptDecryptRoundtripV3(t *testing.T) {
	plaintext := []byte("super secret config contents")
	password := []byte("correct horse battery staple")

	ct, err := Encrypt(plaintext, password)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !strings.HasPrefix(string(ct), magicV3) {
		t.Fatalf("Encrypt did not produce a v3 envelope, got prefix %q", ct[:len(magicV3)])
	}
	if !IsEncrypted(ct) {
		t.Fatalf("IsEncrypted returned false for v3 ciphertext")
	}
	if NeedsResealV3(ct) {
		t.Fatalf("NeedsResealV3 returned true for a fresh v3 envelope")
	}

	pt, err := Decrypt(ct, password)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatalf("roundtrip mismatch: got %q want %q", pt, plaintext)
	}
}

func TestDecryptWrongPassword(t *testing.T) {
	ct, err := Encrypt([]byte("data"), []byte("correct-password"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := Decrypt(ct, []byte("wrong-password")); err == nil {
		t.Fatalf("Decrypt succeeded with wrong password")
	}
}

// TestDecryptLegacyV2 pins backward-compat reads of the old weak-Argon2id
// envelope. If this breaks, every pre-upgrade --secure config/data file
// becomes unreadable.
func TestDecryptLegacyV2(t *testing.T) {
	plaintext := []byte("legacy v2 payload")
	password := []byte("legacy-password")

	salt := make([]byte, saltLen)
	for i := range salt {
		salt[i] = byte(i)
	}
	key := argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, keyLen)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		t.Fatalf("NewX: %v", err)
	}
	nonce := make([]byte, 24)
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)
	combined := append(append(append([]byte{}, salt...), nonce...), ciphertext...)
	encoded := base64.StdEncoding.EncodeToString(combined)
	legacy := []byte(magicV2 + encoded + "\n")

	if !IsEncrypted(legacy) {
		t.Fatalf("IsEncrypted returned false for legacy v2 data")
	}
	if !NeedsResealV3(legacy) {
		t.Fatalf("NeedsResealV3 returned false for legacy v2 data")
	}

	pt, err := Decrypt(legacy, password)
	if err != nil {
		t.Fatalf("Decrypt legacy v2: %v", err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatalf("legacy v2 roundtrip mismatch: got %q want %q", pt, plaintext)
	}
}

func TestExtractSaltAllVersions(t *testing.T) {
	password := []byte("pw")
	ct, err := Encrypt([]byte("x"), password)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	salt, err := ExtractSalt(ct)
	if err != nil {
		t.Fatalf("ExtractSalt: %v", err)
	}
	if len(salt) != saltLen {
		t.Fatalf("ExtractSalt returned %d bytes, want %d", len(salt), saltLen)
	}
}
