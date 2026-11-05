package server

import (
	"path/filepath"
	"testing"
)

// A request's cases folder is resolved under the working directory and can
// never leave it, whatever the text.
func TestConfineCasesDirStaysUnderRoot(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "work", "dir")
	for rel, want := range map[string]string{
		"a/b":          filepath.Join(root, "a", "b"),
		"./a":          filepath.Join(root, "a"),
		"../../etc":    filepath.Join(root, "etc"),
		"a/../../..":   root,
		"/abs/looking": filepath.Join(root, "abs", "looking"),
	} {
		got, ok := confineCasesDir(root, rel)
		if !ok || got != want {
			t.Fatalf("confineCasesDir(%q) = %q, %v; want %q", rel, got, ok, want)
		}
	}
}
