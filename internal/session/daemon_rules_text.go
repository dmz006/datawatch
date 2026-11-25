// The texts the daemon writes into a project's rules file, given exactly so
// that the protected-paths check can tell them from text someone else put
// under the same markers or headings.
package session

import (
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

// FixedRulesFileText returns the daemon's rules-file blocks that are the same
// for every session: the RTK block, the cross-session rule and the memory
// section.
func FixedRulesFileText() []string {
	return []string{rtkInstructions(), crossSessionCommunicationRule(), memoryInstructions()}
}

// RulesFileTextFor returns the blocks that depend on the configuration or on a
// session and that the daemon may have written into the rules file of dir: the
// platform context block with the operator's note and, only while
// rtk.write_rules_files lets the daemon write rules files at all, the Session
// Guardrails header of each known session that runs in dir.
func (m *Manager) RulesFileTextFor(dir string) []string {
	if m == nil || m.cfg == nil {
		return nil
	}
	var out []string
	if note := strings.TrimSpace(m.cfg.PlatformContext.Note); note != "" {
		out = append(out, platformcontext.Block(m.cfg.PlatformContext.AppendNote(platformcontext.PlatformContext), platformcontext.PlatformContextVersion))
	}
	if !m.cfg.RTK.WriteRulesFiles || strings.TrimSpace(dir) == "" {
		return out
	}
	want := filepath.Clean(dir)
	for _, sess := range m.ListSessions() {
		if sess.ProjectDir == "" || filepath.Clean(sess.ProjectDir) != want {
			continue
		}
		out = append(out, strings.TrimSpace(minimalSessionGuardrails(sess)))
	}
	return out
}
