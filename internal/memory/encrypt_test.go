package memory

import (
	"crypto/rand"
	"testing"
)

func testKey() []byte {
	key := make([]byte, 32)
	rand.Read(key)
	return key
}

func TestFieldEncryptDecrypt_Roundtrip(t *testing.T) {
	key := testKey()
	plaintext := "this is sensitive memory content"

	ct, err := fieldEncrypt([]byte(plaintext), key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if ct == plaintext {
		t.Error("ciphertext should not equal plaintext")
	}

	pt, err := fieldDecrypt(ct, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(pt) != plaintext {
		t.Errorf("roundtrip failed: got %q, want %q", string(pt), plaintext)
	}
}

func TestFieldDecrypt_WrongKey(t *testing.T) {
	key1 := testKey()
	key2 := testKey()

	ct, _ := fieldEncrypt([]byte("secret"), key1)
	_, err := fieldDecrypt(ct, key2)
	if err == nil {
		t.Error("decrypting with wrong key should fail")
	}
}

func TestStoreEncrypted_SaveAndRead(t *testing.T) {
	key := testKey()
	dir := t.TempDir()
	s, err := NewStoreEncrypted(dir+"/test.db", key)
	if err != nil {
		t.Fatalf("NewStoreEncrypted: %v", err)
	}
	defer s.Close()

	if !s.IsEncrypted() {
		t.Error("store should report encrypted")
	}

	// Save — content should be encrypted in DB
	id, err := s.Save("/proj", "secret content", "secret summary", "manual", "", nil)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify raw DB has encrypted content
	var rawContent string
	s.db.QueryRow(`SELECT content FROM memories WHERE id = ?`, id).Scan(&rawContent)
	if rawContent == "secret content" {
		t.Error("raw DB content should be encrypted, not plaintext")
	}
	if rawContent[:4] != "ENC:" {
		t.Errorf("encrypted content should start with 'ENC:', got %q", rawContent[:20])
	}

	// Read via ListRecent — should decrypt transparently
	memories, err := s.ListRecent("/proj", 1)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	if memories[0].Content != "secret content" {
		t.Errorf("decrypted content = %q, want 'secret content'", memories[0].Content)
	}
	if memories[0].Summary != "secret summary" {
		t.Errorf("decrypted summary = %q, want 'secret summary'", memories[0].Summary)
	}
}

func TestStoreEncrypted_SearchWorks(t *testing.T) {
	key := testKey()
	dir := t.TempDir()
	s, err := NewStoreEncrypted(dir+"/test.db", key)
	if err != nil {
		t.Fatalf("NewStoreEncrypted: %v", err)
	}
	defer s.Close()

	vec := []float32{1.0, 0.0, 0.0}
	s.Save("/proj", "encrypted searchable content", "", "manual", "", vec)

	query := []float32{1.0, 0.0, 0.0}
	results, err := s.Search("/proj", query, 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "encrypted searchable content" {
		t.Errorf("search result content = %q, want decrypted plaintext", results[0].Content)
	}
}

func TestStoreUnencrypted_PlaintextPreserved(t *testing.T) {
	s, _ := tempDB(t)
	defer s.Close()

	if s.IsEncrypted() {
		t.Error("unencrypted store should not report encrypted")
	}

	s.Save("/proj", "plain text", "", "manual", "", nil)
	var raw string
	s.db.QueryRow(`SELECT content FROM memories WHERE id = 1`).Scan(&raw)
	if raw != "plain text" {
		t.Errorf("unencrypted store should save plaintext, got %q", raw)
	}
}

func TestKeyRotation(t *testing.T) {
	oldKey := testKey()
	newKey := testKey()
	dir := t.TempDir()

	s, _ := NewStoreEncrypted(dir+"/test.db", oldKey)
	defer s.Close()

	s.Save("/proj", "content to rotate", "summary", "manual", "", nil)

	count, err := RotateKey(s, oldKey, newKey)
	if err != nil {
		t.Fatalf("RotateKey: %v", err)
	}
	if count != 1 {
		t.Errorf("rotated %d rows, want 1", count)
	}

	// Read with new key should work
	memories, _ := s.ListRecent("/proj", 1)
	if len(memories) != 1 {
		t.Fatal("expected 1 memory after rotation")
	}
	if memories[0].Content != "content to rotate" {
		t.Errorf("after rotation content = %q, want 'content to rotate'", memories[0].Content)
	}
}

func TestMigrateToEncrypted(t *testing.T) {
	dir := t.TempDir()
	key := testKey()

	// Start unencrypted
	s, _ := NewStore(dir + "/test.db")
	s.Save("/proj", "plain1", "", "manual", "", nil)
	s.Save("/proj", "plain2", "", "session", "s1", nil)

	// Migrate
	count, err := MigrateToEncrypted(s, key)
	if err != nil {
		t.Fatalf("MigrateToEncrypted: %v", err)
	}
	if count != 2 {
		t.Errorf("migrated %d, want 2", count)
	}

	// Verify encrypted in DB
	var raw string
	s.db.QueryRow(`SELECT content FROM memories WHERE id = 1`).Scan(&raw)
	if !startsWithENC(raw) {
		t.Error("content should be encrypted after migration")
	}

	// Read should decrypt
	memories, _ := s.ListRecent("/proj", 10)
	if len(memories) != 2 {
		t.Fatalf("expected 2, got %d", len(memories))
	}
	for _, m := range memories {
		if startsWithENC(m.Content) {
			t.Errorf("memory #%d content should be decrypted, got %q", m.ID, m.Content[:20])
		}
	}
	s.Close()
}

func startsWithENC(s string) bool {
	return len(s) >= 4 && s[:4] == "ENC:"
}

func TestKeyFingerprint(t *testing.T) {
	key := testKey()
	fp := KeyFingerprint(key)
	if len(fp) != 8 {
		t.Errorf("fingerprint length = %d, want 8", len(fp))
	}

	// Same key = same fingerprint
	fp2 := KeyFingerprint(key)
	if fp != fp2 {
		t.Error("same key should produce same fingerprint")
	}

	// Nil key = empty
	if KeyFingerprint(nil) != "" {
		t.Error("nil key should return empty fingerprint")
	}
}

func TestKeyManager_GenerateAndLoad(t *testing.T) {
	dir := t.TempDir()
	km := NewKeyManager(dir)

	key, err := km.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("key length = %d, want 32", len(key))
	}

	loaded, err := km.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 32 {
		t.Errorf("loaded key length = %d, want 32", len(loaded))
	}

	// Fingerprints should match
	if km.Fingerprint(key) != km.Fingerprint(loaded) {
		t.Error("generated and loaded key fingerprints should match")
	}
}
