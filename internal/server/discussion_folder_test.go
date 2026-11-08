package server

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscussionFolderPath(t *testing.T) {
	base := t.TempDir()

	accept := struct {
		id   string
		want string
	}{
		id:   "my-topic",
		want: filepath.Join(base, "my-topic"),
	}

	if got, err := discussionFolderPath(base, accept.id); err != nil {
		t.Fatalf("accept case: unexpected error: %v", err)
	} else if got != accept.want {
		t.Fatalf("accept case: got %q, want %q", got, accept.want)
	} else if !strings.HasPrefix(got, base) {
		t.Fatalf("accept case: got %q, does not have prefix %q", got, base)
	}

	refuses := []struct {
		name string
		id   string
	}{
		{"empty string", ""},
		{"single dot", "."},
		{"double dot", ".."},
		{"forward slash", "a/b"},
		{"backslash", `a\b`},
		{"parent escape", "../escape"},
		{"deep escape", "x/../../escape"},
	}
	for _, tc := range refuses {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := discussionFolderPath(base, tc.id); err == nil {
				t.Fatalf("refuse case %q: expected error, got path %q", tc.id, got)
			}
		})
	}
}
