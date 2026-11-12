// Platform context migration sweep — one-shot file migration, re-run as a
// no-op on every pass.
//
// The platform context is owned by the daemon and re-appended at delivery
// time (internal/platformcontext/delivery.go); a copy of it baked into a
// project's rules file is stale by construction. MigrateRulesFile
// (internal/platformcontext/migrate_rules_file.go) removes the marked block
// from an existing rules file and is idempotent, so running it repeatedly —
// at daemon start and on the periodic loop, exactly like the other sweeps —
// is safe: once a file is clean it reports changed=false and the sweep
// records nothing.

package autonomous

import (
	"log"
	"path/filepath"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

// platformContextMigrationFiles are the per-project rules file names the
// sweep migrates inside each stored Automation's project directory.
var platformContextMigrationFiles = []string{"AGENT.md", "CLAUDE.md"}

// platformContextMigratedAction is the audit event logged for every rules
// file the sweep cleaned.
const platformContextMigratedAction = "autonomous_platform_context_migrated"

// sweepPlatformContextMigration is one platform-context migration pass. It
// collects the distinct non-empty ProjectDir values of the stored Automations
// (the same ListAutomations accessor the manager uses elsewhere for its
// stored Automata) and, for each directory, migrates AGENT.md and CLAUDE.md:
// a file that still carries the marked platform context block is rewritten
// without it, in the working copy only — the removal is never committed, and
// the daemon re-appends the current text at delivery time.
//
// For each file that changed it records one entry in the audit event log —
// the same channel the orphan and stuck-running sweeps use for their
// findings — naming the file cleaned and that the removal is not committed.
// Per-file errors are logged and never fatal, and a directory (or file) that
// does not exist simply migrates nothing: a missing project never fails the
// sweep.
func (m *Manager) sweepPlatformContextMigration() {
	seen := map[string]bool{}
	var dirs []string
	for _, a := range m.ListAutomations() {
		dir := a.ProjectDir
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}

	for _, dir := range dirs {
		for _, name := range platformContextMigrationFiles {
			path := filepath.Join(dir, name)
			changed, err := platformcontext.MigrateRulesFile(path)
			if err != nil {
				log.Printf("[autonomous] platform-context-migration: %s: %v", path, err)
				continue
			}
			if !changed {
				continue
			}
			note := "removed the platform context block from " + path + " (working copy; the removal is not committed)"
			log.Printf("[autonomous] platform-context-migration: %s", note)
			m.mu.Lock()
			auditFn := m.auditFn
			m.mu.Unlock()
			if auditFn != nil {
				auditFn(platformContextMigratedAction, map[string]any{
					"path":      path,
					"project":   dir,
					"committed": false,
					"note":      note,
				})
			}
		}
	}
}
