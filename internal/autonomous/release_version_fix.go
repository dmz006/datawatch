// release_version_fix.go — daemon self-correction of fixed release
// numbers. When the release-version gate is the ONLY content failure of
// a decomposed plan, the daemon rewrites each fixed number to neutral
// wording instead of bouncing the plan back to the planner. Pure text
// work: it never touches, kills or signals a session.
package autonomous

import (
	"fmt"
	"strings"
	"time"
)

// Replacement is one fixed release number reworded by
// CorrectFixedReleaseVersions.
type Replacement struct {
	Story string `json:"story"`
	Task  string `json:"task,omitempty"` // empty for a story-level field
	Field string `json:"field"`          // title | description | spec
	Old   string `json:"old"`
	New   string `json:"new"`
}

const (
	releaseNeutralBare = "the next patch above main"
	releaseNeutralWord = "the release"
)

// correctReleaseText rewrites every fixed three-part number in text that
// is the release version of the Automaton (see isReleaseNumberAt); a
// Python, Go, Node or library version, a pin, a path, URL or image tag is
// left exactly as written. A number introduced by the word "version" or "release" is replaced
// together with that word by "the release"; a bare number becomes "the
// next patch above main".
func correctReleaseText(text string) (string, []Replacement) {
	locs := releaseVersionLocs(text)
	if len(locs) == 0 {
		return text, nil
	}
	var b strings.Builder
	var reps []Replacement
	last := 0
	for _, loc := range locs {
		start, repl := loc[0], releaseNeutralBare
		if i := lastWordBefore(text, loc[0]); i >= last {
			if w := strings.ToLower(strings.TrimSpace(text[i:loc[0]])); w == "version" || w == "release" {
				start, repl = i, releaseNeutralWord
			}
		}
		b.WriteString(text[last:start])
		b.WriteString(repl)
		reps = append(reps, Replacement{Old: text[start:loc[1]], New: repl})
		last = loc[1]
	}
	if len(reps) == 0 {
		return text, nil
	}
	b.WriteString(text[last:])
	return b.String(), reps
}

// CorrectFixedReleaseVersions returns a copy of stories with each fixed
// release number in a release story's title/description or a release
// task's title/spec reworded, plus the list of replacements. The input
// is never modified. A plan with no fixed number returns an unchanged
// copy and no replacements.
func CorrectFixedReleaseVersions(stories []Story) (fixed []Story, replacements []Replacement) {
	fixed = make([]Story, len(stories))
	copy(fixed, stories)
	for i := range fixed {
		s := &fixed[i]
		storyTitle := stories[i].Title
		fix := func(task, field string, p *string) {
			out, reps := correctReleaseText(*p)
			for _, r := range reps {
				r.Story, r.Task, r.Field = storyTitle, task, field
				replacements = append(replacements, r)
			}
			*p = out
		}
		if looksLikeReleaseText(s.Title, s.Description) {
			fix("", "title", &s.Title)
			fix("", "description", &s.Description)
		}
		if len(s.Tasks) > 0 {
			s.Tasks = make([]Task, len(stories[i].Tasks))
			copy(s.Tasks, stories[i].Tasks)
		}
		for j := range s.Tasks {
			t := &s.Tasks[j]
			if !looksLikeReleaseText(t.Title, t.Spec) {
				continue
			}
			orig := stories[i].Tasks[j].Title
			fix(orig, "title", &t.Title)
			fix(orig, "spec", &t.Spec)
		}
	}
	return fixed, replacements
}

// releaseFixDecisionNote renders the decision-log line: every
// replacement as 'old phrase -> new phrase' with its story and task.
func releaseFixDecisionNote(reps []Replacement) string {
	parts := make([]string, 0, len(reps))
	for _, r := range reps {
		where := fmt.Sprintf("story %q", r.Story)
		if r.Task != "" {
			where += fmt.Sprintf(" task %q", r.Task)
		}
		parts = append(parts, fmt.Sprintf("%s %s: %q -> %q", where, r.Field, r.Old, r.New))
	}
	return "release-version-fixed self-corrected: " + strings.Join(parts, "; ")
}

// recordReleaseVersionFix writes one decision-log line for the
// replacements and clears LastDecomposeFailure, as a success does.
func (m *Manager) recordReleaseVersionFix(prd *PRD, reps []Replacement) {
	note := releaseFixDecisionNote(reps)
	m.store.WithLock(func() {
		prd.Decisions = append(prd.Decisions, Decision{
			At: time.Now(), Kind: "auto_correct", Actor: "autonomous",
			Note: note, FailureCategory: "content",
		})
		prd.LastDecomposeFailure = nil
	})
}

// tryCorrectReleaseVersions applies the self-correction to s in place
// when the release-version gate is the only content failure. Returns
// true when s was corrected and the correction logged. Otherwise s is
// untouched and the caller takes the old path.
func (m *Manager) tryCorrectReleaseVersions(prd *PRD, s []Story) bool {
	if validateNoFixedReleaseVersion(s) == nil {
		return false
	}
	fixed, reps := CorrectFixedReleaseVersions(s)
	if len(reps) == 0 || validateNoFixedReleaseVersion(fixed) != nil {
		return false
	}
	// Every other gate must pass on the corrected plan.
	if validateVerificationScriptCoverage(prd.ProjectDir, fixed) != nil ||
		validateNoUnscopedTmux(prd.ProjectDir, fixed) != nil ||
		validateVerificationScriptScope(prd.ProjectDir, fixed) != nil {
		return false
	}
	copy(s, fixed)
	m.recordReleaseVersionFix(prd, reps)
	return true
}
