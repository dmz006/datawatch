package planrecords

import (
	"fmt"
	"regexp"
	"strings"
)

// Where a migrated plan's title came from. Only registry rows have one.
const (
	// TitleDerived means the title was found in the row's title or
	// description text.
	TitleDerived = "derived"
	// TitleFromLinked means the row had no title, so the plan takes the title
	// of the Automaton or Automation the row names.
	TitleFromLinked = "linked"
	// TitleFellBack means the row held no real title, so the plan is named
	// after the row's key and marked untitled.
	TitleFellBack = "fallback"
)

// maxMigratedTitleRunes bounds a migrated plan's whole title, "Plan: " included.
const maxMigratedTitleRunes = MaxPlanTitleRunes

const migratedTitlePrefix = "Plan: "

var (
	// statusWordRe matches a status word at the start of a cell, with the
	// separators after it.
	statusWordRe = regexp.MustCompile(`(?i)^(?:not started|in progress|needs[ _]review|done|shipped|completed?|failed|blocked|cancelled|canceled|dropped|decided|implemented|merged|superseded|deferred|planned|started|running|draft|open|closed)\b[\s,:;.\-–—]*`)
	// statusNoteRe matches text that reads as a status note, not a name: a
	// version it shipped in or a PRD id.
	statusNoteRe = regexp.MustCompile(`(?i)\bshipped in\b|\bPRD\s+[0-9a-f]{8}\b|^(?:in|at|by|since|on)\s+\d|^\d+(?:\.\d+)+\b|^\(|^\d+[a-z]?\s+shipped\b`)
	// versionLedRe matches a cell that starts with a version or a bracket.
	versionLedRe = regexp.MustCompile(`^\(|^\d+(?:\.\d+)+\b`)
	// prdLedRe matches a cell that starts with a PRD id.
	prdLedRe = regexp.MustCompile("(?i)^PRD\\s+`?[0-9a-f]{8}\\b")
	// bodyTitleFields are the body lines (other columns of the row) that can
	// hold a real title when the Item cell does not.
	bodyTitleFields = map[string]bool{"description": true, "summary": true, "goal": true, "title": true, "item": true}
)

// realRowTitle returns the title text found in one cell, or "" when the cell
// is a status note. Leading status words are dropped ("Done: Fix it" is "Fix
// it"); a cell that is only a status note ("done, shipped in 9.5.92 (PRD
// 332c0001)") or that starts with a PRD id has no title.
func realRowTitle(cell string) string {
	s := mdLinkRe.ReplaceAllString(cell, "$1")
	s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || prdLedRe.MatchString(s) {
		return ""
	}
	statusLed := false
	for {
		loc := statusWordRe.FindStringIndex(s)
		if loc == nil {
			break
		}
		statusLed = true
		s = strings.TrimSpace(s[loc[1]:])
	}
	if s == "" {
		return ""
	}
	if statusLed && statusNoteRe.MatchString(s) {
		return ""
	}
	if !statusLed && versionLedRe.MatchString(s) {
		return ""
	}
	title, _ := splitTitle(s)
	return strings.Join(strings.Fields(title), " ")
}

// rowTitleCandidates lists the cells of a row that may hold its title: the
// stored title first, then the title-like columns kept in the body.
func rowTitleCandidates(r Record) []string {
	out := []string{r.Title}
	for _, ln := range strings.Split(r.Body, "\n") {
		name, val, ok := strings.Cut(ln, ":")
		if !ok || !bodyTitleFields[strings.ToLower(strings.TrimSpace(name))] {
			continue
		}
		out = append(out, strings.TrimSpace(val))
	}
	return out
}

// migratedRowTitle names the plan made for a registry row. It takes the real
// title from the row (see realRowTitle), one line, at most 100 characters in
// all, and says which way it was found. A row with no real title is named
// from its key: "Plan: (untitled row 15)".
func migratedRowTitle(r Record) (title, source string) {
	for _, c := range rowTitleCandidates(r) {
		if t := realRowTitle(c); t != "" {
			return clipRunes(migratedTitlePrefix+t, maxMigratedTitleRunes), TitleDerived
		}
	}
	key := r.ID
	if r.LegacyNumber > 0 {
		key = fmt.Sprintf("%d", r.LegacyNumber)
	}
	return fmt.Sprintf("%s(untitled row %s)", migratedTitlePrefix, key), TitleFellBack
}

// linkedPlanTitle makes a plan title from the title of an Automaton or
// Automation: one line, at most 100 characters, "Plan: " included. It is ""
// when that title is empty.
func linkedPlanTitle(t string) string {
	t = strings.Join(strings.Fields(t), " ")
	if t == "" {
		return ""
	}
	return clipRunes(migratedTitlePrefix+t, maxMigratedTitleRunes)
}
