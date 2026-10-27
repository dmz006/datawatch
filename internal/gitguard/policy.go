// Package gitguard holds the staging policy shared by the automatic
// git add/commit paths (session snapshots and autonomous attempt snapshots).
// It decides which candidate files may be staged (size cap, ignored cache
// and build directories, model-weight patterns), bounds how many files one
// commit may stage, removes interrupted pack debris, and detects when the
// repository under auto-commit is the user's home directory. It is a
// standalone stdlib-only package so the session and autonomous packages can
// both import it without importing each other.
package gitguard

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Skip records one candidate file that Keep refused to stage and why.
// Reason is one of "size", "ignore-dir", "pattern", or "outside-root".
type Skip struct {
	File   string
	Reason string
}

// Policy bounds what an automatic commit may stage. A zero-value Policy
// imposes no limits; callers opt out explicitly, while Default always bounds.
type Policy struct {
	// MaxFileBytes is the per-file staging cap. Zero means no limit.
	MaxFileBytes int64
	// IgnoreDirs lists directory components that may never be staged.
	IgnoreDirs []string
	// IgnorePatterns lists file patterns matched with path.Match against the
	// base name of each candidate.
	IgnorePatterns []string
	// MaxStagedFiles bounds the total number of staged files per commit.
	// Zero means no limit.
	MaxStagedFiles int
	// AddTimeout bounds how long staging may take.
	AddTimeout time.Duration
	// Home optionally overrides the home-directory resolver. Nil means
	// os.UserHomeDir.
	Home func() (string, error)
}

// Default is the operator-facing staging policy: 50 MB per file, cache and
// build-output directories and model-weight files never staged, and at most
// 10000 staged files per commit.
var Default = Policy{
	MaxFileBytes: 50 * 1024 * 1024,
	IgnoreDirs: []string{
		".cache", ".ollama", ".gradle", ".android", "go", "snap",
		"node_modules", "model", "models", "build", "dist", "out", "target",
	},
	IgnorePatterns: []string{
		"*.gguf", "*.safetensors", "*.pt", "*.pth", "*.bin", "*.ckpt", "*.onnx", "*.tflite",
	},
	MaxStagedFiles: 10000,
	AddTimeout:     5 * time.Minute,
}

// Keep filters candidate file paths for staging. Relative candidates are
// resolved against repoDir. Missing files are kept, because git add of a
// vanished file is a harmless no-op. The returned kept slice preserves the
// input order and holds the candidates exactly as given.
func (p Policy) Keep(repoDir string, files []string) (kept []string, skipped []Skip) {
	root := repoDir
	if abs, err := filepath.Abs(repoDir); err == nil {
		root = abs
	}
	for _, f := range files {
		full := f
		if !filepath.IsAbs(full) {
			full = filepath.Join(repoDir, full)
		}
		checked, cerr := containedEntry(root, full)
		if errors.Is(cerr, ErrPathEscapes) {
			skipped = append(skipped, Skip{File: f, Reason: "outside-root"})
			continue
		}
		if cerr != nil {
			kept = append(kept, f)
			continue
		}
		full = checked
		info, err := os.Stat(full)
		if err != nil {
			kept = append(kept, f)
			continue
		}
		if p.MaxFileBytes > 0 && info.Size() > p.MaxFileBytes {
			skipped = append(skipped, Skip{File: f, Reason: "size"})
			continue
		}
		if p.inIgnoredDir(root, full, info.IsDir()) {
			skipped = append(skipped, Skip{File: f, Reason: "ignore-dir"})
			continue
		}
		if p.matchesIgnoredPattern(filepath.Base(full)) {
			skipped = append(skipped, Skip{File: f, Reason: "pattern"})
			continue
		}
		kept = append(kept, f)
	}
	return kept, skipped
}

// inIgnoredDir reports whether any directory component of full, relative to
// repoDir when full lies inside it, is listed in IgnoreDirs. The file's own
// base name is not a directory component, unless full is itself a directory.
// Checking relative to repoDir keeps a repository that lives under a
// directory such as ~/go from being rejected wholesale.
func (p Policy) inIgnoredDir(repoDir, full string, isDir bool) bool {
	if len(p.IgnoreDirs) == 0 {
		return false
	}
	target := full
	if rel, err := filepath.Rel(repoDir, full); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		target = rel
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(target)), "/")
	if !isDir && len(parts) > 0 {
		parts = parts[:len(parts)-1]
	}
	for _, part := range parts {
		for _, dir := range p.IgnoreDirs {
			if part == dir {
				return true
			}
		}
	}
	return false
}

// matchesIgnoredPattern reports whether base matches any IgnorePatterns entry.
// A malformed pattern never matches.
func (p Policy) matchesIgnoredPattern(base string) bool {
	for _, pat := range p.IgnorePatterns {
		if ok, err := path.Match(pat, base); err == nil && ok {
			return true
		}
	}
	return false
}

// RefuseReason returns the operator-facing message for a commit that would
// stage n files, or "" when n is within MaxStagedFiles or no limit is set.
// Callers include the returned text verbatim in the error they return, and
// prefix it with the repository directory so the operator sees where the
// commit stopped.
func (p Policy) RefuseReason(n int) string {
	if p.MaxStagedFiles <= 0 || n <= p.MaxStagedFiles {
		return ""
	}
	return fmt.Sprintf(
		"auto-commit refused: %d files staged exceeds the limit of %d (MaxStagedFiles). "+
			"Narrow the write scope (read_dirs/write_dirs) so the commit stages fewer files, "+
			"or point the project directory at a repository rather than a home directory.",
		n, p.MaxStagedFiles)
}

// CleanTempPacks removes in-progress and orphaned pack debris from
// <repoDir>/.git/objects/pack. It removes files named "tmp_pack_*", and
// "*.pack" files with neither a sibling "*.idx" nor a sibling "*.rev", since
// an interrupted pack-objects leaves a pack without an index. Nothing else is
// touched. A missing pack directory is not an error, and repeated calls are
// safe. It returns the paths removed.
func CleanTempPacks(repoDir string) (removed []string, err error) {
	dir, err := ResolveWithin(repoDir, filepath.Join(".git", "objects", "pack"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	entries, err := readDirWithin(repoDir, dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			present[e.Name()] = true
		}
	}
	var errs []error
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		orphan := false
		switch {
		case strings.HasPrefix(name, "tmp_pack_"):
			orphan = true
		case strings.HasSuffix(name, ".pack"):
			stem := strings.TrimSuffix(name, ".pack")
			orphan = !present[stem+".idx"] && !present[stem+".rev"]
		}
		if !orphan {
			continue
		}
		target := filepath.Join(dir, name)
		if rmErr := removeWithin(dir, target); rmErr != nil {
			if errors.Is(rmErr, os.ErrNotExist) {
				continue
			}
			errs = append(errs, rmErr)
			continue
		}
		removed = append(removed, target)
	}
	return removed, errors.Join(errs...)
}

// containedEntry is ContainedPath for a path that names a directory entry,
// where the entry itself may be a symlink. A link is judged by where it
// lies, not by where it points: when path is a symlink whose parent directory
// is inside root, the entry is accepted, because stat-ing it follows the link
// but listing, staging or removing it touches only the link. Any other path
// is accepted only when ContainedPath accepts it. The returned value is the
// checked path and is what the caller should use.
func containedEntry(root, path string) (string, error) {
	checked, err := ContainedPath(root, path)
	if err == nil {
		return checked, nil
	}
	if !errors.Is(err, ErrPathEscapes) {
		return "", err
	}
	abs, aerr := filepath.Abs(path)
	if aerr != nil {
		return "", err
	}
	parent, perr := ContainedPath(root, filepath.Dir(abs))
	if perr != nil {
		return "", err
	}
	entry := filepath.Join(parent, filepath.Base(abs))
	if fi, lerr := os.Lstat(entry); lerr != nil || fi.Mode()&os.ModeSymlink == 0 {
		return "", err
	}
	return entry, nil
}

// readDirWithin lists dir after checking that it lies inside root. A dir
// outside root is refused with ErrPathEscapes and nothing is read.
func readDirWithin(root, dir string) ([]os.DirEntry, error) {
	checked, err := ContainedPath(root, dir)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(checked)
}

// removeWithin removes the entry at target after checking that it lies inside
// root. A target outside root is refused with ErrPathEscapes and nothing is
// removed. A symlink target is removed as a link, never followed.
func removeWithin(root, target string) error {
	checked, err := containedEntry(root, target)
	if err != nil {
		return err
	}
	return os.Remove(checked)
}

// InHome reports whether dir equals or lies inside the user's home directory.
// homeResolver supplies the home directory; nil means os.UserHomeDir. Both
// paths are made absolute and symlink-resolved where the link resolves,
// otherwise only cleaned.
func InHome(dir string, homeResolver func() (string, error)) (bool, error) {
	if homeResolver == nil {
		homeResolver = os.UserHomeDir
	}
	home, err := homeResolver()
	if err != nil {
		return false, err
	}
	homeAbs, err := normalizePath(home)
	if err != nil {
		return false, err
	}
	dirAbs, err := normalizePath(dir)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(homeAbs, dirAbs)
	if err != nil {
		return false, nil
	}
	if rel == "." {
		return true, nil
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel), nil
}

// normalizePath returns the absolute, symlink-resolved form of p when the
// link resolves, otherwise the absolute, cleaned form.
func normalizePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}
