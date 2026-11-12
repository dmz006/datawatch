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
// "v9.5.27"), or "" if there is none. Two-part numbers (Go/library
// versions) and four-part dotted strings (IP addresses) are ignored.
func firstFixedReleaseVersion(text string) string {
	for _, m := range dottedChainRE.FindAllString(text, -1) {
		if len(strings.Split(strings.TrimPrefix(m, "v"), ".")) == 3 {
			return m
		}
	}
	return ""
}

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
