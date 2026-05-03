package secrets

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// keepassxcAvailable skips the test when keepassxc-cli is not installed.
func keepassxcAvailable(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli not in PATH — skipping KeePass integration tests")
	}
	return p
}

// createTestDB creates a fresh .kdbx database for tests using keepassxc-cli db-create.
func createTestDB(t *testing.T, binary, dir, password string) string {
	t.Helper()
	dbPath := filepath.Join(dir, "test.kdbx")
	cmd := exec.Command(binary, "db-create", "--set-password", dbPath)
	cmd.Stdin = newPasswordPipe(password)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("db-create: %v — %s", err, out)
	}
	return dbPath
}

func newPasswordPipe(password string) *passwordReader {
	return &passwordReader{s: password + "\n" + password + "\n"}
}

type passwordReader struct{ s string; pos int }

func (p *passwordReader) Read(b []byte) (int, error) {
	if p.pos >= len(p.s) {
		return 0, nil
	}
	n := copy(b, p.s[p.pos:])
	p.pos += n
	return n, nil
}

func TestKeePassStore_CRUD(t *testing.T) {
	binary := keepassxcAvailable(t)
	dir := t.TempDir()
	const dbPass = "testpassword123"
	dbPath := createTestDB(t, binary, dir, dbPass)

	store, err := NewKeePassStore(binary, dbPath, dbPass, "")
	if err != nil {
		t.Fatalf("NewKeePassStore: %v", err)
	}

	// Empty list
	list, err := store.List()
	if err != nil {
		t.Fatalf("List (empty): %v", err)
	}
	// KeePass may have a default "Sample Entry" — filter to known names
	for _, s := range list {
		if s.Name == "token" {
			t.Fatal("unexpected entry 'token' in fresh DB")
		}
	}

	// Set (create)
	if err := store.Set("token", "supersecret", []string{"git", "cloud"}, "GitHub PAT"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Exists
	ok, err := store.Exists("token")
	if err != nil || !ok {
		t.Fatalf("Exists: want true, got %v (%v)", ok, err)
	}
	ok, _ = store.Exists("missing-entry-xyz")
	if ok {
		t.Fatal("Exists(missing): want false")
	}

	// Get
	sec, err := store.Get("token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sec.Value != "supersecret" {
		t.Fatalf("Get value: want supersecret, got %q", sec.Value)
	}
	if sec.Backend != "keepass" {
		t.Fatalf("Get backend: want keepass, got %q", sec.Backend)
	}
	if sec.Description != "GitHub PAT" {
		t.Fatalf("Get description: want %q, got %q", "GitHub PAT", sec.Description)
	}
	if len(sec.Tags) != 2 {
		t.Fatalf("Get tags: want 2, got %d (%v)", len(sec.Tags), sec.Tags)
	}

	// Set (update)
	if err := store.Set("token", "newsecret", nil, "updated"); err != nil {
		t.Fatalf("Set update: %v", err)
	}
	sec2, err := store.Get("token")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if sec2.Value != "newsecret" {
		t.Fatalf("update value: want newsecret, got %q", sec2.Value)
	}

	// Delete
	if err := store.Delete("token"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get("token"); err != ErrSecretNotFound {
		t.Fatalf("Get after delete: want ErrSecretNotFound, got %v", err)
	}

	// Delete missing
	if err := store.Delete("gone-xyz"); err != ErrSecretNotFound {
		t.Fatalf("Delete missing: want ErrSecretNotFound, got %v", err)
	}
}

func TestKeePassStore_parseKeePassShow(t *testing.T) {
	// Unit-test the parser without a real database.
	input := `Title: mytoken
UserName:
Password: s3cr3t
URL:
Notes: My API token
Modified: 2024-03-15T10:30:00
 datawatch-tags: api,prod
`
	sec := parseKeePassShow("mytoken", input)
	if sec.Value != "s3cr3t" {
		t.Errorf("value: want s3cr3t, got %q", sec.Value)
	}
	if sec.Description != "My API token" {
		t.Errorf("description: want %q, got %q", "My API token", sec.Description)
	}
	if len(sec.Tags) != 2 || sec.Tags[0] != "api" || sec.Tags[1] != "prod" {
		t.Errorf("tags: want [api prod], got %v", sec.Tags)
	}
	if sec.UpdatedAt.IsZero() {
		t.Error("updated_at should be parsed")
	}
	if sec.Backend != "keepass" {
		t.Errorf("backend: want keepass, got %q", sec.Backend)
	}
}

func TestKeePassStore_Group(t *testing.T) {
	binary := keepassxcAvailable(t)
	dir := t.TempDir()
	const dbPass = "testpassword123"
	dbPath := createTestDB(t, binary, dir, dbPass)

	store, err := NewKeePassStore(binary, dbPath, dbPass, "datawatch")
	if err != nil {
		t.Fatalf("NewKeePassStore: %v", err)
	}

	// Create a group first
	cmd := exec.Command(binary, "mkdir", dbPath, "datawatch")
	cmd.Stdin = newPasswordPipe(dbPass)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mkdir: %v — %s", err, out)
	}

	if err := store.Set("api-key", "myvalue", []string{"prod"}, "Production API key"); err != nil {
		t.Fatalf("Set in group: %v", err)
	}
	sec, err := store.Get("api-key")
	if err != nil {
		t.Fatalf("Get from group: %v", err)
	}
	if sec.Value != "myvalue" {
		t.Fatalf("Get value: want myvalue, got %q", sec.Value)
	}
}
