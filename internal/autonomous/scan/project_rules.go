// BL406 Phase 1 — the fourth, operator-defined scan category. Unlike
// sast.go/secrets.go/deps.go (one hardcoded Go check each),
// ProjectRulesScanner is data-driven: it evaluates whatever rules the
// operator configured (scan.Config.ProjectRules), each one a small
// typed check rather than a bespoke function.
//
// Pattern formats, by RuleType:
//   - RuleTypeContent:     "<glob>|<regex>" — every file in dir matching
//     <glob> is scanned for <regex>; each match is a finding.
//   - RuleTypeConsistency: "<fileA>|<fileB>|<regex>" — <regex> (exactly
//     one capture group) is applied to both files; a finding fires if
//     either file doesn't match, or if the two captured values differ.
//   - RuleTypePresence:    "<glob>" — a finding fires if no file in dir
//     matches <glob>.
//   - RuleTypeParity:      no pattern — built in (BL406 Phase 2). Needs
//     PRDContext (below); produces an informative finding, not a crash
//     or a silent skip, when run without it (e.g. a bare directory scan
//     outside any PRD).
//
// A malformed rule (bad pattern syntax, wrong field count) produces one
// Finding reporting the rule itself as broken, at SeverityWarning,
// rather than crashing the whole scan or silently doing nothing.
package scan

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ProjectRulesScanner evaluates the operator-defined project-rules
// category against a project directory.
type ProjectRulesScanner struct {
	Rules []ProjectRule
	// PRDContext (BL406 Phase 2) supplies the PRD/story data the
	// built-in parity rule type needs — this package can't import
	// internal/autonomous's PRD/Story types directly (would create an
	// import cycle, since autonomous already imports scan), so the
	// caller translates. nil when scanning outside a PRD context; the
	// parity rule type then reports that explicitly rather than
	// silently producing nothing.
	PRDContext *PRDParityContext
}

// PRDParityContext is the PRD/story data RuleTypeParity needs.
type PRDParityContext struct {
	// ParentSurfaceText is the parent plan's own "## Parity surface"
	// section, raw text — the set of canonical surface names
	// (RST/MCP/CLI/comm/YAML/PWA/Android/iPhone) mentioned within it is
	// what every story must inherit.
	ParentSurfaceText string
	Stories           []StoryParityInfo
}

// StoryParityInfo is the subset of autonomous.Story the parity rule
// reads.
type StoryParityInfo struct {
	ID          string
	Title       string
	Description string
}

// NewProjectRulesScanner returns a new project-rules scanner bound to
// the given rule set (typically cfg.ProjectRules at scan time).
// prdCtx is nil for scans outside a PRD context; only the parity rule
// type consults it.
func NewProjectRulesScanner(rules []ProjectRule, prdCtx *PRDParityContext) Scanner {
	return ProjectRulesScanner{Rules: rules, PRDContext: prdCtx}
}

func (ProjectRulesScanner) Name() string { return "project-rules" }

func (s ProjectRulesScanner) Scan(dir string) ([]Finding, error) {
	var findings []Finding
	for _, r := range s.Rules {
		sev := r.Severity
		if sev == "" {
			sev = SeverityWarning
		}
		switch r.Type {
		case RuleTypeContent:
			findings = append(findings, scanContentRule(dir, r, sev)...)
		case RuleTypeConsistency:
			findings = append(findings, scanConsistencyRule(dir, r, sev)...)
		case RuleTypePresence:
			findings = append(findings, scanPresenceRule(dir, r, sev)...)
		case RuleTypeParity:
			findings = append(findings, scanParityRule(s.PRDContext, r, sev)...)
		default:
			findings = append(findings, brokenRuleFinding(r, "unknown rule type %q"))
		}
	}
	return findings, nil
}

func brokenRuleFinding(r ProjectRule, format string, args ...any) Finding {
	msg := "project rule " + r.ID + " (" + r.Name + ") is misconfigured: " + sprintfSafe(format, args...)
	return Finding{Scanner: "project-rules", Severity: SeverityWarning, RuleID: r.ID, Message: msg}
}

// sprintfSafe avoids importing fmt into this file's hot path just for
// one error-formatting helper; args is always either empty or a single
// string in practice (rule type / pattern text).
func sprintfSafe(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	out := format
	for _, a := range args {
		s, _ := a.(string)
		out = strings.Replace(out, "%q", "\""+s+"\"", 1)
	}
	return out
}

func scanContentRule(dir string, r ProjectRule, sev Severity) []Finding {
	parts := strings.SplitN(r.Pattern, "|", 2)
	if len(parts) != 2 {
		return []Finding{brokenRuleFinding(r, "content pattern must be \"<glob>|<regex>\", got %q", r.Pattern)}
	}
	glob, reStr := parts[0], parts[1]
	re, err := regexp.Compile(reStr)
	if err != nil {
		return []Finding{brokenRuleFinding(r, "invalid regex %q", reStr)}
	}
	var findings []Finding
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[filepath.Base(path)] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		ok, _ := filepath.Match(glob, rel)
		if !ok {
			ok, _ = filepath.Match(glob, filepath.Base(path))
		}
		if !ok {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 1<<20 {
			return nil
		}
		for lineNo, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				findings = append(findings, Finding{
					Scanner:  "project-rules",
					File:     rel,
					Line:     lineNo + 1,
					Severity: sev,
					RuleID:   r.ID,
					Message:  r.Name,
				})
			}
		}
		return nil
	})
	return findings
}

func scanConsistencyRule(dir string, r ProjectRule, sev Severity) []Finding {
	parts := strings.SplitN(r.Pattern, "|", 3)
	if len(parts) != 3 {
		return []Finding{brokenRuleFinding(r, "consistency pattern must be \"<fileA>|<fileB>|<regex>\", got %q", r.Pattern)}
	}
	fileA, fileB, reStr := parts[0], parts[1], parts[2]
	re, err := regexp.Compile(reStr)
	if err != nil || re.NumSubexp() < 1 {
		return []Finding{brokenRuleFinding(r, "consistency regex must have exactly one capture group, got %q", reStr)}
	}
	valA, okA := extractFirstMatch(dir, fileA, re)
	valB, okB := extractFirstMatch(dir, fileB, re)
	if !okA {
		return []Finding{{Scanner: "project-rules", File: fileA, Severity: sev, RuleID: r.ID,
			Message: r.Name + ": pattern not found in " + fileA}}
	}
	if !okB {
		return []Finding{{Scanner: "project-rules", File: fileB, Severity: sev, RuleID: r.ID,
			Message: r.Name + ": pattern not found in " + fileB}}
	}
	if valA != valB {
		return []Finding{{Scanner: "project-rules", File: fileA + ", " + fileB, Severity: sev, RuleID: r.ID,
			Message: r.Name + ": " + fileA + "=" + valA + " but " + fileB + "=" + valB}}
	}
	return nil
}

func extractFirstMatch(dir, relPath string, re *regexp.Regexp) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, relPath))
	if err != nil {
		return "", false
	}
	m := re.FindStringSubmatch(string(data))
	if m == nil || len(m) < 2 {
		return "", false
	}
	return m[1], true
}

func scanPresenceRule(dir string, r ProjectRule, sev Severity) []Finding {
	glob := r.Pattern
	if glob == "" {
		return []Finding{brokenRuleFinding(r, "presence pattern (a glob) must not be empty")}
	}
	found := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() {
			if skipDirs[filepath.Base(path)] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if ok, _ := filepath.Match(glob, rel); ok {
			found = true
			return nil
		}
		if ok, _ := filepath.Match(glob, filepath.Base(path)); ok {
			found = true
		}
		return nil
	})
	if found {
		return nil
	}
	return []Finding{{Scanner: "project-rules", Severity: sev, RuleID: r.ID,
		Message: r.Name + ": no file matched required pattern " + glob}}
}

// canonicalSurfaces matches AGENT.md's Mobile-Parity Rule's own full
// parity-surface set: REST, MCP, CLI, comm channel, YAML/config, PWA,
// Android, iPhone/iOS. Word-bounded, case-insensitive — "REST" in
// particular would false-positive on "arrest"/"interest" without \b.
var canonicalSurfaces = []struct {
	name string
	re   *regexp.Regexp
}{
	{"REST", regexp.MustCompile(`(?i)\bREST\b`)},
	{"MCP", regexp.MustCompile(`(?i)\bMCP\b`)},
	{"CLI", regexp.MustCompile(`(?i)\bCLI\b`)},
	{"comm channel", regexp.MustCompile(`(?i)\bcomm[\s-]?channel\b`)},
	{"YAML/config", regexp.MustCompile(`(?i)\bYAML\b|\bconfig\.yaml\b`)},
	{"PWA", regexp.MustCompile(`(?i)\bPWA\b`)},
	{"Android", regexp.MustCompile(`(?i)\bAndroid\b`)},
	{"iPhone", regexp.MustCompile(`(?i)\biPhone\b|\biOS\b`)},
}

// surfacesMentionedIn returns which canonical parity surfaces appear
// anywhere in text, in AGENT.md's own canonical order.
func surfacesMentionedIn(text string) []string {
	var found []string
	for _, c := range canonicalSurfaces {
		if c.re.MatchString(text) {
			found = append(found, c.name)
		}
	}
	return found
}

// scanParityRule (BL406 Phase 2) is the first built-in, non-data-driven
// rule type: every surface named in the parent plan's own "## Parity
// surface" section must be inherited by every decomposed story. This
// check is deliberately a *candidate detector*, not a full semantic
// judge of "was there a stated reason" — AGENT.md's own text requires
// "without a stated reason" for a true scope-drift verdict, which this
// regex-level check cannot verify on its own. It reuses the existing
// scan.Config.RulesGraderEnabled + GraderFn pipeline (already wired
// into scan.Run since BL221 Phase 3) for that judgment call, the same
// way every other scanner's findings get graded — not a second,
// parallel LLM-grading mechanism.
func scanParityRule(ctx *PRDParityContext, r ProjectRule, sev Severity) []Finding {
	if ctx == nil {
		return []Finding{{Scanner: "project-rules", Severity: SeverityInfo, RuleID: r.ID,
			Message: r.Name + ": parity rule needs PRD/story context, not supplied for this scan (e.g. a bare-directory scan outside any PRD) — skipped, not evaluated"}}
	}
	parentSurfaces := surfacesMentionedIn(ctx.ParentSurfaceText)
	if len(parentSurfaces) == 0 {
		return []Finding{{Scanner: "project-rules", Severity: SeverityInfo, RuleID: r.ID,
			Message: r.Name + ": parent plan has no parseable Parity surface section to inherit from"}}
	}
	var findings []Finding
	for _, story := range ctx.Stories {
		storySet := map[string]bool{}
		for _, s := range surfacesMentionedIn(story.Description) {
			storySet[s] = true
		}
		for _, want := range parentSurfaces {
			if !storySet[want] {
				findings = append(findings, Finding{
					Scanner: "project-rules", File: story.ID, Severity: sev, RuleID: r.ID,
					Message: r.Name + ": story \"" + story.Title + "\" (" + story.ID +
						") doesn't mention the \"" + want + "\" surface the parent plan's Parity " +
						"surface section names — either a stated exclusion reason belongs in the " +
						"story text, or this is scope drift",
				})
			}
		}
	}
	return findings
}

// FilterRulesByGranularity (BL406 Phase 3) returns only the rules
// declared at g, preserving order. Used at each completion checkpoint
// (task/story/PRD) so a task-level rule doesn't also fire at story or
// PRD completion, and vice versa.
func FilterRulesByGranularity(rules []ProjectRule, g RuleGranularity) []ProjectRule {
	var out []ProjectRule
	for _, r := range rules {
		if r.Granularity == g {
			out = append(out, r)
		}
	}
	return out
}
