// Package platformcontext: local_file.go delivers the platform context through
// an untracked file.
//
// The local file CLAUDE.local.md sits beside a project's own CLAUDE.md but is
// never committed. In a git repository the file name is added to the
// repository's info/exclude file (a per-clone ignore list that is not itself
// tracked), so the delivery leaves no change in `git status` and cannot reach a
// commit. A CLAUDE.local.md that the project already tracks is never touched,
// because writing it would modify a committed file. Text the operator wrote
// outside the marked block is always kept.
package platformcontext

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const platformContextLocalFileName = "CLAUDE.local.md"

// isGitRepo reports whether dir is inside a git work tree.
func isGitRepo(dir string) bool {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// localFileTracked reports whether CLAUDE.local.md is tracked in the repository.
func localFileTracked(dir string) bool {
	return exec.Command("git", "-C", dir, "ls-files", "--error-unmatch", "--", platformContextLocalFileName).Run() == nil
}

// ensureExcluded appends the local file name to info/exclude when absent.
func ensureExcluded(dir string) error {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return fmt.Errorf("resolve git exclude file: %w", err)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return errors.New("resolve git exclude file: empty path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimRight(line, "\r") == platformContextLocalFileName {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	add := platformContextLocalFileName + "\n"
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		add = "\n" + add
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(add); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// WritePlatformContextLocalFile writes the marked platform context block into
// projectDir/CLAUDE.local.md. written is true only when the file content
// changed. In a git repository the file is excluded locally first, and a
// tracked CLAUDE.local.md is refused with an error.
func WritePlatformContextLocalFile(projectDir, body string, version int) (written bool, err error) {
	if isGitRepo(projectDir) {
		if localFileTracked(projectDir) {
			return false, fmt.Errorf("%s is tracked by git; refusing to write platform context into it", platformContextLocalFileName)
		}
		if err := ensureExcluded(projectDir); err != nil {
			return false, err
		}
	}
	path := filepath.Join(projectDir, platformContextLocalFileName)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	existing := string(data)
	next := UpsertSection(existing, body, version)
	if next == existing && err == nil {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// RemovePlatformContextLocalFile strips the marked block from CLAUDE.local.md
// and deletes the file when nothing else is left. removed is true when the file
// changed or was deleted.
func RemovePlatformContextLocalFile(projectDir string) (removed bool, err error) {
	path := filepath.Join(projectDir, platformContextLocalFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	existing := string(data)
	next := RemoveSection(existing)
	if next == existing {
		return false, nil
	}
	if strings.TrimSpace(next) == "" {
		if err := os.Remove(path); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
