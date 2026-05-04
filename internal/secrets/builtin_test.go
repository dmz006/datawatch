package secrets

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestBuiltinStore_CRUD(t *testing.T) {
	dir := t.TempDir()
	bs, err := NewBuiltinStore(dir)
	if err != nil {
		t.Fatalf("NewBuiltinStore: %v", err)
	}

	// Keyfile created, 32 bytes
	key, _ := os.ReadFile(dir + "/secrets.key")
	if len(key) != 32 {
		t.Fatalf("keyfile: want 32 bytes, got %d", len(key))
	}

	// Empty list
	list, err := bs.List()
	if err != nil {
		t.Fatalf("List (empty): %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List (empty): want 0, got %d", len(list))
	}

	// Set
	if err := bs.Set("token", "supersecret", []string{"git", "cloud"}, "GitHub PAT", nil); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Exists
	ok, err := bs.Exists("token")
	if err != nil || !ok {
		t.Fatalf("Exists: want true, got %v (%v)", ok, err)
	}
	ok, _ = bs.Exists("missing")
	if ok {
		t.Fatal("Exists(missing): want false")
	}

	// Get — value present
	sec, err := bs.Get("token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sec.Value != "supersecret" {
		t.Fatalf("Get value: want supersecret, got %q", sec.Value)
	}
	if sec.Backend != "builtin" {
		t.Fatalf("Get backend: want builtin, got %q", sec.Backend)
	}
	if len(sec.Tags) != 2 {
		t.Fatalf("Get tags: want 2, got %d", len(sec.Tags))
	}
	if sec.CreatedAt.IsZero() || sec.UpdatedAt.IsZero() {
		t.Fatal("timestamps not set")
	}

	// List — value omitted
	list, _ = bs.List()
	if len(list) != 1 {
		t.Fatalf("List: want 1, got %d", len(list))
	}
	if list[0].Value != "" {
		t.Fatal("List must not expose value")
	}
	if list[0].Name != "token" {
		t.Fatalf("List name: want token, got %q", list[0].Name)
	}

	// Update preserves created_at
	createdAt := sec.CreatedAt
	time.Sleep(2 * time.Millisecond)
	if err := bs.Set("token", "newsecret", nil, "updated", nil); err != nil {
		t.Fatalf("Set update: %v", err)
	}
	sec2, _ := bs.Get("token")
	if !sec2.CreatedAt.Equal(createdAt) {
		t.Fatalf("update changed created_at: want %v, got %v", createdAt, sec2.CreatedAt)
	}
	if sec2.Value != "newsecret" {
		t.Fatalf("update value: want newsecret, got %q", sec2.Value)
	}

	// Delete
	if err := bs.Delete("token"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := bs.Get("token"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get after delete: want ErrSecretNotFound, got %v", err)
	}

	// Delete missing
	if err := bs.Delete("gone"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Delete missing: want ErrSecretNotFound, got %v", err)
	}
}

func TestBuiltinStore_EnvKeyOverride(t *testing.T) {
	dir := t.TempDir()
	bs, _ := NewBuiltinStore(dir)
	_ = bs.Set("k", "v", nil, "", nil)

	// Wrong env key should fail to decrypt
	t.Setenv("DATAWATCH_SECRETS_KEY", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") // 32 'a's but wrong key
	_, err := bs.Get("k")
	if err == nil {
		t.Fatal("expected decrypt error with wrong env key")
	}
}

func TestBuiltinStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	bs1, _ := NewBuiltinStore(dir)
	_ = bs1.Set("persist", "value123", []string{"tag1"}, "persisted", nil)

	bs2, _ := NewBuiltinStore(dir)
	sec, err := bs2.Get("persist")
	if err != nil {
		t.Fatalf("Get after re-open: %v", err)
	}
	if sec.Value != "value123" {
		t.Fatalf("persisted value: want value123, got %q", sec.Value)
	}
}

func TestBuiltinStore_ListSorted(t *testing.T) {
	dir := t.TempDir()
	bs, _ := NewBuiltinStore(dir)
	for _, name := range []string{"z", "a", "m"} {
		_ = bs.Set(name, "v", nil, "", nil)
	}
	list, _ := bs.List()
	names := make([]string, len(list))
	for i, s := range list {
		names[i] = s.Name
	}
	if names[0] != "a" || names[1] != "m" || names[2] != "z" {
		t.Fatalf("List not sorted: %v", names)
	}
}
