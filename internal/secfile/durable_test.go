package secfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileDurableReplacesTheFileAndSeals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r")
	key := []byte("0123456789abcdef0123456789abcdef")
	for _, body := range []string{"one", "two"} {
		if err := WriteFileDurable(path, []byte(body), 0o600, key); err != nil {
			t.Fatal(err)
		}
		got, err := ReadFile(path, key)
		if err != nil || string(got) != body {
			t.Fatalf("%q %v", got, err)
		}
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "two") {
		t.Fatalf("not sealed: %q", raw)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary file left")
	}
	if err := WriteFileDurable(filepath.Join(path, "under-a-file"), []byte("x"), 0o600, nil); err == nil {
		t.Fatal("want an error")
	}
}
