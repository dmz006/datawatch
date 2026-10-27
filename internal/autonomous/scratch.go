// Scratch artifacts datawatch itself writes during PRD decompose/execution
// (decompose output, checkpoint notes) were landing directly in the
// worker session's project_dir -- a shared, operator-owned repository
// that multiple PRDs can point at. Operator-reported (2026-10-07): these
// files polluted git status, never got cleaned up, and nothing prevented
// two PRDs sharing a project_dir from treading on each other's scratch
// state.
//
// Fix: a durable, PRD-scoped home under the daemon's own data directory
// (<data_dir>/autonomous/scratch/<prd_id>/), separate from the project
// repo entirely. The constraint this has to work around: a worker
// session's own tool access is sandboxed to project_dir, so it can only
// ever WRITE these files there -- it has no way to reach the data
// directory directly. The resolution is per-file:
//
//   - .decompose-output-*.json: datawatch's own Go code reads it exactly
//     once, right after the spawned session finishes. Nothing else ever
//     needs to read it from project_dir again, so it's safe to relocate
//     immediately after that one read (see cmd/datawatch/main.go's
//     decomposeFnSession).
//
//   - CHECKPOINT.md: by design, a LATER worker session (a fresh retry
//     after the prior one failed/crashed) needs to read it to resume --
//     and that later session is *also* sandboxed to project_dir, so it
//     can't read a data-dir copy either. Moving it there directly would
//     break resumability. Instead: datawatch itself (which has no
//     sandbox) reads it right after each attempt ends, relocates it to
//     the scratch dir for the durable record, and folds its content
//     directly into the next attempt's retry hint -- so the next session
//     gets the checkpoint as literal prompt text and never needs to read
//     any file to resume. See executor.go's relocateCheckpoint.
package autonomous

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// errBadPathElement is returned for a name or PRD id that is not a single
// safe path element.
var errBadPathElement = errors.New("not a single safe path element")

// validPathElement reports whether s may be used as one path element: not
// empty, not '.', no path separator, no NUL byte and no '..'.
func validPathElement(s string) bool {
	return s != "" && s != "." &&
		!strings.ContainsAny(s, "/\\\x00") &&
		!strings.Contains(s, "..")
}

// containedElement joins the single path element name under base and checks
// the result with gitguard.ContainedPath. name that is not a single safe path
// element, or a result that lies outside base, is refused before any file is
// touched. The returned path is the checked one and is what the caller must
// use.
func containedElement(base, name string) (string, error) {
	if !validPathElement(name) {
		return "", fmt.Errorf("%w: %q", errBadPathElement, name)
	}
	return gitguard.ContainedPath(base, filepath.Join(base, name))
}

// scratchRoot is <dataDir>/autonomous/scratch, the parent of every PRD's
// scratch directory.
func scratchRoot(dataDir string) string {
	return filepath.Join(dataDir, "autonomous", "scratch")
}

// scratchPRDDir is the checked scratch directory path for prdID. prdID comes
// from the API, so it must be one safe path element.
func scratchPRDDir(dataDir, prdID string) (string, error) {
	return containedElement(scratchRoot(dataDir), prdID)
}

// ScratchDir returns (creating if needed) the durable, PRD-scoped scratch
// directory for prdID under dataDir. Safe to call repeatedly.
func ScratchDir(dataDir, prdID string) (string, error) {
	if dataDir == "" || prdID == "" {
		return "", fmt.Errorf("scratch dir: dataDir and prdID are both required")
	}
	dir, err := scratchPRDDir(dataDir, prdID)
	if err != nil {
		return "", fmt.Errorf("scratch dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("mkdir scratch dir: %w", err)
	}
	return dir, nil
}

// RelocateProjectFile reads <projectDir>/<srcName> if present, copies its
// content into <scratchDir>/<destName> (best-effort -- a write failure
// there doesn't block the relocation's real goal, which is getting the
// file OUT of projectDir), deletes the projectDir copy, and returns the
// content. Best-effort throughout: a session-written scratch file is a
// nicety, never load-bearing for task completion, so any I/O error here
// just returns ("", false) rather than propagating.
func RelocateProjectFile(projectDir, srcName, scratchDir, destName string) (string, bool) {
	if projectDir == "" || srcName == "" {
		return "", false
	}
	src := filepath.Join(projectDir, srcName)
	content, err := os.ReadFile(src) // #nosec G304 -- srcName is a fixed internal constant, projectDir is this daemon's own PRD record
	if err != nil || len(content) == 0 {
		return "", false
	}
	if scratchDir != "" && destName != "" {
		dst := filepath.Join(scratchDir, destName)
		_ = os.WriteFile(dst, content, 0o600) // #nosec G304 G703 -- scratchDir/destName are built from internal identifiers (data dir + PRD/task IDs), not external input
	}
	_ = os.Remove(src)
	return string(content), true
}

// decomposeOutputName is the artifact the decompose session's output is
// relocated to (see RelocateProjectFile's destName in decomposeFnSession).
const decomposeOutputName = "decompose-output.json"

// decomposeOutputFailedName is where a decompose output that failed
// validation is kept for forensics.
const decomposeOutputFailedName = decomposeOutputName + ".failed"

// MarkDecomposeOutputArtifactFailed keeps a failed decompose's output for
// post-mortem: it renames <dataDir>/autonomous/scratch/<prdID>/
// decompose-output.json to decompose-output.json.failed. When that target
// already exists from an earlier failure, the name gains a unix-seconds
// suffix (and a counter if even that collides), so no earlier artifact is
// ever overwritten. Returns the final path, or ("", nil) when there was no
// artifact to keep.
func MarkDecomposeOutputArtifactFailed(dataDir, prdID string) (string, error) {
	if dataDir == "" || prdID == "" {
		return "", fmt.Errorf("mark decompose output: dataDir and prdID are both required")
	}
	dir, err := scratchPRDDir(dataDir, prdID)
	if err != nil {
		return "", fmt.Errorf("mark decompose output: %w", err)
	}
	src, err := containedElement(dir, decomposeOutputName)
	if err != nil {
		return "", fmt.Errorf("mark decompose output: %w", err)
	}
	if _, err := os.Stat(src); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("stat decompose output: %w", err)
	}
	dst := filepath.Join(dir, decomposeOutputFailedName)
	if _, err := os.Stat(dst); err == nil {
		stamp := time.Now().Unix()
		dst = filepath.Join(dir, fmt.Sprintf("%s-%d", decomposeOutputFailedName, stamp))
		for n := 1; ; n++ {
			if _, err := os.Stat(dst); errors.Is(err, fs.ErrNotExist) {
				break
			}
			dst = filepath.Join(dir, fmt.Sprintf("%s-%d-%d", decomposeOutputFailedName, stamp, n))
		}
	}
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("rename decompose output: %w", err)
	}
	return dst, nil
}

// RemoveDecomposeOutputArtifact deletes <dataDir>/autonomous/scratch/
// <prdID>/decompose-output.json once its stories are confirmed persisted.
// Best-effort: a missing file is not an error, and any other failure only
// leaves a stale scratch file behind, which never blocks the decompose.
func RemoveDecomposeOutputArtifact(dataDir, prdID string) {
	if dataDir == "" || prdID == "" {
		return
	}
	dir, err := scratchPRDDir(dataDir, prdID)
	if err != nil {
		return
	}
	target, err := containedElement(dir, decomposeOutputName)
	if err != nil {
		return
	}
	_ = os.Remove(target)
}

// PeekProjectFile returns the content of <projectDir>/<srcName> and true when
// the file exists and is non-empty. It never deletes or moves the file: the
// keep-until-recorded contract means the caller removes the project copy only
// after the content is durably recorded elsewhere (see RemoveJudgeOutputFile).
// It returns ("", false) for empty arguments, a missing file, an empty file,
// or any read error.
func PeekProjectFile(projectDir, srcName string) (string, bool) {
	if projectDir == "" || srcName == "" {
		return "", false
	}
	content, err := os.ReadFile(filepath.Join(projectDir, srcName)) // #nosec G304 -- srcName is a fixed internal constant, projectDir is this daemon's own PRD record
	if err != nil || len(content) == 0 {
		return "", false
	}
	return string(content), true
}

// RemoveJudgeOutputFile deletes the file at path. It is the companion to
// PeekProjectFile under the keep-until-recorded contract: call it only once
// the content has been recorded. A missing file is not an error and returns
// nil; any other failure is returned to the caller.
func RemoveJudgeOutputFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
