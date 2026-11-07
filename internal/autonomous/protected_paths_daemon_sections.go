package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

// withoutDaemonOnlyChanges drops the protected-path hits whose only change
// against base is the daemon's own sections: a hit is dropped when the working
// tree file and the file at the base commit strip
// (platformcontext.StripDaemonSections — platform-context, RTK,
// cross-session, Memory and Session Guardrails blocks) to equal text.
//
// The check fails closed: a hit is kept when the working tree file is deleted
// or missing, when the base version cannot be read (a new file is the
// expected case there) or when the stripped versions still differ. The base
// read is `git show <base>:<path>` through the manager's injected
// baseCommitRunner, exactly like the rest of the base-commit reads — never
// through the working copy.
func (m *Manager) withoutDaemonOnlyChanges(ctx context.Context, dir, base string, hits []string) []string {
	run := m.baseCommitRunner()
	var out []string
	for _, p := range hits {
		rel := normalizeDriftPath(p)
		if rel == "" || strings.HasPrefix(rel, "/") || rel == ".." || strings.HasPrefix(rel, "../") || strings.ContainsRune(rel, 0) {
			out = append(out, p)
			continue
		}
		cur, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			// Deleted, missing, or unreadable: keep the hit.
			out = append(out, p)
			continue
		}
		if ctx.Err() != nil {
			out = append(out, p)
			continue
		}
		baseContent, err := run(ctx, dir, "show", base+":"+rel)
		if err != nil {
			// Absent at base (a new file) or unreadable: keep the hit.
			out = append(out, p)
			continue
		}
		curStripped := platformcontext.StripDaemonSections(string(cur))
		if curStripped != platformcontext.StripDaemonSections(baseContent) {
			out = append(out, p)
		}
	}
	return out
}
