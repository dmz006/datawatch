package autonomous

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// fakeUnarchiveOps keeps the live state a real manager would hold: the
// candidate records, their current status, and whether the one-time
// completion is recorded. Restore moves a status; it is a no-op success
// when the record already sits at the restored status, and it refuses a
// record that moved to some other state meanwhile.
type fakeUnarchiveOps struct {
	cands     []UnarchiveCandidate
	status    map[string]PRDStatus
	restores  int
	remembers int
	done      bool
	// dropID/dropAfter simulate a record leaving the state between the
	// detect and the apply: it leaves the listing after the dropAfter-th
	// Candidates call.
	dropID    string
	dropAfter int
	listed    int
}

func (o *fakeUnarchiveOps) Done(context.Context) bool { return o.done }

func (o *fakeUnarchiveOps) Candidates(_ context.Context) []UnarchiveCandidate {
	o.listed++
	var out []UnarchiveCandidate
	for _, c := range o.cands {
		if c.ID == o.dropID && o.dropAfter > 0 && o.listed >= o.dropAfter {
			continue // the record left the state after the earlier listing
		}
		out = append(out, c)
	}
	return out
}

func (o *fakeUnarchiveOps) Restore(_ context.Context, c UnarchiveCandidate) error {
	cur, known := o.status[c.ID]
	if !known {
		cur = PRDArchived
	}
	if cur == c.RestoreTo {
		return nil // already restored: the repair stays a one-time idempotent move
	}
	if cur != PRDArchived {
		return fmt.Errorf("the record is at %s now, not archived; the repair changes nothing", cur)
	}
	o.restores++
	o.status[c.ID] = c.RestoreTo
	return nil
}

func (o *fakeUnarchiveOps) Remember(context.Context) error {
	o.remembers++
	o.done = true
	return nil
}

var _ UnarchiveRepairOps = (*fakeUnarchiveOps)(nil)

type unarchiveHarness struct {
	r      *Repairer
	logs   []string
	alerts []string
	ops    *fakeUnarchiveOps
}

func newUnarchiveHarness(mode string, cands []UnarchiveCandidate) *unarchiveHarness {
	h := &unarchiveHarness{ops: &fakeUnarchiveOps{cands: cands, status: map[string]PRDStatus{}}}
	for _, c := range cands {
		h.ops.status[c.ID] = PRDArchived
	}
	zero := 0
	set := config.HousekeepingRepairsConfig{Unarchive: mode, GraceSeconds: &zero}
	h.r = NewRepairer(RepairerHooks{
		Settings: func() config.HousekeepingRepairsConfig { return set },
		Log:      func(line string) { h.logs = append(h.logs, line) },
		Alert:    func(level, title, body string) { h.alerts = append(h.alerts, title+" "+body) },
	}, NewUnarchiveRepairs(h.ops)...)
	return h
}

func unarchiveCands() []UnarchiveCandidate {
	return []UnarchiveCandidate{
		{ID: "p1", Title: "The completed one", Branch: "work-one", Commits: 3, RestoreTo: PRDCompleted},
		{ID: "p2", Title: "The cancelled one", Branch: "work-two", Commits: 5, RestoreTo: PRDCancelled},
		{ID: "p3", Title: "The unverifiable one", Branch: "work-three", Unsafe: true, UnsafeReason: "the merge state of branch work-three could not be determined (git failed)"},
	}
}

// TestHousekeepingRepairUnarchiveUnmerged is the main pass: every archived
// record with unmerged work is restored to its pre-archive status and
// reported with title and commit count, the unverifiable one is raised as
// unsafe with one alert, every entry carries the undo hint, the decisions
// land in the log, and the one-time completion is recorded exactly once so
// a second run is a no-op.
func TestHousekeepingRepairUnarchiveUnmerged(t *testing.T) {
	h := newUnarchiveHarness(config.HousekeepingModeOn, unarchiveCands())
	recs := h.r.Run(context.Background())

	if len(recs) != 3 {
		t.Fatalf("the pass recorded %d entries, want 3", len(recs))
	}
	did, unsafe := 0, 0
	for _, rec := range recs {
		if rec.Group != RepairGroupUnarchive {
			t.Fatalf("entry for group %q, want %q", rec.Group, RepairGroupUnarchive)
		}
		if rec.Undo == "" {
			t.Fatal("the entry carries no undo hint")
		}
		switch rec.Kind {
		case RepairDid:
			did++
			if !strings.Contains(rec.Done, "restored") {
				t.Fatalf("the repair says %q; it does not say what it restored", rec.Done)
			}
			if !strings.Contains(rec.Done, "no session was killed, ended or signaled at all") {
				t.Fatalf("the repair report %q must say no session was touched", rec.Done)
			}
		case RepairSkippedUnsafe:
			unsafe++
			if !strings.Contains(rec.Done, "git failed") {
				t.Fatalf("the skipped entry %q does not carry the reason", rec.Done)
			}
		default:
			t.Fatalf("unexpected kind %q in on mode", rec.Kind)
		}
	}
	if did != 2 || unsafe != 1 {
		t.Fatalf("did %d, unsafe %d; want 2 and 1", did, unsafe)
	}
	if h.ops.restores != 2 {
		t.Fatalf("the harness restored %d records, want 2", h.ops.restores)
	}
	if h.ops.status["p1"] != PRDCompleted || h.ops.status["p2"] != PRDCancelled {
		t.Fatalf("restored to %q / %q; want completed / cancelled", h.ops.status["p1"], h.ops.status["p2"])
	}
	if len(h.alerts) != 1 || !strings.Contains(h.alerts[0], "p3") {
		t.Fatalf("the pass raised alerts (one expected); the one-time repair is done: %v", h.alerts)
	}
	if len(h.logs) != 3 {
		t.Fatalf("the pass wrote %d decision-log lines, want 3", len(h.logs))
	}
	for _, line := range h.logs {
		if !strings.Contains(line, "housekeeping repair") || !strings.Contains(line, RepairGroupUnarchive) ||
			!strings.Contains(line, "found:") || !strings.Contains(line, "undo:") {
			t.Fatalf("decision-log line %q is missing its group, founding or undo", line)
		}
	}
	if !h.ops.done || h.ops.remembers != 1 {
		t.Fatalf("completion recorded %d times, want exactly once", h.ops.remembers)
	}

	// It ran once. A second pass (restart, mode change, idle tick) touches
	// nothing.
	recs2 := h.r.Run(context.Background())
	if len(recs2) != 0 {
		t.Fatalf("the second pass recorded %d entries; the one-time repair must not re-run", len(recs2))
	}
	if h.ops.restores != 2 || h.ops.remembers != 1 {
		t.Fatalf("the second pass touched the records (restores=%d, remembers=%d)", h.ops.restores, h.ops.remembers)
	}
	if len(h.alerts) != 1 {
		t.Fatalf("the second pass raised alerts; the one-time repair is done: %v", h.alerts)
	}
}

// TestHousekeepingRepairUnarchiveUnmergedReport proves report mode records
// what would be restored and changes nothing: no restore, no completion
// marker, one unsafe alert only.
func TestHousekeepingRepairUnarchiveUnmergedReport(t *testing.T) {
	h := newUnarchiveHarness(config.HousekeepingModeReport, unarchiveCands())
	recs := h.r.Run(context.Background())
	if len(recs) != 3 {
		t.Fatalf("the report pass recorded %d entries, want 3", len(recs))
	}
	would := 0
	for _, rec := range recs {
		if rec.Kind == RepairSkippedUnsafe {
			continue
		}
		if rec.Kind != RepairWould || !strings.Contains(rec.Done, "nothing changed") {
			t.Fatalf("report mode recorded %q %q; want would-restore with nothing changed", rec.Kind, rec.Done)
		}
		would++
	}
	if would != 2 {
		t.Fatalf("report mode recorded %d would entries, want 2", would)
	}
	if h.ops.restores != 0 || h.ops.done {
		t.Fatalf("report mode restored %d records and done=%v; it must act on nothing", h.ops.restores, h.ops.done)
	}
	if len(h.alerts) != 1 {
		t.Fatalf("the report pass raised %d alerts, want one", len(h.alerts))
	}
}

// TestHousekeepingRepairUnarchiveUnmergedOff proves off mode does nothing
// at all: no findings, no restore, no marker, no alert.
func TestHousekeepingRepairUnarchiveUnmergedOff(t *testing.T) {
	h := newUnarchiveHarness(config.HousekeepingModeOff, unarchiveCands())
	recs := h.r.Run(context.Background())
	if len(recs) != 0 {
		t.Fatalf("off mode recorded %d entries", len(recs))
	}
	if h.ops.restores != 0 || h.ops.done || len(h.alerts) != 0 {
		t.Fatalf("off mode restored %d, done=%v, alerts=%d; want none of it", h.ops.restores, h.ops.done, len(h.alerts))
	}
}

// TestHousekeepingRepairUnarchiveUnmergedGoneMidPass covers the race the
// one-time nature guards: the record leaves the state (a person restores it)
// after the detect but before the apply. The apply is a clean no-op, and
// the pass still records its completion.
func TestHousekeepingRepairUnarchiveUnmergedGoneMidPass(t *testing.T) {
	h := newUnarchiveHarness(config.HousekeepingModeOn, []UnarchiveCandidate{
		{ID: "p1", Title: "Restored meanwhile", Branch: "work-one", Commits: 3, RestoreTo: PRDCompleted},
	})
	h.ops.dropID = "p1"
	h.ops.dropAfter = 2 // still listed at the detect, gone at the apply
	recs := h.r.Run(context.Background())
	if len(recs) != 1 {
		t.Fatalf("the pass recorded %d entries, want 1", len(recs))
	}
	if recs[0].Kind != RepairDid {
		t.Fatalf("kind %q for a record restored meanwhile; want did with nothing changed", recs[0].Kind)
	}
	if !strings.Contains(recs[0].Done, "nothing was changed") {
		t.Fatalf("the report %q does not say nothing was changed", recs[0].Done)
	}
	if h.ops.restores != 0 {
		t.Fatalf("the pass restored %d records though none was still archived", h.ops.restores)
	}
	if !h.ops.done {
		t.Fatal("the pass must record its completion even when everything was already gone")
	}
}

// TestUnarchiveRestoreDecision fixes what the decision log means for the
// pre-archive terminal status, and which records the repair never touches.
func TestUnarchiveRestoreDecision(t *testing.T) {
	d := func(kind, actor string) Decision { return Decision{Kind: kind, Actor: actor} }
	cases := []struct {
		name     string
		ds       []Decision
		want     PRDStatus
		wantOK   bool
		wantPart string // substring of the reason when !wantOK
	}{
		{"no archive decision", []Decision{d("run", "autonomous")}, "", false, "manual archive"},
		{"archived by operator", []Decision{d("run", "autonomous"), d("archive", "operator")}, "", false, "operator-abandoned"},
		{"cancelled by operator", []Decision{d("cancel", "operator"), d("archive", "supervisor-loop")}, "", false, "operator-abandoned"},
		{"cancelled automatically", []Decision{d("cancel", "supervisor-loop"), d("archive", "supervisor-loop")}, PRDCancelled, true, ""},
		{"rejected before archive", []Decision{d("reject", "autonomous"), d("archive", "supervisor-loop")}, "", false, "rejected"},
		{"work after the cancel", []Decision{
			d("cancel", "supervisor-loop"), d("decompose", "autonomous"), d("verify", "autonomous"), d("archive", "supervisor-loop"),
		}, PRDCompleted, true, ""},
		{"finished completed", []Decision{
			d("decompose", "autonomous"), d("run", "autonomous"), d("verify", "autonomous"), d("archive", "supervisor-loop"),
		}, PRDCompleted, true, ""},
		{"reset to draft then finished", []Decision{
			d("cancel", "supervisor-loop"), d("reset_to_draft", "autonomous"), d("run", "autonomous"), d("archive", "supervisor-loop"),
		}, PRDCompleted, true, ""},
	}
	for _, tc := range cases {
		got, ok, reason := unarchiveRestoreDecision(tc.ds)
		if tc.wantOK != ok {
			t.Fatalf("%s: ok=%v, want %v, reason %q", tc.name, ok, tc.wantOK, reason)
		}
		if tc.wantOK {
			if got != tc.want {
				t.Fatalf("%s: wants %q, got %q", tc.name, tc.want, got)
			}
			continue
		}
		if !strings.Contains(reason, tc.wantPart) {
			t.Fatalf("%s: reason %q does not say %q", tc.name, reason, tc.wantPart)
		}
	}
}

// TestUnarchiveRestoreIdempotent proves the restore move itself is safe to
// repeat: a record already at its restored status is a no-op success, and a
// record that moved to some other status is refused.
func TestUnarchiveRestoreIdempotent(t *testing.T) {
	ctx := context.Background()
	c := UnarchiveCandidate{ID: "p1", Title: "t", Branch: "work", Commits: 2, RestoreTo: PRDCompleted}

	already := &fakeUnarchiveOps{status: map[string]PRDStatus{"p1": PRDCompleted}}
	if err := already.Restore(ctx, c); err != nil {
		t.Fatalf("a record already restored refused the no-op: %v", err)
	}

	moved := &fakeUnarchiveOps{status: map[string]PRDStatus{"p1": PRDPlanning}}
	if err := moved.Restore(ctx, c); err == nil {
		t.Fatal("the restore moved a record that is no longer archived; it must refuse")
	}
}
