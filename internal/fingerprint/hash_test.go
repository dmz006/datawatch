package fingerprint

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestPairKeyDiffersPerToken(t *testing.T) {
	a, err := PairKey("token-a")
	if err != nil {
		t.Fatalf("PairKey(token-a): %v", err)
	}
	b, err := PairKey("token-b")
	if err != nil {
		t.Fatalf("PairKey(token-b): %v", err)
	}
	// HMAC-SHA256 output is 32 bytes; different tokens must give
	// different keys.
	if len(a) != 32 {
		t.Fatalf("pair key length = %d, want 32", len(a))
	}
	if string(a) == string(b) {
		t.Fatalf("pair keys for different tokens are identical: %x", a)
	}
}

func TestHashStableForSameInput(t *testing.T) {
	key, err := PairKey("token-a")
	if err != nil {
		t.Fatalf("PairKey: %v", err)
	}
	const value = "GPU-0f88de7f-9c8b-0d4a-c6e0-ba9e5c1d0a2f"
	h1, err := Hash(key, KindNode, value)
	if err != nil {
		t.Fatalf("Hash first: %v", err)
	}
	h2, err := Hash(key, KindNode, value)
	if err != nil {
		t.Fatalf("Hash second: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("Hash unstable for identical input: %q != %q", h1, h2)
	}
	// 32-byte HMAC => 64 lowercase hex chars.
	raw, err := hex.DecodeString(h1)
	if err != nil {
		t.Fatalf("hash %q is not valid lowercase hex: %v", h1, err)
	}
	if len(raw) != 32 {
		t.Fatalf("hash decodes to %d bytes, want 32", len(raw))
	}
	if strings.ToLower(h1) != h1 {
		t.Fatalf("hash %q is not lowercase", h1)
	}
}

func TestHashDiffersPerPeerPair(t *testing.T) {
	// Same underlying value, two different peer-pair tokens: the
	// fingerprints must differ so a third party seeing one pair's
	// hashes cannot link them to the other pair's.
	keyA, err := PairKey("token-a")
	if err != nil {
		t.Fatalf("PairKey(token-a): %v", err)
	}
	keyB, err := PairKey("token-b")
	if err != nil {
		t.Fatalf("PairKey(token-b): %v", err)
	}
	const value = "GPU-0f88de7f-9c8b-0d4a-c6e0-ba9e5c1d0a2f"
	hA, err := Hash(keyA, KindNode, value)
	if err != nil {
		t.Fatalf("Hash(keyA): %v", err)
	}
	hB, err := Hash(keyB, KindNode, value)
	if err != nil {
		t.Fatalf("Hash(keyB): %v", err)
	}
	if hA == hB {
		t.Fatalf("same value under different peer-pair keys gives identical hashes: %q", hA)
	}
}

func TestHashDiffersPerKind(t *testing.T) {
	key, err := PairKey("token-a")
	if err != nil {
		t.Fatalf("PairKey: %v", err)
	}
	const value = "shared-raw-value"
	// A kind collision would let a node fingerprint be mistaken for a
	// billing-group fingerprint even when the raw values coincide.
	hNode, err := Hash(key, KindNode, value)
	if err != nil {
		t.Fatalf("Hash(KindNode): %v", err)
	}
	hBilling, err := Hash(key, KindBilling, value)
	if err != nil {
		t.Fatalf("Hash(KindBilling): %v", err)
	}
	if hNode == hBilling {
		t.Fatalf("same value under different kinds gives identical hashes: %q", hNode)
	}
}

func TestHashRejectsBadInput(t *testing.T) {
	key, err := PairKey("token-a")
	if err != nil {
		t.Fatalf("PairKey: %v", err)
	}

	badPairs := []struct {
		label string
		fn    func() (string, error)
	}{
		{"empty key", func() (string, error) { return Hash(nil, KindNode, "value") }},
		{"unknown kind", func() (string, error) { return Hash(key, "node", "value") }},
		{"empty value", func() (string, error) { return Hash(key, KindNode, "") }},
		{"whitespace value", func() (string, error) { return Hash(key, KindNode, "   ") }},
	}
	for _, tc := range badPairs {
		if _, err := tc.fn(); err == nil {
			t.Errorf("%s: expected error, got nil", tc.label)
		}
	}

	if _, err := PairKey(""); err == nil {
		t.Error("PairKey(\"\"): expected error, got nil")
	}
	if _, err := PairKey("   \n"); err == nil {
		t.Error("PairKey(whitespace): expected error, got nil")
	}
}
