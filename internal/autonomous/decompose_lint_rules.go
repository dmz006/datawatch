// decompose_lint_rules.go — the built-in decompose-lint default pack.
//
// The pack is data: a slice of config.DecomposeLintRule values,
// defined with the same types an operator writes in
// autonomous.decompose_lint.rules. EffectiveRules
// (internal/autonomous/decompose_lint.go) merges this pack with the
// operator's rules (same id = operator rule replaces this one;
// disabled rules are dropped), so the pack is the floor every
// decompose-lint run starts from and the operator's config the
// ceiling over it.
//
// The regular expressions are deliberately generic — they describe
// the *shape* of a dangerous task spec (a version bump, a hosting-CLI
// call, a daemon start, an operator dot-folder read, a tracker-id
// reference) and must match for any operator on any host; they never
// name a particular project, repository or home directory.
package autonomous

import (
	"github.com/dmz006/datawatch/internal/config"
)

// decomposeLintRO is the shared read_only=true pointer used by the
// predicate rules (a rules-level zero value must not be shared, but a
// constant pointer inside the pack is immutable by contract: callers
// copy rule values, and the match engine reads the flag only).
var decomposeLintRO = true

// trackerIDPattern is the tracker-id shape the names_tracker_id rule
// matches; the spec lint (spec_lint.go) compiles the same string so the
// two never drift.
//
// A bare #123 is a tracker id only after a tracker word ("issue #123",
// "fixes #4521"). A bare #\d{3,} on its own is also a CSS hex colour
// (#123456, #abc123), so it never matches without that context; the
// trailing \b keeps a digit run that continues into hex letters
// (#123abc) from matching.
const trackerIDPattern = "(?i)\\b(BL|GH|DW|PRD|ISSUE|TICKET|BUG)[#-]?\\d{2,}\\b" +
	"|\\b(?:issues?|pr|pull request|tickets?|bugs?|fix(?:es|ed)?|close[sd]?|resolve[sd]?|refs?)\\s+#\\d{3,}\\b"

// versionNumberPattern is a dotted version that belongs to a tool or
// library (a Python, Go or Node version, a library pin) rather than to
// the release; the version_number_in_spec rule matches it in a spec or
// title.
const versionNumberPattern = `(?i)\b(?:python3?|py|node(?:\.?js)?|golang|ruby|java|jdk|rust|rustc|php|perl|django|flask|fastapi|numpy|pandas|scipy|pytorch|torch|tensorflow|react|vue|angular|rails|postgres(?:ql)?|mysql|redis|kubernetes|docker|ollama|cuda|npm|pip|library|package|module|dependency|sdk|runtime|interpreter|compiler)\s*(?:version\s*|v)?\d+\.\d+(?:\.\d+)?\b` +
	`|\bgo\s*(?:version\s*|v)?1\.\d{1,2}(?:\.\d+)?\b` +
	`|\b[A-Za-z][A-Za-z0-9_-]*==\d+\.\d+(?:\.\d+)?\b`

// DefaultDecomposeLintRules returns the built-in default pack fresh
// (a new slice each call, so no caller can mutate the pack).
//
// Twenty-one rules, in the order findings should surface them: the sixteen
// block-severity rules (tasks that cross a hard boundary — including the
// two spec-line rules that follow read_only_unverifiable — plus the
// verification-script stability and script-content rules) first, then the
// five warn-severity hygiene rules.
func DefaultDecomposeLintRules() []Rule {
	return []Rule{
		{
			ID:       "edits_version_variable",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "Task spec targets a version variable (a bump or edit of a version field/constant); versioning is owned by the release path, not by tasks.",
			Match: config.DecomposeLintMatch{
				SpecRegex: "(?i)\\bversion\\b[^.\\n]{0,24}[=:]|\\bbump\\b[^.\\n]{0,30}\\bversion\\b|\\b(set|update|edit|writ)[a-z]*[^.\\n]{0,40}\\bversion\\s+(constant|variable|field)\\b",
			},
		},
		{
			ID:       "runs_hosting_cli",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "Task spec invokes the hosting CLI (gh/glab/hub); hosting operations run on the operator's host, not inside a task.",
			Match: config.DecomposeLintMatch{
				SpecRegex: "(^|[\\s;&|`])(gh|glab|hub)\\s+(pr|branch|repo|issue|issues|run|runs|api|project|projects|release|releases|auth|search|workflow)\\b",
			},
		},
		{
			ID:       "starts_daemon_outside_sandbox",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "Task spec starts or restarts a daemon outside the sandbox (systemctl/launchctl/supervisorctl/pm2/nohup); long-running services are started by the operator, not by a task.",
			Match: config.DecomposeLintMatch{
				SpecRegex: "(?i)\\b(systemctl|launchctl|supervisorctl|pm2|service|systemd-run)\\b[^.\\n;]{0,16}\\b(start|restart|reload|enable|add|up|daemon-reload)\\b|\\bnohup\\b[^;&|]{1,64}&",
			},
		},
		{
			ID:       "reads_operator_data_or_config_folders",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "Task spec reads the operator's data or config folders (a home-directory dot folder); tasks operate on the project tree only.",
			Match: config.DecomposeLintMatch{
				SpecRegex: "(~|\\$HOME|%(?:APPDATA|USERPROFILE)%|/home/[A-Za-z0-9._-]+|/Users/[A-Za-z0-9._-]+)(/|\\s)*\\.(datawatch|config|local|cache|ssh|aws|gnupg|netrc|dotfiles)\\b",
			},
		},
		{
			ID:       "read_only_but_writes_files",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "Task is marked read_only but lists files it will change; a read_only task must declare no files.",
			Match: config.DecomposeLintMatch{
				ReadOnly:   &decomposeLintRO,
				FilesRegex: ".+",
			},
		},
		{
			ID:       "read_only_unverifiable",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "Task is marked read_only and declares neither a verification script nor a report path; without one, a failure of it produces nothing to inspect. Give it a verification script (its own checks, without modifying the tree), or a report path under .datawatch/reports/ where it can record its verdict.",
			// A task is read-only by its read_only flag or by the
			// READ-ONLY: marker opening its spec; any_of covers both.
			// The regex is the first-non-empty-line rule of
			// specIsReadOnly, so the lint and the plan-coverage gate
			// agree on which tasks are read-only.
			Match: config.DecomposeLintMatch{
				AnyOf: []config.DecomposeLintMatch{
					{ReadOnly: &decomposeLintRO},
					{SpecRegex: `(?i)\A\s*` + planReadOnlyMarker},
				},
				VerificationScriptEmpty: true,
				ReportPathEmpty:         true,
			},
		},
		{
			ID:       "read_only_task_writes_file",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "read_only_task_writes_file: a read-only task's spec tells it to write a file other than its report path; give it a report path under .datawatch/reports instead.",
			// Needs the task's report_path and the READ-ONLY: marker, so it
			// is a predicate rather than a regex.
			Match: config.DecomposeLintMatch{
				ReadOnlyWritesOutsideReport: true,
			},
		},
		{
			ID:       "spec_path_outside_allowed_dirs",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "spec_path_outside_allowed_dirs: the spec names an absolute path outside the project directory; use a repo-relative path or drop the line.",
			Match: config.DecomposeLintMatch{
				SpecAbsolutePathOutsideProject: true,
			},
		},
		{
			ID:       "verify_script_task_files_only",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "verify_script_task_files_only: the task's verification script compares only against the task's own files (a literal allowed-file list or a diff of HEAD, the working tree or main) and never against the merge base, so it fails once earlier tasks land; compare against `git merge-base` (directly or via a sourced helper) so the check is cumulative.",
			Match: config.DecomposeLintMatch{
				VerificationScriptTaskFilesOnly: true,
			},
		},
		{
			ID:       "verify_script_docs_without_sync",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "verify_script_docs_without_sync: the task's verification script checks docs, the embedded docs copy or how-to coverage but never runs `make sync-docs`, so it fails on a stale embedded copy; run `make sync-docs` first (directly or via a sourced helper).",
			Match: config.DecomposeLintMatch{
				VerificationScriptDocsWithoutSync: true,
			},
		},
		{
			ID:       "verify_script_rejects_own_files",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "verify_script_rejects_own_files: the task's verification script builds its allowed-file list from literals that omit files the task itself writes or the standard allowed set (changelog, README, docs, locales, tests), so it fails on the task's own correct work; list the task's files and the standard set, or use the shared allowed list.",
			Match: config.DecomposeLintMatch{
				VerificationScriptRejectsOwnFiles: true,
			},
		},
		{
			ID:       "verify_script_needs_later_task_output",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "verify_script_needs_later_task_output: the task's verification script requires a file or phrase that only a task it does not depend on creates, so it fails when this task is verified; add the depends_on edge, or move the check to the later task's script.",
			Match: config.DecomposeLintMatch{
				VerificationScriptNeedsLaterTaskOutput: true,
			},
		},
		{
			ID:       "verify_script_sh_for_bash",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "verify_script_sh_for_bash: the task's verification script uses bash-only syntax ([[ ]], arrays, process substitution, pipefail) under an sh shebang, or a spec or script line runs a bash script with sh; use a bash shebang and run it with bash.",
			Match: config.DecomposeLintMatch{
				VerificationScriptShForBash: true,
			},
		},
		{
			ID:       "verify_script_pipe_grep_q",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "verify_script_pipe_grep_q: the task's verification script pipes into grep -q; grep -q exits at the first match, the producer gets SIGPIPE and the pipeline fails under pipefail. Capture the output first (out=$(cmd); grep -q pat <<<\"$out\") or drop -q and send the output to /dev/null.",
			Match: config.DecomposeLintMatch{
				VerificationScriptPipeGrepQ: true,
			},
		},
		{
			ID:       "verify_script_hex_pattern",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "verify_script_hex_pattern: a grep pattern in the task's verification script matches a bare #<digits> run, which also matches CSS hex colours (#123456) as tracker ids; require a tracker word before it (issue, fixes, closes) or use the BL/GH-prefixed id forms.",
			Match: config.DecomposeLintMatch{
				VerificationScriptHexPattern: true,
			},
		},
		{
			ID:       "final_gate_without_script",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "final_gate_without_script: a final_gate task declares no verification script, no verification_script_reason and no usable report path; the end-to-end gate must be verifiable, so give it a script, a report path under .datawatch/reports/, or state why it has none.",
			Match: config.DecomposeLintMatch{
				Kind:                          TaskKindFinalGate,
				VerificationScriptEmpty:       true,
				VerificationScriptReasonEmpty: true,
				// A usable report path is a verification surface (the
				// built-in final-gate template relies on it), as in
				// read_only_unverifiable.
				ReportPathEmpty: true,
			},
		},
		{
			ID:       "no_verification_script",
			Severity: config.DecomposeLintSeverityWarning,
			Message:  "Task has no verification script; every decomposed task should declare one so its result can be checked.",
			Match: config.DecomposeLintMatch{
				VerificationScriptEmpty: true,
			},
		},
		{
			ID:       "unexecutable_verification_script",
			Severity: config.DecomposeLintSeverityWarning,
			Message:  "Task's verification script is not a runnable script file (a command string, or a path that is missing, not executable or without a shebang); give it a repo-relative script file that exists and starts with a shebang.",
			Match: config.DecomposeLintMatch{
				VerificationScriptUnexecutable: true,
			},
		},
		{
			ID:       "names_tracker_id",
			Severity: config.DecomposeLintSeverityWarning,
			Message:  "Task spec or title names a tracker id; task text should describe the work itself, not reference external tracking items by id.",
			Match: config.DecomposeLintMatch{
				// spec-or-title (any_of): a bare spec_regex+title_regex
				// pair would AND the two fields instead of OR-ing them.
				AnyOf: []config.DecomposeLintMatch{
					{SpecRegex: trackerIDPattern},
					{TitleRegex: trackerIDPattern},
				},
			},
		},
		{
			ID:       "version_number_in_spec",
			Severity: config.DecomposeLintSeverityWarning,
			Message:  "Spec or title names a dotted version of a tool or library (not the release); keep it unambiguous: name the tool, never write it as the release number.",
			Match: config.DecomposeLintMatch{
				AnyOf: []config.DecomposeLintMatch{
					{SpecRegex: versionNumberPattern},
					{TitleRegex: versionNumberPattern},
				},
			},
		},
		{
			ID:       "unbounded_load_step",
			Severity: config.DecomposeLintSeverityWarning,
			Message:  "A CPU-load or stress step was requested in the spec; bound it before it runs — run it niced (nice -n 19), use at most a quarter of the cores, give it a hard timeout, and clean up its children with a trap — so it never outlives its owning task.",
			Match: config.DecomposeLintMatch{
				SpecRegex: `(?i)(stress|soak|load)\s*[-_ ]?test|busy\s*[- ]?loop|\bcpu\s*[- ]?(load|stress)\b|pin(ning)?\s+(the\s+)?(cpu|cores?)\b|max(ing|ed)?\s+(out\s+)?(the\s+)?(cpu|cores?)\b|all\s+(the\s+)?(cpu|cores?)\b`,
			},
		},
	}
}
