package autonomous

import (
	"encoding/json"
	"testing"
)

func newConsentAPI(t *testing.T) *API {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return NewAPI(m)
}

// TestSetConfigModelPullConsent — the autonomous API write path (used by
// PUT /api/config propagation, the autonomous_config_set MCP tool, and the
// comm config-set surface) must reject a malformed consent block before
// anything is merged: the live config keeps the consent it has, and the
// refusal is visible in the error.
func TestSetConfigModelPullConsent(t *testing.T) {
	t.Run("a valid consent merges into the live config", func(t *testing.T) {
		a := newConsentAPI(t)
		b, _ := json.Marshal(map[string]any{
			"model_pull_consent": map[string]any{
				"models":     []any{map[string]any{"name": "gpt-oss", "publisher": "openai", "allowed": true}},
				"publishers": []any{"org"},
			},
		})
		if err := a.SetConfig(json.RawMessage(b)); err != nil {
			t.Fatalf("SetConfig: %v", err)
		}
		got := a.M.Config().ModelPullConsent
		if len(got.Models) != 1 || got.Models[0].Name != "gpt-oss" || !got.Models[0].Allowed || got.Models[0].Publisher != "openai" {
			t.Fatalf("consent = %+v", got.Models)
		}
		if len(got.Publishers) != 1 || got.Publishers[0] != "org" {
			t.Fatalf("publishers = %v", got.Publishers)
		}
	})

	t.Run("a malformed consent is rejected before merge", func(t *testing.T) {
		cases := []struct {
			name string
			block any
		}{
			{"empty model name", map[string]any{"models": []any{map[string]any{"name": "  ", "allowed": true}}}},
			{"duplicate model across case and tag", map[string]any{"models": []any{
				map[string]any{"name": "gpt-oss", "allowed": true},
				map[string]any{"name": "GPT-OSS:20b", "allowed": false},
			}}},
			{"empty publisher", map[string]any{"publishers": []any{""}}},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				a := newConsentAPI(t)
				// Baseline consent holds before the attempt…
				baseline, _ := json.Marshal(map[string]any{"model_pull_consent": map[string]any{
					"models": []any{map[string]any{"name": "keepme", "allowed": true}},
				}})
				if err := a.SetConfig(json.RawMessage(baseline)); err != nil {
					t.Fatalf("baseline SetConfig: %v", err)
				}
				b, _ := json.Marshal(map[string]any{"model_pull_consent": c.block})
				err := a.SetConfig(json.RawMessage(b))
				if err == nil {
					t.Fatalf("expected a refusal for %s", c.name)
				}
				// …and the live consent is exactly what it was: a bad
				// block must not clear or partial-apply it.
				got := a.M.Config().ModelPullConsent
				if len(got.Models) != 1 || got.Models[0].Name != "keepme" || !got.Models[0].Allowed {
					t.Fatalf("consent after a refused SetConfig = %+v, want the baseline untouched", got.Models)
				}
			})
		}
	})

	t.Run("a well-formed consent still applies other fields in the same write", func(t *testing.T) {
		a := newConsentAPI(t)
		body := json.RawMessage(`{
			"max_concurrent_tasks": 7,
			"model_pull_consent": {"models":[{"name":"gpt-oss","allowed":true}]}
		}`)
		if err := a.SetConfig(body); err != nil {
			t.Fatalf("SetConfig: %v", err)
		}
		if got := a.M.Config().MaxConcurrentTasks; got != 7 {
			t.Fatalf("max_concurrent_tasks = %d, want 7", got)
		}
		got := a.M.Config().ModelPullConsent
		if len(got.Models) != 1 || got.Models[0].Name != "gpt-oss" {
			t.Fatalf("consent = %+v", got.Models)
		}
	})
}
