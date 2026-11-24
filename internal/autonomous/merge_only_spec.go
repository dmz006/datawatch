// merge_only_spec.go — the "merge-only spec" detector behind the docs
// set rule.
//
// A merge-only spec is an Automaton spec whose entire scope is landing
// work that is already finished in an existing branch: merging it,
// rebasing it, resolving conflicts on it, or landing a finished
// branch. Such an Automaton changes none of the behavior the
// operator-facing docs describe, so the docs set standard task does not
// apply to it: ApplyTaskTemplatesForSpec consults IsMergeOnlySpec and
// skips the docs set template (TemplateDocsSet) for those Automata,
// while every other standard task (including the final gate) is
// applied as usual.
//
// Detection is deliberately conservative and prefix-safe: the spec must
// carry at least one merge signal (merge, rebase, land, conflict, or a
// finished-branch phrase) and must carry none of the work verbs (add,
// implement, create, write, fix, build — each with its inflected forms,
// word-bounded, so "address" is not "add" and "implementation" is not
// "implement"). Any doubt keeps the spec not-merge-only, so the docs
// set task stays: an extra docs task is the safe failure.
package autonomous

import (
	"regexp"
	"strings"
)

// MergeOnlySpecSkipNote is the note recorded (decompose log line) when
// a merge-only Automaton's docs set task is skipped.
const MergeOnlySpecSkipNote = "docs set task skipped: merge-only Automaton"

// mergeOnlySignalPatterns: at least one must match the lower-cased spec
// for it to count as merge-only. Word-bounded stems with their common
// inflected forms; the finished-branch phrases cover the "branch is
// finished" word orders.
var mergeOnlySignalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(merge|merged|merging)\b`),
	regexp.MustCompile(`\b(rebase|rebased|rebasing)\b`),
	regexp.MustCompile(`\b(land|landed|landing)\b`),
	regexp.MustCompile(`\b(conflict|conflicts)\b`),
	regexp.MustCompile(`\bfinished branches?\b`),
	regexp.MustCompile(`\bbranches? (is|are) finished\b`),
}

// mergeOnlyRejectPatterns: any match means the spec carries new work and
// is not merge-only. Inflected forms are listed explicitly and word
// bounded so an unrelated word sharing a prefix never counts as a work
// verb.
var mergeOnlyRejectPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(add|added|adds|adding)\b`),
	regexp.MustCompile(`\b(implement|implements|implemented|implementing)\b`),
	regexp.MustCompile(`\b(create|creates|created|creating)\b`),
	regexp.MustCompile(`\b(write|writes|written|writing|wrote)\b`),
	regexp.MustCompile(`\b(fix|fixes|fixed|fixing)\b`),
	regexp.MustCompile(`\b(build|builds|built|building)\b`),
}

// IsMergeOnlySpec reports whether spec is merge-only: it carries at
// least one merge/land/rebase/conflict/finished-branch signal and none
// of the work verbs (add, implement, create, write, fix, build).
// Matching is case-insensitive and word-bounded. An empty or signalless
// spec is not merge-only.
func IsMergeOnlySpec(spec string) bool {
	s := strings.ToLower(spec)
	signals := 0
	for _, re := range mergeOnlySignalPatterns {
		if re.MatchString(s) {
			signals++
		}
	}
	if signals == 0 {
		return false
	}
	for _, re := range mergeOnlyRejectPatterns {
		if re.MatchString(s) {
			return false
		}
	}
	return true
}
