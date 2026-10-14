package main

import (
	"strings"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// Two distinct "effort" concepts meet in autonomousSpawn:
//
//  1. session.Effort (BL41) — datawatch's legacy thoroughness hint
//     (quick/normal/thorough), sent as the "effort" key of the
//     /api/sessions/start body. It never reaches the claude CLI.
//  2. claude's own --effort reasoning level (low/medium/high/xhigh/max),
//     sent as the "claude_effort" key and applied by session.Manager.Start
//     via backend.SetEffort.
//
// The autonomous Effort enum feeds both. The claude_effort key is
// ADDITIVE: the BL41 "effort" value is unchanged, so existing behavior
// for every backend is preserved and claude-code tasks additionally get
// a real --effort flag.

// mapAutonomousEffortToSession translates an autonomous Effort to the
// BL41 session.Effort thoroughness vocabulary (concept 1 above).
//
// v5.26.11 — translate autonomous.Effort enum
// (low/medium/high/max) to session.EffortLevels enum
// (quick/normal/thorough) before POSTing to /api/sessions/start.
// /api/sessions/start rejects unknown values with "invalid
// effort: must be one of quick, normal, thorough" — every
// PRD with the default `low` effort tripped this and tasks
// went straight to TaskFailed without ever spawning a session.
// The autonomous Effort already lists the session-side aliases
// (quick/normal/thorough) so explicit operator picks pass
// through unchanged.
func mapAutonomousEffortToSession(e autonomouspkg.Effort) string {
	switch e {
	case autonomouspkg.EffortLow, autonomouspkg.EffortQuick:
		return "quick"
	case autonomouspkg.EffortMedium, autonomouspkg.EffortNormal:
		return "normal"
	case autonomouspkg.EffortHigh, autonomouspkg.EffortMax, autonomouspkg.EffortThorough:
		return "thorough"
	}
	return "" // empty → daemon falls back to session.default_effort
}

// mapAutonomousEffortToClaude translates an autonomous Effort to the
// claude CLI's --effort vocabulary (concept 2 above; the same set
// handleClaudeEfforts advertises: low|medium|high|xhigh|max). The
// legacy BL41 aliases map onto their nearest claude level. "xhigh" has
// no autonomous constant but passes through so an operator who typed it
// gets it. Empty or unknown returns "" — no flag; never invent a value.
func mapAutonomousEffortToClaude(e autonomouspkg.Effort) string {
	switch e {
	case autonomouspkg.EffortLow, autonomouspkg.EffortQuick:
		return "low"
	case autonomouspkg.EffortMedium, autonomouspkg.EffortNormal:
		return "medium"
	case autonomouspkg.EffortHigh, autonomouspkg.EffortThorough:
		return "high"
	case autonomouspkg.EffortMax:
		return "max"
	case "xhigh":
		return "xhigh"
	}
	return ""
}

// autonomousClaudeEffortApplies reports whether the effective backend
// for an autonomous spawn is claude-code, i.e. whether a claude_effort
// key (concept 2 above) should be added to the session-start body.
// reqBackend wins when set; otherwise defaultBackend (cfg.Session.LLMBackend)
// decides, with empty meaning the config default, claude-code.
func autonomousClaudeEffortApplies(reqBackend, defaultBackend string) bool {
	b := strings.TrimSpace(reqBackend)
	if b == "" {
		b = strings.TrimSpace(defaultBackend)
		if b == "" {
			return true
		}
	}
	return b == "claude-code"
}

// buildAutonomousSessionBody builds the /api/sessions/start request body
// for autonomousSpawn's local-session path. spec is the fully decorated
// task text; defaultBackend is cfg.Session.LLMBackend.
func buildAutonomousSessionBody(req autonomouspkg.SpawnRequest, spec, defaultBackend string) map[string]any {
	sessionBody := map[string]any{
		"task":            spec,
		"project_dir":     req.ProjectDir,
		"project_profile": req.ProjectProfile,
		"backend":         req.Backend,
		"name":            "autonomous:" + req.Title,
		"effort":          mapAutonomousEffortToSession(req.Effort),
		"permission_mode": req.PermissionMode,
		"model":           req.Model,
		"lsp_language":    req.LSPLanguage,
		"one_shot":        true,
		"prd_id":          req.PRDID,
		"task_id":         req.TaskID,
		"story_id":        req.StoryID, // BL385 Phase 4 — memory scope routing
	}
	// Real claude --effort pass-through (additive to "effort" above).
	// session.Manager.Start applies ClaudeEffort via an interface check
	// (SetEffort(string)) that only the claude-code backend implements,
	// so it would be a harmless no-op elsewhere — gating keeps request
	// bodies for other backends unchanged.
	//
	// KNOWN LIMITATION (pre-existing for the REST claude_effort field,
	// not fixed here): llm.Get returns a shared singleton claude-code
	// Backend, so SetEffort is sticky across sessions — a later session
	// sent without claude_effort inherits the previous level.
	if autonomousClaudeEffortApplies(req.Backend, defaultBackend) {
		if ce := mapAutonomousEffortToClaude(req.Effort); ce != "" {
			sessionBody["claude_effort"] = ce
		}
	}
	// BL407 Phase 2 — a worktree-mode PRD needs real commits on
	// its branch regardless of this daemon's session.auto_git_commit
	// default; only ever forces true, never forces false (an
	// operator who wants commits elsewhere keeps getting them).
	if req.ForceAutoGitCommit {
		sessionBody["auto_git_commit"] = true
	}
	return sessionBody
}
