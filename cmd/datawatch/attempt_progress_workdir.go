package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// decomposeOutputNamePrefix is the stem of the harness's scratch decompose
// output files (.decompose-output-<hash>.json, written by the decompose
// session in the project directory). It matches the ignore pattern .decompose-output-*.json
// used by internal/autonomous.
const decomposeOutputNamePrefix = ".decompose-output"

// harnessScratchDir is the harness's own scratch directory inside a work
// directory. It is excluded from content fingerprints so harness noise never
// looks like worker activity.
const harnessScratchDir = ".datawatch"

// isWorkDirScratchPath reports whether relPath (a path relative to the work
// directory, slash-separated) is one of the harness's own scratch files:
// CHECKPOINT.md (doneCheckpointFile), anything under the .datawatch directory,
// or any file whose name starts with .decompose-output.
func isWorkDirScratchPath(relPath string) bool {
	relPath = filepath.ToSlash(relPath)
	name := filepath.Base(relPath)
	if name == doneCheckpointFile {
		return true
	}
	if strings.HasPrefix(name, decomposeOutputNamePrefix) {
		return true
	}
	// .datawatch at the root or as any path segment (a work directory that
	// itself lives inside a .datawatch directory must be filtered too).
	if relPath == harnessScratchDir {
		return true
	}
	for _, seg := range strings.Split(relPath, "/") {
		if seg == harnessScratchDir {
			return true
		}
	}
	return false
}

// runGit runs a git subcommand with its binary path already resolved by the
// caller (via exec.LookPath) and returns the combined stdout. stderr is
// appended to the error so callers can classify it (e.g. "not a git
// repository"). Never panics.
func runGit(workDir, gitPath string, args ...string) (string, error) {
	cmd := exec.Command(gitPath, args...) // #nosec G204 -- gitPath came from exec.LookPath; fixed subcommands
	cmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &gitRunError{args: args, err: err, stderr: strings.TrimSpace(stderr.String())}
	}
	return stdout.String(), nil
}

// gitRunError wraps a failed git invocation with the subcommand, the exit
// error and captured stderr.
type gitRunError struct {
	args   []string
	err    error
	stderr string
}

func (e *gitRunError) Error() string {
	return "git " + strings.Join(e.args, " ") + ": " + e.err.Error() + ": " + e.stderr
}

func (e *gitRunError) Unwrap() error { return e.err }

// isGitDirUnusable reports whether a git failure means git cannot inspect the
// work directory itself — it is not a repository, the repo is owned by
// another user ("dubious ownership"), or git cannot reach it. Those cases
// fall back to the bounded walk; a genuine transient failure still reports
// an empty fingerprint.
func isGitDirUnusable(err error) bool {
	s := err.Error()
	return strings.Contains(s, "not a git repository") ||
		strings.Contains(s, "not a working tree") ||
		strings.Contains(s, "repository does not exist") ||
		strings.Contains(s, "dubious ownership") ||
		strings.Contains(s, "cannot change to")
}

// gitStatusEntries parses the NUL-separated output of
// `git status --porcelain=v1 -z --untracked-files=all` and returns one
// hashable string per entry: path, status code, file size and mtime in
// nanoseconds. Scratch files (CHECKPOINT.md, .datawatch, .decompose-output*)
// are excluded. A stat failure keeps the entry with size 0 and mtime 0 so a
// vanished file still changes the fingerprint.
func gitStatusEntries(workDir string, out []byte) []string {
	var entries []string
	for _, raw := range bytes.Split(out, []byte{0}) {
		if len(raw) < 3 {
			continue
		}
		status := string(raw[:2])
		// raw[2] is the separating space; the rest is the path.
		relPath := string(raw[3:])
		if relPath == "" || isWorkDirScratchPath(relPath) {
			continue
		}
		size, mtimeNano := int64(0), int64(0)
		if fi, err := os.Stat(filepath.Join(workDir, relPath)); err == nil {
			size = fi.Size()
			mtimeNano = fi.ModTime().UnixNano()
		}
		entries = append(entries, buildWorkDirFingerprintEntry(relPath, status, size, mtimeNano))
	}
	if len(entries) == 0 {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entryKey(entries[i]) < entryKey(entries[j]) })
	return entries
}

// entryKey sorts by path; the path is the unique stable identity of an entry.
func entryKey(entry string) string {
	if i := strings.IndexByte(entry, '\x1f'); i >= 0 {
		return entry[:i]
	}
	return entry
}

const fingerprintSeparator = "\x1f"

func buildWorkDirFingerprintEntry(path, status string, size int64, mtimeNano int64) string {
	return path + fingerprintSeparator + status + fingerprintSeparator +
		strconv.FormatInt(size, 10) + fingerprintSeparator + strconv.FormatInt(mtimeNano, 10)
}

// sumEntries hashes the sorted entries into a hex string, empty for no
// entries.
func sumEntries(entries []string) string {
	if len(entries) == 0 {
		return ""
	}
	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte(e))
		h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

// workDirFingerprint returns (1) a stable fingerprint that changes whenever a
// tracked or untracked file in workDir changes — including re-editing an
// already-dirty file — and (2) the current HEAD commit sha ("" when workDir
// is not a git repository or has no commits).
//
// In a git repository the fingerprint is a sha256 over the sorted
// path+status+size+mtime(nanoseconds) entries of
// `git status --porcelain=v1 -z --untracked-files=all`, excluding the
// harness's scratch files (CHECKPOINT.md, .datawatch, .decompose-output*).
// When workDir is not a git repository (or git is not on PATH) it falls back
// to a bounded walk: hash of path+size+mtime over regular files, skipping
// .git and the same scratch files, capped at staleEvidenceMaxEntries
// entries. Any other git failure returns an empty fingerprint and empty head
// instead of panicking.
func workDirFingerprint(workDir string) (fingerprint, head string) {
	if workDir == "" {
		return "", ""
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return walkFingerprint(workDir), ""
	}
	statusOut, err := runGit(workDir, gitPath, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		if isGitDirUnusable(err) {
			return walkFingerprint(workDir), ""
		}
		// Some other git failure (broken config, unreadable dir, ...):
		// report no fingerprint rather than panic or guess.
		return "", ""
	}
	head = ""
	if headOut, herr := runGit(workDir, gitPath, "rev-parse", "HEAD"); herr == nil {
		head = strings.TrimSpace(headOut)
	}
	return sumEntries(gitStatusEntries(workDir, []byte(statusOut))), head
}

// walkFingerprint is the non-git fallback: a bounded walk over workDir
// hashing path+size+mtime(nanoseconds) of regular files. It skips .git and
// the harness's scratch files and stops after staleEvidenceMaxEntries
// entries, so a huge tree cannot stall the caller.
func walkFingerprint(workDir string) string {
	entries := make([]string, 0, 256)
	_ = filepath.WalkDir(workDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if len(entries) >= staleEvidenceMaxEntries {
			return fs.SkipAll
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == harnessScratchDir {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		relPath, rerr := filepath.Rel(workDir, path)
		if rerr != nil || isWorkDirScratchPath(relPath) {
			return nil
		}
		fi, ierr := d.Info()
		if ierr != nil {
			relPath += fingerprintSeparator + "?"
			entries = append(entries, relPath)
			return nil
		}
		entries = append(entries, buildWorkDirFingerprintEntry(relPath, "", fi.Size(), fi.ModTime().UnixNano()))
		return nil
	})
	return sumEntries(entries)
}
