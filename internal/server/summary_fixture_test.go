package server

import "github.com/dmz006/datawatch/internal/autonomous"

// summaryOf derives the summary-index entries of fixture records.
func summaryOf(prds []*autonomous.PRD) []autonomous.SummaryEntry {
	out := make([]autonomous.SummaryEntry, 0, len(prds))
	for _, p := range prds {
		if p != nil {
			out = append(out, autonomous.SummaryEntryOf(p))
		}
	}
	return out
}

// loaderOf returns the full-record lookup over fixture records.
func loaderOf(prds []*autonomous.PRD) func(string) *autonomous.PRD {
	return func(id string) *autonomous.PRD {
		for _, p := range prds {
			if p != nil && p.ID == id {
				return p
			}
		}
		return nil
	}
}
