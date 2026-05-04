package secrets

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"testing"
)

// opAvailable skips the test when the op CLI is not installed.
func opAvailable(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("op")
	if err != nil {
		t.Skip("op (1Password CLI) not in PATH — skipping 1Password integration tests")
	}
	return p
}

func TestOnePasswordStore_ParseItem(t *testing.T) {
	raw := []byte(`{
		"title": "api-key",
		"tags": ["prod", "cloud"],
		"created_at": "2024-03-15T10:00:00Z",
		"updated_at": "2024-03-16T12:00:00Z",
		"fields": [
			{"label": "username", "value": ""},
			{"label": "password", "value": "s3cr3t"},
			{"label": "notesPlain", "value": "Production API key"}
		]
	}`)

	var it opItem
	if err := json.Unmarshal(raw, &it); err != nil {
		t.Fatalf("parse: %v", err)
	}

	sec := Secret{Name: it.Title, Tags: it.Tags, Backend: "onepassword",
		CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt}
	for _, f := range it.Fields {
		switch f.Label {
		case "password":
			sec.Value = f.Value
		case "notesPlain":
			sec.Description = f.Value
		}
	}

	if sec.Value != "s3cr3t" {
		t.Errorf("value: want s3cr3t, got %q", sec.Value)
	}
	if sec.Description != "Production API key" {
		t.Errorf("description: want %q, got %q", "Production API key", sec.Description)
	}
	if len(sec.Tags) != 2 || sec.Tags[0] != "prod" || sec.Tags[1] != "cloud" {
		t.Errorf("tags: want [prod cloud], got %v", sec.Tags)
	}
	if sec.CreatedAt.IsZero() || sec.UpdatedAt.IsZero() {
		t.Error("timestamps should be parsed from RFC3339")
	}
}

func TestOnePasswordStore_isOPNotFound(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{`op item get: exit status 1 — "my-token" isn't an item in the vault`, true},
		{"op item get: exit status 1 — could not find item", true},
		{"op item get: exit status 1 — no item named x", true},
		{"op item get: exit status 1 — does not exist", true},
		{"op item get: exit status 1 — authentication failed", false},
		{"op item get: exit status 1 — network error", false},
	}
	for _, tc := range cases {
		err := fmt.Errorf("%s", tc.msg)
		got := isOPNotFound(err)
		if got != tc.want {
			t.Errorf("isOPNotFound(%q): want %v, got %v", tc.msg, tc.want, got)
		}
	}
}

func TestOnePasswordStore_CRUD(t *testing.T) {
	binary := opAvailable(t)
	// integration test: requires OP_SERVICE_ACCOUNT_TOKEN in the environment
	store, err := NewOnePasswordStore(binary, "datawatch-test", "")
	if err != nil {
		t.Fatalf("NewOnePasswordStore: %v", err)
	}

	const name = "datawatch-test-secret-xyz"
	_ = store.Delete(name) // clean prior run

	if err := store.Set(name, "testvalue", []string{"test"}, "Test secret", nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(name) })

	ok, err := store.Exists(name)
	if err != nil || !ok {
		t.Fatalf("Exists: want true, got %v (%v)", ok, err)
	}

	sec, err := store.Get(name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sec.Value != "testvalue" {
		t.Errorf("value: want testvalue, got %q", sec.Value)
	}
	if sec.Backend != "onepassword" {
		t.Errorf("backend: want onepassword, got %q", sec.Backend)
	}

	if err := store.Set(name, "newvalue", nil, "updated", nil); err != nil {
		t.Fatalf("Set update: %v", err)
	}
	sec2, _ := store.Get(name)
	if sec2.Value != "newvalue" {
		t.Errorf("updated value: want newvalue, got %q", sec2.Value)
	}

	if err := store.Delete(name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(name); err != ErrSecretNotFound {
		t.Fatalf("Get after delete: want ErrSecretNotFound, got %v", err)
	}
	if err := store.Delete("gone-xyz-nonexistent"); err != ErrSecretNotFound {
		t.Fatalf("Delete missing: want ErrSecretNotFound, got %v", err)
	}
}
