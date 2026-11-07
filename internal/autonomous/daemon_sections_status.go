// Report whether a tracked file differs from HEAD only by the daemon's own
// sections.
//
// daemonSectionsOnlyChange answers one question for a single path: is the
// working copy different from its HEAD copy, and is that difference confined
// to the blocks the daemon injects (platform context, RTK, cross-session,
// Memory, Session Guardrails)? It needs no Manager: StripDaemonSections removes
// only the daemon's marked blocks, so the comparison is safe for any tracked
// path. It reuses the read pattern of pathspecWithoutDaemonOnlyChanges
// (daemon_sections_commit.go) and withoutDaemonOnlyChanges
// (protected_paths_daemon_sections.go) and fails closed: whenever the answer
// cannot be established, it is false.
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

// daemonSectionsOnlyChange returns true only when the working copy of rel (under
// dir) exists and is readable, `git show HEAD:rel` through run succeeds, the
// two copies are equal after StripDaemonSections, and the raw contents differ
// (an unchanged file is not a daemon-only change). Every other case — missing
// or unreadable working copy, file absent from HEAD, cancelled context, or an
// unsafe path (absolute, '..' prefix, NUL byte) — returns false.
func daemonSectionsOnlyChange(ctx context.Context, dir, rel string, run driftGitRunner) bool {
	if run == nil {
		return false
	}
	rel = normalizeDriftPath(rel)
	if rel == "" || strings.HasPrefix(rel, "/") || rel == ".." || strings.HasPrefix(rel, "../") || strings.ContainsRune(rel, 0) {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	cur, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	head, err := run(ctx, dir, "show", "HEAD:"+rel)
	if err != nil {
		return false
	}
	if string(cur) == head {
		return false
	}
	return platformcontext.StripDaemonSections(string(cur)) == platformcontext.StripDaemonSections(head)
}
