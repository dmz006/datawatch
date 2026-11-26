package mergepipeline

import (
	"errors"
	"path"
	"strings"
)

// conflictRule is the project's rule for a conflicted file: the first rule
// whose path names the file or matches it, or "".
func (d Definition) conflictRule(file string) string {
	for _, r := range d.ConflictRules {
		if r.Path == file {
			return r.Rule
		}
		if ok, err := path.Match(r.Path, file); err == nil && ok {
			return r.Rule
		}
	}
	return ""
}

// resolveByRule settles each conflicting hunk of text by a project rule.
// Lines outside the hunks, which git already merged from both sides, are kept.
// keep_main takes main's side of each hunk and keep_both takes main's lines
// followed by the branch's. The ask rule, and a file with no readable hunk (a
// deleted or renamed one), is left for the operator.
func resolveByRule(rule, file, text string) (string, []ResolvedHunk, error) {
	if rule != ConflictRuleKeepMain && rule != ConflictRuleKeepBoth {
		return "", nil, errors.New("the project's rule is to ask")
	}
	segs, err := parseConflicts(file, text)
	if err != nil {
		return "", nil, errors.New("no usable conflict markers")
	}
	var b strings.Builder
	var resolved []ResolvedHunk
	for _, s := range segs {
		if s.hunk == nil {
			b.WriteString(strings.Join(s.plain, ""))
			continue
		}
		b.WriteString(strings.Join(ensureNewline(s.hunk.Ours), ""))
		if rule == ConflictRuleKeepBoth {
			b.WriteString(strings.Join(ensureNewline(s.hunk.Theirs), ""))
		}
		kept := len(s.hunk.Ours)
		if rule == ConflictRuleKeepBoth {
			kept += len(s.hunk.Theirs)
		}
		resolved = append(resolved, ResolvedHunk{File: file, Index: s.hunk.Index, Line: s.hunk.Line,
			OursLines: len(s.hunk.Ours), TheirsLines: len(s.hunk.Theirs), KeptLines: kept, Method: rule})
	}
	if len(resolved) == 0 {
		return "", nil, errors.New("no usable conflict markers")
	}
	return b.String(), resolved, nil
}
