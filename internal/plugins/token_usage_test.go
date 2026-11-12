// Token usage scanning (GET /api/plugins/{name}/tokens) — a manifest's
// ${secret:...} references: names and the fields that carry them, never
// a secret's value.

package plugins

import (
	"reflect"
	"testing"
)

func TestSecretRefs_ConnectionAuthAndEnv(t *testing.T) {
	m := Manifest{
		Name: "svc",
		Kind: KindService,
		Connection: &PluginConnection{
			BaseURL: "http://127.0.0.1:8123",
			Auth:    &PluginAuth{Type: "bearer", Token: "${secret:svc_token}"},
		},
		Env: map[string]string{
			"API_KEY": "${secret:api_key}",
			"PLAIN":   "not-a-ref",
			"MULTI":   "pre ${secret:alpha} mid ${secret:beta} post",
		},
	}
	got := SecretRefs(m)
	want := []SecretRefUsage{
		{Name: "alpha", Fields: []string{"env.MULTI"}},
		{Name: "api_key", Fields: []string{"env.API_KEY"}},
		{Name: "beta", Fields: []string{"env.MULTI"}},
		{Name: "svc_token", Fields: []string{"connection.auth.token"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SecretRefs = %+v, want %+v", got, want)
	}
}

func TestSecretRefs_SameSecretTwoFields(t *testing.T) {
	m := Manifest{
		Name: "svc",
		Connection: &PluginConnection{
			BaseURL: "http://127.0.0.1:8123",
			Auth:    &PluginAuth{Token: "${secret:shared}"},
		},
		Env: map[string]string{"API_KEY": "${secret:shared}"},
	}
	got := SecretRefs(m)
	want := []SecretRefUsage{
		{Name: "shared", Fields: []string{"connection.auth.token", "env.API_KEY"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SecretRefs = %+v, want %+v", got, want)
	}
}

func TestSecretRefs_NoSecrets(t *testing.T) {
	cases := []Manifest{
		{Name: "bare"},
		{Name: "literal",
			Connection: &PluginConnection{BaseURL: "http://127.0.0.1:8123",
				Auth: &PluginAuth{Token: "no-ref-here"}},
			Env: map[string]string{"A": "1", "B": "still literal"}},
	}
	for _, m := range cases {
		if got := SecretRefs(m); len(got) != 0 {
			t.Errorf("SecretRefs(%q) = %+v, want empty", m.Name, got)
		}
	}
}

func TestSecretRefs_NeverReturnsValues(t *testing.T) {
	m := Manifest{
		Name: "svc",
		Connection: &PluginConnection{
			BaseURL: "http://127.0.0.1:8123",
			Auth:    &PluginAuth{Token: "${secret:svc_token}"},
		},
		Env: map[string]string{"API_KEY": "${secret:api_key}"},
	}
	got := SecretRefs(m)
	for _, u := range got {
		for _, f := range u.Fields {
			if f == u.Name {
				continue
			}
			if isSecretValueLike(f) {
				t.Errorf("field %q looks like a secret value, not a dot-path", f)
			}
		}
	}
}

// isSecretValueLike rejects anything that is not a plain dot-path: values
// carry spaces, brackets or other characters dot-paths never do.
func isSecretValueLike(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '.', r == '-':
		default:
			return true
		}
	}
	return false
}
