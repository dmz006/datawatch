// BL406 — the plan's own Context section promised "the decomposer/
// task-spec generation and the new scanner both read whichever path
// is configured" for autonomous.rules_file/context_file, so an
// operator with a differently-named file (this repo's own
// DATAWATCH-CONTEXT.md) could point the config at it instead of
// renaming it. Found live 2026-10-11, after all 6 of that plan's
// phases had been marked Done, that this was never actually
// implemented: RulesFile/ContextFile had full REST/MCP/CLI/comm
// read-write config plumbing but zero consumers anywhere in the
// codebase. This file closes that gap for the decomposer half.
//
// Revised same day (B122): the first version inlined the full file
// content into the planning prompt. For this repo that's ~160KB
// (AGENT.md + DATAWATCH-CONTEXT.md combined) — large enough to expose
// a real timing bug in the tmux delivery path for session-based
// backends (fixed separately, B122), and wasteful regardless: a
// session-based decompose backend (claude-code, opencode, ...) is
// running *in* prd.ProjectDir with real file-reading tools of its
// own. Operator-raised: "do we need to directly inject agent and
// context, or just point to the files?" — point to them. The
// decomposer reads them itself, with its own full context budget
// dedicated to that read rather than sharing a budget with
// everything else packed into one giant prompt. A headless/ask-based
// backend (ollama/openwebui, no tool access) can't act on the pointer
// either way — harmless, a few bytes of inert text, not a regression
// from the content-inlining version for that case.
//
// Per-project rules/context since the autonomous.rules_context
// sub-configuration: the file names are resolved per PRD project
// (project override > global rules_context > legacy
// autonomous.rules_file/context_file > the defaults AGENT.md /
// CONTEXT.md), and ContextFiles may name more than one context file.
// The digest sub-configuration (DigestMode, DigestMaxBytes) rides
// along in the same resolved policy.
package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dmz006/datawatch/internal/config"
)

// DefaultRulesFile / DefaultContextFile — file names applied when
// neither autonomous.rules_context nor the legacy autonomous.rules_file /
// autonomous.context_file keys name one.
const (
	DefaultRulesFile   = "AGENT.md"
	DefaultContextFile = "CONTEXT.md"
)

// ResolvedRulesContext is the rules/context policy resolved for one
// project directory: the rules file name, the list of context file
// names, and the digest sub-settings (mode plus byte cap).
type ResolvedRulesContext struct {
	RulesFile      string
	ContextFiles   []string
	DigestMode     string
	DigestMaxBytes int
}

// defaultRulesContext is the resolution result when nothing is
// configured: AGENT.md plus a single CONTEXT.md, digest on with no
// tools and the 4096-byte cap.
func defaultRulesContext() ResolvedRulesContext {
	return ResolvedRulesContext{
		RulesFile:      DefaultRulesFile,
		ContextFiles:   []string{DefaultContextFile},
		DigestMode:     config.RulesContextDigestModeNoTools,
		DigestMaxBytes: config.RulesContextDigestMaxBytesDefault,
	}
}

var (
	rulesContextMu  sync.RWMutex
	rulesContextCfg config.AutonomousRulesContextConfig
)

// SetRulesContextConfig installs the global autonomous.rules_context
// config that RulesContextFor reads. Call it at startup and on config
// reload, next to SetChecksConfig.
func SetRulesContextConfig(c config.AutonomousRulesContextConfig) {
	rulesContextMu.Lock()
	rulesContextCfg = c
	rulesContextMu.Unlock()
}

// rulesContextConfigSnapshot is a copy of the installed config for
// lock-free resolution by callers that hold their own value.
func rulesContextConfigSnapshot() config.AutonomousRulesContextConfig {
	rulesContextMu.RLock()
	c := rulesContextCfg
	rulesContextMu.RUnlock()
	return c
}

// RulesContextFor returns the resolved rules/context policy for a
// project directory from the installed autonomous.rules_context config.
// An unset or invalid config yields the defaults rather than a failure.
func RulesContextFor(projectDir string) ResolvedRulesContext {
	p, err := ResolveRulesContext(rulesContextConfigSnapshot(), "", "", projectDir)
	if err != nil {
		p = defaultRulesContext()
	}
	return p
}

// ResolveRulesContext resolves the rules/context policy for projectDir
// from c, layered in precedence order: a per-project override entry,
// then the global rules_context values, then the legacy
// rules_file/context_file values (legacyContext names a single context
// file), ending in the defaults ("AGENT.md" plus a single
// "CONTEXT.md"). An empty entry field falls through to the next layer,
// so a project that renames only one name keeps the other inherited.
// Invalid values are an error.
func ResolveRulesContext(c config.AutonomousRulesContextConfig, legacyRules, legacyContext, projectDir string) (ResolvedRulesContext, error) {
	if err := c.Validate(); err != nil {
		return ResolvedRulesContext{}, err
	}
	p := defaultRulesContext()
	if f := strings.TrimSpace(legacyRules); f != "" {
		p.RulesFile = f
	}
	if f := strings.TrimSpace(legacyContext); f != "" {
		p.ContextFiles = []string{f}
	}
	overlay := func(rulesFile string, contextFiles []string, mode string, maxBytes int) {
		if f := strings.TrimSpace(rulesFile); f != "" {
			p.RulesFile = f
		}
		if files := nonBlankStrings(contextFiles); len(files) > 0 {
			p.ContextFiles = files
		}
		if m := config.NormalizeRulesContextMode(mode); m != "" {
			p.DigestMode = m
		}
		if b := maxBytes; b != 0 {
			p.DigestMaxBytes = b
		}
	}
	overlay(c.RulesFile, c.ContextFiles, c.DigestMode, c.DigestMaxBytes)
	if o, ok := lookupRulesContextOverride(c.Projects, projectDir); ok {
		overlay(o.RulesFile, o.ContextFiles, o.DigestMode, o.DigestMaxBytes)
	}
	return p, nil
}

type rulesContextOverride struct {
	RulesFile      string
	ContextFiles   []string
	DigestMode     string
	DigestMaxBytes int
}

// lookupRulesContextOverride finds the override entry for dir, matching
// exactly or after filepath.Clean, like lookupChecksOverride.
func lookupRulesContextOverride(projects []config.AutonomousRulesContextProject, dir string) (rulesContextOverride, bool) {
	if len(projects) == 0 {
		return rulesContextOverride{}, false
	}
	clean := filepath.Clean(strings.TrimSpace(dir))
	if clean == "" || clean == "." {
		return rulesContextOverride{}, false
	}
	for _, p := range projects {
		if strings.TrimSpace(p.ProjectDir) == "" {
			continue
		}
		if filepath.Clean(p.ProjectDir) == clean {
			return rulesContextOverride{
				RulesFile:      p.RulesFile,
				ContextFiles:   p.ContextFiles,
				DigestMode:     p.DigestMode,
				DigestMaxBytes: p.DigestMaxBytes,
			}, true
		}
	}
	return rulesContextOverride{}, false
}

func nonBlankStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// rulesContextSuffix returns a short pointer to the resolved rules
// files of the PRD's own project directory (rules file first, then the
// context files in configured order; only names that are actually
// present are mentioned). Does not read or inline any file's content —
// see this file's own top comment for why. Empty when ProjectDir is
// unset (nothing to point at yet, e.g. before worktree creation) or no
// named file exists.
func (m *Manager) rulesContextSuffix(prd *PRD) string {
	if prd.ProjectDir == "" {
		return ""
	}
	names := presentRulesContextNames(prd, m.resolvedRulesContext(prd))
	if len(names) == 0 {
		return ""
	}
	return "\n\n--- Project rules ---\nBefore planning, read " + joinRulesContextNames(names) +
		" at the root of your working directory — they define this" +
		" project's development/testing/documentation rules and" +
		" current state. Follow them when decomposing this PRD."
}

// resolvedRulesContext resolves the rules/context policy for this PRD's
// project directory: the installed autonomous.rules_context config
// (project override first, then its global values), then the legacy
// rules_file/context_file keys this manager carries, ending in the
// defaults.
func (m *Manager) resolvedRulesContext(prd *PRD) ResolvedRulesContext {
	m.mu.Lock()
	legacyRules := m.cfg.RulesFile
	legacyContext := m.cfg.ContextFile
	m.mu.Unlock()
	p, err := ResolveRulesContext(rulesContextConfigSnapshot(), legacyRules, legacyContext, prd.ProjectDir)
	if err != nil {
		p = defaultRulesContext()
	}
	return p
}

// presentRulesContextNames returns the configured file names (rules
// file first, then context files in configured order, deduplicated)
// that actually exist at the root of prd.ProjectDir.
func presentRulesContextNames(prd *PRD, p ResolvedRulesContext) []string {
	if prd.ProjectDir == "" {
		return nil
	}
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		for _, existing := range names {
			if existing == name {
				return
			}
		}
		if fileExists(filepath.Join(prd.ProjectDir, name)) {
			names = append(names, name)
		}
	}
	add(p.RulesFile)
	for _, c := range p.ContextFiles {
		add(c)
	}
	return names
}

// joinRulesContextNames joins file names for prose: "A", "A and B",
// "A, B and C".
func joinRulesContextNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path) // #nosec G304 -- path is joined from operator-configured rules_file/context_file and a PRD's own worktree dir, same trust model as every other project-dir file read in this package
	return err == nil
}
