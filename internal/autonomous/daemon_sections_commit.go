// Keep the daemon's own rules-file sections out of task commits.
//
// The datawatch daemon rewrites a session's instructions file (CLAUDE.md,
// AGENT.md, …) at session start and injects sections of its own (platform
// context, RTK, cross-session rules, memory, session guardrails). By the time
// a task completes, that injection can be the only difference between a rules
// file's working copy and its HEAD copy. Those sections are the daemon's, not
// the task's: committing them records a change nobody in the task made, and
// the operator cannot review it as their own edit.
//
// pathspecWithoutDaemonOnlyChanges compares each path's working copy with its
// HEAD copy after stripping the daemon sections from both and drops the path
// when the two are equal. Everything else — real edits, deletions, additions,
// unreadable files — stays in the pathspec: fail closed, and when we cannot
// tell, the path is committed. The worktree is never cleaned up here (no
// reset, checkout, or stash); this only decides which paths a commit takes.
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

// pathspecWithoutDaemonOnlyChanges returns paths minus the files whose working
// content equals their HEAD content once the daemon's own sections are
// stripped from both sides. The read approach mirrors
// withoutDaemonOnlyChanges (protected_paths_daemon_sections.go): the working
// copy comes from disk, the HEAD copy from the injectable git runner as
// `git show HEAD:<path>`. Fail closed: any read error — the working file,
// the HEAD copy, or the git call itself — keeps the path, as do unsafe
// path shapes (absolute, `..`, NUL).
func (m *Manager) pathspecWithoutDaemonOnlyChanges(ctx context.Context, dir string, paths []string) []string {
	run := m.baseCommitRunner()
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		rel := normalizeDriftPath(p)
		if rel == "" || strings.HasPrefix(rel, "/") || rel == ".." || strings.HasPrefix(rel, "../") || strings.ContainsRune(rel, 0) {
			out = append(out, p) // unsafe shape: never compare, keep it
			continue
		}
		cur, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			out = append(out, p) // no working copy to compare: keep it
			continue
		}
		if ctx.Err() != nil {
			out = append(out, p)
			continue
		}
		headContent, err := run(ctx, dir, "show", "HEAD:"+rel)
		if err != nil {
			out = append(out, p) // no HEAD copy (new file) or git failed: keep it
			continue
		}
		if platformcontext.StripDaemonSections(string(cur)) == platformcontext.StripDaemonSections(headContent) {
			continue // only the daemon's own sections differ: leave it out
		}
		out = append(out, p)
	}
	return out
}
