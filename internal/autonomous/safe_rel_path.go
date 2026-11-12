package autonomous

import (
	"path"
	"path/filepath"
	"strings"
)

// localRelPath guards a slash-form path as it appears in git status output
// before it is joined onto a directory. It returns the cleaned slash-form
// path and true only when the path is non-empty, free of NUL bytes, relative,
// and stays inside the directory it is joined to. Absolute paths, '..'
// escapes (including 'a/../../x') and a cleaned result of '.' are rejected.
func localRelPath(rel string) (string, bool) {
	if rel == "" || strings.ContainsRune(rel, 0) {
		return "", false
	}
	if strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) {
		return "", false
	}
	cleaned := path.Clean(rel)
	if cleaned == "." {
		return "", false
	}
	if !filepath.IsLocal(filepath.FromSlash(cleaned)) {
		return "", false
	}
	return cleaned, true
}
