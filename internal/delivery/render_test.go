package delivery

import (
	"strings"
	"testing"
	"time"
)

var renderAt = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

func renderEntry(kind Kind, id, state string) Entry {
	return Entry{
		Key: string(kind) + ":" + id + ":" + state, Kind: kind, ID: id, State: state,
		Invariant: InvOverdue, Reason: "over_limit", WaitsOn: WaitsOnOperator,
		Since: renderAt.Add(-48 * time.Hour), ReviewBy: renderAt.Add(-24 * time.Hour),
		Source: "automata",
	}
}

func renderSample() Report {
	accepted := renderEntry(KindAutomaton, "a3", "held")
	accepted.Accepted = &Accept{Reason: "parked", Until: renderAt.Add(72 * time.Hour), By: "operator", At: renderAt}
	return Report{
		ReportOnly: true, Result: ResultRed, Red: []string{RedItems, RedNoChannel},
		At: renderAt, Pass: 7,
		Items:     []Entry{renderEntry(KindAutomaton, "a1", "blocked"), renderEntry(KindTask, "t1", "failed")},
		Backlog:   []Entry{renderEntry(KindBug, "b1", "open")},
		Accepted:  []Entry{accepted},
		Guarded:   []string{"schedule:s1:pending"},
		Sources:   []SourceStatus{{Name: "automata", OK: true, Candidates: 3}, {Name: "schedules"}},
		Channels:  []string{},
		Heartbeat: HeartbeatCannotTell, NonOperatorActions: 2,
		Uncovered: []string{"other_hosts"},
	}
}

func TestRenderReportShowsResultListsAndCounts(t *testing.T) {
	out := RenderReport(renderSample(), 0)
	for _, want := range []string{
		"red", RedItems, RedNoChannel, "report only",
		"2026-11-26T12:00:00Z", "pass 7",
		"automaton:a1:blocked", "task:t1:failed", "over_limit", InvOverdue,
		"bug:b1:open", "automaton:a3:held", "parked", "2026-11-29T12:00:00Z",
		"schedule:s1:pending",
		"schedules", "channels: none", HeartbeatCannotTell, "other_hosts",
		"by a session or peer: 2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "New (2)") || !strings.Contains(out, "Backlog (1)") || !strings.Contains(out, "Accepted (1)") {
		t.Errorf("render lacks group counts:\n%s", out)
	}
}

func TestRenderReportLimitsEachGroup(t *testing.T) {
	r := Report{Result: ResultRed, At: renderAt}
	for _, id := range []string{"a1", "a2", "a3", "a4", "a5"} {
		r.Items = append(r.Items, renderEntry(KindAutomaton, id, "blocked"))
		r.Backlog = append(r.Backlog, renderEntry(KindBug, id, "open"))
	}
	out := RenderReport(r, 2)
	if got := strings.Count(out, "automaton:a"); got != 2 {
		t.Errorf("new lines = %d, want 2:\n%s", got, out)
	}
	if got := strings.Count(out, "bug:a"); got != 2 {
		t.Errorf("backlog lines = %d, want 2:\n%s", got, out)
	}
	if got := strings.Count(out, "and 3 more"); got != 2 {
		t.Errorf("'and 3 more' lines = %d, want 2:\n%s", got, out)
	}
	if got := strings.Count(RenderReport(r, 0), "automaton:a"); got != 5 {
		t.Errorf("no limit: %d lines, want 5", got)
	}
}

func TestRenderReportGreenAndNeverRan(t *testing.T) {
	out := RenderReport(Report{ReportOnly: true, Result: ResultGreen, At: renderAt, Pass: 1,
		Channels: []string{"signal"}, Heartbeat: HeartbeatOK}, 0)
	if !strings.Contains(out, "green") || !strings.Contains(out, "signal") || strings.Contains(out, "red") {
		t.Errorf("green render:\n%s", out)
	}
	out = RenderReport(Report{Result: ResultRed, Red: []string{RedNeverRan}}, 0)
	if !strings.Contains(out, RedNeverRan) || strings.Contains(out, "0001-01-01") {
		t.Errorf("never-ran render:\n%s", out)
	}
}

// The text holds IDs, kinds, states, reason codes and times only. Who did
// something, send results and other free text never reach it.
func TestRenderReportIsContentFree(t *testing.T) {
	const marker = "MARKERTEXT"
	e := renderEntry(KindAutomaton, "a1", "blocked")
	e.WaitsOn = marker
	e.Source = marker
	e.SeenBy = marker
	e.ClosedBy = marker
	e.Sends = []SendResult{{Channel: "signal", Code: marker}}
	acc := renderEntry(KindTask, "t1", "failed")
	acc.Accepted = &Accept{Reason: "parked", Until: renderAt, By: marker, At: renderAt}
	out := RenderReport(Report{Result: ResultRed, At: renderAt, Items: []Entry{e}, Accepted: []Entry{acc}}, 0)
	if strings.Contains(out, marker) {
		t.Errorf("render carries a field that is not an ID, kind, state, code or time:\n%s", out)
	}
}

// A value with a line break cannot forge a line of the report.
func TestRenderReportKeepsOneLinePerEntry(t *testing.T) {
	e := renderEntry(KindSchedule, "s1", "pending")
	e.Key = "schedule:s1\nResult: green:pending"
	out := RenderReport(Report{Result: ResultRed, At: renderAt, Items: []Entry{e}}, 0)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Result: green") {
			t.Errorf("a key forged a report line:\n%s", out)
		}
	}
}
