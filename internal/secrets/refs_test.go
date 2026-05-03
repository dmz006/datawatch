package secrets

import (
	"errors"
	"testing"
)

// mockStore is a minimal Store for testing ref resolution.
type mockStore struct {
	data map[string]string
}

func (m *mockStore) List() ([]Secret, error) { return nil, nil }
func (m *mockStore) Get(name string) (Secret, error) {
	v, ok := m.data[name]
	if !ok {
		return Secret{}, ErrSecretNotFound
	}
	return Secret{Name: name, Value: v, Backend: "mock"}, nil
}
func (m *mockStore) Set(name, value string, tags []string, desc string) error { return nil }
func (m *mockStore) Delete(name string) error                                  { return nil }
func (m *mockStore) Exists(name string) (bool, error) {
	_, ok := m.data[name]
	return ok, nil
}

func newMock(kv ...string) *mockStore {
	m := &mockStore{data: make(map[string]string)}
	for i := 0; i < len(kv)-1; i += 2 {
		m.data[kv[i]] = kv[i+1]
	}
	return m
}

func TestResolveRef_NoRef(t *testing.T) {
	s, err := ResolveRef("plain string", newMock())
	if err != nil || s != "plain string" {
		t.Fatalf("want plain string, got %q (%v)", s, err)
	}
}

func TestResolveRef_SingleRef(t *testing.T) {
	store := newMock("api-key", "tok123")
	s, err := ResolveRef("Bearer ${secret:api-key}", store)
	if err != nil {
		t.Fatal(err)
	}
	if s != "Bearer tok123" {
		t.Fatalf("want %q, got %q", "Bearer tok123", s)
	}
}

func TestResolveRef_MultipleRefs(t *testing.T) {
	store := newMock("user", "alice", "pass", "hunter2")
	s, err := ResolveRef("${secret:user}:${secret:pass}", store)
	if err != nil {
		t.Fatal(err)
	}
	if s != "alice:hunter2" {
		t.Fatalf("want alice:hunter2, got %q", s)
	}
}

func TestResolveRef_Missing(t *testing.T) {
	_, err := ResolveRef("${secret:gone}", newMock())
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("want ErrSecretNotFound, got %v", err)
	}
}

func TestResolveMap(t *testing.T) {
	store := newMock("gh-token", "ghp_abc")
	m := map[string]string{
		"GH_TOKEN":  "${secret:gh-token}",
		"PLAIN_VAR": "no-ref",
	}
	out, err := ResolveMapRefs(m, store)
	if err != nil {
		t.Fatal(err)
	}
	if out["GH_TOKEN"] != "ghp_abc" {
		t.Errorf("GH_TOKEN: want ghp_abc, got %q", out["GH_TOKEN"])
	}
	if out["PLAIN_VAR"] != "no-ref" {
		t.Errorf("PLAIN_VAR: want no-ref, got %q", out["PLAIN_VAR"])
	}
	// Original map not mutated
	if m["GH_TOKEN"] != "${secret:gh-token}" {
		t.Error("ResolveMap must not mutate original map")
	}
}

func TestResolveMap_PartialError(t *testing.T) {
	store := newMock("exists", "val")
	m := map[string]string{
		"A": "${secret:exists}",
		"B": "${secret:missing}",
	}
	out, err := ResolveMapRefs(m, store)
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
	if out["A"] != "val" {
		t.Errorf("A should still resolve, got %q", out["A"])
	}
	// B keeps original on error
	if out["B"] != "${secret:missing}" {
		t.Errorf("B should keep original token on error, got %q", out["B"])
	}
}

func TestResolveConfig_StringField(t *testing.T) {
	store := newMock("token", "secret123")
	cfg := &struct {
		Token string
		Num   int
	}{Token: "${secret:token}", Num: 42}

	if err := ResolveConfig(cfg, store); err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "secret123" {
		t.Errorf("Token: want secret123, got %q", cfg.Token)
	}
	if cfg.Num != 42 {
		t.Error("non-string fields must be untouched")
	}
}

func TestResolveConfig_NestedStruct(t *testing.T) {
	store := newMock("discord-token", "Bot abc123")
	cfg := &struct {
		Discord struct {
			Token string
		}
	}{}
	cfg.Discord.Token = "${secret:discord-token}"

	if err := ResolveConfig(cfg, store); err != nil {
		t.Fatal(err)
	}
	if cfg.Discord.Token != "Bot abc123" {
		t.Errorf("nested: want %q, got %q", "Bot abc123", cfg.Discord.Token)
	}
}

func TestResolveConfig_MapStringString(t *testing.T) {
	store := newMock("api-key", "key999")
	cfg := &struct {
		Env map[string]string
	}{Env: map[string]string{"API_KEY": "${secret:api-key}", "OTHER": "plain"}}

	if err := ResolveConfig(cfg, store); err != nil {
		t.Fatal(err)
	}
	if cfg.Env["API_KEY"] != "key999" {
		t.Errorf("map: want key999, got %q", cfg.Env["API_KEY"])
	}
	if cfg.Env["OTHER"] != "plain" {
		t.Error("map: plain value mutated")
	}
}

func TestResolveConfig_SliceOfStructs(t *testing.T) {
	store := newMock("s1", "v1", "s2", "v2")
	type item struct{ Val string }
	cfg := &struct{ Items []item }{
		Items: []item{{Val: "${secret:s1}"}, {Val: "${secret:s2}"}},
	}
	if err := ResolveConfig(cfg, store); err != nil {
		t.Fatal(err)
	}
	if cfg.Items[0].Val != "v1" || cfg.Items[1].Val != "v2" {
		t.Errorf("slice: got %v", cfg.Items)
	}
}

func TestResolveConfig_NoRef(t *testing.T) {
	store := newMock()
	cfg := &struct{ X string }{X: "no refs here"}
	if err := ResolveConfig(cfg, store); err != nil {
		t.Fatal(err)
	}
	if cfg.X != "no refs here" {
		t.Error("no-ref string was mutated")
	}
}
