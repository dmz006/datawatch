package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The save replaces the file whole. A second name for the old file keeps the
// old content; writing into the file in place would change it too, and an
// interruption would leave half a queue.
func TestQueueSaveNeverRewritesTheFileInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	q, err := NewQueueStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Push("role", map[string]any{"n": "first"}); err != nil {
		t.Fatal(err)
	}
	old := path + ".old"
	if err := os.Link(path, old); err != nil {
		t.Skipf("no hard links here: %v", err)
	}
	if _, err := q.Push("role", map[string]any{"n": "second"}); err != nil {
		t.Fatal(err)
	}
	kept, _ := os.ReadFile(old)
	if strings.Contains(string(kept), "second") {
		t.Fatal("the queue file was rewritten in place")
	}
	// A save that cannot finish leaves the queue as it was.
	before, _ := os.ReadFile(path)
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Push("role", map[string]any{"n": "third"}); err == nil {
		t.Fatal("a save that could not write reported success")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("the queue file changed")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v err=%v", info.Mode(), err)
	}
}
