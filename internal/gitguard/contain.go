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
// is refused even though the lexical path looks contained; a dangling link
// is followed to its missing target and judged by where that target lies. Nothing is created.
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
	if !isWithin(baseReal, real) {
		return "", fmt.Errorf("%w: %q resolves outside %s", ErrPathEscapes, rel, base)
	}
	return joined, nil
}

// maxLinkHops bounds how many dangling links evalDeepest follows.
const maxLinkHops = 40

// evalDeepest resolves symlinks in the longest existing prefix of p and
// appends the not-yet-existing remainder unchanged. A dangling symlink met on
// the way is followed to its (missing) target so the caller sees where the
// link really points.
func evalDeepest(p string) (string, error) {
	for hops := 0; hops <= maxLinkHops; hops++ {
		rest := ""
		cur := p
		redirected := false
		for {
			real, err := filepath.EvalSymlinks(cur)
			if err == nil {
				return filepath.Join(real, rest), nil
			}
			if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
			if fi, lerr := os.Lstat(cur); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
				target, rerr := os.Readlink(cur)
				if rerr != nil {
					return "", rerr
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(cur), target)
				}
				p = filepath.Join(target, rest)
				redirected = true
				break
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				return "", err
			}
			rest = filepath.Join(filepath.Base(cur), rest)
			cur = parent
		}
		if !redirected {
			break
		}
	}
	return "", fmt.Errorf("%w: too many levels of symbolic links", ErrPathEscapes)
}

// CleanProjectDir normalises a project directory that is allowed to be
// absolute (an operator-chosen location rather than a path under a base). It
// refuses an empty or NUL-bearing value and a filesystem root, and returns the
// cleaned absolute path. A relative value is made absolute against the
// process working directory.
func CleanProjectDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("project directory is empty")
	}
	if strings.ContainsRune(dir, 0) {
		return "", fmt.Errorf("%w: path contains a NUL byte", ErrPathEscapes)
	}
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return "", err
	}
	if filepath.Dir(abs) == abs {
		return "", fmt.Errorf("%w: %q is a filesystem root", ErrPathEscapes, dir)
	}
	return abs, nil
}
