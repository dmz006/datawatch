// v7.0.0 S2 — JSON-backed Registry constructor + cfg shim helpers.

package inference

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// NewRegistryFromFile opens / creates the JSON-backed registry at
// the given path. <data-dir>/inference/llms.json is the convention.
func NewRegistryFromFile(path string) (*Registry, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("llm registry: mkdir: %w", err)
	}
	r := NewRegistry()
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		var stored []LLM
		if err := json.Unmarshal(b, &stored); err != nil {
			return nil, fmt.Errorf("llm registry: parse: %w", err)
		}
		r.LoadSnapshot(stored)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("llm registry: read: %w", err)
	}
	r.SetPersistFn(func() error {
		// Caller already holds r.mu (write lock); use the locked
		// snapshot helper to avoid deadlock on RLock.
		out := r.snapshotLocked()
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return fmt.Errorf("llm registry: marshal: %w", err)
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return fmt.Errorf("llm registry: write: %w", err)
		}
		return os.Rename(tmp, path)
	})
	return r, nil
}

// LegacyBackend is one v6 backend block + the canonical LLM name we
// migrate it to. Used by MigrateLegacyConfig (alpha.3) and the
// expanded MigrateAllLegacyBackends (alpha.15).
type LegacyBackend struct {
	Name      string // canonical LLM registry name (e.g. "ollama-default")
	Kind      Kind
	Model     string
	APIKeyRef string
	// Address is the binary path or URL; surfaced via LLM struct's
	// optional Address field (v6 backends used this to find the binary).
	Address string
}

// MigrateLegacyConfig (alpha.3) — restricted to ollama + openwebui.
// Kept for backward compat with alpha.3 callers.
func MigrateLegacyConfig(reg *Registry, ollamaHost, ollamaModel, openWebUIURL, openWebUIModel, openWebUIKey string) []string {
	return MigrateAllLegacyBackends(reg, []LegacyBackend{
		{Name: "ollama-default", Kind: KindOllama, Model: ollamaModel, Address: ollamaHost},
		{Name: "openwebui-default", Kind: KindOpenWebUI, Model: openWebUIModel, APIKeyRef: openWebUIKey, Address: openWebUIURL},
	})
}

// MigrateAllLegacyBackends (v7.0.0-alpha.15 #229) — extended migration
// covering every v6 cfg.<Backend> block. Operator-spec'd 2026-05-09:
// "promote all differences to new configuration, the old version can
// be deprecating. on-start migration and 'just-work'".
//
// Each backend with non-empty address (URL or binary path) gets a
// matching LLM registry entry on first v7 startup. Idempotent: skips
// any name that already exists in the registry. Returns the names
// created (for log emission and the migration toast surfaced by the
// daemon to the PWA on next load).
//
// Caller (cmd/datawatch/main.go) builds the LegacyBackend list from
// the populated cfg.<Backend> blocks.
func MigrateAllLegacyBackends(reg *Registry, backends []LegacyBackend) []string {
	created := []string{}
	for _, b := range backends {
		if b.Address == "" || b.Name == "" || b.Kind == "" {
			continue
		}
		if _, err := reg.Get(b.Name); err != ErrNotFound {
			// Already exists; skip.
			continue
		}
		llm := &LLM{
			Name:        b.Name,
			Kind:        b.Kind,
			Model:       b.Model,
			APIKeyRef:   b.APIKeyRef,
			AutoCreated: true,
		}
		if err := reg.Add(llm); err == nil {
			created = append(created, b.Name)
		}
	}
	return created
}
