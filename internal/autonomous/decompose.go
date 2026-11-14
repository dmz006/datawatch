// BL24 — LLM-driven PRD → stories+tasks planning.
//
// The LLM is asked to return JSON shaped like:
//   {
//     "title": "...",
//     "stories": [
//       {
//         "title": "...",
//         "description": "...",
//         "depends_on": ["story-id-or-title"],
//         "tasks": [
//           {"title": "...", "spec": "...", "depends_on": ["task-id-or-title"]}
//         ]
//       }
//     ]
//   }
//
// We tolerate fenced code blocks, smart quotes, and trailing single-
// line comments — common LLM JSON-output pathologies that nightwire
// also handles in prd_builder.py.

package autonomous

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// planWriteFilesRuleBullet is the Rules bullet, shared by both planning
// prompts, that tells the planner to declare files for write tasks and to
// mark read-only tasks. The verb list is written out here because
// planWriteTaskVerbs is a slice; decompose_write_files_rule_test.go checks
// every verb in the slice appears in both prompts. The marker is the
// planReadOnlyMarker constant so the prompt and the plan-coverage check
// stay in step. No percent sign: PlanningPrompt goes through fmt.Sprintf.
const planWriteFilesRuleBullet = `- Every task whose title starts with a write verb (Write, Add, Update, Create,
  Fix, Bump, Implement, Document, Register, Wire, Make, Move, Replace, Rename,
  Extend, Remove) must list in its "files" the repo-relative paths it will
  change. A task changing no files must start its spec with the line
  ` + planReadOnlyMarker + ` followed by a reason; gates, smoke tests, suite runs, evals,
  installs and investigations are read-only and take that line.
`

// planReadOnlyFlagRuleBullet is the Rules bullet, shared by both planning
// prompts, that tells the planner when to set the task-level read_only
// flag. It is the structured counterpart of the READ-ONLY: spec line in
// planWriteFilesRuleBullet and no percent sign: PlanningPrompt goes
// through fmt.Sprintf.
const planReadOnlyFlagRuleBullet = `- "read_only" (optional boolean): set it to true only for a task that
  changes no files (a gate, smoke test, suite run, eval, install or
  investigation), and give such a task an empty "files" list. A task that
  writes, edits, creates, moves or removes any file must omit "read_only"
  or set it false. When unsure, omit it. A read-only task is never planned
  with a file to write: if its only output is a report, it declares that
  one path in "report_path" under .datawatch/reports/ and keeps "files" empty.
`

// planProjectPathRuleBullet is the Rules bullet, shared by both planning
// prompts, that keeps every path a spec names inside the project directory.
// No percent sign: PlanningPrompt goes through fmt.Sprintf.
const planProjectPathRuleBullet = `- No spec line names a path outside the project directory: write every path
  repo-relative (no absolute path, no home folder, no "..").
`

// planReleaseVersionRuleBullet is the Rules bullet, shared by both planning
// prompts, that limits the release-version rewrite to the release itself.
// No percent sign: PlanningPrompt goes through fmt.Sprintf.
const planReleaseVersionRuleBullet = `- Release tasks never contain a fixed version number. The release version is
  taken at merge time as the next patch above main; write 'next patch above
  main' in task titles, specs and descriptions for the release version only,
  and do not repeat a number even if a revision note or earlier plan used one.
  Keep the version numbers of other things (Python, libraries, tools) exactly
  as written; 'the next patch above main' never replaces them.
`

// planRulesAppliedRuleBullet is the Rules bullet, shared by both planning
// prompts, that asks for the per-task rules checklist. No percent sign:
// PlanningPrompt goes through fmt.Sprintf.
const planRulesAppliedRuleBullet = `- "rules_applied": for every task list the sections of the project's rules
  file the task must follow (for example parity, documentation, tests,
  localization, versioning, security, configuration), using the section
  names from the rules file or the rules digest. Use an empty list only for
  a read-only task.
`

// planWorkerProfileRuleBullet is the Rules bullet, shared by both planning
// prompts, that tells the planner how a task names the worker profile it
// needs (a tool or an MCP server). It names the profile line that
// applyWorkerProfileLines reads back out of the spec. No percent sign:
// PlanningPrompt goes through fmt.Sprintf.
const planWorkerProfileRuleBullet = `- A task that needs a tool or an MCP server names its profile with the line
  'worker_profile: datawatch-tools' in its spec, and no other profile line is needed.
`

// PlanningPrompt is the system+user prompt prefix the LLM sees.
// Operators can override via autonomous.planning_prompt config.
const PlanningPrompt = `You are planning a feature request into a structured Product Requirements Document with stories and tasks.

Return ONLY a JSON object with this shape:
{
  "title": "<short feature title>",
  "stories": [
    {
      "title": "<story title>",
      "description": "<one-paragraph description>",
      "files": ["<repo-relative path>", "..."],
      "depends_on": [],
      "tasks": [
        {
          "title": "<task title>",
          "spec": "<concrete instructions for one worker session>",
          "files": ["<repo-relative path>", "..."],
          "depends_on": [],
          "read_only": false, // optional boolean, see the read_only rule below
          "report_path": "", // optional string: only valid on a read_only task; the one report under .datawatch/reports/ it may write
          "rules_applied": ["<rules section name>", "..."],
          "kind": "<smoke|final_gate>" // optional, see the kind rule below
        }
      ]
    }
  ]
}

Rules:
- depends_on lists either story or task titles that must complete first.
- Each task's spec must be self-contained — the worker won't see other tasks.
- Aim for 1-3 stories per PRD, 1-5 tasks per story. Prefer fewer, well-scoped tasks.
- "kind" (optional): tag a task that is a live smoke test with "smoke",
  and the task that verifies the whole feature end-to-end as a final gate
  with "final_gate". Ordinary tasks should omit the field (or use "").
- "files" (Phase 4, v5.26.67): list repo-relative paths each story or
  task is expected to touch. Include both code and docs/test files.
  Empty array is valid when paths are unknown ahead of time. Do NOT
  invent paths that obviously can't exist.
` + planWriteFilesRuleBullet + planReadOnlyFlagRuleBullet + planRulesAppliedRuleBullet + planWorkerProfileRuleBullet + planProjectPathRuleBullet + `- Verification scripts must check only the files their unit of work
  changes (e.g. list them from 'git diff --name-only' plus the untracked
  files, or name them explicitly) and must never run gofmt or a similar
  formatter over a whole package or directory — unrelated files that
  were already unformatted would fail the task.
- Never write a literal absolute path to a system binary (e.g. /usr/bin/foo
  or /bin/bar) in a task spec. Refer to standard system tools by command
  name only (e.g. "the time command") and have the task resolve them at
  runtime via "command -v <name>" / $PATH, with a graceful fallback if
  unavailable. Any literal absolute path in a spec must be one the task
  will actually read or write under the PRD's allowed directories.
` + planReleaseVersionRuleBullet + `- Do not include markdown fences. Return raw JSON only.

Feature request:
%s
`

// PlanningPromptSession is the task delivered to a one-shot coding session
// when decompose_backend is a session-capable backend (claude-code, opencode …).
// First %s = scope block (see ScopeBlock).
// Second %s = absolute path of the JSON output file.
// Third %s = the raw PRD spec (feature request text).
const PlanningPromptSession = `You are decomposing a feature request into a structured Product Requirements Document.

%s
The project directory above is the ONLY place this work happens. Every task you
write must be doable inside the directories above; never tell a worker to work in
another repository or directory, and use paths relative to the project directory.
If the feature request names another repository or URL, treat it as a subject to
research (web or read-only), not as a place to work.

Before writing the plan, explore the project to understand what already exists —
read files that are relevant to the feature request (source code, config, docs,
or whatever the spec calls for).

IMPORTANT: follow the feature request faithfully.
- If the spec asks for research, research.
- If the spec asks for implementation, explore source code.
- Do NOT use prior planning documents, historical PRD outputs, or other
  decomposition artifacts as a substitute for what the spec asks for. Those are
  records of past work, not instructions for this task.
- Base the decomposition on the feature request below and what you observe in
  the project — not on what previous plans happened to produce.

After exploring, write the decomposition as raw JSON to this file:
%s

The JSON must follow this schema exactly:
{
  "title": "<short feature title>",
  "stories": [
    {
      "title": "<story title>",
      "description": "<one-paragraph description>",
      "files": ["<repo-relative path>", "..."],
      "depends_on": [],
      "verification_script": "<repo-relative path to an executable verification script you wrote, or empty string — see the Verification scripts section below>",
      "files_generated": ["<repo-relative path>", "..."],
      "tasks": [
        {
          "title": "<task title>",
          "spec": "<concrete instructions for one worker session — include file paths and context from your exploration>",
          "files": ["<repo-relative path>", "..."],
          "depends_on": [],
          "verification_script": "<repo-relative path to an executable verification script you wrote, or empty string — see the Verification scripts section below>",
          "files_generated": ["<repo-relative path>", "..."],
          "read_only": false, // optional boolean — true only for a task that changes no files, see the read_only rule below
          "report_path": "", // optional string — only valid on a read_only task: the one report under .datawatch/reports/ it may write, with files empty
          "rules_applied": ["<rules section name>", "..."],
          "kind": "<smoke|final_gate>" // optional — "smoke" for a live smoke-test task, "final_gate" for the end-to-end verification task
        }
      ]
    }
  ]
}

Rules:
- depends_on lists either story or task titles that must complete first.
- Each task spec must be self-contained — the worker won't see other tasks.
- Aim for 1-3 stories per PRD, 1-5 tasks per story. Prefer fewer, well-scoped tasks.
- "kind" (optional): tag a task that is a live smoke test with "smoke",
  and the task that verifies the whole feature end-to-end as a final gate
  with "final_gate". Ordinary tasks should omit the field (or use "").
- verification_script / files_generated (B124/BL416): these two fields are
  part of this schema, not optional extras — fill them in per any
  "Verification scripts" instructions appended below. An empty string for
  verification_script is only correct for a unit with no applicable
  check (e.g. a pure-research story producing no files); every unit that
  touches real files should have one.
` + planWriteFilesRuleBullet + planReadOnlyFlagRuleBullet + planRulesAppliedRuleBullet + planWorkerProfileRuleBullet + planProjectPathRuleBullet + `- Verification scripts must check only the files their unit of work
  changes (e.g. list them from 'git diff --name-only' plus the untracked
  files, or name them explicitly) and must never run gofmt or a similar
  formatter over a whole package or directory — unrelated files that
  were already unformatted would fail the task.
- Never write a literal absolute path to a system binary (e.g. /usr/bin/foo
  or /bin/bar) in a task spec. Refer to standard system tools by command
  name only (e.g. "the time command") and have the task resolve them at
  runtime via "command -v <name>" / $PATH, with a graceful fallback if
  unavailable. Any literal absolute path in a spec must be one the task
  will actually read or write under the directories above.
` + planReleaseVersionRuleBullet + `- Write ONLY raw JSON to the file — no markdown fences, no explanatory prose.

Feature request:
%s
`

// DecomposeRequest captures the LLM call inputs.
type DecomposeRequest struct {
	Spec       string
	Backend    string // empty = caller default
	Model      string // empty = backend default; passed to session-based decompose path
	Effort     Effort
	ProjectDir string // for session-based decompose path
	ScopeBlock string // boundary contract rendered by ScopeBlock
	PRDID      string // links the planning session to its PRD so boot-resume can clean it up
	// TimeoutSeconds, when > 0, overrides the effort-scaled default
	// timeout used by decomposeFn (5 min normal, 15 min high/max).
	// Set from autonomous.planning_timeout_seconds config.
	TimeoutSeconds int
	// OnPlannerSpawned, when set, is called once the session-based planner
	// has been created — after the start request succeeds and before the
	// reader starts waiting on the output. It receives the spawned session id
	// and the output file path the planner writes to. A spawn that fails
	// never calls it. decomposeAdmitted wires it to RecordPlannerSession when
	// the caller leaves it nil. Callers that do not spawn a planner ignore it.
	OnPlannerSpawned func(sessionID, outputFile string)
}

// DecomposeFn is the indirection that lets tests inject a fake LLM
// while production wires through to /api/ask or a session.Manager.Start.
type DecomposeFn func(req DecomposeRequest) (jsonResponse string, err error)

// validateParsedPlan rejects a parsed decompose result that cannot
// become a runnable PRD: no stories at all, or stories with no task in
// any of them. ParsePlanning accepts valid JSON regardless of content,
// so a finished decompose must pass this check before its stories are
// stored.
func validateParsedPlan(stories []Story) error {
	if len(stories) == 0 {
		return fmt.Errorf("decompose output contained no stories")
	}
	for _, s := range stories {
		if len(s.Tasks) > 0 {
			return nil
		}
	}
	return fmt.Errorf("decompose output contained %d stories but none had any tasks", len(stories))
}

// normalizeReadOnlyTasks keeps the read_only flag a decomposed task carries
// only when it is consistent: a task that also lists files it will change
// is not read-only, so the flag is cleared and the task takes the tree
// lock as before. report_path follows the same rule — it is only valid
// on a read_only task, so it is kept when the flag stands and cleared
// when the flag is cleared or absent. It reports how many flags it
// cleared.
func normalizeReadOnlyTasks(stories []Story) int {
	cleared := 0
	for si := range stories {
		for ti := range stories[si].Tasks {
			t := &stories[si].Tasks[ti]
			if t.ReadOnly && len(t.FilesPlanned) > 0 {
				t.ReadOnly = false
				cleared++
			}
			if !t.ReadOnly {
				t.ReportPath = ""
			}
		}
	}
	return cleared
}

const (
	rulesAppliedMaxEntries = 24
	rulesAppliedMaxBytes   = 64
)

// normalizeRulesApplied trims, lower-cases and whitespace-collapses each
// entry, drops empties and duplicates, keeps order, and caps the list at
// rulesAppliedMaxEntries entries of at most rulesAppliedMaxBytes bytes.
func normalizeRulesApplied(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range in {
		e = strings.ToLower(strings.Join(strings.Fields(e), " "))
		if len(e) > rulesAppliedMaxBytes {
			e = e[:rulesAppliedMaxBytes]
			for !utf8.ValidString(e) {
				e = e[:len(e)-1]
			}
			e = strings.TrimSpace(e)
		}
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
		if len(out) == rulesAppliedMaxEntries {
			break
		}
	}
	return out
}

// ParsePlanning cleans common LLM JSON pathologies (fences,
// smart quotes, // comments) and unmarshals into a Story slice. Task
// objects may optionally carry a "kind" string ("smoke" / "final_gate" /
// anything else); it lands on Task.Kind, and an absent or unknown value
// is simply the empty/unknown string — never a parse error, so the
// parser stays tolerant of plans that omit the field.
func ParsePlanning(raw string) (title string, stories []Story, err error) {
	cleaned := cleanLLMJSON(raw)
	var doc struct {
		Title   string  `json:"title"`
		Stories []Story `json:"stories"`
	}
	if err := json.Unmarshal([]byte(cleaned), &doc); err != nil {
		return "", nil, fmt.Errorf("parse: %w (cleaned=%q)", err, truncate(cleaned, 200))
	}
	// Tolerate models that emit task keys "name"/"description" instead of
	// "title"/"spec": backfill empty Title/Spec from the same cleaned input,
	// indexed the same way.
	var alt struct {
		Stories []struct {
			Tasks []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tasks"`
		} `json:"stories"`
	}
	// Backfill is best-effort: if the cleaned JSON parses into the
	// primary shape (above) but not the alt-key shape, keep the already
	// parsed fields and carry on rather than hard-failing.
	if err := json.Unmarshal([]byte(cleaned), &alt); err == nil {
		for si, story := range doc.Stories {
			for ti := range story.Tasks {
				if story.Tasks[ti].Title == "" && si < len(alt.Stories) && ti < len(alt.Stories[si].Tasks) {
					story.Tasks[ti].Title = alt.Stories[si].Tasks[ti].Name
				}
				if story.Tasks[ti].Spec == "" && si < len(alt.Stories) && ti < len(alt.Stories[si].Tasks) {
					story.Tasks[ti].Spec = alt.Stories[si].Tasks[ti].Description
				}
			}
			doc.Stories[si] = story
		}
	}
	for _, story := range doc.Stories {
		for ti, task := range story.Tasks {
			if task.Title == "" || task.Spec == "" {
				return "", nil, fmt.Errorf("task in story %q (index %d) has empty title or spec", story.ID, ti)
			}
		}
	}
	for si := range doc.Stories {
		for ti := range doc.Stories[si].Tasks {
			t := &doc.Stories[si].Tasks[ti]
			t.RulesApplied = normalizeRulesApplied(t.RulesApplied)
		}
	}
	applyWorkerProfileLines(doc.Stories)
	return doc.Title, doc.Stories, nil
}

// workerProfileLinePrefix is the key a task spec uses to name its worker
// profile. The key is case-sensitive.
const workerProfileLinePrefix = "worker_profile:"

// ParseWorkerProfileLine returns the profile name from the first line of spec
// that is exactly 'worker_profile: <name>' once surrounding whitespace is
// trimmed. It reports false when no such line exists, or when the name is
// empty or holds internal whitespace.
func ParseWorkerProfileLine(spec string) (string, bool) {
	for _, line := range strings.Split(spec, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, workerProfileLinePrefix) {
			continue
		}
		rest := line[len(workerProfileLinePrefix):]
		if !strings.HasPrefix(rest, " ") {
			continue
		}
		name := strings.TrimSpace(rest)
		if name == "" || strings.ContainsAny(name, " \t") {
			continue
		}
		return name, true
	}
	return "", false
}

// applyWorkerProfileLines copies each task's worker_profile line into its
// WorkerProfile field. A field that is already set keeps its value.
func applyWorkerProfileLines(stories []Story) {
	for si := range stories {
		for ti := range stories[si].Tasks {
			t := &stories[si].Tasks[ti]
			if t.WorkerProfile != "" {
				continue
			}
			if name, ok := ParseWorkerProfileLine(t.Spec); ok {
				t.WorkerProfile = name
			}
		}
	}
}

// cleanLLMJSON strips fences, replaces smart quotes, removes // comments.
// Ported from nightwire/prd_builder.py clean_json_string.
func cleanLLMJSON(s string) string {
	s = strings.TrimSpace(s)
	// Remove surrounding ``` json fences.
	s = regexp.MustCompile("^```(?:json)?\\s*").ReplaceAllString(s, "")
	s = regexp.MustCompile("\\s*```$").ReplaceAllString(s, "")
	// Smart quotes → straight quotes.
	s = strings.NewReplacer(
		"\u201c", "\"", "\u201d", "\"",
		"\u2018", "'", "\u2019", "'",
	).Replace(s)
	// Strip line-comments outside strings.
	var out strings.Builder
	for _, line := range strings.Split(s, "\n") {
		out.WriteString(stripLineComment(line))
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String())
}

// stripLineComment removes `// foo` from a line, respecting in-string
// occurrences of `//` (e.g. URLs).
func stripLineComment(line string) string {
	inStr := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch == '\\' && inStr && i+1 < len(line) {
			i++ // skip next
			continue
		}
		if ch == '"' {
			inStr = !inStr
			continue
		}
		if !inStr && ch == '/' && i+1 < len(line) && line[i+1] == '/' {
			return strings.TrimRight(line[:i], " \t")
		}
	}
	return line
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// BL304 — backward-compat aliases so any external callers still compile.
// DecompositionPrompt is an alias for PlanningPrompt.
const DecompositionPrompt = PlanningPrompt

// ParseDecomposition is an alias for ParsePlanning.
func ParseDecomposition(raw string) (string, []Story, error) { return ParsePlanning(raw) }
