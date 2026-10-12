// BL416 Phase 2 — decompose contract change (plan:
// docs/plans/2026-10-12-tooled-deterministic-guardrails.md, Decisions
// 2, 3 and 7). Phase 1 (v9.0.36) added Task/Story.VerificationScript
// and FilesGenerated as inert schema; this is their first writer. The
// planning session itself — not datawatch — writes a real executable
// script per task/story under .datawatch/verify/ and reports its path
// back in the plan JSON, so ParsePlanning needs no change (both json
// tags already exist).
//
// Gate reuse: same `prd.ProjectDir == ""` check as rulesContextSuffix.
// An empty ProjectDir is already the signal that the decompose backend
// is headless/ask-compatible (no file access, nothing to write into) —
// no new backend detection, and no duplicate of cmd/datawatch's private
// askCompatible closure. Headless backends keep VerificationScript
// empty; quality_gates stays their only mechanical check (plan Phase 2).
//
// Like rulesContextSuffix after B122, the text is static instruction
// plus the two configured filenames — it never reads, lists or inlines
// anything from the repo, so its size does not scale with repo
// contents. Script bodies go to disk via the session's own tools, never
// into the response.
package autonomous

// verificationScriptSuffix returns static instructions telling a
// tooled planning session (running in prd.ProjectDir) to Reuse-and-
// Expand existing scripts/rules commands, then write per-task or
// per-story verification scripts under .datawatch/verify/ and record
// each path in both verification_script and files_generated. Falls
// back to ecosystem-convention detection when neither the rules file
// nor the context file exists (Decision 7). Empty when ProjectDir is
// unset — see this file's own top comment for why.
func (m *Manager) verificationScriptSuffix(prd *PRD) string {
	if prd.ProjectDir == "" {
		return ""
	}
	rulesFile, contextFile := m.rulesContextFilenames()
	return "\n\n--- Verification scripts ---\n" +
		"Before finalizing your JSON plan, give each task (or story, when a" +
		" story-level check fits that unit of work better) whose verification" +
		" this project's rules require a real verification script:\n" +
		"1. Reuse first. Run a real `ls scripts/` and `head`/`grep` the" +
		" candidates — do not trust a hand-copied list of scripts, it drifts;" +
		" check what's already there. Also check " + rulesFile + " and " +
		contextFile + " for commands that already cover what the unit needs" +
		" to verify, before writing anything new.\n" +
		"2. Write an executable script at `.datawatch/verify/<name>.sh`" +
		" (relative to your working directory) running whatever this repo's" +
		" rules require for the files that unit touches. Work out what" +
		" applies to THIS repo — no specific tool is assumed. IDs are" +
		" assigned only after your plan is parsed, so <name> is a short," +
		" unique, filesystem-safe slug of the task (or story) title." +
		" `chmod +x` it.\n" +
		"3. In the JSON, set that unit's `verification_script` to the" +
		" relative path and also list the same path in its" +
		" `files_generated` array.\n" +
		"4. If neither " + rulesFile + " nor " + contextFile + " exists, do" +
		" not skip verification: detect the ecosystem conventions actually" +
		" present (Makefile lint/test targets, package.json scripts," +
		" pyproject.toml, go.mod + standard go tooling, etc.) and script" +
		" from those.\n" +
		"Do NOT inline any file content (scripts, rules files, source) in" +
		" your response — only the verification_script path goes in the" +
		" JSON; write script bodies to disk with your own tools."
}
