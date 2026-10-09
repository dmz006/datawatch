package federation

import (
	"context"
	"testing"
)

func TestHopChain_SignVerifyRoundTrip(t *testing.T) {
	entry, err := SignHop(nil, "admin", "daemon-a", 1000, "secret-ab")
	if err != nil {
		t.Fatalf("SignHop: %v", err)
	}
	chain := Chain{entry}
	if !VerifyLastHop(chain, "secret-ab") {
		t.Fatal("expected verification to succeed with the correct key")
	}
}

func TestHopChain_WrongKeyFails(t *testing.T) {
	entry, err := SignHop(nil, "admin", "daemon-a", 1000, "secret-ab")
	if err != nil {
		t.Fatalf("SignHop: %v", err)
	}
	chain := Chain{entry}
	if VerifyLastHop(chain, "wrong-secret") {
		t.Fatal("expected verification to fail with the wrong key")
	}
}

func TestHopChain_TamperDetection(t *testing.T) {
	entry, err := SignHop(nil, "admin", "daemon-a", 1000, "secret-ab")
	if err != nil {
		t.Fatalf("SignHop: %v", err)
	}
	cases := []struct {
		name  string
		mut   func(e HopEntry) HopEntry
	}{
		{"actor", func(e HopEntry) HopEntry { e.Actor = "attacker"; return e }},
		{"daemon", func(e HopEntry) HopEntry { e.Daemon = "evil-daemon"; return e }},
		{"ts", func(e HopEntry) HopEntry { e.TS = 9999; return e }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tampered := Chain{c.mut(entry)}
			if VerifyLastHop(tampered, "secret-ab") {
				t.Fatalf("tampering with %s should have invalidated the signature", c.name)
			}
		})
	}
}

func TestHopChain_EmptyChainNeverVerifies(t *testing.T) {
	if VerifyLastHop(nil, "any-key") {
		t.Fatal("an empty chain must never verify")
	}
	if VerifyLastHop(Chain{}, "any-key") {
		t.Fatal("an empty chain must never verify")
	}
}

func TestHopChain_MultiHopEachLinkVerifiableWithItsOwnKey(t *testing.T) {
	// A -> B -> C. Alice acts on daemon-a; daemon-a forwards to
	// daemon-b (shared secret ab); daemon-b forwards to daemon-c
	// (shared secret bc). Each receiving daemon verifies only the
	// entry signed with the key IT shares with the sender.
	e0, err := SignHop(nil, "Alice", "daemon-a", 1000, "secret-ab")
	if err != nil {
		t.Fatalf("hop0: %v", err)
	}
	chainAtB := Chain{e0}
	if !VerifyLastHop(chainAtB, "secret-ab") {
		t.Fatal("daemon-b should verify hop0 with secret-ab")
	}

	e1, err := SignHop(chainAtB, OriginActor(chainAtB), "daemon-b", 2000, "secret-bc")
	if err != nil {
		t.Fatalf("hop1: %v", err)
	}
	chainAtC := append(Chain{}, chainAtB...)
	chainAtC = append(chainAtC, e1)
	if !VerifyLastHop(chainAtC, "secret-bc") {
		t.Fatal("daemon-c should verify hop1 with secret-bc")
	}
	// daemon-c does NOT hold secret-ab and can't re-verify hop0 — by
	// design (see package doc). Confirm the origin actor still reads
	// back correctly across the full 2-hop chain.
	if got := OriginActor(chainAtC); got != "Alice" {
		t.Fatalf("OriginActor = %q, want Alice", got)
	}
	if len(chainAtC) != 2 {
		t.Fatalf("expected a full 2-entry hop list, got %d entries", len(chainAtC))
	}
	if chainAtC[0].Daemon != "daemon-a" || chainAtC[1].Daemon != "daemon-b" {
		t.Fatalf("hop list daemons = %q, %q — want daemon-a, daemon-b", chainAtC[0].Daemon, chainAtC[1].Daemon)
	}

	// A chain forged by daemon-b claiming a DIFFERENT origin actor,
	// re-signed correctly with secret-bc, still verifies at C — this
	// is the documented limitation, not a bug: C can only check the
	// hop it directly shares a secret with.
	forged, err := SignHop(Chain{{Actor: "NotAlice", Daemon: "daemon-a", TS: 1000, Sig: "forged"}}, "NotAlice", "daemon-b", 2000, "secret-bc")
	if err != nil {
		t.Fatalf("forged hop1: %v", err)
	}
	forgedChain := Chain{{Actor: "NotAlice", Daemon: "daemon-a", TS: 1000, Sig: "forged"}, forged}
	if !VerifyLastHop(forgedChain, "secret-bc") {
		t.Fatal("expected the documented hop-by-hop limitation: C can't catch B re-attributing a chain it already holds a valid key for")
	}
}

func TestHopChain_EncodeDecodeRoundTrip(t *testing.T) {
	entry, err := SignHop(nil, "admin", "daemon-a", 1000, "secret-ab")
	if err != nil {
		t.Fatalf("SignHop: %v", err)
	}
	chain := Chain{entry}
	enc := EncodeChain(chain)
	if enc == "" {
		t.Fatal("EncodeChain returned empty for a non-empty chain")
	}
	dec, err := DecodeChain(enc)
	if err != nil {
		t.Fatalf("DecodeChain: %v", err)
	}
	if len(dec) != 1 || dec[0] != chain[0] {
		t.Fatalf("round trip mismatch: got %+v, want %+v", dec, chain)
	}
}

func TestHopChain_EncodeEmptyChain(t *testing.T) {
	if got := EncodeChain(nil); got != "" {
		t.Fatalf("EncodeChain(nil) = %q, want empty", got)
	}
	if got := EncodeChain(Chain{}); got != "" {
		t.Fatalf("EncodeChain(Chain{}) = %q, want empty", got)
	}
}

func TestHopChain_DecodeEmptyString(t *testing.T) {
	c, err := DecodeChain("")
	if err != nil || c != nil {
		t.Fatalf("DecodeChain(\"\") = %v, %v; want nil, nil", c, err)
	}
}

func TestHopChain_DecodeGarbageReturnsError(t *testing.T) {
	if _, err := DecodeChain("not-valid-base64!!!"); err == nil {
		t.Fatal("expected a decode error for invalid base64")
	}
	if _, err := DecodeChain("aGVsbG8="); err == nil { // valid base64, not JSON
		t.Fatal("expected a decode error for non-JSON payload")
	}
}

func TestHopChain_OriginActorEmptyChain(t *testing.T) {
	if got := OriginActor(nil); got != "" {
		t.Fatalf("OriginActor(nil) = %q, want empty", got)
	}
}

func TestHopChain_BuildOrExtendChain_OriginatesWhenNoPriorChain(t *testing.T) {
	ctx := ContextWithPrincipal(context.Background(), "admin")
	chain, err := BuildOrExtendChain(ctx, "daemon-a", "secret-ab")
	if err != nil {
		t.Fatalf("BuildOrExtendChain: %v", err)
	}
	if len(chain) != 1 {
		t.Fatalf("expected a fresh 1-entry chain, got %d", len(chain))
	}
	if chain[0].Actor != "admin" || chain[0].Daemon != "daemon-a" {
		t.Fatalf("unexpected origin entry: %+v", chain[0])
	}
	if !VerifyLastHop(chain, "secret-ab") {
		t.Fatal("the originated entry must itself verify")
	}
}

func TestHopChain_BuildOrExtendChain_ExtendsPriorChain(t *testing.T) {
	e0, _ := SignHop(nil, "Alice", "daemon-a", 1000, "secret-ab")
	ctx := context.Background()
	ctx = ContextWithPrincipal(ctx, "peer:daemon-a") // the LOCAL principal (irrelevant once a prior chain exists)
	ctx = ContextWithChain(ctx, Chain{e0})
	chain, err := BuildOrExtendChain(ctx, "daemon-b", "secret-bc")
	if err != nil {
		t.Fatalf("BuildOrExtendChain: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("expected the chain to grow to 2 entries, got %d", len(chain))
	}
	// The origin actor (Alice) must be carried forward, NOT overwritten
	// by this hop's own local principal (peer:daemon-a).
	if chain[1].Actor != "Alice" {
		t.Fatalf("expected origin actor Alice to be carried forward, got %q", chain[1].Actor)
	}
	if chain[1].Daemon != "daemon-b" {
		t.Fatalf("expected the new entry's Daemon to be daemon-b, got %q", chain[1].Daemon)
	}
	if !VerifyLastHop(chain, "secret-bc") {
		t.Fatal("the extended entry must verify with the new hop's key")
	}
}

func TestHopChain_BuildOrExtendChain_NoKeyMeansNoHeader(t *testing.T) {
	ctx := ContextWithPrincipal(context.Background(), "admin")
	chain, err := BuildOrExtendChain(ctx, "daemon-a", "")
	if err != nil {
		t.Fatalf("BuildOrExtendChain: %v", err)
	}
	if chain != nil {
		t.Fatalf("expected nil chain when no key is available, got %+v", chain)
	}
}

func TestHopChain_BuildOrExtendChain_NoPrincipalMeansNoHeader(t *testing.T) {
	chain, err := BuildOrExtendChain(context.Background(), "daemon-a", "secret-ab")
	if err != nil {
		t.Fatalf("BuildOrExtendChain: %v", err)
	}
	if chain != nil {
		t.Fatalf("expected nil chain when no principal is resolvable, got %+v", chain)
	}
}

func TestHopChain_ChainFromContext_DefaultsToNil(t *testing.T) {
	if got := ChainFromContext(context.Background()); got != nil {
		t.Fatalf("ChainFromContext on a bare context = %+v, want nil", got)
	}
}

func TestHopChain_PrincipalFromContext_DefaultsToEmpty(t *testing.T) {
	if got := PrincipalFromContext(context.Background()); got != "" {
		t.Fatalf("PrincipalFromContext on a bare context = %q, want empty", got)
	}
}
