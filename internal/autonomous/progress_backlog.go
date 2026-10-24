package autonomous

import (
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// BacklogRow is one row of the backlog table in the plans registry, read for the
// progress engine.
type BacklogRow struct {
	Number  string   `json:"number"`
	ID      string   `json:"id,omitempty"` // tracker token found in Item; data only, never shown to operators
	Item    string   `json:"item"`
	Status  string   `json:"status"`
	Kind    string   `json:"kind"`
	PRDIDs  []string `json:"prd_ids"`
	Phases  int      `json:"phases"`
	Open    bool     `json:"open"`
	Unfiled bool     `json:"unfiled"`
}

var (
	backlogRowRE     = regexp.MustCompile("^\\| (\\d+[a-z]?) \\| ")
	backlogLinkedRE  = regexp.MustCompile("`([0-9a-f]{8})`")
	backlogTokenRE   = regexp.MustCompile(`\b[A-Z]{2}\d+\b`)
	backlogKindMark  = regexp.MustCompile(`\s*\{kind=([^{}|]*)\}`)
	backlogPhaseMark = regexp.MustCompile(`\s*\{phases=([^{}|]*)\}`)

	backlogOpenRE = regexp.MustCompile(`(?i)not started|not filed|draft|merge pending|running|approved|blocked|` +
		`queued|planning|failed|in progress|decomposed|needs design`)
	backlogPost10RE = regexp.MustCompile(`(?i)post-10\.0\.0|deferred|after 10\.0\.0|post 10\.0`)
	// backlogDoneRE holds the done phrases that need no boundary check; "done"
	// and "completed" are handled in statusIsDone.
	backlogDoneRE   = regexp.MustCompile(`(?i)\*\*done\*\*|shipped|superseded|rejected|closed|cancelled|implemented`)
	backlogDoneWord = compileAnchoredAlternatives("done")
)

const (
	backlogDoneRunes = 140
	backlogOpenRunes = 200
	completedWord    = "completed"
	mergePendingTail = ", merge pending"
)

// ParseBacklog reads the registry table rows from r. Lines that are not table
// rows (prose, header, separator, blank) and malformed rows are skipped.
func ParseBacklog(r io.Reader) []BacklogRow {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil
	}
	var rows []BacklogRow
	for _, line := range strings.Split(string(data), "\n") {
		if row, ok := parseBacklogLine(line); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

// LoadBacklog reads the registry table rows from the file at path.
func LoadBacklog(path string) ([]BacklogRow, error) {
	f, err := os.Open(path) // #nosec G304 -- path is the caller's registry file
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseBacklog(f), nil
}

// UnfiledRows returns the open rows that no record is linked to.
func UnfiledRows(rows []BacklogRow) []BacklogRow {
	var out []BacklogRow
	for _, r := range rows {
		if r.Open && len(r.PRDIDs) == 0 {
			out = append(out, r)
		}
	}
	return out
}

// RowKinds maps each linked record id to the kind of the row that lists it. When
// several rows list the same id the first one wins.
func RowKinds(rows []BacklogRow) map[string]string {
	out := map[string]string{}
	for _, r := range rows {
		for _, id := range r.PRDIDs {
			if _, seen := out[id]; !seen {
				out[id] = r.Kind
			}
		}
	}
	return out
}

func parseBacklogLine(line string) (BacklogRow, bool) {
	m := backlogRowRE.FindStringSubmatch(line)
	if m == nil {
		return BacklogRow{}, false
	}
	cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	if len(cells) < 3 {
		return BacklogRow{}, false
	}
	rawItem, status := cells[1], cells[2]
	joined := strings.Join(cells, " ")

	item, kind, phases := splitBacklogMarkers(rawItem)
	if kind == "" {
		kind = DeriveKind(item, "")
	}
	row := BacklogRow{
		Number: m[1],
		ID:     backlogTokenRE.FindString(item),
		Item:   item,
		Status: status,
		Kind:   kind,
		Phases: phases,
	}
	row.PRDIDs = linkedIDs(joined)
	row.Open = backlogRowOpen(status, joined)
	row.Unfiled = row.Open && len(row.PRDIDs) == 0
	return row, true
}

// splitBacklogMarkers strips the optional {kind=...} and {phases=N} markers from
// an Item cell. kind is empty when no valid kind marker is present; phases is
// never below 1.
func splitBacklogMarkers(item string) (clean, kind string, phases int) {
	phases = 1
	if m := backlogKindMark.FindStringSubmatch(item); m != nil {
		if k, ok := NormalizePRDKind(m[1]); ok {
			kind = k
		}
	}
	if m := backlogPhaseMark.FindStringSubmatch(item); m != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(m[1])); err == nil && n > phases {
			phases = n
		}
	}
	clean = backlogKindMark.ReplaceAllString(item, "")
	clean = backlogPhaseMark.ReplaceAllString(clean, "")
	return strings.TrimSpace(clean), kind, phases
}

// linkedIDs returns every backticked 8-hex id in s, de-duplicated, in order.
func linkedIDs(s string) []string {
	var ids []string
	seen := map[string]bool{}
	for _, m := range backlogLinkedRE.FindAllStringSubmatch(s, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			ids = append(ids, m[1])
		}
	}
	return ids
}

// backlogRowOpen applies the registry rules: a row is open when its status is
// not done (a status holding "merge pending" is never done), reads as open work,
// and nothing in the row defers it past the ten-zero release.
func backlogRowOpen(status, joined string) bool {
	if statusIsDone(firstRunes(status, backlogDoneRunes)) && !strings.Contains(status, "merge pending") {
		return false
	}
	if !backlogOpenRE.MatchString(firstRunes(status, backlogOpenRunes)) {
		return false
	}
	return !backlogPost10RE.MatchString(joined)
}

// statusIsDone reports whether s (already cut to its first characters) reads as
// finished work.
func statusIsDone(s string) bool {
	if backlogDoneRE.MatchString(s) || containsWholeWord(s, backlogDoneWord) {
		return true
	}
	// "completed" counts unless it is followed by ", merge pending".
	lower := strings.ToLower(s)
	for from := 0; ; {
		i := strings.Index(lower[from:], completedWord)
		if i < 0 {
			return false
		}
		rest := lower[from+i+len(completedWord):]
		if !strings.HasPrefix(rest, mergePendingTail) {
			return true
		}
		from += i + len(completedWord)
	}
}
