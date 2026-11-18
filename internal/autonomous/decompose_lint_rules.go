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
const trackerIDPattern = "(?i)\\b(BL|GH|DW|PRD|ISSUE|TICKET|BUG)[#-]?\\d{2,}\\b|\\b#\\d{3,}\\b"

// DefaultDecomposeLintRules returns the built-in default pack fresh
// (a new slice each call, so no caller can mutate the pack).
//
// Seventeen rules, in the order findings should surface them: the
// thirteen block-severity rules (tasks that cross a hard boundary,
// plus the seven verification-script stability rules that follow
// read_only_unverifiable) first, then the four warn-severity hygiene
// rules.
//
// The two newest stability rules read the script against ground truth
// at lint time: verify_script_unknown_flag checks each invoked
// command's own `--help` for the long flags the script passes, and
// verify_script_text_not_in_spec requires every checked markdown
// heading or output phrase to appear verbatim in the task spec. Both
// stay out of the way by design — commands the environment cannot
// find, and help output that cannot be read, are skipped silently —
// and, like every rule here, an operator keeps them off by listing
// their ids in autonomous.decompose_lint.disabled_rules.
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
			ID:       "artefact_files_allowed",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "artefact_files_allowed: the task's verification script flags unexpected changed files but does not exempt the daemon-generated artefact folders docs/plans/drafts/, docs/plans/parity-issues/, docs/plans/reports/ and the file CHANGELOG.md, so it fails when the daemon writes them; exclude those paths from the changed-file set (or check only the files the task adds).",
			Match: config.DecomposeLintMatch{
				VerificationScriptArtefactFiles: true,
			},
		},
		{
			ID:       "final_gate_artefact_allowed",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "final_gate_artefact_allowed: the task's verification script asserts the whole changed-file set against a finite allow-list with no guard for the final gate rerunning it on the cumulative tree, where every earlier task's files appear; guard the set check (skip it on an env-var or argument the final gate sets) or check only the files this task adds.",
			Match: config.DecomposeLintMatch{
				VerificationScriptFinalGateRerun: true,
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
			ID:       "verify_script_unknown_flag",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "verify_script_unknown_flag: the task's verification script passes a long flag to a command the script invokes (or to one of its subcommands) that the command's own `--help` output does not list; a flag the installed binary does not have cannot be asserted. Use only flags the command actually accepts (check with `<command> --help`), or rephrase the check so it does not depend on that option. Commands the environment cannot find are not checked.",
			Match: config.DecomposeLintMatch{
				VerificationScriptUnknownFlag: true,
			},
		},
		{
			ID:       "verify_script_text_not_in_spec",
			Severity: config.DecomposeLintSeverityBlock,
			Message:  "verify_script_text_not_in_spec: the task's verification script greps or compares against a markdown heading (a `# …` line) or a multi-word output phrase that does not appear verbatim in the task spec; the spec is the contract, so an expectation stated nowhere is unverifiable. State the exact heading or the exact format being checked, word for word, in the task spec.",
			Match: config.DecomposeLintMatch{
				VerificationScriptTextNotInSpec: true,
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
			ID:       "unbounded_load_step",
			Severity: config.DecomposeLintSeverityWarning,
			Message:  "A CPU-load or stress step was requested in the spec; bound it before it runs — run it niced (nice -n 19), use at most a quarter of the cores, give it a hard timeout, and clean up its children with a trap — so it never outlives its owning task.",
			Match: config.DecomposeLintMatch{
				SpecRegex: `(?i)(stress|soak|load)\s*[-_ ]?test|busy\s*[- ]?loop|\bcpu\s*[- ]?(load|stress)\b|pin(ning)?\s+(the\s+)?(cpu|cores?)\b|max(ing|ed)?\s+(out\s+)?(the\s+)?(cpu|cores?)\b|all\s+(the\s+)?(cpu|cores?)\b`,
			},
		},
	}
}
