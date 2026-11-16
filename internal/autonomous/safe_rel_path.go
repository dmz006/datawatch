package autonomous

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/gitguard"
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

// repoFileInside returns the cleaned join of rel under dir — the value the
// caller should open or stat directly — and an error when the join is not
// demonstrably inside dir. It validates in one place so the guard and the
// use stay adjacent: localRelPath rejects empty, NUL, absolute and '..'
// paths; gitguard.ResolveWithin then resolves the base and the deepest
// existing part of the join through EvalSymlinks, so a symlink chain that
// leaves dir is refused too. A base that does not exist yet resolves
// against its deepest existing ancestor, so its result still stays under
// the cleaned base path (a read of it simply finds nothing). Every
// error wraps gitguard.ErrPathEscapes for callers that want one type.
func repoFileInside(dir, rel string) (string, error) {
	if dir == "" || strings.ContainsRune(dir, 0) {
		return "", fmt.Errorf("%w: %q", gitguard.ErrPathEscapes, rel)
	}
	cleaned, ok := localRelPath(rel)
	if !ok {
		return "", fmt.Errorf("%w: %q", gitguard.ErrPathEscapes, rel)
	}
	return gitguard.ResolveWithin(dir, filepath.FromSlash(cleaned))
}
