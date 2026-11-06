package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// readOnlyReportPresent reports whether a read-only task wrote its declared
// report file under treeRoot. It applies the executor's rules: the report
// path counts only for a read-only task and only when it is valid (repo
// relative, clean, no .. segment, under .datawatch/reports/). The file is
// checked with Lstat so a symlink or directory is refused; it must be a
// regular file with size > 0. declared is the (non-empty) report path of a
// read-only task, whether or not the file is usable.
func readOnlyReportPresent(t *autonomouspkg.Task, treeRoot string) (present bool, declared string) {
	rel := autonomouspkg.ReportPathOf(t)
	if rel == "" || treeRoot == "" {
		return false, ""
	}
	if autonomouspkg.ValidateReportPath(rel) != nil {
		return false, rel
	}
	fi, err := os.Lstat(filepath.Join(treeRoot, filepath.FromSlash(rel)))
	if err != nil || !fi.Mode().IsRegular() || fi.Size() <= 0 {
		return false, rel
	}
	return true, rel
}

// verdictForReadOnlyReport builds the verifier verdict for a task that
// produced no diff. handled is false when the caller should fall through to
// its existing behavior. When a report is present the verdict is OK (the
// caller must still honor the task's own verification script, see
// scriptFailure); when a read-only task declared a report that is absent or
// empty the returned verdict is a failure naming the report path.
func verdictForReadOnlyReport(present bool, declared string) (autonomouspkg.VerificationResult, bool) {
	switch {
	case declared == "":
		return autonomouspkg.VerificationResult{}, false
	case present:
		return autonomouspkg.VerificationResult{
			OK:         true,
			Severity:   "low",
			Summary:    fmt.Sprintf("verifier: read-only task produced no tree changes; declared report %s exists and is non-empty", declared),
			VerifiedAt: time.Now(),
		}, true
	default:
		return autonomouspkg.VerificationResult{
			OK:         false,
			Severity:   "medium",
			Summary:    fmt.Sprintf("verifier: read-only task's report file %s is missing or empty (or its path is invalid); task produced no output", declared),
			VerifiedAt: time.Now(),
		}, true
	}
}
