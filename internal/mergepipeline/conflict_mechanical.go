package mergepipeline

import (
	"fmt"
	"regexp"
	"strings"
)

// versionLineRe matches a line that carries a version number such as 1.2.3 or
// v10.0, which is what a version or release line holds.
var versionLineRe = regexp.MustCompile(`\d+\.\d+(\.\d+)?`)

func isVersionLine(l string) bool { return versionLineRe.MatchString(l) }

func allVersionLines(lines []string) bool {
	for _, l := range lines {
		if !isVersionLine(l) {
			return false
		}
	}
	return true
}

// ResolveMechanicalFile settles a conflicted file by the rule of its class.
// ClassChangelog goes through ResolveAdditive and is settled only when no hunk
// is left. ClassVersionLine and ClassReadmeLine keep the ours side of every
// hunk that changes only a version or release line; any other hunk leaves the
// file unsettled, with its markers kept, and is not an error. Other classes
// are never settled. text is the file after the hunks that could be settled.
func ResolveMechanicalFile(class FileClass, rel string, conflicted string, ours string, theirs string) (text string, settled bool, notes []string, err error) {
	switch class {
	case ClassChangelog:
		out, resolved, remaining, aerr := ResolveAdditive(rel, conflicted)
		if aerr != nil {
			return conflicted, false, nil, aerr
		}
		if len(resolved) > 0 {
			notes = append(notes, fmt.Sprintf("%s: %d additive hunk(s) kept from both sides", rel, len(resolved)))
		}
		if len(remaining) > 0 {
			notes = append(notes, fmt.Sprintf("%s: %d hunk(s) are not additive", rel, len(remaining)))
			return out, false, notes, nil
		}
		return out, true, notes, nil
	case ClassVersionLine, ClassReadmeLine:
		segs, perr := parseConflicts(rel, conflicted)
		if perr != nil {
			return conflicted, false, nil, perr
		}
		var b strings.Builder
		settled = true
		for _, s := range segs {
			if s.hunk == nil {
				for _, l := range s.plain {
					b.WriteString(l)
				}
				continue
			}
			h := s.hunk
			if len(h.Ours) > 0 && allVersionLines(h.Ours) && allVersionLines(h.Theirs) && allVersionLines(h.Base) {
				for _, l := range ensureNewline(h.Ours) {
					b.WriteString(l)
				}
				notes = append(notes, fmt.Sprintf("%s: hunk %d is a version line; kept ours", rel, h.Index))
				continue
			}
			settled = false
			notes = append(notes, fmt.Sprintf("%s: hunk %d changes more than the version line", rel, h.Index))
			b.WriteString(renderHunk(h))
		}
		return b.String(), settled, notes, nil
	}
	return conflicted, false, []string{fmt.Sprintf("%s: class %q has no mechanical rule", rel, class)}, nil
}
