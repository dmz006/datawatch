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
// unsafe path (empty, absolute, escaping upward, NUL byte, or a symlink chain
// leaving dir) — returns false.
func daemonSectionsOnlyChange(ctx context.Context, dir, rel string, run driftGitRunner) bool {
	if run == nil {
		return false
	}
	rel = normalizeDriftPath(rel)
	if rel == "" || strings.ContainsRune(rel, 0) || strings.ContainsRune(dir, 0) || dir == "" {
		return false
	}
	relOS := filepath.Clean(filepath.FromSlash(rel))
	if !filepath.IsLocal(relOS) {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	// Containment is checked inline against the symlink-resolved base: the
	// resolved target must sit under the base plus the separator, so a
	// sibling directory named like the base is refused.
	realBase, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	realBase = filepath.Clean(realBase)
	curPath, err := filepath.EvalSymlinks(filepath.Join(realBase, relOS))
	if err != nil {
		return false
	}
	if !strings.HasPrefix(curPath, realBase+string(filepath.Separator)) {
		return false
	}
	cur, err := os.ReadFile(curPath) // #nosec G304 -- curPath is cleaned, local and resolved inside the resolved base above
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

// daemonSectionsOnlyUntracked is the untracked sibling of
// daemonSectionsOnlyChange: for a file git reports as untracked ("??") it
// answers whether the file holds nothing of the task's own — its content, after
// platformcontext.StripDaemonSections removes the daemon's marked blocks
// (platform context, RTK, cross-session, Memory, Session Guardrails), is empty
// or whitespace-only. A rules file that exists only to carry the daemon's
// injected sections is not the task's work. It needs no git: the working copy
// alone is enough, so it needs no runner. It fails closed like its sibling: a
// missing or unreadable working copy, an unsafe path (empty, absolute,
// escaping upward, NUL byte) or a cancelled context all return false.
func daemonSectionsOnlyUntracked(ctx context.Context, dir, rel string) bool {
	if ctx.Err() != nil {
		return false
	}
	rel = normalizeDriftPath(rel)
	if rel == "" || strings.ContainsRune(rel, 0) || strings.ContainsRune(dir, 0) || dir == "" {
		return false
	}
	relOS := filepath.Clean(filepath.FromSlash(rel))
	if !filepath.IsLocal(relOS) {
		return false
	}
	realBase, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	realBase = filepath.Clean(realBase)
	curPath, err := filepath.EvalSymlinks(filepath.Join(realBase, relOS))
	if err != nil {
		return false
	}
	if !strings.HasPrefix(curPath, realBase+string(filepath.Separator)) {
		return false
	}
	cur, err := os.ReadFile(curPath) // #nosec G304 -- curPath is cleaned, local and resolved inside the resolved base above
	if err != nil {
		return false
	}
	stripped := platformcontext.StripDaemonSections(string(cur))
	return strings.TrimSpace(stripped) == ""
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
// ("XY path"). Two records may be dropped: an untracked file ("??") that is
// one of the launcher's own root files (isLauncherToolingFile), and an
// unstaged modification of a tracked file (" M") that differs from HEAD only
// by the daemon's marked blocks (daemonSectionsOnlyChange). Every other
// record — other untracked paths, staged, deleted, renamed, or a
// quoted/unparseable path — is kept. Fails closed: any doubt keeps the line.
func ignoreDaemonOnlyPorcelainLine(ctx context.Context, dir, line string, run driftGitRunner) bool {
	if len(line) < 4 || line[2] != ' ' {
		return false
	}
	if p := line[3:]; p == "" || p[0] == '"' {
		return false // quoted paths carry git's C-style escapes; do not interpret them here
	}
	if line[:2] == "??" {
		return isLauncherToolingFile(line[3:])
	}
	if line[:2] != " M" {
		return false
	}
	return daemonSectionsOnlyChange(ctx, dir, line[3:], run)
}
