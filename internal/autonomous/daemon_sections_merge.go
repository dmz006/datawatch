// Strip the daemon's own sections out of a task branch's rules files before
// the branch is merged into the PRD branch.
//
// The datawatch daemon rewrites a session's instructions file (AGENT.md,
// CLAUDE.md, …) at session start and injects sections of its own (platform
// context, RTK, cross-session rules, Memory, Session Guardrails). If a task
// commits those sections into its rules file, the merge folds them into the
// PRD branch as a change nobody in the task made, and the operator cannot
// review it as their own edit. cleanDaemonSectionsBeforeMerge settles that on
// the task branch before the merge runs: a rules file the branch changed such
// that its committed content differs from the base blob only by daemon
// sections is restored to the base blob; one that also carries a real edit is
// rewritten with the daemon's sections removed (and nothing else). A file the
// branch did not change, or whose committed content has no daemon sections,
// is left alone — and a branch with nothing of the sort gets no cleanup
// commit at all, so a merge of it moves the base branch exactly once.
//
// Every write goes through the task worktree's ordinary git add / git commit
// path (git show for reads, the manager's injectable baseCommitRunner): no
// reset, no checkout, no stash, no force — the branch only ever advances by
// one plain commit. A rules file with uncommitted changes beyond the daemon's
// own sections is an error, and the merge must not run.
package autonomous

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

// cleanDaemonSectionsBeforeMerge rewrites the rules files that dir's current
// branch changed against base so they no longer carry the daemon's own
// sections, in one cleanup commit. dir is the task worktree (HEAD on the
// task branch), base the commit the branch was cut from. See the file comment
// for the three outcomes; an error means the merge must not proceed.
func (m *Manager) cleanDaemonSectionsBeforeMerge(ctx context.Context, dir, base string) error {
	base = strings.TrimSpace(base)
	if base == "" || strings.HasPrefix(base, "-") || strings.ContainsAny(base, " \t\r\n\x00:") {
		return fmt.Errorf("clean daemon sections before merge: %q is not a commit", base)
	}
	run := m.baseCommitRunner()

	rulesFile, contextFiles := m.rulesAndContextFiles(dir)
	var files []string
	seen := map[string]bool{}
	for _, name := range append([]string{rulesFile}, contextFiles...) {
		rel := normalizeDriftPath(name)
		if rel == "" || seen[rel] {
			continue
		}
		seen[rel] = true
		files = append(files, rel)
	}

	var changed []string
	for _, rel := range files {
		headOut, herr := run(ctx, dir, "show", "HEAD:"+rel)
		if herr != nil {
			continue // absent on the branch (or unreadable): leave it alone
		}
		baseOut, berr := run(ctx, dir, "show", base+":"+rel)
		newFile := berr != nil
		if !newFile && headOut == baseOut {
			continue // the branch did not change this file
		}
		stripped := platformcontext.StripDaemonSections(headOut)
		if stripped == headOut {
			continue // no daemon sections in the committed content
		}
		var want string
		if !newFile && stripped == platformcontext.StripDaemonSections(baseOut) {
			want = baseOut // daemon sections are the whole diff: restore the base blob
		} else {
			want = stripped // real edit on top: keep the edit, drop the sections
			if strings.HasSuffix(headOut, "\n") {
				want += "\n" // StripDaemonSections trims trailing whitespace; keep the file's final newline
			}
		}

		full, err := safeJoin(dir, rel)
		if err != nil {
			return fmt.Errorf("clean daemon sections before merge: %w", err)
		}
		if work, werr := os.ReadFile(full); werr == nil {
			if string(work) != headOut && // committed, so a real difference, not a daemon-only working diff
				platformcontext.StripDaemonSections(string(work)) != stripped {
				return fmt.Errorf("clean daemon sections before merge: %s has uncommitted changes beyond the daemon's own sections; refusing to overwrite them", rel)
			}
		} // a missing working copy is safe: the write only replaces the committed content
		if err := os.WriteFile(full, []byte(want), 0o644); err != nil { // #nosec G306 -- rules file inside the task worktree
			return fmt.Errorf("clean daemon sections before merge: write %s: %w", rel, err)
		}
		if _, aerr := run(ctx, dir, "add", "--", rel); aerr != nil {
			return fmt.Errorf("clean daemon sections before merge: stage %s: %w", rel, aerr)
		}
		changed = append(changed, rel)
	}
	if len(changed) == 0 {
		return nil
	}
	// --quiet exits non-zero when the index differs from HEAD; a clean exit
	// means nothing actually staged and there is no cleanup commit to make.
	if _, err := run(ctx, dir, append([]string{"diff", "--cached", "--quiet", "--"}, changed...)...); err == nil {
		return nil
	}
	msg := fmt.Sprintf("Strip daemon sections from %s before merge", strings.Join(changed, ", "))
	if _, cerr := run(ctx, dir, append(append(gitIdentityArgs(ctx, dir), "commit", "-m", msg, "--"), changed...)...); cerr != nil {
		return fmt.Errorf("clean daemon sections before merge: commit %s: %w", strings.Join(changed, ", "), cerr)
	}
	return nil
}
