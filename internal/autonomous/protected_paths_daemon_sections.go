package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

// withoutDaemonOnlyChanges is the commit gate's filter: it drops the
// protected-path hits where the task changed nothing beyond the daemon's own
// sections (its rewrites of a rules file: platform-context, RTK,
// cross-session, Memory and Session Guardrails blocks).
//
// A hit is dropped only for a rules file the daemon writes into (AGENT.md,
// CLAUDE.md or the configured rules file) whose working tree copy is a regular
// file and whose base version can be read, when the two strip
// (platformcontext.StripDaemonSections) to equal text. That includes an
// unchanged file and a blank-line difference, both of which the
// checkpoint-entry revert can leave behind.
//
// Every other hit is kept. Any other file has no daemon-written sections,
// whatever text it holds: in a YAML file a line such as "# Session
// Guardrails" is an ordinary comment. A new file, an unknown base commit and
// no base commit at all (git would answer from the index) are unreadable.
//
// The base read is `git show <base>:<path>` through the manager's injected
// baseCommitRunner, exactly like the rest of the base-commit reads — never
// through the working copy.
func (m *Manager) withoutDaemonOnlyChanges(ctx context.Context, dir, base string, hits []string) []string {
	return m.hitsOutsideDaemonSections(ctx, dir, base, hits, func(cur, baseContent string) bool {
		return platformcontext.StripDaemonSections(cur) == platformcontext.StripDaemonSections(baseContent)
	})
}

// pathsNotProvenDaemonOnly is the stricter filter of the false-positive hold
// release. A task was held for a change, so a path is only cleared when that
// change is there to be read and is the daemon's: the two versions differ, at
// least one carries a daemon section, and they strip to equal text. An empty
// diff means the change is not in the tree that was read, and proves nothing.
func (m *Manager) pathsNotProvenDaemonOnly(ctx context.Context, dir, base string, paths []string) []string {
	return m.hitsOutsideDaemonSections(ctx, dir, base, paths, daemonSectionsDiff)
}

// hitsOutsideDaemonSections returns the hits that are not cleared. Only a
// rules file with two readable versions can be, and only when daemonOwned
// accepts the pair.
func (m *Manager) hitsOutsideDaemonSections(ctx context.Context, dir, base string, hits []string, daemonOwned func(cur, baseContent string) bool) []string {
	rulesFile, _ := m.rulesAndContextFiles(dir)
	rulesFiles := checkpointCandidates(rulesFile)
	run := m.baseCommitRunner()
	var out []string
	for _, p := range hits {
		rel := normalizeDriftPath(p)
		if rulesFiles[rel] {
			if cur, baseContent, ok := bothVersions(ctx, run, dir, base, rel); ok && daemonOwned(cur, baseContent) {
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

// bothVersions reads rel from the working tree in dir and at base. ok is false
// when either cannot be read, or the working copy is not a regular file.
func bothVersions(ctx context.Context, run driftGitRunner, dir, base, rel string) (cur, baseContent string, ok bool) {
	if strings.TrimSpace(base) == "" || rel == "" || strings.HasPrefix(rel, "/") || rel == ".." ||
		strings.HasPrefix(rel, "../") || strings.ContainsRune(rel, 0) {
		return "", "", false
	}
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if fi, err := os.Lstat(full); err != nil || !fi.Mode().IsRegular() {
		return "", "", false
	}
	b, err := os.ReadFile(full)
	if err != nil || ctx.Err() != nil {
		return "", "", false
	}
	baseContent, err = run(ctx, dir, "show", base+":"+rel)
	if err != nil {
		return "", "", false
	}
	return string(b), baseContent, true
}

// daemonSectionsDiff reports whether cur and base differ, at least one carries
// a daemon section, and they are equal once the daemon's sections are stripped.
func daemonSectionsDiff(cur, base string) bool {
	return cur != base &&
		(platformcontext.HasDaemonSections(cur) || platformcontext.HasDaemonSections(base)) &&
		platformcontext.StripDaemonSections(cur) == platformcontext.StripDaemonSections(base)
}
