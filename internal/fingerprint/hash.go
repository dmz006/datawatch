// Package fingerprint computes the keyed resource fingerprint that federation
// peers exchange for fingerprint reconciliation
// (docs/plans/2026-10-14-concurrency-and-budget-design.md §3c): two peers
// detect that they expose the same underlying resource without either side
// ever learning the other's raw GPU UUID, node-instance ID, or any other
// identifying detail. The fingerprint is the only thing allowed to cross the
// federation wire; the value it is derived from never does.
package fingerprint

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Resource kinds a fingerprint may be computed for. The kind is part of the
// MAC input so a node fingerprint can never be mistaken for a billing-group
// fingerprint (or vice versa) even when the underlying values coincide.
const (
	KindNode    = "compute_node"
	KindBilling = "billing_group"
)

// pairKeyMessage is the fixed message MACed under the pair key derivation.
// It namespaces the HMAC output so a pair key never collides with some other
// HMAC an operator might derive from the same token.
const pairKeyMessage = "datawatch-fingerprint-v1"

// PairKey derives the per-peer-pair key for fingerprint hashes from the
// bearer token the pulling daemon presents to the peer. That token is the
// existing federation auth material: the puller holds it in its multiserver
// entry for the peer and the responder holds it as the token of the
// authenticated caller, so both sides arrive at the same key without any new
// secret channel. The fixed message is MACed under the token, not the token
// itself, so the pair key is one-way in the token.
func PairKey(token string) ([]byte, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("pair key: token must not be empty")
	}
	return hmacSHA256([]byte(token), []byte(pairKeyMessage)), nil
}

// Hash returns the lowercase-hex HMAC-SHA256 fingerprint of value under key,
// MACed over kind + "\x00" + value. value must be the resource's own
// identity (e.g. the GPU UUID the node reports, or its disk-persisted
// node-instance ID) — never the daemon-local name or address a daemon has
// configured for it. Only the returned hash is ever sent to a peer; value
// must not be.
func Hash(key []byte, kind, value string) (string, error) {
	if len(key) == 0 {
		return "", fmt.Errorf("fingerprint: key must not be empty")
	}
	if kind != KindNode && kind != KindBilling {
		return "", fmt.Errorf("fingerprint: kind %q is not a known kind", kind)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("fingerprint: value must not be empty")
	}
	sum := hmacSHA256(key, []byte(kind+"\x00"+value))
	return hex.EncodeToString(sum), nil
}

func hmacSHA256(key, message []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	return mac.Sum(nil)
}
