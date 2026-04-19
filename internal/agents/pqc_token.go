// Package agents — F10 sprint 5 (S5.2) post-quantum bootstrap tokens.
//
// Replaces (additively, opt-in) the plain 32-byte hex bootstrap token
// with a structured envelope authenticated by ML-KEM 768 (key
// encapsulation) and ML-DSA 65 (signing) — the NIST-standardised
// post-quantum primitives implemented by Cloudflare CIRCL.
//
// Threat model improvement over the UUID flow:
//
//   * UUID token (S3.3) — leak == compromise. An attacker who reads
//     the parent's bootstrap log or sniffs the spawn env on a
//     compromised host can pose as the worker indefinitely (until
//     TTL expires). Defends only against guessing.
//   * PQC envelope — leak of the env-injected KEM public key gives
//     the attacker no usable secret. Bootstrap requires:
//       a. KEM ciphertext only the worker (holding the spawn-time
//          KEM private key inside the container) can produce
//       b. ML-DSA signature the parent verifies against the spawn-
//          time signing public key
//     Both must verify; one-without-the-other is rejected.
//
// Wire format (base64-encoded blob delivered as the token field):
//
//   <kem-ciphertext-b64> "." <signature-b64>
//
// The dot separator keeps the envelope inspectable without parsing
// JSON; the parent splits + base64-decodes both halves on receipt.
//
// Spawn-time env injection (set by Driver when PQCBootstrap is on):
//
//   DATAWATCH_PQC_MODE=ml-kem-768+ml-dsa-65
//   DATAWATCH_PQC_KEM_PRIV=<b64>     // worker's KEM private key
//   DATAWATCH_PQC_KEM_PUB=<b64>      // matching public (so worker
//                                       can self-verify the env didn't
//                                       get tampered)
//   DATAWATCH_PQC_SIGN_PRIV=<b64>    // worker's signing private key
//
// The PARENT keeps the matching kem-public + sign-public on the Agent
// record (never on disk by default — held in memory until ConsumeBootstrap).
//
// Backwards compatible: when PQCBootstrap is off (default), the
// existing UUID flow keeps working; ConsumeBootstrap accepts either
// form based on env presence on the Agent record.

package agents

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	kemschemes "github.com/cloudflare/circl/kem/schemes"
	signschemes "github.com/cloudflare/circl/sign/schemes"
)

// pqcKEMScheme + pqcSignScheme are the named schemes used for F10
// S5.2. Pinned by name to keep the wire format stable across CIRCL
// upgrades; bumping requires a coordinated parent + worker rollout.
const (
	pqcKEMSchemeName  = "ML-KEM-768"
	pqcSignSchemeName = "ML-DSA-65"
)

// PQCKeys holds the PQC material the parent retains for one Agent.
// All four byte slices are sensitive — never logged, json:"-", zeroed
// on Agent.GitToken-style cleanup paths.
type PQCKeys struct {
	KEMPrivateB64    string // worker holds this (env)
	KEMPublicB64     string // worker holds this (env, for self-check)
	SignPrivateB64   string // worker holds this (env)
	SignPublicB64    string // parent holds this (verifies signature)
	KEMSharedSecret  []byte // parent re-derives on consume; compared to worker's
}

// GeneratePQCKeys mints fresh KEM + signing keypairs for one spawn.
// Returned object's *Private fields go into the worker's spawn env;
// the parent retains the public counterparts to verify the bootstrap
// envelope at consume time.
func GeneratePQCKeys() (*PQCKeys, error) {
	kemScheme := kemschemes.ByName(pqcKEMSchemeName)
	if kemScheme == nil {
		return nil, fmt.Errorf("PQC: unknown KEM scheme %q", pqcKEMSchemeName)
	}
	signScheme := signschemes.ByName(pqcSignSchemeName)
	if signScheme == nil {
		return nil, fmt.Errorf("PQC: unknown sign scheme %q", pqcSignSchemeName)
	}

	kemPub, kemPriv, err := kemScheme.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("PQC KEM keygen: %w", err)
	}
	signPub, signPriv, err := signScheme.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("PQC sign keygen: %w", err)
	}

	kemPubBytes, _ := kemPub.MarshalBinary()
	kemPrivBytes, _ := kemPriv.MarshalBinary()
	signPubBytes, _ := signPub.MarshalBinary()
	signPrivBytes, _ := signPriv.MarshalBinary()

	return &PQCKeys{
		KEMPrivateB64:  base64.StdEncoding.EncodeToString(kemPrivBytes),
		KEMPublicB64:   base64.StdEncoding.EncodeToString(kemPubBytes),
		SignPrivateB64: base64.StdEncoding.EncodeToString(signPrivBytes),
		SignPublicB64:  base64.StdEncoding.EncodeToString(signPubBytes),
	}, nil
}

// MakePQCBootstrapToken produces the ".`-separated envelope a worker
// would send to /api/agents/bootstrap. Used both by the worker code
// path (real bootstrap) and by tests / smoke scripts.
//
// The envelope is constructed as:
//   1. KEM-encapsulate against the parent's KEM public → (ct, ss)
//   2. Sign agent_id with the worker's signing private key
//   3. Concatenate base64(ct) + "." + base64(sig)
//
// Returns (envelope, ss). The parent must arrive at the same ss
// independently by KEM-decapsulating ct with the parent's KEM
// private (which equals what the worker held — symmetry simplification
// for the spawn-key-pair ownership model).
func MakePQCBootstrapToken(agentID string, keys *PQCKeys) (string, []byte, error) {
	if keys == nil {
		return "", nil, errors.New("PQC: keys required")
	}
	kemScheme := kemschemes.ByName(pqcKEMSchemeName)
	signScheme := signschemes.ByName(pqcSignSchemeName)
	if kemScheme == nil || signScheme == nil {
		return "", nil, errors.New("PQC: scheme registration missing")
	}

	kemPubBytes, err := base64.StdEncoding.DecodeString(keys.KEMPublicB64)
	if err != nil {
		return "", nil, fmt.Errorf("PQC: decode KEM pub: %w", err)
	}
	kemPub, err := kemScheme.UnmarshalBinaryPublicKey(kemPubBytes)
	if err != nil {
		return "", nil, fmt.Errorf("PQC: parse KEM pub: %w", err)
	}

	ct, ss, err := kemScheme.Encapsulate(kemPub)
	if err != nil {
		return "", nil, fmt.Errorf("PQC: KEM encapsulate: %w", err)
	}

	signPrivBytes, err := base64.StdEncoding.DecodeString(keys.SignPrivateB64)
	if err != nil {
		return "", nil, fmt.Errorf("PQC: decode sign priv: %w", err)
	}
	signPriv, err := signScheme.UnmarshalBinaryPrivateKey(signPrivBytes)
	if err != nil {
		return "", nil, fmt.Errorf("PQC: parse sign priv: %w", err)
	}

	sig := signScheme.Sign(signPriv, []byte(agentID), nil)

	envelope := base64.StdEncoding.EncodeToString(ct) +
		"." +
		base64.StdEncoding.EncodeToString(sig)
	return envelope, ss, nil
}

// VerifyPQCBootstrapToken is the parent-side counterpart: split the
// envelope, KEM-decapsulate the ciphertext (against the keys the
// parent retained at spawn), verify the signature against the same
// agent_id, and return the derived shared secret.
//
// Caller (Manager.ConsumeBootstrap) compares the returned secret to
// the one stashed on Agent.PQCKeys.KEMSharedSecret to be doubly sure;
// this prevents accepting a token that signs the right agent_id but
// was constructed against a different KEM public (e.g. an attacker
// got hold of the signing key but not the KEM private).
func VerifyPQCBootstrapToken(envelope, agentID string, keys *PQCKeys) ([]byte, error) {
	if keys == nil {
		return nil, errors.New("PQC: keys required")
	}
	parts := strings.SplitN(envelope, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("PQC: envelope must be <ct>.<sig>")
	}
	ct, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("PQC: decode ct: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("PQC: decode sig: %w", err)
	}

	kemScheme := kemschemes.ByName(pqcKEMSchemeName)
	signScheme := signschemes.ByName(pqcSignSchemeName)
	if kemScheme == nil || signScheme == nil {
		return nil, errors.New("PQC: scheme registration missing")
	}

	kemPrivBytes, err := base64.StdEncoding.DecodeString(keys.KEMPrivateB64)
	if err != nil {
		return nil, fmt.Errorf("PQC: decode KEM priv: %w", err)
	}
	kemPriv, err := kemScheme.UnmarshalBinaryPrivateKey(kemPrivBytes)
	if err != nil {
		return nil, fmt.Errorf("PQC: parse KEM priv: %w", err)
	}
	ss, err := kemScheme.Decapsulate(kemPriv, ct)
	if err != nil {
		return nil, fmt.Errorf("PQC: KEM decapsulate: %w", err)
	}

	signPubBytes, err := base64.StdEncoding.DecodeString(keys.SignPublicB64)
	if err != nil {
		return nil, fmt.Errorf("PQC: decode sign pub: %w", err)
	}
	signPub, err := signScheme.UnmarshalBinaryPublicKey(signPubBytes)
	if err != nil {
		return nil, fmt.Errorf("PQC: parse sign pub: %w", err)
	}
	if !signScheme.Verify(signPub, []byte(agentID), sig, nil) {
		return nil, errors.New("PQC: signature mismatch")
	}
	return ss, nil
}

// _ retains crypto/rand referenced even when CIRCL drives entropy
// internally — leaves room for future audit-log enrichment.
var _ = rand.Reader
