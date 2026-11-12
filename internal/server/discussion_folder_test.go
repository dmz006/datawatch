package server

import (
	"os"
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

// TestDiscussionFolderForID runs Server.discussionFolderForID against a
// temporary state directory: a normal id must resolve to a folder under the
// discussions folder, and every path-traversal-shaped id must be refused
// without the method creating any file or folder.
func TestDiscussionFolderForID(t *testing.T) {
	s := &Server{dataDir: t.TempDir()}
	base := filepath.Join(s.dataDir, "discussions")

	tests := []struct {
		name    string
		id      string
		want    string
		wantErr bool
	}{
		{name: "normal id", id: "my-topic", want: filepath.Join(base, "my-topic")},
		{name: "single dot", id: ".", wantErr: true},
		{name: "double dot", id: "..", wantErr: true},
		{name: "id with slash", id: "a/b", wantErr: true},
		{name: "id with backslash", id: `a\b`, wantErr: true},
		{name: "cleaned path leaves the discussions folder", id: "x/../../escape", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := os.ReadDir(s.dataDir)
			if err != nil {
				t.Fatalf("snapshot: reading %q: %v", s.dataDir, err)
			}
			got, callErr := s.discussionFolderForID(tc.id)

			if tc.wantErr {
				if callErr == nil {
					t.Fatalf("refuse case %q: expected error, got path %q", tc.id, got)
				}
				// A refusal must not have created any file or folder: the
				// state directory listing is unchanged and the discussions
				// folder does not exist.
				if _, statErr := os.Stat(base); !os.IsNotExist(statErr) {
					t.Fatalf("refuse case %q: discussions folder %q appears to have been created", tc.id, base)
				}
				after, readErr := os.ReadDir(s.dataDir)
				if readErr != nil {
					t.Fatalf("refuse case %q: reading %q: %v", tc.id, s.dataDir, readErr)
				}
				if len(snapshot) != len(after) {
					t.Fatalf("refuse case %q: state directory %q contents changed", tc.id, s.dataDir)
				}
				for i := range snapshot {
					if snapshot[i].Name() != after[i].Name() {
						t.Fatalf("refuse case %q: state directory %q contents changed", tc.id, s.dataDir)
					}
				}
				return
			}

			if callErr != nil {
				t.Fatalf("accept case %q: unexpected error: %v", tc.id, callErr)
			}
			if got != tc.want {
				t.Fatalf("accept case %q: got %q, want %q", tc.id, got, tc.want)
			}
			if !strings.HasPrefix(got, base+string(filepath.Separator)) {
				t.Fatalf("accept case %q: got %q, not under %q", tc.id, got, base)
			}
		})
	}
}
