// Release-version gate. A release task's whole point is to bump the
// version, and the version is defined to be whatever merge decides: the
// next patch above main at merge time. So a spec that hard-codes a
// fixed version number (e.g. "bump to v9.5.27") is self-defeating — it
// either ships the wrong number or forces a follow-up fix-up task once
// main has moved on in parallel. A spec that merely mentions some Go or
// third-party library version while doing unrelated work is fine, and
// must pass; only release-flavoured text (a version bump, a release
// version, a CHANGELOG version heading, `var Version`, or the merge
// version scripts) is held to the "no fixed number" bar. This function
// is the mechanical check for that rule; it is not wired into a call
// site yet.
package autonomous

import (
	"fmt"
	"regexp"
	"strings"
)

// releaseMarkers — the phrases that mark a task (or story) as a release
// task, matched case-insensitively against Title/Spec/Description.
var releaseMarkers = []string{
	"version bump",
	"bump version",
	"release version",
	"changelog",
	"var version",
	"set-release-version",
	"merge-branches-bump-version",
}

// dottedChainRE matches a maximal run of dot-separated numeric parts,
// optionally preceded by a lowercase `v`. Matching the whole chain (not
// just any three parts) is what keeps four-part dotted strings like IP
// addresses from ever reading as a fixed three-part version number.
var dottedChainRE = regexp.MustCompile(`\bv?(?:\d+(?:\.\d+)+)\b`)

// looksLikeReleaseText reports whether any of the given fields mentions
// a version bump, a release version, a CHANGELOG version heading,
// `var Version`, or one of the merge version scripts.
func looksLikeReleaseText(fields ...string) bool {
	for _, f := range fields {
		lower := strings.ToLower(f)
		for _, marker := range releaseMarkers {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	return false
}

// firstFixedReleaseVersion returns the first dotted version number in
// the text with exactly three numeric parts (e.g. "9.5.27" or
// "v9.5.27") that is the release version of the Automaton, or "" if
// there is none. Two-part numbers (Go/library versions), four-part
// dotted strings (IP addresses) and any number that belongs to a
// language, tool or library (see isReleaseNumberAt) are ignored.
func firstFixedReleaseVersion(text string) string {
	if locs := releaseVersionLocs(text); len(locs) > 0 {
		return text[locs[0][0]:locs[0][1]]
	}
	return ""
}

// releaseVersionLocs returns the [start,end) offsets of every
// three-part number in text that is the release version of the
// Automaton. It is the single detector shared by the gate, the
// self-correction rewrite and the spec lint.
func releaseVersionLocs(text string) [][]int {
	var out [][]int
	for _, loc := range dottedChainRE.FindAllStringIndex(text, -1) {
		if len(strings.Split(strings.TrimPrefix(text[loc[0]:loc[1]], "v"), ".")) != 3 {
			continue
		}
		if isReleaseNumberAt(text, loc[0], loc[1]) {
			out = append(out, loc)
		}
	}
	return out
}

// releaseLinkers are words that only link a number to what introduces
// it ("bump TO 9.5.27", "from 9.5.26 TO 9.5.27", "release version
// 9.5.27"): the backward walk skips them and looks at what comes before.
var releaseLinkers = map[string]bool{
	"to": true, "from": true, "at": true, "is": true, "was": true, "are": true,
	"were": true, "be": true, "been": true, "as": true, "now": true,
	"currently": true, "into": true, "and": true, "or": true, "than": true,
	"then": true, "the": true, "a": true, "an": true, "of": true, "in": true,
	"on": true, "for": true, "with": true, "by": true, "this": true,
	"that": true, "new": true, "next": true, "old": true, "current": true,
	"previous": true, "latest": true, "version": true, "versions": true,
	"release": true, "released": true, "releases": true,
	"->": true, "=>": true, "→": true, "-": true, "–": true, "—": true,
	"•": true, "=": true, "": true,
}

// releaseCues are words that, directly before a number, make it the
// release version of the Automaton ("bump 9.5.27", "CHANGELOG heading
// 9.5.27", `var Version = "9.5.27"`).
var releaseCues = map[string]bool{
	"bump": true, "bumps": true, "bumped": true, "bumping": true,
	"changelog": true, "changelog.md": true, "heading": true, "headings": true,
	"entry": true, "number": true, "set": true, "setting": true,
	"ship": true, "shipping": true, "cut": true, "cutting": true,
	"publish": true, "publishing": true, "prepare": true, "preparing": true,
	"patch": true, "minor": true, "major": true, "label": true, "rename": true,
	"var": true, "const": true, "string": true,
}

var numericTokenRE = regexp.MustCompile(`^v?\d+(?:\.\d+)+$`)

// isReleaseNumberAt reports whether the dotted number text[s:e] is the
// release version of the Automaton rather than a version of something
// else. It is the release version only when it is introduced by the
// release vocabulary (release / version / bump / CHANGELOG heading /
// `var Version` / "from X to Y" between release numbers) or stands bare
// at the start of a clause. It is never the release version when it is
// attached to or follows a language, tool or library name (Python 3.11.4,
// Go 1.22.5, requests 2.31.0, "library version 2.31.0"), is a pin
// (`>=`, `==`, `~=`, `^`, `~`), is part of a path, URL, package path or
// image tag (`/1.2.3`, `@v1.2.3`, `python:3.11.4-slim`, `pkg-1.2.3`), or
// is followed by a file extension or a tag suffix.
func isReleaseNumberAt(text string, s, e int) bool {
	// What follows the number.
	if e < len(text) {
		switch c := text[e]; {
		case c == '/':
			return false // path or URL segment
		case (c == '-' || c == '_' || c == '+') && e+1 < len(text) && isAlnum(text[e+1]):
			return false // image tag or build suffix
		case c == '.' && e+1 < len(text) && isAlpha(text[e+1]):
			return false // file name such as 1.2.3.tar.gz
		}
	}
	// What is attached to, or pins, the number.
	trimmed := strings.TrimRight(text[:s], " \t")
	arrow := strings.HasSuffix(trimmed, "->") || strings.HasSuffix(trimmed, "=>")
	if !arrow {
		for _, op := range []string{"==", ">=", "<=", "~=", "!=", "^", "~", ">", "<"} {
			if strings.HasSuffix(trimmed, op) {
				return false
			}
		}
		if s > 0 && strings.IndexByte("/@_-.+", text[s-1]) >= 0 {
			return false
		}
	}
	// Walk backwards over linking words to what introduces the number.
	pos := s
	for pos > 0 {
		tok, start := prevToken(text, pos)
		switch {
		case numericTokenRE.MatchString(tok):
			return isReleaseNumberAt(text, start, start+len(tok))
		case releaseLinkers[tok]:
			if start >= pos {
				return true
			}
			pos = start
			if tok == "" {
				return true
			}
		case releaseCues[tok], strings.Contains(tok, "set-release-version"), strings.Contains(tok, "bump-version"):
			return true
		default:
			return false // a name: language, tool, library, package, image
		}
	}
	return true
}

// prevToken returns the lower-cased whitespace-delimited word that ends
// before pos (skipping whitespace, quotes, brackets and separators), and
// its start offset. An empty token means the start of the text.
func prevToken(text string, pos int) (string, int) {
	const skip = " \t\n\r`\"'([{*#:,;"
	j := pos
	for j > 0 && strings.IndexByte(skip, text[j-1]) >= 0 {
		j--
	}
	k := j
	for k > 0 && strings.IndexByte(skip, text[k-1]) < 0 {
		k--
	}
	tok := strings.TrimRight(text[k:j], ".!?)]}")
	return strings.ToLower(tok), k
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isAlnum(c byte) bool { return isAlpha(c) || c >= '0' && c <= '9' }

// validateNoFixedReleaseVersion walks every story and task and rejects
// any release task whose Title or Spec — or any release story whose
// Title or Description — names a fixed three-part version number. A
// release task is one whose Title or Spec, case-insensitively, mentions
// a version bump, a release version, a CHANGELOG version heading,
// `var Version`, or the merge version scripts
// (set-release-version / merge-branches-bump-version). Non-release
// tasks are never scrutinised, so a spec that merely states a Go or
// library version passes untouched. An empty slice passes.
func validateNoFixedReleaseVersion(stories []Story) error {
	if len(stories) == 0 {
		return nil
	}
	const guidance = "release tasks must not contain a fixed version number; the version is taken at merge time as the next patch above main; remove the number and say 'next patch above main' instead"
	for _, s := range stories {
		if looksLikeReleaseText(s.Title, s.Description) {
			for _, field := range []struct{ name, text string }{
				{"title", s.Title},
				{"description", s.Description},
			} {
				if v := firstFixedReleaseVersion(field.text); v != "" {
					return fmt.Errorf("story %q: fixed version number %q in the story %s — %s",
						s.Title, v, field.name, guidance)
				}
			}
		}
		for _, t := range s.Tasks {
			if !looksLikeReleaseText(t.Title, t.Spec) {
				continue
			}
			for _, field := range []struct{ name, text string }{
				{"title", t.Title},
				{"spec", t.Spec},
			} {
				if v := firstFixedReleaseVersion(field.text); v != "" {
					return fmt.Errorf("story %q, task %q: fixed version number %q in the task %s — %s",
						s.Title, t.Title, v, field.name, guidance)
				}
			}
		}
	}
	return nil
}
