// decompose_lint.go — decompose-lint settings (autonomous.decompose_lint):
// the check over a freshly decomposed task graph, its valid modes, the
// retry budget normalization, and the rule/template definitions it
// enforces. Mirrors the plan-coverage settings style
// (internal/config/plan_coverage.go): raw unset values resolve to safe
// defaults, and a misconfigured value is a config error (Load refuses
// the file) rather than a silent fallback — a typo'd mode must never
// quietly mean "enforce".
package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Decompose-lint modes (AutonomousConfig.DecomposeLint.Mode).
const (
	// DecomposeLintModeOff disables the decompose-lint check.
	DecomposeLintModeOff = "off"
	// DecomposeLintModeShadow runs the check and records findings but
	// never blocks decomposition.
	DecomposeLintModeShadow = "shadow"
	// DecomposeLintModeEnforce stops decomposition on a blocking
	// finding (after the retry budget is exhausted). This is the
	// default when the setting is empty/unset.
	DecomposeLintModeEnforce = "enforce"
)

// ValidDecomposeLintModes lists the accepted mode values, for error
// messages and UI validation.
var ValidDecomposeLintModes = []string{DecomposeLintModeOff, DecomposeLintModeShadow, DecomposeLintModeEnforce}

// Decompose-lint severities (DecomposeLintRule.Severity).
const (
	// DecomposeLintSeverityWarning surfaces the finding but does not
	// block decomposition (even in enforce mode).
	DecomposeLintSeverityWarning = "warn"
	// DecomposeLintSeverityBlock stops decomposition in enforce mode.
	DecomposeLintSeverityBlock = "block"
)

// ValidDecomposeLintSeverities lists the accepted severity values.
var ValidDecomposeLintSeverities = []string{DecomposeLintSeverityWarning, DecomposeLintSeverityBlock}

// DefaultDecomposeLintMaxRetries is how many times a blocked
// decomposition is re-run with the findings fed back before the block
// stands. An explicit 0 disables auto-retry and is a valid value.
const DefaultDecomposeLintMaxRetries = 2

// DecomposeLintMaxRetriesCap is the upper bound for max_retries; values
// above it clamp to the cap. Negative values clamp to 0.
const DecomposeLintMaxRetriesCap = 5

// decomposeLintRuleIDPattern is the shape of a legal rule/template id:
// one or more of [a-z0-9_.-]. Uppercase letters and anything else are a
// config error (ids are referenced in findings and logs in lowercase).
var decomposeLintRuleIDPattern = regexp.MustCompile(`^[a-z0-9_.-]+$`)

// decomposeLintMatchMaxDepth caps any_of/all_of nesting, so a corrupt
// or hostile config can not stack-overflow the validator.
const decomposeLintMatchMaxDepth = 8

// DecomposeLintConfig is the autonomous.decompose_lint sub-config.
//
// No omitempty on any field, deliberately: handleAutonomousConfig
// (internal/server/autonomous.go) round-trips the manager's live config
// through json.Marshal + json.Unmarshal onto AutonomousConfig before
// persisting, and omitempty on Mode/Rules elides an empty/explicitly
// cleared value and leaves the previous stored value in place. The
// same gotcha as AutonomousConfig.PlanCoverageCheck.
type DecomposeLintConfig struct {
	// Mode is "off", "shadow" or "enforce" (default when empty) — see
	// ValidDecomposeLintModes. Unknown values are a config error.
	Mode string `yaml:"mode" json:"mode"`
	// MaxRetries is how many times a blocked decomposition is re-run
	// with the findings fed back. 0 (never retry) is a valid explicit
	// value; negatives clamp to 0; values above
	// DecomposeLintMaxRetriesCap clamp to the cap — see
	// NormalizeDecomposeLintMaxRetries.
	MaxRetries int `yaml:"max_retries" json:"max_retries"`
	// Rules is the ordered list of lint rules. Empty (unset) means no
	// rules: the check finds nothing in any mode.
	Rules []DecomposeLintRule `yaml:"rules" json:"rules"`
	// DisabledRules lists rule ids to drop from the effective rule set
	// (the internal/autonomous package resolves which rules apply: the
	// built-in default pack merged with these operator rules, minus any
	// rule whose Disabled flag is set or whose id appears here).
	// No omitempty, deliberately (same round-trip guarantee as Rules).
	DisabledRules []string `yaml:"disabled_rules" json:"disabled_rules"`
}

// DecomposeLintRule is one decompose-lint rule: the fields of a task
// (and its spec) that must match Match, and the finding text (Message)
// emitted when they do.
type DecomposeLintRule struct {
	// ID is required, unique across the config and restricted to
	// [a-z0-9_.-] — it is named in findings and operator docs.
	ID string `yaml:"id" json:"id"`
	// Severity is "block" (stops decomposition in enforce mode) or
	// "warn" (reported only). Empty normalizes to "warn"; any other
	// value is a config error.
	Severity string `yaml:"severity" json:"severity"`
	// Match describes the task fields the rule fires on.
	Match DecomposeLintMatch `yaml:"match" json:"match"`
	// Message is the finding text. Required (a rule with no message
	// cannot be surfaced) — except for disabled rules, which need only
	// a legal, unique ID to name the rule they disable.
	Message string `yaml:"message" json:"message"`
	// Disabled removes this rule from the effective set. A bare
	// {id: ..., disabled: true} entry is a valid way to disable a
	// built-in rule of the same id (or a previously added operator
	// rule): severity, message and match stay unused and are not
	// validated.
	Disabled bool `yaml:"disabled" json:"disabled"`
}

// DecomposeLintMatch describes which tasks a rule fires on. The string
// fields are Go regular expressions matched against the named field
// (SpecRegex → task spec, TitleRegex → task title, FilesRegex → task
// files list). A non-nil ReadOnly asserts the task's read_only flag
// (exact match); VerificationScriptEmpty asserts the task declares no
// verification script and ReportPathEmpty asserts the task has no
// usable report path (empty, or not a clean path under
// .datawatch/reports/). AnyOf/AllOf are lists of nested conditions:
// matching any (all) of them is sufficient (required), combined with
// the rule-match conditions that are present at this level.
type DecomposeLintMatch struct {
	SpecRegex  string `yaml:"spec_regex,omitempty" json:"spec_regex,omitempty"`
	TitleRegex string `yaml:"title_regex,omitempty" json:"title_regex,omitempty"`
	// ReadOnly is tri-state: nil = do not check, true/false = assert the
	// task's read_only flag. Pointer so an explicit false survives a YAML
	// or JSON round-trip (same pattern as
	// AutonomousConfig.PlanCoverageSuppressCreatedFiles).
	ReadOnly *bool `yaml:"read_only,omitempty" json:"read_only,omitempty"`
	// VerificationScriptEmpty asserts the task has no verification
	// script. Absent (false) = do not check.
	VerificationScriptEmpty bool `yaml:"verification_script_empty,omitempty" json:"verification_script_empty,omitempty"`
	// ReportPathEmpty asserts the task has no usable report path —
	// report_path is empty, or it fails validation (a non-empty,
	// repo-relative, clean path under .datawatch/reports/). A read-only
	// task with neither a verification script nor a usable report path
	// emits nothing to inspect when it fails. Use it with
	// verification_script_empty to flag such tasks. Absent (false) = do
	// not check.
	ReportPathEmpty bool `yaml:"report_path_empty,omitempty" json:"report_path_empty,omitempty"`
	// VerificationScriptUnexecutable asserts the task's verification
	// script is not a runnable file: a command string, or (when the
	// project directory is known) a path that is missing, not
	// executable, shebang-less or outside the project. A script the
	// task lists in its own files is not flagged. Absent (false) = do
	// not check.
	VerificationScriptUnexecutable bool `yaml:"verification_script_unexecutable,omitempty" json:"verification_script_unexecutable,omitempty"`
	// VerificationScriptReasonEmpty asserts the task states no reason for
	// lacking a verification script (verification_script_reason is
	// empty). Absent (false) = do not check.
	VerificationScriptReasonEmpty bool `yaml:"verification_script_reason_empty,omitempty" json:"verification_script_reason_empty,omitempty"`
	// VerificationScriptTaskFilesOnly asserts the task's verification
	// script (read from the project directory, plus any repo-relative
	// helper it sources, one level deep) compares only against the
	// task's own files — a literal allowed-file list equal to them, or
	// a file-listing diff of HEAD, the working tree or main — and never
	// against the merge base. A script that does not exist yet is
	// skipped. Absent (false) = do not check.
	VerificationScriptTaskFilesOnly bool `yaml:"verification_script_task_files_only,omitempty" json:"verification_script_task_files_only,omitempty"`
	// VerificationScriptDocsWithoutSync asserts the task's verification
	// script (and followed helpers) checks docs, the embedded docs copy
	// or how-to coverage but does not run `make sync-docs`. A script
	// that does not exist yet is skipped. Absent (false) = do not check.
	VerificationScriptDocsWithoutSync bool `yaml:"verification_script_docs_without_sync,omitempty" json:"verification_script_docs_without_sync,omitempty"`
	// VerificationScriptRejectsOwnFiles asserts the task's verification
	// script (and followed helpers) builds an allowed-file list or a
	// "fail on any changed file outside a short list" check from literals
	// only — without the shared allowed list — that omits a file the task
	// itself plans or the standard allowed set (changelog, README, docs,
	// locales, tests). The offending line is the finding excerpt. A script
	// that does not exist yet is skipped. Absent (false) = do not check.
	VerificationScriptRejectsOwnFiles bool `yaml:"verification_script_rejects_own_files,omitempty" json:"verification_script_rejects_own_files,omitempty"`
	// VerificationScriptNeedsLaterTaskOutput asserts the script requires
	// a file (a path test or a grep target) or a phrase that only a task
	// it does not depend on creates, judged against the files and
	// files_generated of every other task in the plan and the depends_on
	// edges of the task and its story. Needs the whole plan, so it holds
	// only inside a plan-wide lint. A script that does not exist yet is
	// skipped. Absent (false) = do not check.
	VerificationScriptNeedsLaterTaskOutput bool `yaml:"verification_script_needs_later_task_output,omitempty" json:"verification_script_needs_later_task_output,omitempty"`
	// VerificationScriptShForBash asserts the script uses bash-only syntax
	// ([[ ]], arrays, process substitution, pipefail) under an sh
	// shebang, or that the task's spec (or a line of the script) runs a
	// bash script with sh instead of bash. A script that does not exist
	// yet is skipped. Absent (false) = do not check.
	VerificationScriptShForBash bool `yaml:"verification_script_sh_for_bash,omitempty" json:"verification_script_sh_for_bash,omitempty"`
	// VerificationScriptPipeGrepQ asserts the script pipes into
	// grep -q (the early exit of grep -q breaks the producer with SIGPIPE
	// under pipefail). A script that does not exist yet is skipped.
	// Absent (false) = do not check.
	VerificationScriptPipeGrepQ bool `yaml:"verification_script_pipe_grep_q,omitempty" json:"verification_script_pipe_grep_q,omitempty"`
	// VerificationScriptHexPattern asserts a grep pattern in the script
	// matches a bare #<digits> run — the tracker-id shape that also matches
	// CSS hex colours (#123456) — without a tracker word before it. A
	// script that does not exist yet is skipped. Absent (false) = do not
	// check.
	VerificationScriptHexPattern bool `yaml:"verification_script_hex_pattern,omitempty" json:"verification_script_hex_pattern,omitempty"`
	// ReadOnlyWritesOutsideReport asserts the task is read-only (the
	// read_only flag, or a spec whose first non-empty line opens with
	// the READ-ONLY: marker) and a spec line tells it to write, create
	// or edit a file other than its single report_path. The offending
	// spec line is the finding excerpt. Absent (false) = do not check.
	ReadOnlyWritesOutsideReport bool `yaml:"read_only_writes_outside_report,omitempty" json:"read_only_writes_outside_report,omitempty"`
	// SpecAbsolutePathOutsideProject asserts a spec line names an
	// absolute filesystem path (under a system root such as /usr, /opt,
	// /etc, /var, /home or /tmp) that is not inside the project
	// directory. Repo-relative paths and paths inside the project tree
	// are ignored; so are shebang lines and /dev paths. The offending
	// spec line is the finding excerpt. Absent (false) = do not check.
	SpecAbsolutePathOutsideProject bool `yaml:"spec_absolute_path_outside_project,omitempty" json:"spec_absolute_path_outside_project,omitempty"`
	// Kind asserts the task's kind (for example "final_gate" or
	// "smoke"), compared case-insensitively. Empty = do not check.
	Kind       string `yaml:"kind,omitempty" json:"kind,omitempty"`
	FilesRegex string `yaml:"files_regex,omitempty" json:"files_regex,omitempty"`
	// AnyOf: at least one of these nested conditions must match.
	AnyOf []DecomposeLintMatch `yaml:"any_of,omitempty" json:"any_of,omitempty"`
	// AllOf: every one of these nested conditions must match.
	AllOf []DecomposeLintMatch `yaml:"all_of,omitempty" json:"all_of,omitempty"`
}

// TaskTemplatesConfig is the autonomous.task_templates sub-config: the
// reusable task definitions offered at decomposition time.
//
// No omitempty on Enabled/Templates, deliberately (same round-trip
// gotcha as DecomposeLintConfig).
type TaskTemplatesConfig struct {
	// Enabled gates the templates. Default true when the key is absent
	// (set in DefaultConfig; an explicit false is a valid value and
	// survives a YAML/JSON round-trip).
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Templates is the list of available template definitions.
	Templates []TaskTemplateDef `yaml:"templates" json:"templates"`
	// Projects holds per-project overrides, keyed by project directory
	// or alias. Each override may carry an enabled flag; an absent
	// flag (nil) means "follow the global enabled".
	Projects map[string]TaskTemplateProjectOverride `yaml:"projects,omitempty" json:"projects,omitempty"`
	// Mode maps a template id to how its task is handled: llm, daemon
	// or off. An absent id falls back to DefaultTaskTemplateModes, then
	// to llm (see EffectiveTaskTemplateMode).
	Mode map[string]string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

// TaskTemplateMode says who carries out a template's task.
type TaskTemplateMode string

const (
	// TaskTemplateModeLLM: a model session does the task.
	TaskTemplateModeLLM TaskTemplateMode = "llm"
	// TaskTemplateModeDaemon: the daemon does the task itself, no model.
	TaskTemplateModeDaemon TaskTemplateMode = "daemon"
	// TaskTemplateModeOff: the task is not generated.
	TaskTemplateModeOff TaskTemplateMode = "off"
)

// taskTemplateModeValues lists the allowed mode values for messages.
const taskTemplateModeValues = "llm, daemon, off"

// DaemonCapableTaskTemplateIDs returns the template ids that have a
// daemon action.
func DaemonCapableTaskTemplateIDs() []string {
	return []string{"changelog_entry", "parity_issue_draft", "bounded_load_step", "version_bump_left_to_merge"}
}

// DefaultTaskTemplateModes returns the shipped mode per built-in id.
func DefaultTaskTemplateModes() map[string]TaskTemplateMode {
	m := map[string]TaskTemplateMode{
		"docs_set":   TaskTemplateModeLLM,
		"final_gate": TaskTemplateModeLLM,
	}
	for _, id := range DaemonCapableTaskTemplateIDs() {
		m[id] = TaskTemplateModeDaemon
	}
	return m
}

func isDaemonCapableTaskTemplate(id string) bool {
	for _, d := range DaemonCapableTaskTemplateIDs() {
		if d == id {
			return true
		}
	}
	return false
}

// TaskTemplateProjectOverride is one entry of TaskTemplatesConfig
// Projects. Enabled nil = inherit the global enabled.
type TaskTemplateProjectOverride struct {
	Enabled *bool `yaml:"enabled" json:"enabled"`
}

// TaskTemplateDef is one reusable task definition. Spec carries the
// task spec text with the slots {{feature}}, {{files}} and
// {{files_list}} (see RenderTaskTemplateSpec).
type TaskTemplateDef struct {
	// ID is required and unique across the config — templates are
	// referenced by id (e.g. from a rule or an operator selection).
	ID string `yaml:"id" json:"id"`
	// Title is the task title (may carry slots).
	Title string `yaml:"title,omitempty" json:"title,omitempty"`
	// Spec is the task spec text with slots.
	Spec string `yaml:"spec,omitempty" json:"spec,omitempty"`
	// Files is the task's files list (plain, may be a comma-separated
	// list).
	Files string `yaml:"files,omitempty" json:"files,omitempty"`
	// Kind is the task kind tag (free text, e.g. "docs", "test").
	Kind     string `yaml:"kind,omitempty" json:"kind,omitempty"`
	ReadOnly bool   `yaml:"read_only" json:"read_only"`
	// VerificationScript, when set, is copied verbatim onto the task the
	// template produces. A read-only gate carries its own check this way
	// (the build and test commands its spec asserts) so it is not an
	// unverifiable read-only task while it changes nothing.
	VerificationScript string `yaml:"verification_script,omitempty" json:"verification_script,omitempty"`
	// VerificationScriptReason is the explicit reason a task from the
	// template has no verification script (a note that changes no files
	// carries one). It is copied onto the task so the verifier and the
	// structural plan check can tell a deliberate exemption from a
	// forgotten script.
	VerificationScriptReason string `yaml:"verification_script_reason,omitempty" json:"verification_script_reason,omitempty"`
	// ReportPath, for a read-only template, is the one repo-relative
	// file under .datawatch/reports/ the task may write its verdict
	// and evidence to. It takes the same slots as the other string
	// fields ({{feature}} is slugged into a file-name-safe form) and is
	// copied onto the instantiated task after validation.
	ReportPath string `yaml:"report_path,omitempty" json:"report_path,omitempty"`
	// DependsOn lists ids of other templates this one depends on.
	DependsOn []string `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	// AppliesWhen is a free-text condition for when this template is
	// offered.
	AppliesWhen string `yaml:"applies_when,omitempty" json:"applies_when,omitempty"`
}

// Task-template spec slots. Spec/Title text may embed these; they are
// substituted by RenderTaskTemplateSpec.
const (
	TaskTemplateSlotFeature   = "{{feature}}"
	TaskTemplateSlotFiles     = "{{files}}"
	TaskTemplateSlotFilesList = "{{files_list}}"
)

// TaskTemplateSlots carries the values that fill a template's spec
// slots (Feature, Files, FilesList).
type TaskTemplateSlots struct {
	Feature   string
	Files     string
	FilesList string
}

// RenderTaskTemplateSpec substitutes the known slots ({{feature}},
// {{files}}, {{files_list}}) in spec with the supplied values; unknown
// {{...}} tokens are left untouched.
func RenderTaskTemplateSpec(spec string, s TaskTemplateSlots) string {
	r := strings.NewReplacer(
		TaskTemplateSlotFeature, s.Feature,
		TaskTemplateSlotFiles, s.Files,
		TaskTemplateSlotFilesList, s.FilesList,
	)
	return r.Replace(spec)
}

// DefaultDecomposeLint returns the shipped decompose-lint settings:
// enforce mode, the default retry budget, no rules.
func DefaultDecomposeLint() DecomposeLintConfig {
	return DecomposeLintConfig{
		Mode:       DecomposeLintModeEnforce,
		MaxRetries: DefaultDecomposeLintMaxRetries,
	}
}

// DefaultTaskTemplates returns the shipped task-template settings:
// enabled with no templates and no per-project overrides.
func DefaultTaskTemplates() TaskTemplatesConfig {
	return TaskTemplatesConfig{Enabled: true}
}

// NormalizeDecomposeLintMode resolves a raw decompose-lint mode to its
// canonical form. Empty (unset) normalizes to "enforce" — the shipped
// default. Comparison is case-insensitive. Any other value is an error;
// callers must not silently fall back on a typo ("enforc" must never
// quietly mean "enforce").
func NormalizeDecomposeLintMode(s string) (string, error) {
	m := strings.ToLower(strings.TrimSpace(s))
	if m == "" {
		return DecomposeLintModeEnforce, nil
	}
	switch m {
	case DecomposeLintModeOff, DecomposeLintModeShadow, DecomposeLintModeEnforce:
		return m, nil
	default:
		return "", fmt.Errorf("invalid decompose_lint.mode %q: must be one of %q, %q, %q (or empty for the %q default)",
			s, DecomposeLintModeOff, DecomposeLintModeShadow, DecomposeLintModeEnforce, DecomposeLintModeEnforce)
	}
}

// NormalizeDecomposeLintSeverity resolves a raw rule severity to its
// canonical form. Empty (unset) normalizes to "warn" — the lenient
// default. Comparison is case-insensitive. Any other value is an error.
func NormalizeDecomposeLintSeverity(s string) (string, error) {
	m := strings.ToLower(strings.TrimSpace(s))
	if m == "" {
		return DecomposeLintSeverityWarning, nil
	}
	switch m {
	case DecomposeLintSeverityWarning, DecomposeLintSeverityBlock:
		return m, nil
	default:
		return "", fmt.Errorf("invalid decompose_lint rule severity %q: must be %q or %q (or empty for the %q default)",
			s, DecomposeLintSeverityWarning, DecomposeLintSeverityBlock, DecomposeLintSeverityWarning)
	}
}

// NormalizeDecomposeLintMaxRetries clamps a raw retry budget into the
// valid range [0, DecomposeLintMaxRetriesCap]: negative values clamp
// to 0 (never auto-retry — 0 is a valid explicit value), values above
// the cap clamp to the cap. Values in [0, cap] pass through unchanged.
func NormalizeDecomposeLintMaxRetries(n int) int {
	switch {
	case n < 0:
		return 0
	case n > DecomposeLintMaxRetriesCap:
		return DecomposeLintMaxRetriesCap
	}
	return n
}

// ValidateDecomposeLintRuleID rejects an empty or malformed
// rule/template id: ids must match [a-z0-9_.-]+ (lowercase).
func ValidateDecomposeLintRuleID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("decompose_lint rule id: must not be empty")
	}
	if !decomposeLintRuleIDPattern.MatchString(id) {
		return fmt.Errorf("decompose_lint rule id %q: must match [a-z0-9_.-]+ (lowercase letters, digits, underscore, dot, dash)", id)
	}
	return nil
}

// validateMatch compiles every regular expression in match (including
// nested any_of/all_of, up to decomposeLintMatchMaxDepth levels) and
// reports the first unparseable pattern.
func validateMatch(m DecomposeLintMatch, path string, depth int) error {
	if depth > decomposeLintMatchMaxDepth {
		return fmt.Errorf("%s: any_of/all_of nesting deeper than %d levels is not allowed", path, decomposeLintMatchMaxDepth)
	}
	checks := []struct {
		key     string
		pattern string
	}{
		{"spec_regex", m.SpecRegex},
		{"title_regex", m.TitleRegex},
		{"files_regex", m.FilesRegex},
	}
	for _, c := range checks {
		if strings.TrimSpace(c.pattern) == "" {
			continue
		}
		if _, err := regexp.Compile(c.pattern); err != nil {
			return fmt.Errorf("%s.%s: invalid regular expression %q: %v", path, c.key, c.pattern, err)
		}
	}
	for i, sub := range m.AnyOf {
		if err := validateMatch(sub, fmt.Sprintf("%s.any_of[%d]", path, i), depth+1); err != nil {
			return err
		}
	}
	for i, sub := range m.AllOf {
		if err := validateMatch(sub, fmt.Sprintf("%s.all_of[%d]", path, i), depth+1); err != nil {
			return err
		}
	}
	return nil
}

// validateRule checks one rule (id, severity, message, match) and
// registers its id in seen, rejecting a duplicate. A disabled rule is
// checked for id only — it carries finding text it never emits — so a
// bare {id, disabled: true} entry is legal.
func validateRule(r DecomposeLintRule, i int, seen map[string]bool) error {
	prefix := fmt.Sprintf("decompose_lint.rules[%d]", i)
	id := strings.TrimSpace(r.ID)
	if err := ValidateDecomposeLintRuleID(id); err != nil {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	if seen[id] {
		return fmt.Errorf("%s: duplicate rule id %q (ids must be unique)", prefix, id)
	}
	seen[id] = true
	if r.Disabled {
		return nil
	}
	if _, err := NormalizeDecomposeLintSeverity(r.Severity); err != nil {
		return fmt.Errorf("%s: %v", prefix, err)
	}
	if strings.TrimSpace(r.Message) == "" {
		return fmt.Errorf("%s: message must not be empty (rule %q)", prefix, id)
	}
	if err := validateMatch(r.Match, prefix+".match", 0); err != nil {
		return err
	}
	return nil
}

// Validate checks the decompose-lint sub-config: legal mode, legal
// rule ids (non-empty, [a-z0-9_.-]+, unique), legal severities,
// non-empty messages, and every configured regular expression
// compiles. Any violation is a config error (Load refuses the file).
func (d DecomposeLintConfig) Validate() error {
	if _, err := NormalizeDecomposeLintMode(d.Mode); err != nil {
		return fmt.Errorf("autonomous.%v", err)
	}
	seen := make(map[string]bool, len(d.Rules))
	for i, r := range d.Rules {
		if err := validateRule(r, i, seen); err != nil {
			return fmt.Errorf("autonomous.%v", err)
		}
	}
	return nil
}

// Validate checks the task-template sub-config: template ids present,
// shaped and unique. (Enabled and the per-project overrides are
// free-form by design: an arbitrary directory or alias is a legal
// key, and an absent override.enabled is legal.)
func (t TaskTemplatesConfig) Validate() error {
	seen := make(map[string]bool, len(t.Templates))
	for i, def := range t.Templates {
		prefix := fmt.Sprintf("task_templates.templates[%d]", i)
		id := strings.TrimSpace(def.ID)
		if err := ValidateDecomposeLintRuleID(id); err != nil {
			return fmt.Errorf("autonomous.%s: %w", prefix, err)
		}
		if seen[id] {
			return fmt.Errorf("autonomous.%s: duplicate template id %q (ids must be unique)", prefix, id)
		}
		seen[id] = true
	}
	ids := make([]string, 0, len(t.Mode))
	for id := range t.Mode {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		key := "autonomous.task_templates.mode[" + id + "]"
		if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) {
			return fmt.Errorf("%s: template id must be non-empty with no surrounding space", key)
		}
		if err := ValidateDecomposeLintRuleID(id); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		switch TaskTemplateMode(t.Mode[id]) {
		case TaskTemplateModeLLM, TaskTemplateModeOff:
		case TaskTemplateModeDaemon:
			if id == "final_gate" {
				return fmt.Errorf("%s: daemon is not allowed for final_gate (allowed values: llm, off)", key)
			}
			if !isDaemonCapableTaskTemplate(id) {
				return fmt.Errorf("%s: daemon needs a daemon-capable id (%s); allowed values for this id: llm, off", key, strings.Join(DaemonCapableTaskTemplateIDs(), ", "))
			}
		default:
			return fmt.Errorf("%s: unknown mode %q (allowed values: %s)", key, t.Mode[id], taskTemplateModeValues)
		}
	}
	return nil
}

// EffectiveTaskTemplateMode resolves the mode for a template id: the
// configured value, else the shipped default for the id, else llm.
func (a *AutonomousConfig) EffectiveTaskTemplateMode(id string) TaskTemplateMode {
	if v, ok := a.TaskTemplates.Mode[id]; ok && v != "" {
		return TaskTemplateMode(v)
	}
	if d, ok := DefaultTaskTemplateModes()[id]; ok {
		return d
	}
	return TaskTemplateModeLLM
}

// EffectiveDecomposeLintMode resolves the decompose-lint mode in
// effect: unset (empty) → "enforce"; a typo → an error, not a guess.
func (a *AutonomousConfig) EffectiveDecomposeLintMode() (string, error) {
	return NormalizeDecomposeLintMode(a.DecomposeLint.Mode)
}

// EffectiveDecomposeLintMaxRetries resolves the retry budget in
// effect: negatives → 0, above the cap → the cap, [0, cap] unchanged.
func (a *AutonomousConfig) EffectiveDecomposeLintMaxRetries() int {
	return NormalizeDecomposeLintMaxRetries(a.DecomposeLint.MaxRetries)
}

// EffectiveTaskTemplatesEnabled reports whether task templates are on
// for this config (global Enabled). Per-project resolution — an
// override's enabled when set, else the global flag — is a consumer
// concern; the raw pair is exposed here.
func (a *AutonomousConfig) EffectiveTaskTemplatesEnabled() bool {
	return a.TaskTemplates.Enabled
}

// EffectiveTaskTemplatesEnabledForProject resolves the enabled flag
// for one project: the per-project override's enabled when set, else
// the global Enabled.
func (a *AutonomousConfig) EffectiveTaskTemplatesEnabledForProject(key string) bool {
	ov, ok := a.TaskTemplates.Projects[key]
	if ok && ov.Enabled != nil {
		return *ov.Enabled
	}
	return a.TaskTemplates.Enabled
}

// GenerateDecomposeGuidance renders operator guidance from the
// configured rules, one line per rule (id, severity, message). An
// empty rule list renders the empty string.
func GenerateDecomposeGuidance(rules []DecomposeLintRule) string {
	if len(rules) == 0 {
		return ""
	}
	var b strings.Builder
	for i, r := range rules {
		if i > 0 {
			b.WriteString("\n")
		}
		severity, err := NormalizeDecomposeLintSeverity(r.Severity)
		if err != nil {
			severity = r.Severity
		}
		fmt.Fprintf(&b, "%s (%s): %s", strings.TrimSpace(r.ID), severity, strings.TrimSpace(r.Message))
	}
	return b.String()
}

// EffectiveDecomposeGuidance resolves autonomous.decompose_guidance:
// a non-empty value (literal text or a file path the consumer loads)
// is used as-is; empty (unset) means the guidance is generated from
// the configured decompose-lint rules
// (GenerateDecomposeGuidance).
func (a *AutonomousConfig) EffectiveDecomposeGuidance() string {
	if g := strings.TrimSpace(a.DecomposeGuidance); g != "" {
		return a.DecomposeGuidance
	}
	return GenerateDecomposeGuidance(a.DecomposeLint.Rules)
}
