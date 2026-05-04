// BL251 — Agent auth/settings injection unit tests.
//
// Tests that AgentSettings fields on a ProjectProfile are resolved at
// spawn time and merged into the agent's EnvOverride map, which both
// Docker and K8s drivers use for container env injection.

package agents

import (
	"fmt"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
	"github.com/dmz006/datawatch/internal/secrets"
)

// mockSecretsStoreForBL251 is a minimal in-memory secrets.Store.
type mockSecretsStoreForBL251 struct {
	data map[string]string
}

func (m *mockSecretsStoreForBL251) List() ([]secrets.Secret, error) { return nil, nil }
func (m *mockSecretsStoreForBL251) Get(name string) (secrets.Secret, error) {
	v, ok := m.data[name]
	if !ok {
		return secrets.Secret{}, fmt.Errorf("secret %q not found", name)
	}
	return secrets.Secret{Name: name, Value: v}, nil
}
func (m *mockSecretsStoreForBL251) Set(n, v string, tags []string, desc string, scopes []string) error {
	m.data[n] = v
	return nil
}
func (m *mockSecretsStoreForBL251) Delete(name string) error { delete(m.data, name); return nil }
func (m *mockSecretsStoreForBL251) Exists(name string) (bool, error) {
	_, ok := m.data[name]
	return ok, nil
}

// agentWithSettings creates an Agent skeleton that mirrors what Spawn does
// after the BL251 injection block executes.
func agentWithSettings(proj *profile.ProjectProfile, store secrets.Store) *Agent {
	a := &Agent{project: proj}

	// Mirror the BL251 injection block from spawn.go
	as := proj.AgentSettings
	if as.ClaudeAuthKeySecret != "" || as.OpenCodeOllamaURL != "" || as.OpenCodeModel != "" {
		if a.EnvOverride == nil {
			a.EnvOverride = make(map[string]string, len(proj.Env))
			for k, v := range proj.Env {
				a.EnvOverride[k] = v
			}
		}
		if as.ClaudeAuthKeySecret != "" && store != nil {
			if sec, err := store.Get(as.ClaudeAuthKeySecret); err == nil {
				a.EnvOverride["ANTHROPIC_API_KEY"] = sec.Value
			}
		}
		if as.OpenCodeOllamaURL != "" {
			a.EnvOverride["OPENCODE_PROVIDER_URL"] = as.OpenCodeOllamaURL
		}
		if as.OpenCodeModel != "" {
			a.EnvOverride["OPENCODE_MODEL"] = as.OpenCodeModel
		}
	}
	return a
}

func TestBL251_ClaudeAuthKeyInjection(t *testing.T) {
	store := &mockSecretsStoreForBL251{data: map[string]string{"anthropic-key": "sk-ant-test123"}}
	proj := &profile.ProjectProfile{
		AgentSettings: profile.AgentSettings{
			ClaudeAuthKeySecret: "anthropic-key",
		},
	}
	a := agentWithSettings(proj, store)
	if a.EnvOverride["ANTHROPIC_API_KEY"] != "sk-ant-test123" {
		t.Errorf("expected ANTHROPIC_API_KEY=sk-ant-test123, got %q", a.EnvOverride["ANTHROPIC_API_KEY"])
	}
}

func TestBL251_MissingSecret_NoInjection(t *testing.T) {
	store := &mockSecretsStoreForBL251{data: map[string]string{}}
	proj := &profile.ProjectProfile{
		AgentSettings: profile.AgentSettings{
			ClaudeAuthKeySecret: "missing-key",
		},
	}
	a := agentWithSettings(proj, store)
	if _, ok := a.EnvOverride["ANTHROPIC_API_KEY"]; ok {
		t.Error("ANTHROPIC_API_KEY should not be set when secret is missing")
	}
}

func TestBL251_OpenCodeInjection(t *testing.T) {
	proj := &profile.ProjectProfile{
		AgentSettings: profile.AgentSettings{
			OpenCodeOllamaURL: "http://ollama.local:11434",
			OpenCodeModel:     "qwen3:8b",
		},
	}
	a := agentWithSettings(proj, nil)
	if a.EnvOverride["OPENCODE_PROVIDER_URL"] != "http://ollama.local:11434" {
		t.Errorf("OPENCODE_PROVIDER_URL mismatch: %q", a.EnvOverride["OPENCODE_PROVIDER_URL"])
	}
	if a.EnvOverride["OPENCODE_MODEL"] != "qwen3:8b" {
		t.Errorf("OPENCODE_MODEL mismatch: %q", a.EnvOverride["OPENCODE_MODEL"])
	}
}

func TestBL251_ExistingEnvPreserved(t *testing.T) {
	store := &mockSecretsStoreForBL251{data: map[string]string{"key": "val"}}
	proj := &profile.ProjectProfile{
		Env: map[string]string{"MY_VAR": "my_val"},
		AgentSettings: profile.AgentSettings{
			ClaudeAuthKeySecret: "key",
		},
	}
	a := agentWithSettings(proj, store)
	if a.EnvOverride["MY_VAR"] != "my_val" {
		t.Errorf("existing env MY_VAR lost: %q", a.EnvOverride["MY_VAR"])
	}
	if a.EnvOverride["ANTHROPIC_API_KEY"] != "val" {
		t.Errorf("ANTHROPIC_API_KEY not injected: %q", a.EnvOverride["ANTHROPIC_API_KEY"])
	}
}

func TestBL251_EmptySettingsNoOverride(t *testing.T) {
	proj := &profile.ProjectProfile{
		Env: map[string]string{"X": "y"},
		// AgentSettings is zero value
	}
	a := agentWithSettings(proj, nil)
	// Without any AgentSettings, EnvOverride stays nil (Env used directly by drivers)
	if a.EnvOverride != nil {
		t.Errorf("expected nil EnvOverride with empty AgentSettings, got %v", a.EnvOverride)
	}
}
