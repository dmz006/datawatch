package mergepipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
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
	case ClassLocale:
		return resolveLocale(rel, conflicted, ours, theirs)
	case ClassGenerated:
		// A generated file is never merged by hand: the ours side is kept as
		// the settled text and the caller rebuilds the file from its sources.
		return ours, true, []string{fmt.Sprintf("%s: generated file; kept ours, regenerate it", rel)}, nil
	}
	return conflicted, false, []string{fmt.Sprintf("%s: class %q has no mechanical rule", rel, class)}, nil
}

// MakefileHasDocsIndexTarget reports whether makefileText defines a docs-index
// target, that is, whether any line starts with "docs-index:".
func MakefileHasDocsIndexTarget(makefileText string) bool {
	for _, l := range strings.Split(makefileText, "\n") {
		if strings.HasPrefix(l, "docs-index:") {
			return true
		}
	}
	return false
}

// parseFlatLocale parses a flat JSON object whose values are all strings.
func parseFlatLocale(side, text string) (map[string]string, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("%s side is not a JSON object: %w", side, err)
	}
	if raw == nil {
		return nil, fmt.Errorf("%s side is not a JSON object", side)
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		sv, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s side key %q has a non-string value", side, k)
		}
		out[k] = sv
	}
	return out, nil
}

// resolveLocale merges two flat locale files by key union. Keys changed on
// both sides to different values keep ours and get a note. Output is two-space
// indented JSON with sorted keys and a trailing newline.
func resolveLocale(rel, conflicted, ours, theirs string) (string, bool, []string, error) {
	o, err := parseFlatLocale("ours", ours)
	if err != nil {
		return conflicted, false, []string{fmt.Sprintf("%s: %v", rel, err)}, err
	}
	t, err := parseFlatLocale("theirs", theirs)
	if err != nil {
		return conflicted, false, []string{fmt.Sprintf("%s: %v", rel, err)}, err
	}
	var notes []string
	merged := make(map[string]string, len(o)+len(t))
	for k, v := range o {
		merged[k] = v
	}
	var theirKeys []string
	for k := range t {
		theirKeys = append(theirKeys, k)
	}
	sort.Strings(theirKeys)
	for _, k := range theirKeys {
		if ov, ok := o[k]; ok {
			if ov != t[k] {
				notes = append(notes, fmt.Sprintf("%s: key %q changed on both sides; kept ours", rel, k))
			}
			continue
		}
		merged[k] = t[k]
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(merged); err != nil {
		return conflicted, false, []string{fmt.Sprintf("%s: %v", rel, err)}, err
	}
	return buf.String(), true, notes, nil
}
