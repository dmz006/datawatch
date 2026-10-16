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

// isLightweightHarnessKind (2026-10-15, live-validated) reports whether a
// resolved backend kind is one where the heavy standard task harness
// (CHECKPOINT PROTOCOL + BEFORE YOU FINISH, see buildLightweightOrFullHarnessBlock)
// has been empirically shown to hurt rather than help. Confirmed live: a
// task that stalled at "awaiting_input" with zero tool calls under the
// full boilerplate completed the identical work correctly, end to end, in
// about a minute, once the boilerplate was replaced with a one-line
// equivalent. Scoped narrowly to the two backends this was actually
// tested against — claude-code has shown no evidence the heavy harness
// hurts it and keeps today's unchanged behavior.
func isLightweightHarnessKind(kind string) bool {
	return kind == "opencode" || kind == "opencode-acp"
}

// buildLightweightOrFullHarnessBlock returns the checkpoint+pre-completion
// instructions appended to every one-shot task spec. The full block (long
// form, unchanged from before this change) explains WHY each step matters;
// the lightweight form keeps only the checkpoint directive, for backends
// where the long form has been shown to consume enough of a small model's
// attention to make it state a plan and then stop without ever calling a
// tool. The lightweight form deliberately DROPS the "before you finish,
// check your own commit" self-check (operator-raised 2026-10-15): it's
// redundant harness weight, not new coverage — the verifier (taskProducedNoOutput
// in verifier_diff.go) already catches an empty diff after the fact, and
// the existing RetryHint mechanism already feeds that back as a revision
// on the next attempt. Checking it twice (once in the prompt, once in the
// verifier) costs attention for no extra safety on this path.
func buildLightweightOrFullHarnessBlock(lightweight bool) string {
	if lightweight {
		return "\n\n---\n" +
			"Checkpoint every 5-10 min: write CHECKPOINT.md (steps done/now/remaining). " +
			"Then keep working immediately in the same turn — do not stop or wait. " +
			"CHECKPOINT.md must not be committed. Commit your work when done.\n" +
			"---"
	}
	return "\n\n---\n" +
		"CHECKPOINT PROTOCOL (required every 5–10 minutes of work):\n" +
		"1. Write/update CHECKPOINT.md in the working directory with:\n" +
		"   - Timestamp, task title, steps completed, current step, steps remaining.\n" +
		"2. If the memory_remember MCP tool is available, call it:\n" +
		"   memory_remember(\"task checkpoint\", \"<title>: done=<X>, now=<Y>, remaining=<Z>\")\n" +
		"These checkpoints let work resume from the last save point if this session is reset.\n" +
		"A checkpoint is a SAVE, not a STOP: immediately after writing one, continue working " +
		"on the next step in the SAME turn — do not end your turn or wait for a response. " +
		"No one is watching this session to resume it after a pause; if you stop here, the task " +
		"stalls and fails. Keep going until the task is fully done and you emit DATAWATCH_COMPLETE. " +
		"Before your final commit, make sure CHECKPOINT.md is not staged or committed — it is " +
		"scratch state for this run, not part of the deliverable.\n" +
		"---\n\n" +
		"---\n" +
		"BEFORE YOU FINISH — do this check now, not as a description of having done it:\n" +
		"If this task's spec asked you to commit your work, run `git log --oneline -1` (and " +
		"`git status --short`) in your working directory RIGHT NOW. If there is no new commit " +
		"beyond the baseline, or there are uncommitted changes, your task is NOT done — writing " +
		"a summary of what you changed is not the same as committing it. Stage and commit now, " +
		"THEN emit DATAWATCH_COMPLETE. Only emit DATAWATCH_COMPLETE after this check passes.\n" +
		"---"
}
