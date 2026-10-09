// GH#203 — external-service token store.

package secrets

import (
	"path/filepath"
	"testing"
)

func TestServiceTokenStore_MintLookupRevoke(t *testing.T) {
	dir := t.TempDir()
	s, err := NewServiceTokenStore(filepath.Join(dir, "service_tokens.json"))
	if err != nil {
		t.Fatal(err)
	}

	tok, err := s.Mint("imap-mcp", "imap-mcp secret resolver")
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if len(tok) != 64 { // 32 random bytes, hex-encoded
		t.Errorf("token length = %d, want 64 (32 hex-encoded bytes)", len(tok))
	}

	name, ok := s.Lookup(tok)
	if !ok || name != "imap-mcp" {
		t.Fatalf("Lookup(%q) = (%q, %v), want (imap-mcp, true)", tok, name, ok)
	}

	if _, ok := s.Lookup("not-a-real-token"); ok {
		t.Error("Lookup of an unknown token should return ok=false")
	}

	list := s.List()
	if len(list) != 1 || list[0].Name != "imap-mcp" {
		t.Fatalf("List() = %+v, want 1 entry named imap-mcp", list)
	}
	if list[0].Token != "" {
		t.Error("List() must never include the token value")
	}

	if err := s.Revoke("imap-mcp"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, ok := s.Lookup(tok); ok {
		t.Error("token should no longer resolve after Revoke")
	}

	// Revoking a never-provisioned name is a no-op, not an error.
	if err := s.Revoke("never-existed"); err != nil {
		t.Errorf("Revoke of unknown name should be a no-op, got error: %v", err)
	}
}

func TestServiceTokenStore_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "service_tokens.json")

	s1, err := NewServiceTokenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := s1.Mint("imap-mcp", "")
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a daemon restart: open a fresh store at the same path.
	s2, err := NewServiceTokenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	name, ok := s2.Lookup(tok)
	if !ok || name != "imap-mcp" {
		t.Fatalf("after reopen, Lookup(%q) = (%q, %v), want (imap-mcp, true) — token did not survive a restart", tok, name, ok)
	}
}

func TestServiceTokenStore_MintTwiceReplacesToken(t *testing.T) {
	dir := t.TempDir()
	s, err := NewServiceTokenStore(filepath.Join(dir, "service_tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	tok1, _ := s.Mint("imap-mcp", "")
	tok2, _ := s.Mint("imap-mcp", "")
	if tok1 == tok2 {
		t.Error("re-minting the same name should produce a different token")
	}
	if _, ok := s.Lookup(tok1); ok {
		t.Error("the old token should no longer resolve after re-minting")
	}
	if name, ok := s.Lookup(tok2); !ok || name != "imap-mcp" {
		t.Error("the new token should resolve")
	}
}

func TestServiceTokenStore_CheckScopeIntegration(t *testing.T) {
	// GH#203's whole point: a secret scoped "service:imap-mcp" is
	// readable by that service's CallerCtx and no other.
	sec := Secret{Name: "imap_mcp_token_datawatch", Value: "x", Scopes: []string{"service:imap-mcp"}}
	if err := CheckScope(sec, CallerCtx{Type: "service", Name: "imap-mcp"}); err != nil {
		t.Errorf("expected imap-mcp to pass scope check, got %v", err)
	}
	if err := CheckScope(sec, CallerCtx{Type: "service", Name: "some-other-service"}); err == nil {
		t.Error("expected a different service to be denied")
	}
	if err := CheckScope(sec, CallerCtx{Type: "agent", Name: "imap-mcp"}); err == nil {
		t.Error("expected an agent caller (wrong Type) to be denied even with a matching name")
	}
}
