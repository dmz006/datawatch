package packs

import (
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/trace"
)

// TraceReader is the read side of the trace store that UsageByPack needs.
// *trace.Store satisfies it.
type TraceReader interface {
	ReadAll() ([]trace.Event, error)
}

// PackUsage is the usage of one installed pack name and version as recorded
// in the trace store: how many runs used it, the cost they accrued, and when
// it was last used.
type PackUsage struct {
	Name     string    `json:"pack_name"`
	Version  string    `json:"pack_version"`
	Runs     int       `json:"runs"`
	Cost     float64   `json:"cost"`
	LastUsed time.Time `json:"last_used"`
}

// UsageByPack reads every trace event from reader and groups the events that
// name a pack by pack name and pack version. Events with a blank pack name are
// skipped: a version without a name cannot be attributed to a pack.
//
// A run is one distinct session, task or automaton that produced an event for
// the pack. The run key is the first non-empty of SessionID, TaskID and
// AutomatonID, so events from one session count once no matter how many events
// it wrote. Events with none of those IDs add cost and last-used time but no run.
//
// Cost is the sum of the events' Cost. LastUsed is the latest event timestamp
// of the group. Rows are ordered by pack name, then version.
func UsageByPack(reader TraceReader) ([]PackUsage, error) {
	events, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	return usageFromEvents(events), nil
}

func usageFromEvents(events []trace.Event) []PackUsage {
	type packKey struct{ name, version string }
	type acc struct {
		row  PackUsage
		runs map[string]struct{}
	}
	index := map[packKey]*acc{}
	for _, ev := range events {
		name := strings.TrimSpace(ev.PackName)
		if name == "" {
			continue
		}
		key := packKey{name: name, version: strings.TrimSpace(ev.PackVersion)}
		a, ok := index[key]
		if !ok {
			a = &acc{row: PackUsage{Name: key.name, Version: key.version}, runs: map[string]struct{}{}}
			index[key] = a
		}
		if run := runKey(ev); run != "" {
			a.runs[run] = struct{}{}
		}
		a.row.Cost += ev.Cost
		if ev.Timestamp.After(a.row.LastUsed) {
			a.row.LastUsed = ev.Timestamp
		}
	}

	rows := make([]PackUsage, 0, len(index))
	for _, a := range index {
		a.row.Runs = len(a.runs)
		rows = append(rows, a.row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].Version < rows[j].Version
	})
	return rows
}

// runKey identifies the run an event belongs to: the session, else the task,
// else the automaton. It returns "" when the event carries none of them.
func runKey(ev trace.Event) string {
	for _, id := range []string{ev.SessionID, ev.TaskID, ev.AutomatonID} {
		if id = strings.TrimSpace(id); id != "" {
			return id
		}
	}
	return ""
}
