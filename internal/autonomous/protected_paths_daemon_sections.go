package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/platformcontext"
	"github.com/dmz006/datawatch/internal/session"
)

// withoutDaemonOnlyChanges is the commit gate's filter: it drops the
// protected-path hits where the task changed nothing beyond the text the
// daemon itself writes (its rewrites of a rules file: platform-context, RTK,
// cross-session, Memory and Session Guardrails blocks).
//
// A hit is dropped only for a rules file the daemon writes into (AGENT.md,
// CLAUDE.md or the configured rules file) whose working tree copy is a regular
// file, whose base version can be read, and whose working tree, index and HEAD
// versions each equal the base version once the daemon's exact text is set
// aside (platformcontext.SameBesidesDaemonText). Text between daemon markers,
// under a daemon heading or after an unclosed marker that is not what the
// daemon writes is compared like any other text. A match includes an
// unchanged file and a blank-line difference, both of which the
// checkpoint-entry revert can leave behind.
//
// Every other hit is kept. Any other file has no daemon-written sections,
// whatever text it holds: in a YAML file a line such as "# Session
// Guardrails" is an ordinary comment. A new file, an unknown base commit and
// no base commit at all (git would answer from the index) are unreadable, as
// is a version missing from the index or HEAD.
//
// Reads from git (`git show <base>:<path>`, `HEAD:<path>`, `:<path>`) go
// through the manager's injected baseCommitRunner, exactly like the rest of
// the base-commit reads.
func (m *Manager) withoutDaemonOnlyChanges(ctx context.Context, dir, base string, hits []string) []string {
	written := m.daemonRulesText(dir)
	return m.hitsOutsideDaemonSections(ctx, dir, base, hits, func(versions []string, baseContent string) bool {
		return sameBesidesDaemonText(versions, baseContent, written)
	})
}

// pathsNotProvenDaemonOnly is the stricter filter of the false-positive hold
// release. A task was held for a change, so a path is only cleared when that
// change is there to be read and is the daemon's: every version equals the
// base version besides the daemon's text, and at least one differs from it
// where daemon text is involved. An empty diff means the change is not in the
// tree that was read, and proves nothing.
func (m *Manager) pathsNotProvenDaemonOnly(ctx context.Context, dir, base string, paths []string) []string {
	written := m.daemonRulesText(dir)
	return m.hitsOutsideDaemonSections(ctx, dir, base, paths, func(versions []string, baseContent string) bool {
		return daemonSectionsDiff(versions, baseContent, written)
	})
}

// hitsOutsideDaemonSections returns the hits that are not cleared. Only a
// rules file with all its versions readable can be, and only when daemonOwned
// accepts them.
func (m *Manager) hitsOutsideDaemonSections(ctx context.Context, dir, base string, hits []string, daemonOwned func(versions []string, baseContent string) bool) []string {
	rulesFile, _ := m.rulesAndContextFiles(dir)
	rulesFiles := checkpointCandidates(rulesFile)
	run := m.baseCommitRunner()
	var out []string
	for _, p := range hits {
		rel := normalizeDriftPath(p)
		if rulesFiles[rel] {
			if versions, baseContent, ok := rulesFileVersions(ctx, run, dir, base, rel); ok && daemonOwned(versions, baseContent) {
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

// rulesFileVersions reads rel from the working tree in dir, from the index and
// from HEAD, and at base. A change can sit in any of the three and be restored
// in the others, so all of them are judged. ok is false when any cannot be
// read, or the working copy is not a regular file.
func rulesFileVersions(ctx context.Context, run driftGitRunner, dir, base, rel string) (versions []string, baseContent string, ok bool) {
	if strings.TrimSpace(base) == "" || rel == "" || strings.HasPrefix(rel, "/") || rel == ".." ||
		strings.HasPrefix(rel, "../") || strings.ContainsRune(rel, 0) {
		return nil, "", false
	}
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if fi, err := os.Lstat(full); err != nil || !fi.Mode().IsRegular() {
		return nil, "", false
	}
	b, err := os.ReadFile(full)
	if err != nil || ctx.Err() != nil {
		return nil, "", false
	}
	baseContent, err = run(ctx, dir, "show", base+":"+rel)
	if err != nil {
		return nil, "", false
	}
	recorded, ok := recordedVersions(ctx, run, dir, rel)
	if !ok {
		return nil, "", false
	}
	return append([]string{string(b)}, recorded...), baseContent, true
}

// recordedVersions reads rel as git has recorded it: in the index and at HEAD.
func recordedVersions(ctx context.Context, run driftGitRunner, dir, rel string) ([]string, bool) {
	var out []string
	for _, ref := range []string{":" + rel, "HEAD:" + rel} {
		content, err := run(ctx, dir, "show", ref)
		if err != nil || ctx.Err() != nil {
			return nil, false
		}
		out = append(out, content)
	}
	return out, true
}

// sameBesidesDaemonText reports whether every version equals base once the
// daemon's text is set aside.
func sameBesidesDaemonText(versions []string, base string, written []string) bool {
	for _, v := range versions {
		if !platformcontext.SameBesidesDaemonText(v, base, written) {
			return false
		}
	}
	return true
}

// daemonSectionsDiff reports whether every version equals base besides the
// daemon's text and at least one differs from base with a daemon section in it
// or in base.
func daemonSectionsDiff(versions []string, base string, written []string) bool {
	if !sameBesidesDaemonText(versions, base, written) {
		return false
	}
	for _, v := range versions {
		if v != base && (platformcontext.HasDaemonSections(v) || platformcontext.HasDaemonSections(base)) {
			return true
		}
	}
	return false
}

// SetDaemonRulesTextFn wires the source of the daemon's rules-file blocks that
// depend on configuration or on a session (the session manager's
// RulesFileTextFor). Without it only the fixed blocks are known as the
// daemon's, and a change to any other is held.
func (m *Manager) SetDaemonRulesTextFn(fn func(dir string) []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.daemonRulesTextFn = fn
}

// daemonRulesText lists the texts the daemon may have written into the rules
// file of dir. It must not be called with m.mu held.
func (m *Manager) daemonRulesText(dir string) []string {
	m.mu.Lock()
	fn := m.daemonRulesTextFn
	m.mu.Unlock()
	written := session.FixedRulesFileText()
	if fn != nil {
		written = append(written, fn(dir)...)
	}
	return written
}
