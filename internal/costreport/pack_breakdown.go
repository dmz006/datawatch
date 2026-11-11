package costreport

import (
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/trace"
)

// PackUnattributedName is the pack name a breakdown row carries for events
// that record no pack name. It is the same "unattributed" read as a name
// that the other groupings use (UnattributedKey).
const PackUnattributedName = UnattributedKey

// PackRow is one pack name + version slice of the cost report's pack
// breakdown: the trace events the pack produced and the tokens and cost
// they account for. Events with no pack name fall under a single row
// named PackUnattributedName with an empty Version.
type PackRow struct {
	Name      string  `json:"pack_name"`
	Version   string  `json:"pack_version"`
	Events    int     `json:"events"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	Cost      float64 `json:"cost"`
}

// PackBreakdown groups trace events by the pack name and pack version that
// produced them and sums each group's events, tokens and cost. Events with a
// blank (or whitespace-only) pack name are grouped under PackUnattributedName
// regardless of any version they carry, since a version without a pack name
// cannot be attributed to a pack.
//
// Rows are ordered by pack name, then version, with the unattributed row
// last, so the output is stable across calls.
func PackBreakdown(events []trace.Event) []PackRow {
	type packKey struct{ name, version string }
	index := map[packKey]*PackRow{}
	for _, ev := range events {
		key := packKey{name: strings.TrimSpace(ev.PackName), version: strings.TrimSpace(ev.PackVersion)}
		if key.name == "" {
			key = packKey{name: PackUnattributedName}
		}
		row, ok := index[key]
		if !ok {
			row = &PackRow{Name: key.name, Version: key.version}
			index[key] = row
		}
		row.Events++
		row.TokensIn += ev.TokensIn
		row.TokensOut += ev.TokensOut
		row.Cost += ev.Cost
	}

	rows := make([]PackRow, 0, len(index))
	for _, row := range index {
		rows = append(rows, *row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		aUn, bUn := a.Name == PackUnattributedName, b.Name == PackUnattributedName
		if aUn != bUn {
			return bUn
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Version < b.Version
	})
	return rows
}
