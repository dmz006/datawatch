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
package autonomous

import (
	"os"
	"path/filepath"
)

// rulesContextSuffix returns a short pointer to the project's
// RulesFile (default "AGENT.md", mentioned once a project_dir exists
// and the file is actually present there) and ContextFile (default
// "CONTEXT.md", only when present — not everyone has one), resolved
// relative to prd's own project_dir. Does not read or inline either
// file's content — see this file's own top comment for why. Empty
// when ProjectDir is unset (nothing to point at yet, e.g. before
// worktree creation) or neither file exists.
func (m *Manager) rulesContextSuffix(prd *PRD) string {
	if prd.ProjectDir == "" {
		return ""
	}
	m.mu.Lock()
	rulesFile := m.cfg.RulesFile
	contextFile := m.cfg.ContextFile
	m.mu.Unlock()
	if rulesFile == "" {
		rulesFile = "AGENT.md"
	}
	if contextFile == "" {
		contextFile = "CONTEXT.md"
	}
	var names []string
	if fileExists(filepath.Join(prd.ProjectDir, rulesFile)) {
		names = append(names, rulesFile)
	}
	if fileExists(filepath.Join(prd.ProjectDir, contextFile)) {
		names = append(names, contextFile)
	}
	if len(names) == 0 {
		return ""
	}
	list := names[0]
	if len(names) > 1 {
		list = names[0] + " and " + names[1]
	}
	return "\n\n--- Project rules ---\nBefore planning, read " + list +
		" at the root of your working directory — they define this" +
		" project's development/testing/documentation rules and" +
		" current state. Follow them when decomposing this PRD."
}

func fileExists(path string) bool {
	_, err := os.Stat(path) // #nosec G304 -- path is joined from operator-configured rules_file/context_file and a PRD's own worktree dir, same trust model as every other project-dir file read in this package
	return err == nil
}
