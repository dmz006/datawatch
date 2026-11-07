package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// savedVerifyScriptsDir returns the folder that holds the saved verification
// scripts of one Automaton. It sits beside the Automaton worktrees under the
// worktree base, outside every worktree; the leading dot keeps it apart from
// the PRD id folders.
func savedVerifyScriptsDir(worktreeDir, prdID string) string {
	return filepath.Join(savedVerifyScriptsBase(worktreeDir), prdID)
}

// savedVerifyScriptsBase is the folder that holds the saved folders of every
// Automaton.
func savedVerifyScriptsBase(worktreeDir string) string {
	d := strings.TrimSpace(worktreeDir)
	if d == "" {
		d = defaultWorktreeDir("")
	}
	return filepath.Join(d, ".verify-scripts")
}

// copyVerifyFile copies one regular file to dst, replacing any existing copy.
func copyVerifyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755) // #nosec G306 -- verification scripts must be executable
}

// listVerifyFiles returns the names of the regular files directly in dir.
// A missing directory yields no names and no error.
func listVerifyFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// saveVerifyScripts copies every regular file directly inside srcVerifyDir
// into savedDir, overwriting existing copies. A missing srcVerifyDir returns
// 0, nil.
func saveVerifyScripts(srcVerifyDir, savedDir string) (int, error) {
	names, err := listVerifyFiles(srcVerifyDir)
	if err != nil {
		return 0, err
	}
	if len(names) == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(savedDir, 0o755); err != nil { // #nosec G301
		return 0, err
	}
	n := 0
	for _, name := range names {
		if err := copyVerifyFile(filepath.Join(srcVerifyDir, name), filepath.Join(savedDir, name)); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// validVerifyBasename rejects names that could escape the verify folder.
func validVerifyBasename(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, filepath.Separator) {
		return fmt.Errorf("invalid verification script name %q", name)
	}
	return nil
}

// restoreVerifyScripts copies script files into dstWorktree/.datawatch/verify,
// taking each basename from the first srcDir that holds it. It never
// overwrites a file already present in the destination. When only is non-nil
// it copies only the basenames listed in it.
func restoreVerifyScripts(srcDirs []string, dstWorktree string, only []string) (int, error) {
	var allow map[string]bool
	if only != nil {
		allow = make(map[string]bool, len(only))
		for _, o := range only {
			if err := validVerifyBasename(o); err != nil {
				return 0, err
			}
			allow[o] = true
		}
	}
	dstDir := filepath.Join(dstWorktree, ".datawatch", "verify")
	type pick struct{ dir, name string }
	var picks []pick
	seen := map[string]bool{}
	for _, dir := range srcDirs {
		names, err := listVerifyFiles(dir)
		if err != nil {
			return 0, err
		}
		for _, name := range names {
			if err := validVerifyBasename(name); err != nil {
				return 0, err
			}
			if seen[name] || (allow != nil && !allow[name]) {
				continue
			}
			seen[name] = true
			picks = append(picks, pick{dir, name})
		}
	}
	if len(picks) == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil { // #nosec G301
		return 0, err
	}
	n := 0
	for _, p := range picks {
		dst := filepath.Join(dstDir, p.name)
		if _, err := os.Lstat(dst); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return n, err
		}
		if err := copyVerifyFile(filepath.Join(p.dir, p.name), dst); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// removeSavedVerifyScripts deletes the saved folder of one Automaton; an
// absent folder is not an error.
func removeSavedVerifyScripts(worktreeDir, prdID string) error {
	// The id arrives from the REST path; the cleaned target must stay
	// strictly inside the saved-scripts base, so the removal can never leave it.
	full := savedVerifyScriptsDir(worktreeDir, prdID)
	base := savedVerifyScriptsBase(worktreeDir)
	if prdID == "" || !strings.HasPrefix(full, base+string(filepath.Separator)) {
		return fmt.Errorf("saved verification scripts: refusing unsafe Automaton id %q", prdID)
	}
	return os.RemoveAll(full)
}
