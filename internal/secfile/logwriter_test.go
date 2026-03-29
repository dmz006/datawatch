package secfile

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func testKey() []byte {
	key := make([]byte, 32)
	rand.Read(key)
	return key
}

func TestEncryptedLogRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log.enc")
	key := testKey()

	w, err := NewEncryptedLogWriter(path, key)
	if err != nil {
		t.Fatalf("NewEncryptedLogWriter: %v", err)
	}

	// Write some data
	data := []byte("Hello, encrypted world!\nLine 2\nLine 3\n")
	n, err := w.Write(data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(data) {
		t.Errorf("wrote %d, want %d", n, len(data))
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify file starts with magic header
	raw, _ := os.ReadFile(path)
	if !IsEncryptedLog(raw) {
		t.Error("file doesn't have encrypted log header")
	}

	// Read back
	r, err := NewEncryptedLogReader(path, key)
	if err != nil {
		t.Fatalf("NewEncryptedLogReader: %v", err)
	}
	defer r.Close()
	got, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("round-trip mismatch: got %q, want %q", got, data)
	}
}

func TestEncryptedLogLargeData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.log.enc")
	key := testKey()

	w, err := NewEncryptedLogWriter(path, key)
	if err != nil {
		t.Fatalf("NewEncryptedLogWriter: %v", err)
	}

	// Write more than flushSize to trigger multiple blocks
	data := make([]byte, flushSize*3+500)
	rand.Read(data)
	w.Write(data)
	w.Close()

	r, err := NewEncryptedLogReader(path, key)
	if err != nil {
		t.Fatalf("NewEncryptedLogReader: %v", err)
	}
	defer r.Close()
	got, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("large data round-trip failed: got %d bytes, want %d", len(got), len(data))
	}
}

func TestEncryptedLogWrongKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wrongkey.log.enc")
	key1 := testKey()
	key2 := testKey()

	w, _ := NewEncryptedLogWriter(path, key1)
	w.Write([]byte("secret data"))
	w.Close()

	r, err := NewEncryptedLogReader(path, key2)
	if err != nil {
		t.Fatalf("NewEncryptedLogReader: %v", err)
	}
	defer r.Close()
	_, err = r.ReadAll()
	if err == nil {
		t.Error("expected decrypt error with wrong key")
	}
}

func TestEncryptedLogMultipleWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.log.enc")
	key := testKey()

	w, _ := NewEncryptedLogWriter(path, key)
	w.Write([]byte("line 1\n"))
	w.Write([]byte("line 2\n"))
	w.Write([]byte("line 3\n"))
	w.Flush()
	w.Write([]byte("line 4\n"))
	w.Close()

	r, _ := NewEncryptedLogReader(path, key)
	defer r.Close()
	got, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	expected := "line 1\nline 2\nline 3\nline 4\n"
	if string(got) != expected {
		t.Errorf("got %q, want %q", string(got), expected)
	}
}

func TestEncryptedLogFlush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flush.log.enc")
	key := testKey()

	w, _ := NewEncryptedLogWriter(path, key)
	w.Write([]byte("data"))
	w.Flush()

	// File should have at least one block after flush
	info, _ := os.Stat(path)
	if info.Size() <= int64(len(logMagic)) {
		t.Error("file should have data after flush")
	}
	w.Close()
}

func TestEncryptedLogEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log.enc")
	key := testKey()

	w, _ := NewEncryptedLogWriter(path, key)
	w.Close()

	r, _ := NewEncryptedLogReader(path, key)
	defer r.Close()
	got, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll on empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d bytes", len(got))
	}
}
