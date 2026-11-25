// Real files in ignored folders of a work tree.
//
// git status does not show what a repository ignores, and "git worktree
// remove" deletes it. Several daemon steps and tasks leave real files under
// .datawatch/, which projects commonly ignore. A work tree whose folders below
// hold such a file is not clean, whatever git says.
package autonomous

import (
	"context"
	"sort"
	"strings"
)

// parkedWorkDirs are the folders, relative to a work tree, that can hold the
// only copy of real work:
//
//   - merge-aside: untracked files the task merge moved out of its way;
//   - reports: reports of read-only tasks (the daemon also moves stray ones
//     there) and of gates with a report path of their own;
//   - dependency-update: the results of a dependency update (the Automaton
//     record keeps only the counts);
//   - supervisor: handover documents;
//   - verify: verification scripts, safe only with an identical saved copy.
//
// Everything else the daemon writes under .datawatch/ is its own text or
// scratch (judge prompts, injected skills, lock and shell task files).
var parkedWorkDirs = []string{
	".datawatch/merge-aside",
	".datawatch/reports",
	".datawatch/dependency-update",
	".datawatch/supervisor",
	".datawatch/verify",
}

// parkedWork lists the files in those folders that git does not track, ignored
// or not, and that removing the work tree would lose. The daemon's own default
// gate report and a verification script with an identical copy in one of
// verifyCopies are not listed.
func parkedWork(ctx context.Context, dir string, verifyCopies []string) ([]string, error) {
	// Without --exclude-standard, --others includes ignored files.
	out, err := taskGitOut(ctx, dir, append([]string{"ls-files", "--others", "-z", "--"}, parkedWorkDirs...)...)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, p := range strings.Split(out, "\x00") {
		if p == "" || p == defaultGateReportPath || sameAsSavedVerifyScript(dir, p, verifyCopies) {
			continue
		}
		files = append(files, p)
	}
	sort.Strings(files)
	return files, nil
}
