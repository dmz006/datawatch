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

// DefaultDecomposeLintRules returns the built-in default pack fresh
// (a new slice each call, so no caller can mutate the pack).
//
// Nine rules, in the order findings should surface them: the six
// block-severity rules (tasks that cross a hard boundary) first, then
// the three warn-severity hygiene rules.
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
					{SpecRegex: "(?i)\\b(BL|GH|DW|PRD|ISSUE|TICKET|BUG)[#-]?\\d{2,}\\b|\\b#\\d{3,}\\b"},
					{TitleRegex: "(?i)\\b(BL|GH|DW|PRD|ISSUE|TICKET|BUG)[#-]?\\d{2,}\\b|\\b#\\d{3,}\\b"},
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
