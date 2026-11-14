// decompose_lint.go — the decompose-lint rule engine.
//
// LintTasks evaluates the *effective* rule set (the built-in default
// pack, internal/autonomous/decompose_lint_rules.go, merged with the
// operator's autonomous.decompose_lint rules — see EffectiveRules)
// against a freshly decomposed task graph (a set of Stories and their
// Tasks) and returns one LintFinding per rule/task hit. The engine is
// pure: in-memory values in, findings and rule errors out. No daemon,
// no operator config file, no filesystem access — callers decide what
// to do with a finding (record it in shadow mode, feed it back to the
// decomposer, or block in enforce mode) and which rules apply.
package autonomous

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// Rule is one decompose-lint rule (the config-package types are the
// single source of truth for the rule shape: id, severity, match
// conditions and finding message).
type Rule = config.DecomposeLintRule

// LintFinding is one decompose-lint hit: the rule that fired, the
// story and task it fired on, and the text excerpt the rule matched
// (the matched snippet of the spec, title or files list — or a short
// description of an asserted field state for predicate-only rules).
type LintFinding struct {
	// RuleID names the rule that fired (findings and operator docs
	// reference rules by this id).
	RuleID string `json:"rule_id"`
	// Severity is the rule's severity, normalized: "block" (stops
	// decomposition in enforce mode) or "warn" (reported only).
	Severity string `json:"severity"`
	// Message is the rule's finding text.
	Message string `json:"message"`
	// Story is the ID of the story the offending task belongs to.
	Story string `json:"story"`
	// Task is the ID of the offending task.
	Task string `json:"task"`
	// Matched is the matched excerpt (or the asserted field state).
	Matched string `json:"matched,omitempty"`
	// Script is the verification script path the finding is about, set
	// by the script-content rules (empty for rules that are not).
	Script string `json:"script,omitempty"`
}

// LintSummary tallies lint findings by severity — the compact view
// surfaced in status text and reports. Zero is the empty summary.
type LintSummary struct {
	// Block counts findings from rules with severity "block".
	Block int `json:"block"`
	// Warn counts findings from rules with severity "warn".
	Warn int `json:"warn"`
}

// Total is the number of findings of any severity.
func (s LintSummary) Total() int { return s.Block + s.Warn }

// SummaryOf tallies findings by their (normalized) severity. Findings
// with an unrecognized severity are counted nowhere (rules in the
// built-in pack and operator config only ever carry block/warn).
func SummaryOf(findings []LintFinding) LintSummary {
	var s LintSummary
	for _, f := range findings {
		switch normSeverity(f.Severity) {
		case config.DecomposeLintSeverityBlock:
			s.Block++
		case config.DecomposeLintSeverityWarning:
			s.Warn++
		}
	}
	return s
}

// normSeverity lowercases and trims a severity for tolerant matching
// (an operator rule may carry "BLOCK"; the fold is lossless for the
// two legal values).
func normSeverity(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// compiledMatch is one match condition tree with its regular
// expressions precompiled. A rule fires on a task when every present
// condition holds: the spec/title/files regexes must match their
// field, the read_only predicate, the verification-script-empty and
// report-path-empty predicates must assert, any_of must have at least
// one member match and all_of every member. Empty conditions are
// absent and never restrict.
type compiledMatch struct {
	spec          *regexp.Regexp
	title         *regexp.Regexp
	files         *regexp.Regexp
	readOnly      *bool
	verifEmpty    bool
	reportEmpt    bool
	unexecScript  bool
	reasonEmpty   bool
	taskFilesOnly bool
	docsNoSync    bool
	roWritesOut   bool
	absPathOut    bool
	scriptPreds   scriptPredID
	kind          string
	anyOf         []compiledMatch
	allOf         []compiledMatch
}

// usesScriptContent reports whether this condition tree reads script
// content (so a finding carries the script path).
func (m *compiledMatch) usesScriptContent() bool {
	if m.taskFilesOnly || m.docsNoSync || m.scriptPreds != 0 {
		return true
	}
	for i := range m.anyOf {
		if m.anyOf[i].usesScriptContent() {
			return true
		}
	}
	for i := range m.allOf {
		if m.allOf[i].usesScriptContent() {
			return true
		}
	}
	return false
}

func (m *compiledMatch) matchTask(t Task, dir string, plan *lintPlan) bool {
	if m.spec != nil && !m.spec.MatchString(t.Spec) {
		return false
	}
	if m.title != nil && !m.title.MatchString(t.Title) {
		return false
	}
	if m.readOnly != nil && t.ReadOnly != *m.readOnly {
		return false
	}
	if m.verifEmpty && strings.TrimSpace(t.VerificationScript) != "" {
		return false
	}
	if m.reportEmpt && taskHasUsableReportPath(t) {
		return false
	}
	if m.unexecScript && unexecutableVerificationScriptReason(dir, t) == "" {
		return false
	}
	if m.reasonEmpty && taskHasVerificationReason(t) {
		return false
	}
	if m.kind != "" && !strings.EqualFold(strings.TrimSpace(t.Kind), m.kind) {
		return false
	}
	if m.roWritesOut && readOnlyWriteLine(t) == "" {
		return false
	}
	if m.absPathOut {
		if line, _ := specAbsolutePathOutside(t.Spec, dir); line == "" {
			return false
		}
	}
	if m.taskFilesOnly {
		if _, ok := verifyScriptTaskFilesOnly(dir, t); !ok {
			return false
		}
	}
	if m.docsNoSync {
		if _, ok := verifyScriptDocsWithoutSync(dir, t); !ok {
			return false
		}
	}
	for _, sp := range scriptPredicates {
		if m.scriptPreds&sp.id != 0 {
			if _, ok := sp.check(dir, t, plan); !ok {
				return false
			}
		}
	}
	if m.files != nil {
		matched := false
		for _, f := range t.FilesPlanned {
			if m.files.MatchString(f) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(m.anyOf) > 0 {
		anyMatched := false
		for _, sub := range m.anyOf {
			if sub.matchTask(t, dir, plan) {
				anyMatched = true
				break
			}
		}
		if !anyMatched {
			return false
		}
	}
	for _, sub := range m.allOf {
		if !sub.matchTask(t, dir, plan) {
			return false
		}
	}
	return true
}

// compiledRule is one rule with its match precompiled, or the compile
// error LintTasksWithErrors reports instead of evaluating it.
type compiledRule struct {
	rule       Rule
	match      *compiledMatch
	compiled   bool
	compileErr error
}

// compileRules compiles every rule's regular expressions (including
// nested any_of/all_of). A rule with an invalid pattern is not fatal:
// it comes back with compiled=false and its error, so the caller can
// report it and still apply every healthy rule.
func compileRules(rules []Rule) []compiledRule {
	out := make([]compiledRule, 0, len(rules))
	for i := range rules {
		r := &rules[i]
		cr := compiledRule{rule: *r, compiled: true}
		match, err := compileMatch(r.Match, fmt.Sprintf("rule %q", strings.TrimSpace(r.ID)))
		if err != nil {
			cr.compiled = false
			if strings.TrimSpace(r.ID) != "" {
				cr.compileErr = fmt.Errorf("decompose_lint rule %q: %v", strings.TrimSpace(r.ID), err)
			} else {
				cr.compileErr = fmt.Errorf("decompose_lint rules[%d]: %v", i, err)
			}
			out = append(out, cr)
			continue
		}
		cr.match = match
		out = append(out, cr)
	}
	return out
}

// compileMatch compiles one condition level (regexes, tri-state
// predicates, nested any_of/all_of). An empty pattern means the
// condition is absent, not the empty regular expression.
func compileMatch(m config.DecomposeLintMatch, path string) (*compiledMatch, error) {
	cm := &compiledMatch{readOnly: m.ReadOnly, verifEmpty: m.VerificationScriptEmpty, reportEmpt: m.ReportPathEmpty, unexecScript: m.VerificationScriptUnexecutable,
		reasonEmpty: m.VerificationScriptReasonEmpty, taskFilesOnly: m.VerificationScriptTaskFilesOnly,
		docsNoSync: m.VerificationScriptDocsWithoutSync, scriptPreds: scriptPredsOf(m), roWritesOut: m.ReadOnlyWritesOutsideReport, absPathOut: m.SpecAbsolutePathOutsideProject, kind: strings.ToLower(strings.TrimSpace(m.Kind))}
	var err error
	if strings.TrimSpace(m.SpecRegex) != "" {
		if cm.spec, err = regexp.Compile(m.SpecRegex); err != nil {
			return nil, fmt.Errorf("%s.spec_regex: %v", path, err)
		}
	}
	if strings.TrimSpace(m.TitleRegex) != "" {
		if cm.title, err = regexp.Compile(m.TitleRegex); err != nil {
			return nil, fmt.Errorf("%s.title_regex: %v", path, err)
		}
	}
	if strings.TrimSpace(m.FilesRegex) != "" {
		if cm.files, err = regexp.Compile(m.FilesRegex); err != nil {
			return nil, fmt.Errorf("%s.files_regex: %v", path, err)
		}
	}
	for i, sub := range m.AnyOf {
		s, err := compileMatch(sub, fmt.Sprintf("%s.any_of[%d]", path, i))
		if err != nil {
			return nil, err
		}
		cm.anyOf = append(cm.anyOf, *s)
	}
	for i, sub := range m.AllOf {
		s, err := compileMatch(sub, fmt.Sprintf("%s.all_of[%d]", path, i))
		if err != nil {
			return nil, err
		}
		cm.allOf = append(cm.allOf, *s)
	}
	return cm, nil
}

// LintTasksWithErrors evaluates rules against every task of every
// story and returns the findings plus one error per rule whose regular
// expression (at any nesting level) failed to compile. Invalid rules
// are skipped, never fatal, and are reported so an operator can fix
// the pattern without losing the rest of the lint.
func LintTasksWithErrors(stories []Story, rules []Rule) ([]LintFinding, []error) {
	return LintTasksInDir("", stories, rules)
}

// LintTasksInDir is LintTasksWithErrors with the project directory the
// plan will run in; the verification_script_unexecutable predicate
// checks script paths against it. An empty dir checks command strings
// only (a path cannot be checked without a base directory).
func LintTasksInDir(dir string, stories []Story, rules []Rule) ([]LintFinding, []error) {
	compiled := compileRules(rules)
	plan := newLintPlan(stories)
	var errs []error
	var findings []LintFinding
	for i := range compiled {
		cr := compiled[i]
		if !cr.compiled {
			errs = append(errs, cr.compileErr)
			continue
		}
		for _, s := range stories {
			for _, t := range s.Tasks {
				// A daemon-mode template task runs no session, so no
				// per-task rule applies to it (daemon_mode_checks.go).
				if daemonRunsTask(t) {
					continue
				}
				if !cr.match.matchTask(t, dir, plan) {
					continue
				}
				f := LintFinding{
					RuleID:   strings.TrimSpace(cr.rule.ID),
					Severity: normSeverity(cr.rule.Severity),
					Message:  cr.rule.Message,
					Story:    s.ID,
					Task:     t.ID,
					Matched:  excerptFor(cr.match, t, dir, plan),
				}
				if cr.match.usesScriptContent() {
					f.Script = verifyScriptFindingPathIn(cr.match, dir, t, plan)
					if f.Script != "" {
						f.Message += " (verification script: " + f.Script + ")"
					}
				}
				findings = append(findings, f)
			}
		}
	}
	return findings, errs
}

// LintTasks is LintTasksWithErrors with the rule-compile errors
// dropped — the convenience entry point for callers that only want
// the findings.
func LintTasks(stories []Story, rules []Rule) []LintFinding {
	f, _ := LintTasksWithErrors(stories, rules)
	return f
}

// LintTasksIn is LintTasks with the project directory (see
// LintTasksInDir), the entry point for a PRD that has one.
func LintTasksIn(dir string, stories []Story, rules []Rule) []LintFinding {
	f, _ := LintTasksInDir(dir, stories, rules)
	return f
}

// mergeRules overlays operator rules onto the built-in pack: a
// rule with the same id replaces the built-in (in the built-in
// position), a new id is appended in operator config order.
func mergeRules(builtIn, operator []Rule) []Rule {
	out := make([]Rule, len(builtIn))
	overlay := make(map[string]bool, len(builtIn))
	for i, r := range builtIn {
		out[i] = r
		overlay[strings.TrimSpace(r.ID)] = true
	}
	for _, op := range operator {
		id := strings.TrimSpace(op.ID)
		if overlay[id] {
			replace := false
			for i := range out {
				if strings.TrimSpace(out[i].ID) == id {
					out[i] = op
					replace = true
					break
				}
			}
			if replace {
				continue
			}
		}
		overlay[id] = true
		out = append(out, op)
	}
	return out
}

// EffectiveRules resolves the effective decompose-lint rule set for a
// config: the built-in default pack merged with the operator's rules
// (an operator rule with a built-in id replaces the built-in at its
// original position; a new id is appended in config order), then every
// disabled rule is dropped — flagged Disabled itself, or named in the
// config's DisabledRules list. Order: built-in order for survivors,
// then operator-only rules.
func EffectiveRules(cfg config.DecomposeLintConfig) []Rule {
	merged := mergeRules(DefaultDecomposeLintRules(), cfg.Rules)
	drop := make(map[string]bool, len(cfg.DisabledRules))
	for _, id := range cfg.DisabledRules {
		if id = strings.TrimSpace(id); id != "" {
			drop[id] = true
		}
	}
	out := make([]Rule, 0, len(merged))
	for _, r := range merged {
		if r.Disabled || drop[strings.TrimSpace(r.ID)] {
			continue
		}
		out = append(out, r)
	}
	return out
}

// excerptFor picks the matched snippet a finding should carry: the
// spec match, then a files entry match, then the title match; for
// predicate-only rules (a read_only assertion, a non-empty files list,
// an empty verification script) it describes the asserted field state
// instead.
const maxExcerptLen = 160

func firstMatch(re *regexp.Regexp, s string) string {
	if re == nil || s == "" {
		return ""
	}
	loc := re.FindStringIndex(s)
	if loc == nil {
		return ""
	}
	return clipExcerpt(s[loc[0]:loc[1]])
}

// firstMatchLine is firstMatch widened to the whole line the first hit
// sits on, so a finding quotes the offending spec line rather than a
// bare fragment of it. A long line is clipped around the hit.
func firstMatchLine(re *regexp.Regexp, s string) string {
	if re == nil || s == "" {
		return ""
	}
	loc := re.FindStringIndex(s)
	if loc == nil {
		return ""
	}
	start := strings.LastIndex(s[:loc[0]], "\n") + 1
	end := len(s)
	if i := strings.IndexByte(s[loc[0]:], '\n'); i >= 0 {
		end = loc[0] + i
	}
	line := s[start:end]
	if strings.TrimSpace(line) == "" {
		return clipExcerpt(s[loc[0]:loc[1]])
	}
	hit := s[loc[0]:loc[1]]
	if j := strings.IndexByte(hit, '\n'); j >= 0 {
		hit = hit[:j]
	}
	return quoteSpecLine(line, hit)
}

func clipExcerpt(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= maxExcerptLen {
		return s
	}
	return string(r[:maxExcerptLen-1]) + "…"
}

func excerptFor(m *compiledMatch, t Task, dir string, plan *lintPlan) string {
	if e := regexExcerpt(m, t); e != "" {
		return e
	}
	for i := range m.anyOf {
		if e := excerptFor(&m.anyOf[i], t, dir, plan); e != "" {
			return e
		}
	}
	for i := range m.allOf {
		if e := excerptFor(&m.allOf[i], t, dir, plan); e != "" {
			return e
		}
	}
	return predicateExcerpt(m, t, dir, plan)
}

// regexExcerpt is the first regex hit in priority order: spec, files
// entries, title.
func regexExcerpt(m *compiledMatch, t Task) string {
	if e := firstMatchLine(m.spec, t.Spec); e != "" {
		return e
	}
	if m.files != nil && len(t.FilesPlanned) > 0 {
		for _, f := range t.FilesPlanned {
			if m.files.MatchString(f) {
				return clipExcerpt(f)
			}
		}
	}
	return firstMatchLine(m.title, t.Title)
}

// predicateExcerpt describes the asserted field state of a
// predicate-only condition (read_only, the files list, an empty
// verification script).
func predicateExcerpt(m *compiledMatch, t Task, dir string, plan *lintPlan) string {
	var parts []string
	if m.readOnly != nil {
		parts = append(parts, fmt.Sprintf("read_only=%v", *m.readOnly))
	}
	if m.files != nil {
		if len(t.FilesPlanned) == 0 {
			parts = append(parts, "files=[]")
		} else {
			filed := make([]string, len(t.FilesPlanned))
			for i, f := range t.FilesPlanned {
				filed[i] = clipExcerpt(f)
			}
			parts = append(parts, "files=["+strings.Join(filed, ",")+"]")
		}
	}
	if m.verifEmpty {
		parts = append(parts, "verification_script=<empty>")
	}
	if m.reportEmpt {
		parts = append(parts, "report_path=<none usable>")
	}
	if m.unexecScript {
		parts = append(parts, "verification_script="+strconv.Quote(strings.TrimSpace(t.VerificationScript))+" ("+unexecutableVerificationScriptReason(dir, t)+")")
	}
	if m.reasonEmpty {
		parts = append(parts, "verification_script_reason=<empty>")
	}
	if m.kind != "" {
		parts = append(parts, "kind="+m.kind)
	}
	if m.roWritesOut {
		if l := readOnlyWriteLine(t); l != "" {
			parts = append(parts, quoteSpecLine(l, ""))
		}
	}
	if m.absPathOut {
		if l, p := specAbsolutePathOutside(t.Spec, dir); l != "" {
			parts = append(parts, quoteSpecLine(l, p))
		}
	}
	if m.taskFilesOnly {
		parts = append(parts, "verification_script="+strconv.Quote(strings.TrimSpace(t.VerificationScript))+" (compares only against the task's own files, never the merge base)")
	}
	if m.docsNoSync {
		parts = append(parts, "verification_script="+strconv.Quote(strings.TrimSpace(t.VerificationScript))+" (checks docs without make sync-docs)")
	}
	for _, sp := range scriptPredicates {
		if m.scriptPreds&sp.id == 0 {
			continue
		}
		if hit, ok := sp.check(dir, t, plan); ok {
			parts = append(parts, hit.Path+": "+hit.Line)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return clipExcerpt(strings.Join(parts, "; "))
}
