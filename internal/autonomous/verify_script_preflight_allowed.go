package autonomous

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// RuleVerifyScriptAllowedFiles marks a path the plan changes that a
// verification script's literal allowed-file list does not cover.
const RuleVerifyScriptAllowedFiles = "verify_script_allowed_files"

var preflightPathTokenRe = regexp.MustCompile(`[A-Za-z0-9_./{}\-]+`)

var preflightPathExts = []string{".go", ".md", ".sh", ".json", ".yaml", ".js"}

// preflightLiteralPathTokens returns the literal path tokens a script
// names. Glob tokens (a star) are kept for preflightPathCovered to
// match with path.Match semantics instead of as literals.
func preflightLiteralPathTokens(text []byte) []string {
	var out []string
	seen := map[string]bool{}
	for _, field := range strings.FieldsFunc(string(text), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '"' || r == '\'' || r == '(' || r == ')' || r == ';' || r == '|' || r == '&' || r == '`' || r == '=' || r == ','
	}) {
		if strings.Contains(field, "$") || strings.Contains(field, "://") {
			continue
		}
		// Glob tokens (a star) are allowlist entries the character class
		// below would drop: keep them when they read like a path (a slash,
		// a known file extension, or the star itself).
		if strings.Contains(field, "*") {
			tok := strings.TrimPrefix(field, "./")
			if tok == "" || strings.HasPrefix(tok, "/") || seen[tok] {
				continue
			}
			isGlob := strings.Contains(tok, "/") || strings.Contains(tok, "*")
			for _, e := range preflightPathExts {
				if strings.HasSuffix(tok, e) {
					isGlob = true
				}
			}
			if !isGlob {
				continue
			}
			seen[tok] = true
			out = append(out, tok)
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
		// Glob tokens match path itself or a directory prefix of it
		// (path.Match's star does not cross a slash, so "docs/*" covers
		// "docs/plans/page.md" by matching the prefix "docs/plans").
		if strings.Contains(tok, "*") {
			if preflightGlobCovers(tok, path) {
				return true
			}
			continue
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

// preflightGlobCovers reports whether the glob token tok covers path:
// tok matches path itself, or matches any of path's directory prefixes
// (each shorter ancestor, down to the first component).
func preflightGlobCovers(tok, p string) bool {
	if ok, err := path.Match(tok, p); err == nil && ok {
		return true
	}
	for {
		i := strings.LastIndex(p, "/")
		if i <= 0 {
			return false
		}
		p = p[:i]
		if ok, err := path.Match(tok, p); err == nil && ok {
			return true
		}
	}
}

// preflightGeneratedCovered reports whether p is a standard generated
// file, or under a standard generated folder: a gate that lets them
// through never reports them as missing, and a folder exempted by
// prefix covers everything under it.
func preflightGeneratedCovered(p string) bool {
	for _, g := range preflightStandardGeneratedFiles() {
		if g == p || strings.HasPrefix(p, strings.TrimSuffix(g, "/")+"/") {
			return true
		}
	}
	return false
}

// preflightAllowedFilesFindings reports each path the whole plan changes
// (the story and its tasks' file lists and verification scripts, and the
// paths each task's spec and title name, plus the sibling test files an
// in-tree Go change needs when dir is available) that the script's
// literal allowed-file list does not cover. Standard generated files
// are always treated as covered (never reported as missing). A script
// naming no literal path builds its list at run time and is not checked.
func preflightAllowedFilesFindings(dir, scriptRel string, text []byte, stories []Story) []VerifyScriptPreflightFinding {
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
	// Paths the plan's specs and titles name: the gate must allow these
	// even when the task's file lists omit them.
	for p, who := range gateAllowlistSpecPaths(stories) {
		add(p, who)
	}
	// Sibling test files: a Go change that exists in the tree and already
	// carries a test file must keep that test file in the allowed set
	// (skipped when dir is unavailable).
	if dir != "" {
		for _, p := range pathsOf(owner) {
			if sib := preflightSiblingTestFile(dir, p); sib != "" {
				add(sib, "the change to "+p)
			}
		}
	}
	paths := pathsOf(owner)
	sort.Strings(paths)
	var out []VerifyScriptPreflightFinding
	for _, p := range paths {
		if preflightPathCovered(p, tokens) || preflightGeneratedCovered(p) {
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

// pathsOf returns the map's keys as a fresh slice (iteration order not
// required; callers sort when needed).
func pathsOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
