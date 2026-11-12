// Uncommitted-work check at Automaton finish.
//
// The per-task check (checkUncommittedWork) looks at one task's files. This
// one runs once, in the roll-up, before an Automaton is reported finished: it
// looks at the whole worktree, so files that no single task owns (or that a
// cancel or a backend move left behind) stop the Automaton from reaching
// completed, and so from being listed as merge ready. It never stages or
// commits anything; the operator decides what happens to the files.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

// uncommittedRollupReason is the status-change reason of a roll-up that
// blocked on uncommitted files.
const uncommittedRollupReason = "uncommitted_work"

// UncommittedCauseAutomatonFinished is the cause recorded when the whole
// worktree still holds uncommitted files as the Automaton finishes.
const UncommittedCauseAutomatonFinished = "files were still uncommitted when the Automaton finished"

// checkWorktreeUncommittedAtFinish runs the uncommitted-work check over the
// whole worktree of an Automaton whose tasks are all terminal. It returns
// blocked=true when the Automaton must not complete: the policy is hold, the
// worktree is dirty, and the hold has been recorded (the caller commits the
// blocked status). It returns blocked=false when the Automaton may complete:
// the policy is off (no git call is made), the check does not apply, git
// cannot read the status, the worktree is clean, or the policy is warn and a
// warning was recorded.
//
// The check applies only to an Automaton with a local worktree: it needs a
// work branch, no cluster profile and a project directory. Scope filtering
// does not apply here (the scope setting governs only the per-task check), and
// nothing is committed.
//
// The daemon's own generated documents a person has not changed do not count
// as uncommitted work, and the question lists only the files that count.
func (m *Manager) checkWorktreeUncommittedAtFinish(ctx context.Context, prd *PRD) (blocked bool, err error) {
	policy := m.effectiveUncommittedWorkPolicy(prd)
	if policy == config.UncommittedWorkPolicyOff {
		return false, nil
	}
	if prd.Git.Branch == "" || prd.ClusterProfile != "" || prd.ProjectDir == "" {
		log.Printf("[autonomous] %s uncommitted-work: roll-up check skipped, the Automaton has no local worktree", prd.ID)
		return false, nil
	}

	files, skippedMarkers, derr := DetectUncommittedWorkSkipping(ctx, prd.ProjectDir, m.driftGit)
	if derr != nil {
		log.Printf("[autonomous] %s uncommitted-work: roll-up check skipped: %v", prd.ID, derr)
		return false, nil
	}
	files = dropUneditedGeneratedFiles(prd.ProjectDir, files)
	if len(files) == 0 {
		return false, nil
	}

	cause := UncommittedCauseAutomatonFinished
	if note := skippedMarkerFilesNote(skippedMarkers); note != "" {
		cause += "; " + note
	}

	if policy == config.UncommittedWorkPolicyWarn {
		m.warnUncommittedWork(prd, nil, files, cause)
		return false, nil
	}

	m.holdAutomatonForUncommittedWork(prd, files, cause)
	// A hold that was already open is left as it was; show the files as they
	// are now.
	all, outside := uncommittedFilePaths(files)
	m.store.WithLock(func() {
		if prd.UncommittedWork != nil {
			prd.UncommittedWork.Files = all
			prd.UncommittedWork.OutsideScope = outside
		}
	})
	return true, nil
}

// excludeUneditedGeneratedArtefacts removes from the files the finished check
// looks at the generated artefacts the daemon itself wrote and nobody has
// edited since — together with the recording hash sidecar of each of them.
// They are the daemon's own records, not work the Automaton left uncommitted.
func excludeUneditedGeneratedArtefacts(dir string, files []UncommittedFile) []UncommittedFile {
	if dir == "" {
		return files
	}
	kept := make([]UncommittedFile, 0, len(files))
	for _, f := range files {
		if uneditedGeneratedFile(dir, f.Path) {
			continue
		}
		kept = append(kept, f)
	}
	return kept
}

// uneditedGeneratedFile reports whether rel (relative to dir) is a generated
// artefact or its recording hash sidecar, and the daemon wrote it unedited:
// the artefact still carries the marker and no edited line, and its body
// still matches the hash recorded beside it. A person edit (or any other
// change) to either file makes both count as uncommitted work again.
func uneditedGeneratedFile(dir, rel string) bool {
	base := filepath.Base(rel)
	if strings.HasSuffix(base, generatedSidecarSuffix) {
		// The sidecar is exempt with its artefact: reverse the sidecar name
		// (dir/.name.genhash -> dir/name) and test that.
		artefact := filepath.Join(filepath.Dir(rel), strings.TrimPrefix(strings.TrimSuffix(base, generatedSidecarSuffix), "."))
		return genartifacts.IsUneditedGenerated(dir, artefact)
	}
	return genartifacts.IsUneditedGenerated(dir, rel)
}

// generatedSidecarSuffix is the suffix of the recording hash file the daemon
// keeps next to a generated artefact (<dir>/.<name>.genhash).
const generatedSidecarSuffix = ".genhash"

// uncommittedRollupBlockedReason is the plain sentence stored as the
// Automaton's blocked reason after the roll-up check held it: the cause, the
// files, and what to do.
func uncommittedRollupBlockedReason(prd *PRD) string {
	var hold *UncommittedWorkHold
	if prd != nil {
		hold = prd.UncommittedWork
	}
	if hold == nil || len(hold.Files) == 0 {
		return "Files are still uncommitted in the worktree. Commit or discard them, then run the Automaton again."
	}
	return fmt.Sprintf("%s: %s. Commit or discard them, then run the Automaton again.",
		hold.Reason, boundedFileList(hold.Files, uncommittedQuestionFiles))
}
