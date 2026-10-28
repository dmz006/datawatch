package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// staleEvidenceMaxEntries caps how many directory entries workerActivityEvidence
// inspects under the work directory. A huge tree must not stall the wait loop,
// so the walk gives up after this many entries and treats what it has seen as
// the whole picture.
const staleEvidenceMaxEntries = 5000

// workerActivityEvidence reports whether a running worker has shown no sign of
// life for longer than threshold. The worker is NOT stalled if any one of these
// holds:
//
//   - the log file's mtime is within threshold (the worker wrote output);
//   - childAlive reports a runner child process is still running;
//   - backendBusy reports the backend is still busy;
//   - any regular file under workDir has an mtime within threshold (the worker
//     changed files on disk even though it printed nothing).
//
// Directories named .git are skipped during the workDir walk, and the walk stops
// after staleEvidenceMaxEntries entries. A nil childAlive or backendBusy counts
// as "not alive" / "not busy".
//
// When the worker is stalled, reason reads
// "no output, no children, no file changes for N seconds", where N is the whole
// seconds since the newest evidence timestamp (the newer of the log mtime and
// the newest workDir file mtime). If neither timestamp exists, N is threshold.
//
// threshold <= 0 disables the check and never reports a stall, matching
// staleWorkerCheck.
func workerActivityEvidence(logPath, workDir string, childAlive func() bool, backendBusy func() bool, threshold time.Duration, now time.Time) (stalled bool, reason string) {
	if threshold <= 0 {
		return false, ""
	}

	var newest time.Time
	if logPath != "" {
		if fi, err := os.Stat(logPath); err == nil {
			newest = fi.ModTime()
			if now.Sub(newest) <= threshold {
				return false, ""
			}
		}
	}

	if childAlive != nil && childAlive() {
		return false, ""
	}
	if backendBusy != nil && backendBusy() {
		return false, ""
	}

	if workDir != "" {
		fileNewest, recent := newestWorkDirFileMtime(workDir, now, threshold)
		if recent {
			return false, ""
		}
		if fileNewest.After(newest) {
			newest = fileNewest
		}
	}

	idle := threshold
	if !newest.IsZero() {
		idle = now.Sub(newest)
	}
	return true, fmt.Sprintf("no output, no children, no file changes for %d seconds", int64(idle.Seconds()))
}

// newestWorkDirFileMtime walks workDir and returns the newest mtime among its
// regular files. It skips .git directories and stops after
// staleEvidenceMaxEntries entries. recent is true as soon as a file's mtime is
// within threshold, so the walk can end early without finding the true newest.
func newestWorkDirFileMtime(workDir string, now time.Time, threshold time.Duration) (newest time.Time, recent bool) {
	entries := 0
	_ = filepath.WalkDir(workDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entries >= staleEvidenceMaxEntries {
			return fs.SkipAll
		}
		entries++
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fi, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		if now.Sub(fi.ModTime()) <= threshold {
			recent = true
			return fs.SkipAll
		}
		return nil
	})
	return newest, recent
}

// newestWorkDirMtime walks workDir and returns the newest regular file's mtime.
// Like newestWorkDirFileMtime it skips .git directories and stops after
// staleEvidenceMaxEntries entries, except that it has no recency verdict to
// short-circuit on: the caller needs the actual newest timestamp, not a
// within-threshold yes/no. An unreadable tree or one with no files yields the
// zero time, which the caller must treat as "no such evidence" — never as a
// timestamp newer than anything real.
func newestWorkDirMtime(workDir string) time.Time {
	var newest time.Time
	entries := 0
	_ = filepath.WalkDir(workDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entries >= staleEvidenceMaxEntries {
			return fs.SkipAll
		}
		entries++
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fi, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		return nil
	})
	return newest
}
