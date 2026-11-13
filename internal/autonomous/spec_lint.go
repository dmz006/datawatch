// spec_lint.go — the pure spec-text lint. LintSpecText runs, against an
// Automaton's spec text itself, the same content checks the decompose
// result goes through, so a planner can reword a phrase before it is
// copied into a task title or text and the decompose fails.
//
// It is deterministic and cheap: no LLM, no I/O, no session access. It
// never touches, kills or signals a session.
package autonomous

import (
	"regexp"
	"sort"
	"strings"
)

// Severity values of a SpecFinding.
const (
	// SpecSeverityWarning: the planner would copy the phrase into a task
	// title or text and the daemon can fix it itself.
	SpecSeverityWarning = "warning"
	// SpecSeverityError: the phrase must be reworded by the operator or
	// planner.
	SpecSeverityError = "error"
)

// Rule names. Where the decompose failure already has a source rule
// name, the same name is used here.
const (
	SpecRuleReleaseVersionFixed = "release-version-fixed"
	SpecRuleTrackerID           = "names_tracker_id"
	SpecRuleSystemPath          = "system-binary-path"
	SpecRulePrivateAddress      = "private-address"
	SpecRuleWorktreePath        = "worktree-path"
)

// SpecFinding is one problem found in a spec text.
type SpecFinding struct {
	Rule        string `json:"rule"`
	Phrase      string `json:"phrase"`
	Severity    string `json:"severity"`
	Correctable bool   `json:"correctable"`
	Suggestion  string `json:"suggestion"`
}

var (
	specTrackerRE = regexp.MustCompile(trackerIDPattern)
	// specSystemPathRE matches an absolute path to a system binary.
	specSystemPathRE = regexp.MustCompile(`(?:/usr/local/s?bin|/usr/s?bin|/s?bin|/opt/[A-Za-z0-9._-]+/bin)/[A-Za-z0-9._+-]+`)
	// specPrivateAddrRE matches a dotted-quad candidate; the range is
	// checked in code.
	specPrivateAddrRE = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	// specWorktreePathRE matches a path into a task worktree directory.
	specWorktreePathRE = regexp.MustCompile(`(?:~|\$HOME|/home/[A-Za-z0-9._-]+|/Users/[A-Za-z0-9._-]+)?/?(?:\.datawatch/)?prd-worktrees/[^\s"'` + "`" + `)\]>,;]*`)
)

type specHit struct {
	start int
	f     SpecFinding
}

// LintSpecText checks spec text and returns its findings in the order
// they appear in the text (ties keep rule order). A clean spec returns
// nil.
func LintSpecText(text string) []SpecFinding {
	var hits []specHit
	add := func(start int, f SpecFinding) { hits = append(hits, specHit{start, f}) }

	// Fixed release number: only release-flavoured text is held to it,
	// exactly as validateNoFixedReleaseVersion does.
	if looksLikeReleaseText(text) {
		if v := firstFixedReleaseVersion(text); v != "" {
			phrase := v
			start := strings.Index(text, v)
			// Include a leading "version " / "release " word so the
			// suggestion reads naturally.
			if i := lastWordBefore(text, start); i >= 0 {
				word := strings.ToLower(text[i:start])
				if strings.TrimSpace(word) == "version" || strings.TrimSpace(word) == "release" {
					phrase = text[i : start+len(v)]
					start = i
				}
			}
			sug := "the next patch above main"
			if strings.HasPrefix(strings.ToLower(phrase), "version ") || strings.HasPrefix(strings.ToLower(phrase), "release ") {
				sug = "the release"
			}
			add(start, SpecFinding{SpecRuleReleaseVersionFixed, phrase, SpecSeverityWarning, true,
				"replace '" + phrase + "' with '" + sug + "'; the version is taken at merge time as the next patch above main"})
		}
	}

	for _, loc := range specTrackerRE.FindAllStringIndex(text, -1) {
		p := text[loc[0]:loc[1]]
		add(loc[0], SpecFinding{SpecRuleTrackerID, p, SpecSeverityWarning, true,
			"remove '" + p + "' and describe the work itself instead of naming a tracking item by id"})
	}

	for _, loc := range specWorktreePathRE.FindAllStringIndex(text, -1) {
		p := strings.TrimRight(text[loc[0]:loc[1]], ".")
		add(loc[0], SpecFinding{SpecRuleWorktreePath, p, SpecSeverityError, false,
			"replace '" + p + "' with a repo-relative path; a task never names its worktree directory"})
	}

	for _, loc := range specSystemPathRE.FindAllStringIndex(text, -1) {
		p := text[loc[0]:loc[1]]
		// Skip a match that is the tail of a longer path (a repo-relative
		// or worktree path such as .../prd-worktrees/x/bin/tool).
		if loc[0] > 0 && isPathChar(text[loc[0]-1]) {
			continue
		}
		name := p[strings.LastIndex(p, "/")+1:]
		add(loc[0], SpecFinding{SpecRuleSystemPath, p, SpecSeverityWarning, true,
			"replace '" + p + "' with the command name '" + name + "' resolved at runtime via command -v"})
	}

	for _, loc := range specPrivateAddrRE.FindAllStringIndex(text, -1) {
		p := text[loc[0]:loc[1]]
		if !isPrivateIPv4(p) {
			continue
		}
		add(loc[0], SpecFinding{SpecRulePrivateAddress, p, SpecSeverityError, false,
			"replace '" + p + "' with a configured host name or a placeholder such as 'the target host'"})
	}

	sort.SliceStable(hits, func(i, j int) bool { return hits[i].start < hits[j].start })
	var out []SpecFinding
	for _, h := range hits {
		out = append(out, h.f)
	}
	return out
}

func isPathChar(c byte) bool {
	return c == '/' || c == '.' || c == '-' || c == '_' || c == '~' ||
		(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// lastWordBefore returns the start index of the word (with its trailing
// space) that ends right before pos, or -1.
func lastWordBefore(text string, pos int) int {
	if pos < 2 || text[pos-1] != ' ' {
		return -1
	}
	i := pos - 1
	for i > 0 && text[i-1] != ' ' && text[i-1] != '\n' && text[i-1] != '\t' {
		i--
	}
	return i
}

// isPrivateIPv4 reports whether s is a valid dotted quad in a private,
// loopback or link-local range.
func isPrivateIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	var o [4]int
	for i, p := range parts {
		n := 0
		for _, c := range p {
			n = n*10 + int(c-'0')
		}
		if n > 255 {
			return false
		}
		o[i] = n
	}
	switch {
	case o[0] == 10, o[0] == 127:
		return true
	case o[0] == 172 && o[1] >= 16 && o[1] <= 31:
		return true
	case o[0] == 192 && o[1] == 168:
		return true
	case o[0] == 169 && o[1] == 254:
		return true
	}
	return false
}
