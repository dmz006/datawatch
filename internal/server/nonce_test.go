// SEC-006 — nonce store tests.

package server

import (
	"testing"
	"time"
)

func TestNonceStore_MintConsume_RoundTrip(t *testing.T) {
	n := newNonceStore()
	nonce, expiresAt := n.Mint("admin-token")
	if nonce == "" {
		t.Fatal("expected a non-empty nonce")
	}
	if !expiresAt.After(time.Now()) {
		t.Error("expiresAt should be in the future")
	}
	tok, ok := n.Consume(nonce)
	if !ok || tok != "admin-token" {
		t.Fatalf("Consume() = %q, %v; want admin-token, true", tok, ok)
	}
}

func TestNonceStore_SingleUse(t *testing.T) {
	n := newNonceStore()
	nonce, _ := n.Mint("admin-token")
	if _, ok := n.Consume(nonce); !ok {
		t.Fatal("first consume should succeed")
	}
	if _, ok := n.Consume(nonce); ok {
		t.Error("second consume of the same nonce must fail — nonces are single-use")
	}
}

func TestNonceStore_UnknownNonceRejected(t *testing.T) {
	n := newNonceStore()
	if _, ok := n.Consume("not-a-real-nonce"); ok {
		t.Error("an unminted nonce must never resolve")
	}
	if _, ok := n.Consume(""); ok {
		t.Error("an empty nonce must never resolve")
	}
}

func TestNonceStore_ExpiredNonceRejected(t *testing.T) {
	n := newNonceStore()
	nonce, _ := n.Mint("admin-token")
	// Force-expire by rewriting the entry directly (avoids a real sleep).
	n.mu.Lock()
	e := n.entries[nonce]
	e.expiresAt = time.Now().Add(-time.Second)
	n.entries[nonce] = e
	n.mu.Unlock()

	if _, ok := n.Consume(nonce); ok {
		t.Error("an expired nonce must not resolve")
	}
}

func TestNonceStore_Sweep_RemovesExpiredOnly(t *testing.T) {
	n := newNonceStore()
	liveNonce, _ := n.Mint("admin-token")
	expiredNonce, _ := n.Mint("admin-token")
	n.mu.Lock()
	e := n.entries[expiredNonce]
	e.expiresAt = time.Now().Add(-time.Second)
	n.entries[expiredNonce] = e
	n.mu.Unlock()

	n.sweep()

	n.mu.Lock()
	_, expiredStillThere := n.entries[expiredNonce]
	_, liveStillThere := n.entries[liveNonce]
	n.mu.Unlock()
	if expiredStillThere {
		t.Error("sweep should have removed the expired entry")
	}
	if !liveStillThere {
		t.Error("sweep should not touch a still-live entry")
	}
}
