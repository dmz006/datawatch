package testhome

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// manifestName is the file in the process temp directory that remembers
// every throwaway directory testhome created. A test binary that dies
// hard (SIGKILL, OOM) skips its deferred cleanup, so Run asks SweepStale
// at the next start to reclaim the strays the manifest still lists.
const manifestName = "dw-test-manifest"

// manifestPath is the manifest file. Kept as a variable so tests can
// point it at their own tree instead of the shared real one.
var manifestPath = filepath.Join(os.TempDir(), manifestName)

// Record appends dir as one line (O_APPEND) to the manifest, creating it
// with mode 0600 if missing. It is best-effort by design: a failure only
// loses a future sweep of that one directory, so it is reported on
// stderr and never fatal.
func Record(dir string) {
	f, err := os.OpenFile(manifestPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "home isolation: WARNING: could not record %s: %v\n", dir, err)
		return
	}
	if _, err := fmt.Fprintf(f, "%s\n", dir); err != nil {
		fmt.Fprintf(os.Stderr, "home isolation: WARNING: recording %s: %v\n", dir, err)
	}
}

// forget prunes the manifest on the clean-exit path of Run: it drops dir's
// own line plus any line whose directory no longer exists (a scratch dir
// that was created and removed during the run, or another test binary's
// home that was already cleaned up), and deletes the manifest file when
// nothing is left. A hard-killed run never reaches it, and such a run's
// live homes still exist, so their lines survive for the next sweep.
func forget(dir string) {
	lines, err := readManifest(manifestPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "home isolation: WARNING: forget: %v\n", err)
		}
		return
	}
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if l == dir {
			continue
		}
		if fi, err := os.Stat(l); err != nil || !fi.IsDir() {
			// Already gone: nothing left to sweep, drop the stale line so a
			// fully clean run can delete the manifest entirely.
			continue
		}
		kept = append(kept, l)
	}
	if len(kept) == 0 {
		if err := os.Remove(manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "home isolation: WARNING: forget: %v\n", err)
		}
		return
	}
	if err := writeManifest(manifestPath, kept); err != nil {
		fmt.Fprintf(os.Stderr, "home isolation: WARNING: forget: %v\n", err)
	}
}

// SweepStale reclaims the throwaway directories it remembers from an
// earlier run that no longer exist under any live test process. For each
// dir in the manifest it deletes dir only if every one of the following
// holds: it still exists, it is a directory (never a symlink), it is a
// direct child of tmpDir whose base name starts with "dw-test-home-" or
// "dw-test-tmux-", it is owned by the current user, and its mtime is
// older than maxAge as of now. A dir that fails any check, or that
// cannot be removed, is kept. Afterwards the manifest is rewritten with
// the survivors. A missing manifest is not an error: there is simply
// nothing to sweep.
func SweepStale(tmpDir string, maxAge time.Duration, now time.Time) error {
	tmp, err := filepath.Abs(tmpDir)
	if err != nil {
		return err
	}
	lines, err := readManifest(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	survivors := make([]string, 0, len(lines))
	for _, dir := range lines {
		if dir == "" {
			continue
		}
		// A line whose directory is already gone has nothing left to sweep
		// for: dropping it lets a fully clean run delete the manifest
		// entirely instead of leaving a bookkeeping file in the temp dir.
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue
		}
		if sweepable(tmp, dir, maxAge, now) {
			if remErr := Remove(dir); remErr == nil {
				continue // reclaimed: not a survivor
			} else {
				// Keep it in the manifest for the next attempt.
				fmt.Fprintf(os.Stderr, "home isolation: WARNING: could not sweep %s: %v\n", dir, remErr)
			}
		}
		survivors = append(survivors, dir)
	}
	return writeManifest(manifestPath, survivors)
}

// sweepable reports whether dir passes every safety check that
// SweepStale requires before removal.
func sweepable(tmp, dir string, maxAge time.Duration, now time.Time) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	// Direct child of tmpDir: nothing else (no nested paths, no other
	// roots, no traversal) is ever a candidate.
	if filepath.Dir(abs) != tmp {
		return false
	}
	base := filepath.Base(abs)
	if !strings.HasPrefix(base, "dw-test-home-") && !strings.HasPrefix(base, "dw-test-tmux-") {
		return false
	}
	// Lstat: a symlink named appropriately is not the directory we made.
	fi, err := os.Lstat(abs)
	if err != nil || !fi.IsDir() {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) {
		return false
	}
	return now.Sub(fi.ModTime()) > maxAge
}

func readManifest(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

func writeManifest(path string, lines []string) error {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
