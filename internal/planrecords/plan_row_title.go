package planrecords

import (
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
	// TitleFellBack means no name could be derived from the row. The row is
	// listed under NeedsReviewLabel and no plan is created for it.
	TitleFellBack = "fallback"
)

// NeedsReviewLabel is the exact label under which a row with no derivable
// name is listed in the preview. Such a row is never created on apply.
const NeedsReviewLabel = "needs review"

// clauseEndRe ends the first clause of a description.
var clauseEndRe = regexp.MustCompile(`[,;:]\s|\s[-–—]\s|[.!?](?:\s|$)`)

// firstClause returns the text of s up to its first clause break.
func firstClause(s string) string {
	if loc := clauseEndRe.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}
	return strings.TrimSpace(s)
}

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

// rowTitleCells lists the cells of a row that may hold its title: the stored
// title first, then the title-like columns kept in the body. For each cell it
// also says whether it is a description-like column that gives only its first
// clause.
func rowTitleCells(r Record) ([]string, []bool) {
	out := []string{r.Title}
	clause := []bool{false}
	for _, ln := range strings.Split(r.Body, "\n") {
		name, val, ok := strings.Cut(ln, ":")
		if !ok || !bodyTitleFields[strings.ToLower(strings.TrimSpace(name))] {
			continue
		}
		out = append(out, strings.TrimSpace(val))
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "description", "summary", "goal":
			clause = append(clause, true)
		default:
			clause = append(clause, false)
		}
	}
	return out, clause
}

// migratedRowTitle names the plan made for a registry row. It takes the real
// title from the row (see realRowTitle), one line, at most 100 characters in
// all, and says which way it was found. The title column comes first, else the
// first clause of the description; never the status column. A row with no
// derivable name returns TitleFellBack and an empty title.
func migratedRowTitle(r Record) (title, source string) {
	cells, clause := rowTitleCells(r)
	for i, c := range cells {
		t := realRowTitle(c)
		if t != "" && clause[i] {
			t = firstClause(t)
		}
		if t != "" {
			return clipRunes(migratedTitlePrefix+t, maxMigratedTitleRunes), TitleDerived
		}
	}
	return "", TitleFellBack
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
