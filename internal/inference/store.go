// v7.0.0 S2 — JSON-backed Registry constructor + cfg shim helpers.

package inference

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	Name      string // canonical LLM registry name (e.g. "ollama")
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
		{Name: "ollama", Kind: KindOllama, Model: ollamaModel, Address: ollamaHost},
		{Name: "openwebui", Kind: KindOpenWebUI, Model: openWebUIModel, APIKeyRef: openWebUIKey, Address: openWebUIURL},
	})
}

// StripDefaultSuffix is a one-time startup migration that removes
// legacy *-default registry entries created by alpha.15 auto-migration.
//
// Two cases:
//  1. Canonical name does NOT exist → rename *-default → canonical.
//  2. Canonical name already exists → delete the *-default duplicate.
//
// Returns old→new name map (case 1) and deleted name list (case 2) for
// callers that need to update references. Idempotent.
func (r *Registry) StripDefaultSuffix() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	renames := map[string]string{}
	dirty := false
	for name := range r.llms {
		if !strings.HasSuffix(name, "-default") {
			continue
		}
		newName := strings.TrimSuffix(name, "-default")
		if _, exists := r.llms[newName]; exists {
			// Canonical already exists — delete the stale duplicate.
			delete(r.llms, name)
			dirty = true
			continue
		}
		cp := *r.llms[name]
		cp.Name = newName
		cp.UpdatedAt = time.Now().UTC()
		r.llms[newName] = &cp
		delete(r.llms, name)
		renames[name] = newName
		dirty = true
	}
	if dirty && r.persistFn != nil {
		_ = r.persistFn()
	}
	return renames
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
		// Skip if present (enabled/disabled) OR tombstoned (operator deleted).
		// Using Exists rather than Get so tombstoned entries block re-creation.
		if reg.Exists(b.Name) {
			continue
		}
		llm := &LLM{
			Name:        b.Name,
			Kind:        b.Kind,
			Model:       b.Model,
			APIKeyRef:   b.APIKeyRef,
			AutoCreated: true,
		}
		// v7.0.0 — session-backend kinds store the binary in Binary.
		// For claude-code, also set sensible v6 defaults so existing
		// operators keep working after migration: ChannelEnabled=true,
		// SkipPermissions=true match the v6 SessionConfig defaults.
		switch b.Kind {
		case KindClaudeCode:
			llm.Binary = b.Address
			llm.ChannelEnabled = true
			llm.SkipPermissions = true
			llm.DefaultEffort = "normal"
		case KindAider, KindGoose, KindGemini, KindShell:
			llm.Binary = b.Address
		case KindOpenCode, KindOpenCodeACP, KindOpenCodePrompt:
			llm.Binary = b.Address
		}
		if err := reg.Add(llm); err == nil {
			created = append(created, b.Name)
		}
	}
	return created
}

// EnsureClaudeCode guarantees that a "claude-code" entry exists in the registry.
// Unlike MigrateAllLegacyBackends, this does NOT count as a v6→v7 migration and
// does NOT trigger a migration toast — it is pure auto-detection for fresh installs.
// Idempotent: no-ops when the entry already exists.
func EnsureClaudeCode(reg *Registry, binary string) {
	if binary == "" {
		binary = "claude"
	}
	if reg.Exists("claude-code") {
		return // already present
	}
	_ = reg.Add(&LLM{
		Name:                 "claude-code",
		Kind:                 KindClaudeCode,
		Binary:               binary,
		ChannelEnabled:       true,
		SkipPermissions:      true,
		DefaultEffort:        "normal",
		AutoCreated:          true,
	})
}

// KindDefaultOutputMode returns the canonical output mode for a session-backend kind.
// Matches the per-kind logic in cfg.GetOutputMode (config.go).
func KindDefaultOutputMode(k Kind) string {
	switch k {
	case KindOpenCodeACP, KindOllama, KindOpenWebUI:
		return "chat"
	default:
		return "terminal"
	}
}

// KindDefaultInputMode returns the canonical input mode — all kinds default to tmux.
func KindDefaultInputMode(_ Kind) string { return "tmux" }

// BackfillSessionDefaults fills in empty session-backend fields on LLM registry
// entries that were created before alpha.41 (before these fields were added to the
// struct). Idempotent — skips any entry that already has OutputMode set.
// Returns the names of updated entries.
func (r *Registry) BackfillSessionDefaults() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var updated []string
	dirty := false
	for _, llm := range r.llms {
		changed := false
		if llm.OutputMode == "" {
			llm.OutputMode = KindDefaultOutputMode(llm.Kind)
			changed = true
		}
		if llm.InputMode == "" {
			llm.InputMode = KindDefaultInputMode(llm.Kind)
			changed = true
		}
		// For auto-created claude-code stubs: apply v6 defaults for each
		// field independently so a partially-backfilled entry (e.g.
		// channel_enabled=true but skip_permissions still false) still gets
		// fixed. Each condition is checked separately — no combined &&.
		if llm.Kind == KindClaudeCode && llm.AutoCreated {
			if llm.Binary == "" {
				llm.Binary = "claude"
				changed = true
			}
			if !llm.SkipPermissions {
				llm.SkipPermissions = true
				changed = true
			}
			if !llm.ChannelEnabled {
				llm.ChannelEnabled = true
				changed = true
			}
			if !llm.AutoAcceptDisclaimer {
				llm.AutoAcceptDisclaimer = true
				changed = true
			}
			if llm.DefaultEffort == "" {
				llm.DefaultEffort = "normal"
				changed = true
			}
		}
		if changed {
			llm.UpdatedAt = time.Now().UTC()
			updated = append(updated, llm.Name)
			dirty = true
		}
	}
	if dirty && r.persistFn != nil {
		_ = r.persistFn()
	}
	return updated
}
