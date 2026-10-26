package gitguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrPathEscapes is returned by ResolveWithin when a relative path would land
// outside its base directory.
var ErrPathEscapes = errors.New("path escapes its base directory")

// ResolveWithin joins rel under base and verifies the result stays inside
// base. rel must be relative: an empty, absolute, NUL-bearing or '..'-bearing
// value is refused. The check then resolves symlinks in base and in the
// deepest existing part of the joined path, so a link that points out of base
// is refused even though the lexical path looks contained. Nothing is created.
// The returned path is the lexical join of base and the cleaned rel.
func ResolveWithin(base, rel string) (string, error) {
	if base == "" {
		return "", errors.New("base directory is empty")
	}
	if strings.ContainsRune(rel, 0) {
		return "", fmt.Errorf("%w: path contains a NUL byte", ErrPathEscapes)
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: %q is absolute", ErrPathEscapes, rel)
	}
	for _, part := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == filepath.Separator }) {
		if part == ".." {
			return "", fmt.Errorf("%w: %q contains '..'", ErrPathEscapes, rel)
		}
	}
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(base, filepath.Clean(rel))
	real, err := evalDeepest(filepath.Join(baseReal, filepath.Clean(rel)))
	if err != nil {
		return "", err
	}
	back, err := filepath.Rel(baseReal, real)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q resolves outside %s", ErrPathEscapes, rel, base)
	}
	return joined, nil
}

// evalDeepest resolves symlinks in the longest existing prefix of p and
// appends the not-yet-existing remainder unchanged.
func evalDeepest(p string) (string, error) {
	rest := ""
	cur := p
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(real, rest), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
