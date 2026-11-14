// protected_paths_checkpoint_revert.go — the protected-paths gate's
// checkpoint revert: when a task's changes to a protected rules file
// (AGENT.md, CLAUDE.md, or the configured rules file) consist only of
// worker-added checkpoint / progress entries, the gate strips those entries
// and lets the protected-paths question never fire for that file, instead of
// parking the task. The revert is line-level and deterministic, with no git
// write: the base version (read through baseCommitRunner) must be an ordered
// subsequence of the working copy, every added line must be part of a
// checkpoint entry block — a `## YYYY-MM-DD` block (optionally ` HH:MM`)
// running to the next non-dated `## ` heading or EOF — or a blank line
// adjacent to an entry, and at most one `## Checkpoints` heading when the
// base had none. Anything else keeps the hit and the normal hold/block/warn
// path decides.
package autonomous

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

// checkpointEntryRevertedDecisionKind is the Decision kind recorded, once
// per task and file, when the gate reverted a worker checkpoint entry from
// a protected file.
const checkpointEntryRevertedDecisionKind = "protected_paths_checkpoint_reverted"

// checkpointSectionHeading is the optional section heading a worker may
// add together with its first entry when the file does not use one.
const checkpointSectionHeading = "## Checkpoints"

// checkpointEntryHeadingRe matches the heading line that starts a
// checkpoint entry: `## YYYY-MM-DD`, optionally followed by ` HH:MM`, with
// an optional descriptive tail (both forms appear in the rules files).
var checkpointEntryHeadingRe = regexp.MustCompile(`^## \d{4}-\d{2}-\d{2}( \d{2}:\d{2}(:\d{2})?)?( .+)?$`)

func checkpointEntryHeading(line string) bool {
	return checkpointEntryHeadingRe.MatchString(line)
}

// checkpointCandidates returns the normalised repository-relative paths the
// gate may revert worker checkpoint entries from: AGENT.md, CLAUDE.md, and
// the configured rules file.
func checkpointCandidates(rulesFile string) map[string]bool {
	set := map[string]bool{
		"AGENT.md":  true,
		"CLAUDE.md": true,
	}
	if r := normalizeDriftPath(rulesFile); r != "" {
		set[r] = true
	}
	return set
}

// recordCheckpointEntryReverted records, once per task and path, that the
// gate removed a worker-added checkpoint entry from a protected file before
// the protected-paths question was decided.
func (m *Manager) recordCheckpointEntryReverted(prd *PRD, t *Task, path string) {
	prefix := ""
	if t != nil {
		prefix = "task " + t.ID + ": "
	}
	note := prefix + "removed a worker checkpoint entry from " + path
	seen := false
	m.store.WithLock(func() {
		for _, d := range prd.Decisions {
			if d.Kind == checkpointEntryRevertedDecisionKind && d.Note == note {
				seen = true
				return
			}
		}
	})
	if seen {
		return
	}
	log.Printf("[autonomous] %s %sprotected-paths: reverted a worker checkpoint entry from %s", prd.ID, prefix, path)
	m.appendUncommittedDecision(prd, checkpointEntryRevertedDecisionKind, note, "")
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s %sprotected-paths: save after checkpoint revert: %v", prd.ID, prefix, err)
	}
}

// revertCheckpointOnlyHits is the gate's checkpoint revert, called with the
// daemon-filtered hits. It returns the hits that are not cleanly
// revertible. A hit is reverted only when, on top of the base version
// (read through baseCommitRunner), the file contains only checkpoint
// additions: the worker-added entry lines, the blank lines separating them,
// and at most one added `## Checkpoints` heading when the base had none.
// The file is written back with its original bytes minus those added lines
// — the daemon's own sections are never touched — and the stripped result
// must equal the stripped base version; on any failure the original working
// copy is restored and the hit is kept. No git write command is issued.
func (m *Manager) revertCheckpointOnlyHits(ctx context.Context, prd *PRD, t *Task, dir, base string, hits []string) []string {
	rulesFile, _ := m.rulesAndContextFiles(dir)
	candidates := checkpointCandidates(rulesFile)
	run := m.baseCommitRunner()
	var remaining []string
	for _, p := range hits {
		rel := normalizeDriftPath(p)
		if !candidates[rel] {
			remaining = append(remaining, p)
			continue
		}
		full := filepath.Join(dir, filepath.FromSlash(rel))
		fi, err := os.Lstat(full)
		if ctx.Err() != nil || err != nil || !fi.Mode().IsRegular() {
			// Missing file, symlink or non-regular file: fail closed.
			remaining = append(remaining, p)
			continue
		}
		cur, err := os.ReadFile(full)
		if err != nil {
			remaining = append(remaining, p)
			continue
		}
		baseContent, err := run(ctx, dir, "show", base+":"+rel)
		if ctx.Err() != nil || err != nil {
			// Base version unreadable (new file or failure): fail closed.
			remaining = append(remaining, p)
			continue
		}
		cleaned, ok := stripWorkerCheckpointEntries(baseContent, string(cur))
		if !ok {
			// The worker changed base lines too: indistinguishable from a
			// real edit to the rules — held like one.
			remaining = append(remaining, p)
			continue
		}
		mode := fi.Mode().Perm()
		if err := os.WriteFile(full, []byte(cleaned), mode); err != nil {
			log.Printf("[autonomous] %s protected-paths: write %s after checkpoint revert: %v", prd.ID, rel, err)
			remaining = append(remaining, p)
			continue
		}
		got, verr := os.ReadFile(full)
		if verr != nil ||
			platformcontext.StripDaemonSections(string(got)) != platformcontext.StripDaemonSections(baseContent) {
			// Verification failed: restore the working copy as found and
			// keep the hit.
			if werr := os.WriteFile(full, cur, mode); werr != nil {
				log.Printf("[autonomous] %s protected-paths: restore %s failed: %v", prd.ID, rel, werr)
			}
			remaining = append(remaining, p)
			continue
		}
		m.recordCheckpointEntryReverted(prd, t, rel)
	}
	return remaining
}

// splitFileLines splits content on newlines, trimming a trailing CR from
// each line, and treats empty content as zero lines.
func splitFileLines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, "\r")
	}
	return lines
}

// stripWorkerCheckpointEntries is the pure line-level revert used by
// revertCheckpointOnlyHits. It reports ok and, when ok, a cleaned copy of
// cur with exactly the added checkpoint lines removed (the daemon's own
// sections are preserved line for line) when every addition to the base is
// a checkpoint entry line, a blank line adjacent to an entry, or a single
// `## Checkpoints` heading the base did not carry. ok is false when the
// base is not an ordered subsequence of the working copy (a line was
// changed or removed), or any addition is anything else.
func stripWorkerCheckpointEntries(base, cur string) (string, bool) {
	baseLines := splitFileLines(platformcontext.StripDaemonSections(base))
	curLines := splitFileLines(platformcontext.StripDaemonSections(cur))

	// The base version must be an ordered subsequence of the (stripped)
	// working copy: the worker only added lines, never rewrote or removed.
	matched := make([]bool, len(curLines))
	j := 0
	for i := range curLines {
		if j < len(baseLines) && curLines[i] == baseLines[j] {
			matched[i] = true
			j++
		}
	}
	if j != len(baseLines) {
		return "", false
	}
	added := make([]bool, len(curLines))
	anyAdded := false
	for i, m := range matched {
		if !m {
			added[i] = true
			anyAdded = true
		}
	}
	if !anyAdded {
		// Identical outside the daemon sections: withoutDaemonOnlyChanges
		// already kept this out; nothing to revert.
		return "", false
	}
	if !addedLinesOnlyCheckpointEntries(baseLines, curLines, added) {
		return "", false
	}

	// Drop the added lines from the working copy, never touching the lines
	// that belong to a daemon section.
	curFull := splitFileLines(cur)
	stripIdx := make([]int, len(curFull))
	for i := range stripIdx {
		stripIdx[i] = -1
	}
	k := 0
	for i := range curFull {
		if k < len(curLines) && curFull[i] == curLines[k] {
			stripIdx[i] = k
			k++
		}
	}
	if k != len(curLines) {
		// The working copy does not line up with its stripped form
		// (ambiguous blanks): fail closed.
		return "", false
	}
	kept := make([]string, 0, len(curFull))
	for i, l := range curFull {
		if s := stripIdx[i]; s != -1 && added[s] {
			continue
		}
		kept = append(kept, l)
	}
	out := strings.Join(kept, "\n")
	if len(kept) > 0 {
		out += "\n"
	}
	return out, true
}

// checkpointTag marks what an added line is in addedLinesOnlyCheckpointEntries.
type checkpointTag byte

const (
	tagNone checkpointTag = iota
	tagEntry
	tagHeading
)

// addedLinesOnlyCheckpointEntries reports whether every added line is a
// checkpoint entry line, a blank line adjacent to an entry (or to the
// added `## Checkpoints` heading), or that heading itself at most once and
// only when the base did not already carry one. Entry blocks run from a
// `## YYYY-MM-DD` heading (optional ` HH:MM`) to the next line starting
// with `## ` that is not itself such a dated heading, or to the end.
func addedLinesOnlyCheckpointEntries(baseLines, curLines []string, added []bool) bool {
	baseHasHeading := false
	for _, l := range baseLines {
		if strings.TrimSpace(l) == checkpointSectionHeading {
			baseHasHeading = true
			break
		}
	}
	tags := make([]checkpointTag, len(curLines))
	openEntry := false
	headingCount := 0
	for i := range curLines {
		if !added[i] {
			// A base line closes any entry block open so far.
			openEntry = false
			continue
		}
		line := strings.TrimSpace(curLines[i])
		if line == "" {
			continue // blank lines are checked after the entry sweep
		}
		if checkpointEntryHeading(line) {
			openEntry = true
			tags[i] = tagEntry
			continue
		}
		if line == checkpointSectionHeading {
			headingCount++
			if headingCount > 1 || baseHasHeading {
				return false
			}
			tags[i] = tagHeading
			continue
		}
		if strings.HasPrefix(line, "## ") {
			// A new section that is not a dated checkpoint heading.
			return false
		}
		if !openEntry {
			// Body text outside any entry block.
			return false
		}
		tags[i] = tagEntry
	}
	for i := range curLines {
		if !added[i] || strings.TrimSpace(curLines[i]) != "" {
			continue
		}
		before := i > 0 && added[i-1] && (tags[i-1] == tagEntry || tags[i-1] == tagHeading)
		after := i+1 < len(curLines) && added[i+1] && (tags[i+1] == tagEntry || tags[i+1] == tagHeading)
		if !before && !after {
			return false
		}
	}
	return true
}
