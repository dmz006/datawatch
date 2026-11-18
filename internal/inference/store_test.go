package inference

import (
	"os"
	"path/filepath"
	"testing"
)

// store.go migrations and backfills write LLM rows outside the REST
// set/update path. The claude-code binary rule — only the canonical
// ClaudeCodeRowName row carries a binary, because the launcher runs that one
// binary for every claude-code session — has to hold in those paths too.

func TestStoreEnsureClaudeCodeUsesConfiguredBinary(t *testing.T) {
	r := NewRegistry()
	// The daemon wires the claude-code launcher from this row's binary at
	// start; EnsureClaudeCode must save the configured wrapper verbatim
	// (the canonical row is exactly the row the launcher runs).
	EnsureClaudeCode(r, "claude-lean")
	got, err := r.Get(ClaudeCodeRowName)
	if err != nil || got == nil {
		t.Fatalf("EnsureClaudeCode must create the %q row: %v", ClaudeCodeRowName, err)
	}
	if got.Binary != "claude-lean" {
		t.Fatalf("binary = %q, want %q", got.Binary, "claude-lean")
	}
	if !got.ChannelEnabled || !got.SkipPermissions {
		t.Fatalf("v6 session defaults missing: %+v", got)
	}
	// Idempotent: a second call must not overwrite the first binary.
	EnsureClaudeCode(r, "some-other-wrapper")
	got, _ = r.Get(ClaudeCodeRowName)
	if got.Binary != "claude-lean" {
		t.Fatalf("EnsureClaudeCode is not idempotent: binary = %q", got.Binary)
	}
}

func TestStoreEnsureClaudeCodeDefaultsToClaude(t *testing.T) {
	r := NewRegistry()
	EnsureClaudeCode(r, "")
	got, err := r.Get(ClaudeCodeRowName)
	if err != nil || got == nil {
		t.Fatalf("EnsureClaudeCode with empty binary must still create the row: %v", err)
	}
	if got.Binary != "claude" {
		t.Fatalf("binary = %q, want the %q default", got.Binary, "claude")
	}
}

func TestStoreMigrateLegacyKeepsCanonicalClaudeCodeBinary(t *testing.T) {
	r := NewRegistry()
	created := MigrateAllLegacyBackends(r, []LegacyBackend{
		{Name: ClaudeCodeRowName, Kind: KindClaudeCode, Model: "sonnet", Address: "claude-lean"},
		{Name: "aider", Kind: KindAider, Address: "aider-lean"},
	})
	if len(created) != 2 {
		t.Fatalf("created = %v, want both rows", created)
	}
	cl, err := r.Get(ClaudeCodeRowName)
	if err != nil || cl == nil {
		t.Fatalf("migrated %q row missing: %v", ClaudeCodeRowName, err)
	}
	// The claude-code case keeps the binary: this is the canonical row the
	// launcher is built from, so the binary is honored, not dead config.
	if cl.Binary != "claude-lean" || !cl.ChannelEnabled || !cl.SkipPermissions {
		t.Fatalf("migrated %q row = %+v", ClaudeCodeRowName, cl)
	}
	aid, err := r.Get("aider")
	if err != nil || aid == nil {
		t.Fatalf("aider row missing: %v", err)
	}
	if aid.Binary != "aider-lean" {
		t.Fatalf("aider binary = %q, want %q", aid.Binary, "aider-lean")
	}
}

func TestStoreBackfillDefaultBinaryOnlyOnCanonicalRow(t *testing.T) {
	r := NewRegistry()
	if err := r.Add(&LLM{Name: ClaudeCodeRowName, Kind: KindClaudeCode, AutoCreated: true}); err != nil {
		t.Fatalf("Add canonical stub: %v", err)
	}
	if err := r.Add(&LLM{Name: "claude-lean", Kind: KindClaudeCode, AutoCreated: true}); err != nil {
		t.Fatalf("Add named stub (no binary saves fine): %v", err)
	}
	r.BackfillSessionDefaults()

	canon, _ := r.Get(ClaudeCodeRowName)
	if canon.Binary != "claude" {
		t.Fatalf("canonical auto-created stub must get the launcher default binary, got %q", canon.Binary)
	}
	lean, _ := r.Get("claude-lean")
	if lean.Binary != "" {
		t.Fatalf("backfill must not give a named claude-code row a binary the launcher will not run, got %q", lean.Binary)
	}
	// The other session defaults still apply to every claude-code row —
	// only the binary is honored on the canonical one.
	if !lean.ChannelEnabled || !lean.SkipPermissions {
		t.Fatalf("named claude-code stub must still get the session defaults: %+v", lean)
	}
}

func TestStoreLoadKeepsExistingNamedBinary(t *testing.T) {
	// Rows saved before this rule shipped may already carry a binary on a
	// named claude-code row. Import stays lenient — the row remains
	// readable; the launcher never runs that binary, and the set/update
	// path now refuses to save one, explaining why.
	dir := t.TempDir()
	path := filepath.Join(dir, "llms.json")
	raw := `[{"name":"claude-lean","kind":"claude-code","binary":"claude-lean","channel_enabled":true}]`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	r, err := NewRegistryFromFile(path)
	if err != nil {
		t.Fatalf("NewRegistryFromFile must accept pre-existing rows: %v", err)
	}
	got, err := r.Get("claude-lean")
	if err != nil || got == nil {
		t.Fatalf("pre-existing row must stay readable: %v", err)
	}
	if got.Binary != "claude-lean" {
		t.Fatalf("imported binary = %q, want preserved %q", got.Binary, "claude-lean")
	}
}
