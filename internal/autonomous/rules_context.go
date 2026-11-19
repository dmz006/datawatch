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

// rulesContextSuffix returns the rules/context block for the planning
// prompt, for a planning backend that has file tools (the pointer only,
// unless the digest mode is "all"). Planning call sites use
// rulesContextSuffixFor, which knows the planning backend.
func (m *Manager) rulesContextSuffix(prd *PRD) string {
	s, _, _ := m.rulesContextSuffixFor(prd, "")
	return s
}

// rulesContextSuffixFor builds the planning-prompt block for the given
// planning backend and reports whether a digest was appended and its
// size in bytes (for the planning record). See rulesContextBlock.
func (m *Manager) rulesContextSuffixFor(prd *PRD, backend string) (suffix string, digestInjected bool, digestBytes int) {
	return m.rulesContextBlock(prd.ProjectDir, backend, rulesBlockPlanning)
}

// Purposes of a rules/context block; they only change the lead-in and
// closing sentences so the same pointer reads right in each prompt.
const (
	rulesBlockPlanning = "planning"
	rulesBlockWorker   = "worker"
)

// rulesDigestHeader introduces the digest in every prompt. Fixed text,
// also used to detect a block that is already present in a prompt.
const rulesDigestHeader = "--- Rules digest ---"

// rulesBlockMarker starts every rules/context block; a prompt that
// already holds it is never given a second one.
const rulesBlockMarker = "\n\n--- Project rules ---\n"

// backendHasFileTools reports whether a backend that runs a prompt can
// read files in its working directory. Headless ask-style backends
// (ollama, openwebui — the same pair the decompose wiring treats as
// ask-compatible) have no file tools; every session backend does. An
// empty name is treated as a session backend.
func backendHasFileTools(backend string) bool {
	switch strings.TrimSpace(backend) {
	case "ollama", "openwebui":
		return false
	}
	return true
}

// rulesDigestDue applies the resolved digest mode to a backend: "all"
// always, "no_tools" only for a backend without file tools, "off" never.
func rulesDigestDue(p ResolvedRulesContext, backend string) bool {
	switch p.DigestMode {
	case config.RulesContextDigestModeAll:
		return true
	case config.RulesContextDigestModeNoTools:
		return !backendHasFileTools(backend)
	}
	return false
}

// rulesContextBlock builds the pointer (every present rules/context file
// of the resolved policy), one line per configured file that is missing,
// and — when the digest rule says so for backend — the digest block.
// Empty when dir is unset or nothing is configured. File content is
// only read to build the digest.
func (m *Manager) rulesContextBlock(dir, backend, purpose string) (block string, digestInjected bool, digestBytes int) {
	if dir == "" {
		return "", false, 0
	}
	p := m.resolvedRulesContextDir(dir)
	present, missing := rulesContextPresence(dir, p)
	if len(present) == 0 && len(missing) == 0 {
		return "", false, 0
	}
	var b strings.Builder
	b.WriteString(rulesBlockMarker)
	if len(present) > 0 {
		names := make([]string, 0, len(present))
		for _, f := range present {
			names = append(names, f.name)
		}
		if purpose == rulesBlockWorker {
			b.WriteString("Before starting this task, read " + joinRulesContextNames(names) +
				" at the root of your working directory — they define this" +
				" project's development/testing/documentation rules and" +
				" current state. Follow them while doing this task.")
		} else {
			b.WriteString("Before planning, read " + joinRulesContextNames(names) +
				" at the root of your working directory — they define this" +
				" project's development/testing/documentation rules and" +
				" current state. Follow them when decomposing this PRD.")
		}
	}
	for i, f := range missing {
		if len(present) > 0 || i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(f.label + " " + f.name + " was configured and not found in the project directory.")
	}
	if len(present) > 0 && purpose == rulesBlockPlanning {
		if l := rulesSectionNamesLine(dir, p); l != "" {
			b.WriteString("\n\n" + l)
		}
	}
	if len(present) > 0 && rulesDigestDue(p, backend) {
		if d := m.rulesDigestBlock(dir, p); d != "" {
			b.WriteString("\n\n" + rulesDigestHeader + "\n" + d)
			return b.String(), true, len(d)
		}
	}
	return b.String(), false, 0
}

// rulesDigestBlock builds the digest text: the rules file's digest, then
// the headings of each present context file (the same builder, with the
// mandatory lines dropped), the whole kept within p.DigestMaxBytes.
func (m *Manager) rulesDigestBlock(dir string, p ResolvedRulesContext) string {
	limit := p.DigestMaxBytes
	if limit <= 0 {
		return ""
	}
	var out string
	if fileExists(filepath.Join(dir, p.RulesFile)) {
		if d, _, _, err := m.rulesDigestFor(dir, p.RulesFile, limit); err == nil {
			out = d
		}
	}
	for _, c := range p.ContextFiles {
		c = strings.TrimSpace(c)
		if c == "" || c == p.RulesFile || !fileExists(filepath.Join(dir, c)) {
			continue
		}
		d, _, _, err := m.rulesDigestFor(dir, c, limit)
		if err != nil {
			continue
		}
		var heads []string
		for i, l := range strings.Split(d, "\n") {
			if i == 0 || strings.HasPrefix(l, "  ") || strings.HasPrefix(l, "[digest truncated") {
				continue
			}
			heads = append(heads, l)
		}
		if len(heads) == 0 {
			continue
		}
		section := c + " headings:"
		for _, h := range heads {
			line := "\n  " + h
			sep := 0
			if out != "" {
				sep = 1
			}
			if len(out)+sep+len(section)+len(line) > limit {
				break
			}
			section += line
		}
		if section == c+" headings:" {
			continue
		}
		if out != "" {
			out += "\n"
		}
		out += section
	}
	return out
}

type rulesContextFile struct {
	name  string
	label string
}

// rulesContextPresence splits the policy's files (rules file first, then
// context files in order, deduplicated) into present and missing at the
// root of dir. A name that would leave dir is ignored.
func rulesContextPresence(dir string, p ResolvedRulesContext) (present, missing []rulesContextFile) {
	seen := map[string]bool{}
	base := filepath.Clean(dir)
	add := func(name, label string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		target := filepath.Join(base, name)
		if !strings.HasPrefix(target, base+string(filepath.Separator)) {
			return
		}
		f := rulesContextFile{name: name, label: label}
		if fileExists(target) {
			present = append(present, f)
		} else {
			missing = append(missing, f)
		}
	}
	add(p.RulesFile, "Rules file")
	for _, c := range p.ContextFiles {
		add(c, "Context file")
	}
	return present, missing
}

// withRulesContextBlock appends the worker-prompt rules block to spec
// for the backend that will run the task. It is a no-op for an empty
// dir, and when spec already carries a block (or the no-checkpoints
// rule) it is not appended again, so a retried task is never given
// either twice. Every worker prompt that gets a rules block also gets
// the no-checkpoints-in-the-rules-files rule (withWorkerCheckpointRule);
// the planning block used by decompose does not carry it.
func (m *Manager) withRulesContextBlock(spec, dir, backend string) string {
	if dir == "" {
		return spec
	}
	if strings.Contains(spec, strings.TrimSpace(rulesBlockMarker)) {
		return withWorkerCheckpointRule(spec)
	}
	block, _, _ := m.rulesContextBlock(dir, backend, rulesBlockWorker)
	return withWorkerCheckpointRule(spec + block)
}

// resolvedRulesContext resolves the rules/context policy for this PRD's
// project directory: the installed autonomous.rules_context config
// (project override first, then its global values), then the legacy
// rules_file/context_file keys this manager carries, ending in the
// defaults.
func (m *Manager) resolvedRulesContext(prd *PRD) ResolvedRulesContext {
	return m.resolvedRulesContextDir(prd.ProjectDir)
}

// resolvedRulesContextDir is resolvedRulesContext for a bare directory
// (a task may run in its own directory, not the PRD's).
func (m *Manager) resolvedRulesContextDir(dir string) ResolvedRulesContext {
	m.mu.Lock()
	legacyRules := m.cfg.RulesFile
	legacyContext := m.cfg.ContextFile
	m.mu.Unlock()
	p, err := ResolveRulesContext(rulesContextConfigSnapshot(), legacyRules, legacyContext, dir)
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

// planningRulesBackend names the backend for the digest rule on a
// planning prompt: an unconfigured planning backend falls back to the
// headless ollama default the decompose wiring uses.
func planningRulesBackend(backend string) string {
	if strings.TrimSpace(backend) == "" {
		return "ollama"
	}
	return backend
}

// rulesSectionNamesMaxBytes caps the section-name line given to the planner.
const rulesSectionNamesMaxBytes = 600

// rulesSectionNamesLine returns one short line naming the sections of the
// rules file, the vocabulary for each task's rules_applied list. Empty when
// the rules file is absent or has no headings.
func rulesSectionNamesLine(dir string, p ResolvedRulesContext) string {
	if strings.TrimSpace(p.RulesFile) == "" {
		return ""
	}
	path := filepath.Join(dir, p.RulesFile)
	if !fileExists(path) {
		return ""
	}
	content, err := os.ReadFile(path) // #nosec G304 -- path is the resolved rules file inside the operator-owned project directory
	if err != nil {
		return ""
	}
	const lead = "Rules sections for rules_applied: "
	line := lead
	n := 0
	for _, h := range RulesDigestSections(content) {
		h = strings.Join(strings.Fields(h), " ")
		if h == "" {
			continue
		}
		add := h
		if n > 0 {
			add = "; " + h
		}
		if len(line)+len(add) > rulesSectionNamesMaxBytes {
			break
		}
		line += add
		n++
	}
	if n == 0 {
		return ""
	}
	return line
}
