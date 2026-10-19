package autonomous

import (
	"regexp"
	"strings"
)

// checkpointSaysAllDone reports whether a worker's CHECKPOINT.md text clearly
// says every step is finished. The worker writes steps completed, current
// step and remaining sections (see the CHECKPOINT PROTOCOL in
// cmd/datawatch/autonomous_effort.go). It returns true only when:
//
//   - the Remaining section lists nothing but none, nothing or done, or the
//     section is absent and the text makes an explicit "all steps done" claim;
//   - the Current step section, if present, lists no open work; and
//   - no part of the text says work is still outstanding.
//
// It is conservative on purpose: when unsure it returns false, because a
// false positive would skip a real stall. It does no I/O.
func checkpointSaysAllDone(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	if checkpointOpenPhrase.MatchString(text) {
		return false
	}
	lines := strings.Split(text, "\n")
	remaining, found := checkpointSection(lines, "remaining", "remaining steps", "steps remaining")
	if found {
		if len(remaining) == 0 {
			return false
		}
		for _, item := range remaining {
			if !checkpointNothingLeft[item] {
				return false
			}
		}
	} else if !checkpointAllDonePhrase.MatchString(text) {
		return false
	}
	current, _ := checkpointSection(lines, "current step")
	for _, item := range current {
		if !checkpointStepFinished[item] {
			return false
		}
	}
	return true
}

// checkpointOpenPhrase matches wording that says work is still outstanding
// anywhere in the checkpoint. Any hit means the answer is no.
var checkpointOpenPhrase = regexp.MustCompile(`(?i)\b(still open|still in progress|not (yet )?(done|complete|finished)|in progress|pending)\b`)

// checkpointAllDonePhrase matches an explicit claim that every step is done,
// such as "All steps done" or "all 6 steps are completed".
var checkpointAllDonePhrase = regexp.MustCompile(`(?i)\ball\b[^\n.]{0,30}\b(steps?|tasks?|items?)\b[^\n.]{0,20}\b(done|completed|complete|finished)\b`)

// checkpointNothingLeft holds the Remaining entries that mean no work is left.
var checkpointNothingLeft = map[string]bool{"none": true, "nothing": true, "done": true}

// checkpointStepFinished holds the Current step entries that mean the step is
// no longer open.
var checkpointStepFinished = map[string]bool{
	"none": true, "nothing": true, "done": true,
	"complete": true, "completed": true, "finished": true,
}

// checkpointBullet, checkpointCheckbox and checkpointParens strip list markers
// and parenthetical notes so "- [x] none (all complete)" reads as "none".
var (
	checkpointBullet   = regexp.MustCompile(`^(?:[-*+]|\d+[.)])\s+`)
	checkpointCheckbox = regexp.MustCompile(`^\[[ xX]\]\s*`)
	checkpointParens   = regexp.MustCompile(`\([^)]*\)`)
)

// checkpointLabelLine matches a line that opens a new section in bold or
// "Label:" form, so the section being read ends there.
var checkpointLabelLine = regexp.MustCompile(`^(\*\*[^*]+\*\*|[A-Za-z][A-Za-z ]*):$`)

// checkpointSection returns the normalised items under the first heading whose
// label matches one of names, and whether such a heading was found. The label
// may be a markdown heading, bold or plain, in any case. Text after a colon on
// the heading line counts as an item.
func checkpointSection(lines []string, names ...string) ([]string, bool) {
	for i, line := range lines {
		label, rest := checkpointLabel(line)
		if !checkpointHasName(names, label) {
			continue
		}
		body := []string{rest}
		for _, next := range lines[i+1:] {
			if checkpointBoundary(next) {
				break
			}
			body = append(body, next)
		}
		return checkpointItems(body), true
	}
	return nil, false
}

// checkpointLabel splits a line into its lower-cased label (the text before
// the first colon, with markdown heading and bold markers removed) and the
// remainder after the colon.
func checkpointLabel(line string) (string, string) {
	clean := strings.ReplaceAll(line, "*", "")
	clean = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(clean), "#"))
	label, rest, _ := strings.Cut(clean, ":")
	return strings.ToLower(strings.TrimSpace(label)), strings.TrimSpace(rest)
}

func checkpointHasName(names []string, label string) bool {
	for _, name := range names {
		if label == name {
			return true
		}
	}
	return false
}

// checkpointBoundary reports whether a line starts a new section.
func checkpointBoundary(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "#") || checkpointLabelLine.MatchString(t)
}

// checkpointItems normalises section lines to lower-case entries with list
// markers, checkboxes, parentheticals and trailing punctuation removed. Blank
// lines and horizontal rules are dropped.
func checkpointItems(lines []string) []string {
	var items []string
	for _, line := range lines {
		item := strings.TrimSpace(line)
		item = checkpointBullet.ReplaceAllString(item, "")
		item = checkpointCheckbox.ReplaceAllString(item, "")
		item = checkpointParens.ReplaceAllString(item, "")
		item = strings.ToLower(strings.Trim(strings.TrimSpace(item), ".!"))
		if strings.Trim(item, "-*_=") == "" {
			continue
		}
		items = append(items, item)
	}
	return items
}

// CheckpointSaysAllDone is the exported form of checkpointSaysAllDone for
// callers outside this package (the verify wait loop in cmd/datawatch).
func CheckpointSaysAllDone(text string) bool {
	return checkpointSaysAllDone(text)
}
