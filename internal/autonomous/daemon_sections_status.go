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
// unsafe path (empty, absolute, escaping upward, NUL byte) — returns false.
func daemonSectionsOnlyChange(ctx context.Context, dir, rel string, run driftGitRunner) bool {
	if run == nil {
		return false
	}
	rel = normalizeDriftPath(rel)
	if _, ok := localRelPath(rel); !ok {
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

// filterDaemonOnlyPorcelain removes from line-based `git status --porcelain`
// output every record that reports a tracked file whose only difference from
// HEAD is the daemon's own marked blocks (daemonSectionsOnlyChange), and
// keeps every other line — untracked files, deletions, staged and renamed
// entries, and anything that cannot be established either way all stay as
// dirt. The result is the remaining dirt for a dirty-tree gate to judge: an
// empty result means the tree is clean as far as the gate should care, a
// non-empty one still refuses.
func filterDaemonOnlyPorcelain(ctx context.Context, dir string, porcelain string, run driftGitRunner) string {
	var kept []string
	for _, line := range strings.Split(porcelain, "\n") {
		if ignoreDaemonOnlyPorcelainLine(ctx, dir, line, run) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// ignoreDaemonOnlyPorcelainLine judges one line of `git status --porcelain`
// ("XY path"). Only an unstaged modification of a tracked file (" M") is a
// candidate: the daemon writes its marked blocks into the working copy and
// never stages. Every other record — staged, deleted, untracked, renamed, or
// a quoted/unparseable path — is kept, and a " M" line is dropped only when
// daemonSectionsOnlyChange positively establishes it as daemon-only. Fails
// closed: any doubt keeps the line.
func ignoreDaemonOnlyPorcelainLine(ctx context.Context, dir, line string, run driftGitRunner) bool {
	if len(line) < 4 || line[2] != ' ' || line[:2] != " M" {
		return false
	}
	p := line[3:]
	if p == "" || p[0] == '"' {
		return false // quoted paths carry git's C-style escapes; do not interpret them here
	}
	return daemonSectionsOnlyChange(ctx, dir, p, run)
}
