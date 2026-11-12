// Skipped snapshot paths, compact text form. The CLI (autonomous prd-get)
// and the comm channel (autonomous get) render the body of
// GET /api/autonomous/prds/{id} through this file, so no surface shows a
// different wording; the PWA already labels the same field "Left unstaged".
// The count-first line goes on the text surfaces, the detailed path list
// stays on the raw JSON read only.

package autonomous

import (
	"encoding/json"
	"fmt"
)

// SkippedPathsCounts renders a task's skipped snapshot paths as the
// count-first gate report: one "reason=count" pair per distinct reason,
// sorted for a stable line (e.g. "ignored=1, size=2"). "" when nothing was
// skipped. The same shape stagingGuardCounts prints for the in-run guard
// line, exported so the CLI and comm channels share one wording.
func SkippedPathsCounts(paths []SnapshotSkippedPath) string {
	reasons := make([]string, 0, len(paths))
	for _, p := range paths {
		reasons = append(reasons, p.Reason)
	}
	return stagingGuardCounts(reasons)
}

// RenderSkippedSnapshotLines reads the body of GET /api/autonomous/prds/{id}
// (the tasks of the stories in the full view, or the flat tasks list of the
// tasks view) and returns one compact, counts-first line per task with a
// non-empty skipped_snapshot_paths:
// "left unstaged: <task> N (reason=count, ...)".
// The detailed path list stays on the raw JSON only. Nil when no task
// skipped anything, or the body is not JSON.
func RenderSkippedSnapshotLines(prdJSON []byte) []string {
	type task struct {
		ID                   string                `json:"id"`
		SkippedSnapshotPaths []SnapshotSkippedPath `json:"skipped_snapshot_paths"`
	}
	var v struct {
		Stories []struct {
			Tasks []task `json:"tasks"`
		} `json:"stories"`
		Tasks []task `json:"tasks"`
	}
	if err := json.Unmarshal(prdJSON, &v); err != nil {
		return nil
	}
	all := append([]task(nil), v.Tasks...)
	for _, s := range v.Stories {
		all = append(all, s.Tasks...)
	}
	var out []string
	for _, t := range all {
		if len(t.SkippedSnapshotPaths) == 0 {
			continue
		}
		out = append(out, fmt.Sprintf("left unstaged: %s %d (%s)",
			t.ID, len(t.SkippedSnapshotPaths), SkippedPathsCounts(t.SkippedSnapshotPaths)))
	}
	return out
}
