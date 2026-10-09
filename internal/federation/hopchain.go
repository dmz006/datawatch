// GH#201 Phase 2 — hop-chain origin-actor attribution.
//
// Phase 1 (access.log) records which PEER presented a request to this
// daemon, never who behind that peer actually initiated it on ITS OWN
// daemon: if operator Alice on daemon A triggers an action that daemon
// A forwards to daemon B as a federated peer call, daemon B's access
// log says peer:daemon-a, never Alice. This file threads an
// origin-actor claim across daemon-to-daemon hops (ProxyRouter's LLM
// delegation, the multi-server /api/proxy and /remote aggregation/
// embed routes) via a signed, append-only Chain carried in the
// X-Datawatch-Hop-Chain header.
//
// Trust model (operator decision, 2026-10-09): HMAC over each hop's
// EXISTING shared peer bearer token, not new asymmetric signing keys —
// no asymmetric identity infrastructure exists anywhere in this
// codebase today, and building one was explicitly declined as
// oversized for this ask. A receiving daemon verifies only the LAST
// entry of an incoming chain, using the same token the request just
// authenticated with (symmetric — the one secret both sides of that
// specific hop already share). That makes this hop-by-hop verified,
// not end-to-end re-verifiable by the final daemon alone: in a 3-hop
// chain, the first link was checked by the second daemon at the
// moment it received it, not re-checked by the third. That's an
// accepted tradeoff, not an oversight: a peer that holds a real shared
// token for some hop already has full access at that trust level
// today (an unsigned field would let it fabricate the same claim
// anyway). This chain's value is catching wire tampering and
// forwarding bugs, and giving an honest intermediate a tamper-evident
// way to vouch for what it received — not defending against a peer
// that has already decided to lie about its own traffic.
package federation

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"
)

// HopChainHeader is the HTTP header carrying an encoded Chain on a
// daemon-to-daemon federation/proxy forward.
const HopChainHeader = "X-Datawatch-Hop-Chain"

// HopEntry is one link. Actor stays constant across every entry in a
// Chain — it names who originated the action, carried forward
// unchanged — while Daemon/TS/Sig vary per hop, so the full chain is a
// real hop list ("Alice@daemon-a, daemon-a→daemon-b,
// daemon-b→daemon-c"), not a single collapsed origin field.
type HopEntry struct {
	Actor  string `json:"actor"`
	Daemon string `json:"daemon"`
	TS     int64  `json:"ts"`
	Sig    string `json:"sig"`
}

// Chain is the ordered, origin-first hop list.
type Chain []HopEntry

// signingInput is the deterministic byte sequence a hop's signature
// covers: the prior chain's JSON (so tampering with any earlier entry
// changes every later signature's input too) plus this entry's own
// unsigned fields, delimited so no field can bleed into its neighbor.
func signingInput(prior Chain, actor, daemon string, ts int64) ([]byte, error) {
	// Normalize nil to an empty (non-nil) slice before marshaling: Go's
	// encoding/json renders a nil slice as "null" but an empty one as
	// "[]". SignHop is typically called with a literal nil for the
	// origin entry, while VerifyLastHop derives its prior slice via
	// chain[:len(chain)-1] — for a 1-entry chain that's a non-nil,
	// zero-length slice. Without this normalization the two produce
	// different signing input for the same logical "no prior entries"
	// state and every origin-entry signature would fail to verify.
	if prior == nil {
		prior = Chain{}
	}
	priorJSON, err := json.Marshal(prior)
	if err != nil {
		return nil, err
	}
	in := make([]byte, 0, len(priorJSON)+len(actor)+len(daemon)+24)
	in = append(in, priorJSON...)
	in = append(in, '|')
	in = append(in, actor...)
	in = append(in, '|')
	in = append(in, daemon...)
	in = append(in, '|')
	in = append(in, strconv.FormatInt(ts, 10)...)
	return in, nil
}

func sign(input []byte, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(input) // #nosec G104 -- hash.Hash.Write never returns an error
	return hex.EncodeToString(mac.Sum(nil))
}

// SignHop returns a new, signed entry to append to prior (prior is
// never mutated). key is the bearer token shared with the hop this
// entry is about to be sent to — the only secret the receiving daemon
// can verify against.
func SignHop(prior Chain, actor, daemon string, ts int64, key string) (HopEntry, error) {
	input, err := signingInput(prior, actor, daemon, ts)
	if err != nil {
		return HopEntry{}, err
	}
	return HopEntry{Actor: actor, Daemon: daemon, TS: ts, Sig: sign(input, key)}, nil
}

// VerifyLastHop reports whether chain's final entry's signature is
// valid for the chain that precedes it, under key. False for an empty
// chain or a marshal failure on the prior slice.
func VerifyLastHop(chain Chain, key string) bool {
	if len(chain) == 0 {
		return false
	}
	last := chain[len(chain)-1]
	prior := chain[:len(chain)-1]
	input, err := signingInput(prior, last.Actor, last.Daemon, last.TS)
	if err != nil {
		return false
	}
	want := sign(input, key)
	return hmac.Equal([]byte(want), []byte(last.Sig))
}

// EncodeChain base64-encodes chain's JSON for header transport.
// Returns "" for an empty chain or a marshal failure — callers should
// treat that as "don't set the header", never as an error worth
// failing the forward over.
func EncodeChain(c Chain) string {
	if len(c) == 0 {
		return ""
	}
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

// DecodeChain reverses EncodeChain. ("", nil, nil) for an empty input.
func DecodeChain(s string) (Chain, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	var c Chain
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	return c, nil
}

// OriginActor returns the actor named by chain's first entry, or ""
// for an empty chain.
func OriginActor(chain Chain) string {
	if len(chain) == 0 {
		return ""
	}
	return chain[0].Actor
}

// --- context plumbing ---
//
// Lets a receiving daemon's auth layer (internal/server's
// fedAuthMiddleware) hand a verified chain and the locally-resolved
// principal down through context, so a downstream forwarding call
// (internal/inference's ProxyRouter, or internal/server's own
// /api/proxy and /remote handlers) can extend the chain without
// internal/inference needing to import internal/server (which would
// cycle back through internal/inference's own dependents).

type hopCtxKey int

const (
	chainCtxKey hopCtxKey = iota
	principalCtxKey
)

// ContextWithChain attaches a verified incoming Chain to ctx.
func ContextWithChain(ctx context.Context, c Chain) context.Context {
	return context.WithValue(ctx, chainCtxKey, c)
}

// ChainFromContext returns the Chain attached by ContextWithChain, or
// nil if this request didn't arrive carrying one (including: it's the
// origin, or an incoming chain failed verification).
func ChainFromContext(ctx context.Context) Chain {
	c, _ := ctx.Value(chainCtxKey).(Chain)
	return c
}

// ContextWithPrincipal attaches the locally-resolved principal label
// (e.g. "admin", "session-scoped", "peer:<name>") to ctx.
func ContextWithPrincipal(ctx context.Context, principal string) context.Context {
	return context.WithValue(ctx, principalCtxKey, principal)
}

// PrincipalFromContext returns the principal attached by
// ContextWithPrincipal, or "" if none was set.
func PrincipalFromContext(ctx context.Context) string {
	p, _ := ctx.Value(principalCtxKey).(string)
	return p
}

// BuildOrExtendChain produces the Chain a forwarding call should send
// to its next hop: extends ctx's existing chain (if this request
// arrived carrying one) with one more signed entry, or originates a
// new one from ctx's resolved principal (if this daemon is itself the
// first hop). key is the bearer token about to be used to authenticate
// to the next hop — the same value as the Authorization header this
// call is about to send, since that's the only secret the receiving
// daemon can verify against.
//
// Returns (nil, nil) — meaning "don't set the header" — when there's
// no key to sign with or no resolvable actor; never an error for that
// case, since a forward with no attribution available degrades safely
// to Phase 1's peer:<name> behavior rather than failing the request.
func BuildOrExtendChain(ctx context.Context, selfDaemon, key string) (Chain, error) {
	if key == "" {
		return nil, nil
	}
	prior := ChainFromContext(ctx)
	actor := PrincipalFromContext(ctx)
	if len(prior) > 0 {
		actor = OriginActor(prior)
	}
	if actor == "" {
		return nil, nil
	}
	entry, err := SignHop(prior, actor, selfDaemon, time.Now().Unix(), key)
	if err != nil {
		return nil, err
	}
	out := make(Chain, 0, len(prior)+1)
	out = append(out, prior...)
	out = append(out, entry)
	return out, nil
}
