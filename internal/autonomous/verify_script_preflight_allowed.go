package autonomous

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// RuleVerifyScriptAllowedFiles marks a path the plan changes that a
// verification script's literal allowed-file list does not cover.
const RuleVerifyScriptAllowedFiles = "verify_script_allowed_files"

var preflightPathTokenRe = regexp.MustCompile(`[A-Za-z0-9_./{}\-]+`)

var preflightPathExts = []string{".go", ".md", ".sh", ".json", ".yaml", ".js"}

// preflightLiteralPathTokens returns the literal path tokens a script names.
func preflightLiteralPathTokens(text []byte) []string {
	var out []string
	seen := map[string]bool{}
	for _, field := range strings.FieldsFunc(string(text), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '"' || r == '\'' || r == '(' || r == ')' || r == ';' || r == '|' || r == '&' || r == '`' || r == '=' || r == ','
	}) {
		if strings.Contains(field, "$") || strings.Contains(field, "://") {
			continue
		}
		tok := preflightPathTokenRe.FindString(field)
		if tok == "" || tok != field {
			continue
		}
		tok = strings.TrimPrefix(tok, "./")
		isPath := strings.Contains(tok, "/")
		for _, e := range preflightPathExts {
			if strings.HasSuffix(tok, e) {
				isPath = true
			}
		}
		if !isPath || tok == "/" || strings.HasPrefix(tok, "/") || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// preflightStandardGeneratedFiles returns the files the built-in standard
// task templates name. A templated path is cut to its static directory.
func preflightStandardGeneratedFiles() []string {
	var out []string
	for _, def := range BuiltInTaskTemplates() {
		f := strings.TrimSpace(def.Files)
		if f == "" || strings.HasPrefix(f, "{{") {
			continue
		}
		if i := strings.Index(f, "{{"); i >= 0 {
			f = f[:i]
			if j := strings.LastIndex(f, "/"); j >= 0 {
				f = f[:j+1]
			} else {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

func preflightPathCovered(path string, tokens []string) bool {
	for _, tok := range tokens {
		if tok == path {
			return true
		}
		dir := strings.TrimSuffix(tok, "/") + "/"
		if strings.HasPrefix(path, dir) {
			return true
		}
		// A directory entry (standard generated folder) is covered by any
		// literal file under it.
		if strings.HasSuffix(path, "/") && strings.HasPrefix(tok, path) {
			return true
		}
	}
	return false
}

// preflightAllowedFilesFindings reports each path the whole plan changes
// that the script's literal allowed-file list does not cover. A script
// naming no literal path builds its list at run time and is not checked.
func preflightAllowedFilesFindings(scriptRel string, text []byte, stories []Story) []VerifyScriptPreflightFinding {
	tokens := preflightLiteralPathTokens(text)
	if len(tokens) == 0 {
		return nil
	}
	owner := map[string]string{}
	add := func(p, who string) {
		p = strings.TrimPrefix(strings.TrimSpace(p), "./")
		if p == "" {
			return
		}
		if _, ok := owner[p]; !ok {
			owner[p] = who
		}
	}
	for _, s := range stories {
		who := "story " + s.Title
		for _, p := range s.FilesPlanned {
			add(p, who)
		}
		for _, p := range s.FilesGenerated {
			add(p, who)
		}
		add(s.VerificationScript, who)
		for _, t := range s.Tasks {
			tw := "task " + t.Title
			for _, p := range t.FilesPlanned {
				add(p, tw)
			}
			for _, p := range t.FilesGenerated {
				add(p, tw)
			}
			add(t.VerificationScript, tw)
		}
	}
	for _, p := range preflightStandardGeneratedFiles() {
		add(p, "the standard generated tasks")
	}
	paths := make([]string, 0, len(owner))
	for p := range owner {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out []VerifyScriptPreflightFinding
	for _, p := range paths {
		if preflightPathCovered(p, tokens) {
			continue
		}
		out = append(out, VerifyScriptPreflightFinding{
			Script: scriptRel,
			Line:   0,
			RuleID: RuleVerifyScriptAllowedFiles,
			Message: fmt.Sprintf("allowed-file list omits %s, which %s changes; the list must be cumulative over the whole plan",
				p, owner[p]),
		})
	}
	return out
}
