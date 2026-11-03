// decompose_guidance.go — decompose-time guidance: the text appended
// to the decompose prompt with the knowledge the decomposer needs to
// plan well.
//
// Precedence (resolved by BuildDecomposeGuidance):
//
//  1. autonomous.decompose_guidance set to literal text — that text is
//     the guidance.
//  2. autonomous.decompose_guidance naming an existing readable file —
//     the file's contents are the guidance (a file named in the config
//     keeps the guidance text outside the config file).
//  3. the value looks like a path (it carries a path separator or a
//     common document extension) but the file is missing or unreadable,
//     or the named file is empty — fall back to the generated block.
//  4. autonomous.decompose_guidance unset — the guidance is generated
//     from the active decompose-lint rules (EffectiveRules: the
//     built-in pack merged with operator rules, disabled rules
//     dropped), one line per rule (id, severity, message) — so the
//     decomposer is told exactly what the lint hook will flag.
//
// The block is appended to the decompose prompt by the manager (see
// the decomposeGuidance() callers in manager.go); empty = no-op.
package autonomous

import (
	"os"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// GeneratedDecomposeGuidance renders the guidance block generated from
// the active decompose-lint rules for lintCfg: one line per rule (id,
// severity, message), in the effective pack's order — the built-in
// pack (decompose_lint_rules.go) merged with lintCfg.Rules, disabled
// rules dropped (EffectiveRules). Empty when no rule is active.
func GeneratedDecomposeGuidance(lintCfg config.DecomposeLintConfig) string {
	return config.GenerateDecomposeGuidance(EffectiveRules(lintCfg))
}

// BuildDecomposeGuidance resolves cfg.DecomposeGuidance (autonomous.
// decompose_guidance, plus the decompose-lint rules for the generated
// fallback) into the guidance block the decompose prompt carries:
//
//   - Set: the value used verbatim when it is literal text; when it
//     names an existing file, the file's contents instead. A named
//     file that is missing, unreadable or empty falls back to the
//     generated block.
//   - Unset (empty/whitespace): the block generated from the active
//     lint rules — one line per rule (id, severity, message) — via
//     GeneratedDecomposeGuidance.
func BuildDecomposeGuidance(cfg *config.AutonomousConfig) string {
	if cfg == nil {
		return GeneratedDecomposeGuidance(config.DecomposeLintConfig{})
	}
	if s := strings.TrimSpace(cfg.DecomposeGuidance); s != "" {
		// Set: literal text unless the value is a named file. A value is
		// a file name when it exists as a regular file, or when it looks
		// path-like (path separator / document extension) — that is how a
		// dead path is distinguished from ordinary prose guidance.
		if st, err := os.Stat(cfg.DecomposeGuidance); err == nil && !st.IsDir() {
			if b, rerr := os.ReadFile(cfg.DecomposeGuidance); rerr == nil {
				if t := strings.TrimSpace(string(b)); t != "" {
					return string(b)
				}
			}
			// Named file exists but is unreadable or empty: the operator
			// pointed at a file — an empty guidance block is useless, so
			// the generated block stands in.
			return GeneratedDecomposeGuidance(cfg.DecomposeLint)
		}
		if looksLikeGuidancePath(cfg.DecomposeGuidance) {
			// Path-like value with no readable file: fall back to the
			// generated block (a dead path is not prose to ship verbatim).
			return GeneratedDecomposeGuidance(cfg.DecomposeLint)
		}
		return s
	}
	return GeneratedDecomposeGuidance(cfg.DecomposeLint)
}

// decomposeGuidanceBlock resolves the manager's decompose guidance —
// the m.cfg.DecomposeGuidance mirror (autonomous.decompose_guidance)
// plus the decompose-lint rules for the generated fallback (see
// BuildDecomposeGuidance) — into the block appended to the decompose
// prompt. Empty = append nothing (no active rules and no configured
// text). Shared by both decompose twins, so the block's placement and
// format come from exactly one place.
func (m *Manager) decomposeGuidanceBlock() string {
	m.mu.Lock()
	lintCfg := m.cfg.DecomposeLint
	guidance := m.cfg.DecomposeGuidance
	m.mu.Unlock()
	block := strings.TrimSpace(BuildDecomposeGuidance(&config.AutonomousConfig{DecomposeLint: lintCfg, DecomposeGuidance: guidance}))
	if block == "" {
		return ""
	}
	return "\n\n--- Decompose guidance ---\n" + block
}

// looksLikeGuidancePath reports whether v reads like a file path rather
// than prose: it carries a path separator, or it ends in a document
// extension guidance files have. Prose ("every task must declare a
// verification script") is never path-like.
func looksLikeGuidancePath(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	if strings.ContainsAny(v, "/\\") {
		return true
	}
	for _, ext := range []string{".md", ".txt", ".json", ".yml", ".yaml"} {
		if strings.HasSuffix(v, ext) {
			return true
		}
	}
	return false
}
