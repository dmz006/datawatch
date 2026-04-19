// F10 sprint 5 (S5.2) — PQC bootstrap token tests.

package agents

import (
	"bytes"
	"strings"
	"testing"
)

// Round-trip: keys generated → token built → token verified, secrets match.
func TestPQC_RoundTrip(t *testing.T) {
	keys, err := GeneratePQCKeys()
	if err != nil {
		t.Fatalf("GeneratePQCKeys: %v", err)
	}
	if keys.KEMPublicB64 == "" || keys.KEMPrivateB64 == "" ||
		keys.SignPublicB64 == "" || keys.SignPrivateB64 == "" {
		t.Fatalf("keys missing fields: %+v", keys)
	}

	envelope, ssWorker, err := MakePQCBootstrapToken("agent-xyz", keys)
	if err != nil {
		t.Fatalf("MakePQCBootstrapToken: %v", err)
	}
	if !strings.Contains(envelope, ".") {
		t.Fatalf("envelope missing separator: %q", envelope)
	}

	ssParent, err := VerifyPQCBootstrapToken(envelope, "agent-xyz", keys)
	if err != nil {
		t.Fatalf("VerifyPQCBootstrapToken: %v", err)
	}
	if !bytes.Equal(ssWorker, ssParent) {
		t.Errorf("shared secret mismatch:\n  worker=%x\n  parent=%x", ssWorker, ssParent)
	}
}

// Each spawn yields a unique envelope (different KEM ciphertext per encap).
func TestPQC_EncapsulationIsRandomised(t *testing.T) {
	keys, _ := GeneratePQCKeys()
	e1, _, _ := MakePQCBootstrapToken("a", keys)
	e2, _, _ := MakePQCBootstrapToken("a", keys)
	if e1 == e2 {
		t.Errorf("two encapsulations produced identical envelopes — KEM not randomised")
	}
}

// Signature mismatch (envelope built for a different agent_id) is
// rejected with a clear error.
func TestPQC_SignatureMismatch_AgentIDChanged(t *testing.T) {
	keys, _ := GeneratePQCKeys()
	envelope, _, _ := MakePQCBootstrapToken("agent-A", keys)
	_, err := VerifyPQCBootstrapToken(envelope, "agent-B", keys)
	if err == nil {
		t.Fatal("expected rejection when agent_id differs")
	}
	if !strings.Contains(err.Error(), "signature mismatch") {
		t.Errorf("error wording: %v", err)
	}
}

// Tampered ciphertext fails verification (Decapsulate either errors
// or returns wrong secret; ML-DSA sig still verifies but the test
// asserts at least one failure mode is hit).
func TestPQC_TamperedCiphertext(t *testing.T) {
	keys, _ := GeneratePQCKeys()
	envelope, ssOrig, _ := MakePQCBootstrapToken("agent-A", keys)
	parts := strings.SplitN(envelope, ".", 2)
	// Flip a byte in the middle of the ciphertext base64 (decode-safe
	// — replace 'A' with 'B' or vice versa).
	ct := []byte(parts[0])
	mid := len(ct) / 2
	if ct[mid] == 'A' {
		ct[mid] = 'B'
	} else {
		ct[mid] = 'A'
	}
	tampered := string(ct) + "." + parts[1]

	ss, err := VerifyPQCBootstrapToken(tampered, "agent-A", keys)
	if err == nil && bytes.Equal(ss, ssOrig) {
		t.Fatal("tampered ciphertext silently accepted with same secret")
	}
}

// Malformed envelopes (missing separator, bad base64) fail with
// recognisable errors before any KEM/sign work happens.
func TestPQC_MalformedEnvelope(t *testing.T) {
	keys, _ := GeneratePQCKeys()
	cases := map[string]string{
		"missing separator": "not-a-valid-envelope",
		"bad base64 in ct":  "not-b64.signaturepart",
		"bad base64 in sig": "Y3RwYXJ0.@@@@",
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyPQCBootstrapToken(env, "a", keys); err == nil {
				t.Errorf("expected error for %s", name)
			}
		})
	}
}

// nil keys → clear error rather than panic.
func TestPQC_NilKeysGuarded(t *testing.T) {
	if _, _, err := MakePQCBootstrapToken("a", nil); err == nil {
		t.Error("expected error for nil keys")
	}
	if _, err := VerifyPQCBootstrapToken("x.y", "a", nil); err == nil {
		t.Error("expected error for nil keys")
	}
}
