package inference

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/secrets"
)

// fakeSecretStore implements secrets.Store. Get returns the fixed value
// for known names (default: "known" -> "SUPERSECRETVALUE123") and
// ErrSecretNotFound otherwise.
type fakeSecretStore struct {
	secrets map[string]string
}

func (f *fakeSecretStore) Get(name string) (secrets.Secret, error) {
	v, ok := f.secrets[name]
	if !ok {
		return secrets.Secret{}, secrets.ErrSecretNotFound
	}
	return secrets.Secret{Name: name, Value: v}, nil
}

func (f *fakeSecretStore) List() ([]secrets.Secret, error) {
	out := make([]secrets.Secret, 0, len(f.secrets))
	for n, v := range f.secrets {
		out = append(out, secrets.Secret{Name: n, Value: v})
	}
	return out, nil
}

func (f *fakeSecretStore) Set(name, value string, tags []string, description string, scopes []string) error {
	if f.secrets == nil {
		f.secrets = map[string]string{}
	}
	f.secrets[name] = value
	return nil
}

func (f *fakeSecretStore) Delete(name string) error {
	if _, ok := f.secrets[name]; !ok {
		return secrets.ErrSecretNotFound
	}
	delete(f.secrets, name)
	return nil
}

func (f *fakeSecretStore) Exists(name string) (bool, error) {
	_, ok := f.secrets[name]
	return ok, nil
}

func newFakeStore() *fakeSecretStore {
	return &fakeSecretStore{secrets: map[string]string{"known": "SUPERSECRETVALUE123"}}
}

// assertSecretValueNotIn verifies the error never leaks the secret value
// or any literal key.
func assertSecretValueNotIn(t *testing.T, err error, forbidden ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	for _, bad := range forbidden {
		if strings.Contains(err.Error(), bad) {
			t.Errorf("error %q must not contain %q", err.Error(), bad)
		}
	}
}

func TestAPIKeyEmptyRef(t *testing.T) {
	cases := []struct {
		name  string
		store secrets.Store
	}{
		{"nil store", nil},
		{"with store", newFakeStore()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveAPIKey(&LLM{Name: "claude-entry"}, tc.store)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != "" {
				t.Fatalf("expected empty key, got %q", got)
			}
		})
	}
}

func TestAPIKeyLiteral(t *testing.T) {
	const literal = "sk-live-plain-123"
	store := newFakeStore()
	got, err := ResolveAPIKey(&LLM{Name: "legacy", APIKeyRef: literal}, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != literal {
		t.Fatalf("literal path must return the key unchanged, got %q", got)
	}
}

func TestAPIKeyLiteralNoStore(t *testing.T) {
	const literal = "sk-plain"
	got, err := ResolveAPIKey(&LLM{Name: "legacy", APIKeyRef: literal}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != literal {
		t.Fatalf("literal path must return the key unchanged, got %q", got)
	}
}

func TestAPIKeyResolved(t *testing.T) {
	store := newFakeStore()
	got, err := ResolveAPIKey(&LLM{Name: "claude", APIKeyRef: "${secret:known}"}, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "SUPERSECRETVALUE123" {
		t.Fatalf("expected resolved secret value, got %q", got)
	}
}

func TestAPIKeyMalformed(t *testing.T) {
	store := newFakeStore()
	cases := []struct {
		name string
		ref  string
	}{
		{"surrounding text", "prefix-${secret:known}-suffix"},
		{"trailing text", "${secret:known}tail"},
		{"leading text", "x${secret:known}"},
		{"two references", "${secret:known}${secret:known}"},
		{"two distinct references", "${secret:a}${secret:b}"},
		{"inner text between delimiters", "${secret:in valide}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveAPIKey(&LLM{Name: "claude", APIKeyRef: tc.ref}, store)
			if err == nil {
				t.Fatalf("expected error, got key %q", got)
			}
			secretValue := "SUPERSECRETVALUE123"
			assertSecretValueNotIn(t, err, secretValue)
		})
	}
}

func TestAPIKeyStoreErrors(t *testing.T) {
	const secretValue = "SUPERSECRETVALUE123"
	cases := []struct {
		name  string
		store secrets.Store
	}{
		{"nil store", nil},
		{"secret missing", &fakeSecretStore{secrets: map[string]string{"something-else": secretValue}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveAPIKey(&LLM{Name: "claude", APIKeyRef: "${secret:known}"}, tc.store)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			// The error must name the secret and the LLM entry.
			if !strings.Contains(err.Error(), `"known"`) {
				t.Errorf("error should name the secret: %q", err.Error())
			}
			if !strings.Contains(err.Error(), `"claude"`) {
				t.Errorf("error should name the LLM entry: %q", err.Error())
			}
			assertSecretValueNotIn(t, err, secretValue)
		})
	}
}

func TestAPIKeyRefInResolvedValue(t *testing.T) {
	// A secret whose value itself contains a ${secret: token must be
	// refused, without echoing the value in the error.
	nested := "SUPERSECRETVALUE123"
	store := &fakeSecretStore{secrets: map[string]string{"known": nested + "${secret:inner}"}}
	_, err := ResolveAPIKey(&LLM{Name: "claude", APIKeyRef: "${secret:known}"}, store)
	if err == nil {
		t.Fatal("expected error for ref in resolved value, got nil")
	}
	assertSecretValueNotIn(t, err, nested, "${secret:inner}")
}

func TestAPIKeyEmptySecretValue(t *testing.T) {
	store := &fakeSecretStore{secrets: map[string]string{"known": ""}}
	_, err := ResolveAPIKey(&LLM{Name: "claude", APIKeyRef: "${secret:known}"}, store)
	if err == nil {
		t.Fatal("expected error for empty secret value, got nil")
	}
	assertSecretValueNotIn(t, err)
}
