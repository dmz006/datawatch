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

// MigrateLegacyConfig auto-derives LLM entries from v6.x cfg fields
// at daemon startup when no JSON entries exist for a given conventional
// name. Implements BL295 ASK 26 (operator-confirmed automated
// migration; writes derived entries that the operator reviews).
//
// Conventions:
//   cfg.ollama.host  → llm "ollama-default"   (kind=ollama)
//   cfg.openwebui.url → llm "openwebui-default" (kind=openwebui)
// In v7.x the cfg shim continues to accept legacy fields; in v8.0 it
// becomes hard-required.
//
// Returns the names of LLMs created (for log emission).
func MigrateLegacyConfig(reg *Registry, ollamaHost, ollamaModel, openWebUIURL, openWebUIModel, openWebUIKey string) []string {
	created := []string{}
	if ollamaHost != "" {
		if _, err := reg.Get("ollama-default"); err == ErrNotFound {
			llm := &LLM{
				Name:        "ollama-default",
				Kind:        KindOllama,
				Model:       ollamaModel,
				AutoCreated: true,
				// ComputeNodes intentionally empty — the v6.x shim
				// behaviour is preserved by adapter fallback to the
				// raw cfg.ollama.host. S1 ComputeNode entry for the
				// same host would be linked manually by the operator.
			}
			if err := reg.Add(llm); err == nil {
				created = append(created, "ollama-default")
			}
		}
	}
	if openWebUIURL != "" {
		if _, err := reg.Get("openwebui-default"); err == ErrNotFound {
			llm := &LLM{
				Name:        "openwebui-default",
				Kind:        KindOpenWebUI,
				Model:       openWebUIModel,
				APIKeyRef:   openWebUIKey,
				AutoCreated: true,
			}
			if err := reg.Add(llm); err == nil {
				created = append(created, "openwebui-default")
			}
		}
	}
	return created
}
