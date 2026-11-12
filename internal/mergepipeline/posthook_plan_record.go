// The write-through path of the registry_row built-in: when the operator has
// turned autonomous.plan_records.write_through on and the daemon wired a
// record writer, the hook records the shipped row as a plan record instead of
// editing the registry README. This package never imports the plan-record
// store: the daemon supplies a RecordWriter.

package mergepipeline

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

// ShippedRow is what the registry_row hook hands the record writer.
type ShippedRow struct {
	// Project is the project alias the row belongs to.
	Project string
	// LegacyNumber is the old registry row number when the row pattern
	// carries one; zero otherwise.
	LegacyNumber int
	// MatchTitles are the titles tried, in order, when there is no legacy
	// number or no record holds it: the row pattern text, then the
	// Automaton title.
	MatchTitles []string
	// CreateTitle titles a record the writer has to create.
	CreateTitle string
	// ShippedBy is the Automaton id.
	ShippedBy string
	// ShippedVersion is the shipped version text.
	ShippedVersion string
}

// RecordWriter records a shipped registry row. UpsertShipped updates the
// record that matches (legacy number first, then title or alias within the
// project) and creates one when none does. The returned text is a short
// account of what it did.
type RecordWriter interface {
	UpsertShipped(ctx context.Context, row ShippedRow) (string, error)
}

var (
	legacyNumberRe = regexp.MustCompile(`^\s*\|?\s*#?\s*(\d+)\s*\|`)
	rowWordRe      = regexp.MustCompile(`(?i)^\s*row\s+#?(\d+)\b`)
)

// patternLegacyNumber reads the row number a rendered row pattern carries
// ("| 101 |", "row 101"); zero when it carries none.
func patternLegacyNumber(pattern string) int {
	for _, re := range []*regexp.Regexp{legacyNumberRe, rowWordRe} {
		if m := re.FindStringSubmatch(pattern); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

// patternTitle turns a rendered row pattern into plain title text: table
// pipes and a leading row number are dropped.
func patternTitle(pattern string) string {
	parts := strings.Split(pattern, "|")
	var kept []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) > 1 && patternLegacyNumber(pattern) > 0 {
		kept = kept[1:]
	}
	return strings.Join(kept, " ")
}

// RunRegistryRowWriteThrough is the registry_row built-in's write-through
// path. It never touches the registry file, and a failed write fails the hook
// with a redacted error: it never falls back to editing the README.
func RunRegistryRowWriteThrough(ctx context.Context, w RecordWriter, project, automatonID, automatonTitle, version string, s PostShipSettings) (string, error) {
	pattern := render(s.RegistryRowPattern, project, version)
	if strings.TrimSpace(pattern) == "" {
		return "", builtinErr("the registry_row hook has no registry_row_pattern setting")
	}
	row := ShippedRow{
		Project:        project,
		LegacyNumber:   patternLegacyNumber(pattern),
		ShippedBy:      automatonID,
		ShippedVersion: version,
	}
	pt := patternTitle(pattern)
	at := strings.TrimSpace(automatonTitle)
	for _, t := range []string{pt, at} {
		if t != "" {
			row.MatchTitles = append(row.MatchTitles, t)
		}
	}
	row.CreateTitle = at
	if row.CreateTitle == "" {
		row.CreateTitle = pt
	}
	note, err := w.UpsertShipped(ctx, row)
	if err != nil {
		return "", builtinErr("recording the shipped row: %s", err)
	}
	if strings.TrimSpace(note) == "" {
		note = "recorded the shipped row"
	}
	return redact(note), nil
}
