package secfile

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func migrateTestKey() []byte {
	key := make([]byte, 32)
	rand.Read(key)
	return key
}

func TestMigrateLogOnly(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions", "test-abcd")
	trackDir := filepath.Join(sessDir, "tracking")
	os.MkdirAll(trackDir, 0755)

	// Write plaintext files
	os.WriteFile(filepath.Join(sessDir, "output.log"), []byte("plaintext output\n"), 0644)
	os.WriteFile(filepath.Join(trackDir, "conversation.md"), []byte("# Conv\nhello"), 0644)

	key := migrateTestKey()

	// Migrate in log_only mode
	if err := MigratePlaintextToEncrypted(dir, key, false); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// output.log should be encrypted
	data, _ := os.ReadFile(filepath.Join(sessDir, "output.log"))
	if !IsEncrypted(data) {
		t.Fatalf("output.log not encrypted, starts with: %q", string(data)[:20])
	}
	plain, err := Decrypt(data, key)
	if err != nil {
		t.Fatalf("decrypt output.log: %v", err)
	}
	if !strings.Contains(string(plain), "plaintext output") {
		t.Fatalf("output.log content mismatch: %q", string(plain))
	}

	// conversation.md should stay plaintext
	convData, _ := os.ReadFile(filepath.Join(trackDir, "conversation.md"))
	if IsEncrypted(convData) {
		t.Fatal("conversation.md should NOT be encrypted in log_only mode")
	}

	// Sentinel should exist
	if !IsMigrated(dir) {
		t.Fatal("sentinel file missing")
	}

	// Idempotent re-run
	if err := MigratePlaintextToEncrypted(dir, key, false); err != nil {
		t.Fatalf("idempotent migration failed: %v", err)
	}
}

func TestMigrateFull(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions", "test-efgh")
	trackDir := filepath.Join(sessDir, "tracking")
	os.MkdirAll(trackDir, 0755)

	os.WriteFile(filepath.Join(sessDir, "output.log"), []byte("secret output\n"), 0644)
	os.WriteFile(filepath.Join(trackDir, "conversation.md"), []byte("# Conv\nsecret chat"), 0644)
	os.WriteFile(filepath.Join(trackDir, "timeline.md"), []byte("# Timeline\n- event"), 0644)

	key := migrateTestKey()

	if err := MigratePlaintextToEncrypted(dir, key, true); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// output.log encrypted
	data, _ := os.ReadFile(filepath.Join(sessDir, "output.log"))
	if !IsEncrypted(data) {
		t.Fatal("output.log not encrypted")
	}

	// conversation.md encrypted
	convData, _ := os.ReadFile(filepath.Join(trackDir, "conversation.md"))
	if !IsEncrypted(convData) {
		t.Fatal("conversation.md should be encrypted in full mode")
	}
	plain, err := Decrypt(convData, key)
	if err != nil {
		t.Fatalf("decrypt conversation.md: %v", err)
	}
	if !strings.Contains(string(plain), "secret chat") {
		t.Fatalf("conversation.md content mismatch: %q", string(plain))
	}

	// timeline.md encrypted
	tlData, _ := os.ReadFile(filepath.Join(trackDir, "timeline.md"))
	if !IsEncrypted(tlData) {
		t.Fatal("timeline.md should be encrypted in full mode")
	}

	if !IsMigrated(dir) {
		t.Fatal("sentinel missing")
	}
}

func TestMigrateSkipsEncrypted(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions", "test-skip")
	os.MkdirAll(sessDir, 0755)

	key := migrateTestKey()

	// Write an already-encrypted file
	enc, _ := Encrypt([]byte("already encrypted"), key)
	os.WriteFile(filepath.Join(sessDir, "output.log"), enc, 0644)

	if err := MigratePlaintextToEncrypted(dir, key, false); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Should still be readable (not double-encrypted)
	data, _ := os.ReadFile(filepath.Join(sessDir, "output.log"))
	plain, err := Decrypt(data, key)
	if err != nil {
		t.Fatalf("decrypt failed (double-encrypted?): %v", err)
	}
	if string(plain) != "already encrypted" {
		t.Fatalf("content mismatch: %q", string(plain))
	}
}

func TestMigrateEmptyDir(t *testing.T) {
	dir := t.TempDir()
	key := migrateTestKey()

	// No sessions dir — should not error
	if err := MigratePlaintextToEncrypted(dir, key, false); err != nil {
		t.Fatalf("migration on empty dir failed: %v", err)
	}
	if !IsMigrated(dir) {
		t.Fatal("sentinel missing on empty dir")
	}
}
