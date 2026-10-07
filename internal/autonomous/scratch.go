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
	"fmt"
	"os"
	"path/filepath"
)

// ScratchDir returns (creating if needed) the durable, PRD-scoped scratch
// directory for prdID under dataDir. Safe to call repeatedly.
func ScratchDir(dataDir, prdID string) (string, error) {
	if dataDir == "" || prdID == "" {
		return "", fmt.Errorf("scratch dir: dataDir and prdID are both required")
	}
	dir := filepath.Join(dataDir, "autonomous", "scratch", prdID)
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
