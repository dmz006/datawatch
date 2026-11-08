package genartifacts

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SidecarRelPath returns the repository-relative path of the hash sidecar of
// a tracked file: the same directory, and the base name with a leading '.'
// and a trailing '.genhash' — the naming sidecarPath builds for an absolute
// path. The result is always in clean forward-slash form.
func SidecarRelPath(rel string) string {
	return path.Join(path.Dir(rel), "."+path.Base(rel)+".genhash")
}

// IsUneditedGenerated reports whether the tracked file root/rel (rel relative
// to root, forward slashes) is still exactly the content a Write last
// generated for it: the file is a regular file, its first line is Marker, it
// carries no edited line, and the sha256 of its body equals the hash recorded
// in its sidecar. A missing file, any read error, a non-tracked path, or any
// mismatch all report false.
func IsUneditedGenerated(root, rel string) bool {
	if !MatchesTrackedPath(rel) {
		return false
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	st, err := os.Stat(full)
	if err != nil || !st.Mode().IsRegular() {
		return false
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return false
	}
	s := string(b)
	first, _, _ := strings.Cut(s, "\n")
	if strings.TrimRight(first, "\r") != Marker {
		return false
	}
	body, marked, edited := splitMarker(s)
	if !marked || edited {
		return false
	}
	rec, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(SidecarRelPath(rel))))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(rec)) == bodyHash(body)
}
