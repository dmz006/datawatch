package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

type fakeIntakeStore struct {
	open    []OpenBatch
	created []QueueDefect
	added   map[string][]QueueDefect
	tiny    []QueueDefect
	failAdd error
}

func (f *fakeIntakeStore) ListOpenBatches(string) ([]OpenBatch, error) { return f.open, nil }
func (f *fakeIntakeStore) CreateBatch(d QueueDefect) (string, error) {
	f.created = append(f.created, d)
	return "batch-new-" + d.ID, nil
}
func (f *fakeIntakeStore) AddToBatch(id string, d QueueDefect) error {
	if f.failAdd != nil {
		return f.failAdd
	}
	if f.added == nil {
		f.added = map[string][]QueueDefect{}
	}
	f.added[id] = append(f.added[id], d)
	return nil
}
func (f *fakeIntakeStore) CreateTinyFix(d QueueDefect, _ TinyFixChange) (string, error) {
	f.tiny = append(f.tiny, d)
	return "tiny-" + d.ID, nil
}

type fakeIntakeLog struct{ recs []IntakeRecord }

func (l *fakeIntakeLog) Append(r IntakeRecord) error { l.recs = append(l.recs, r); return nil }

func intakeFixture(open ...OpenBatch) (IntakeState, *fakeIntakeStore, *fakeIntakeLog) {
	st := &fakeIntakeStore{open: open}
	lg := &fakeIntakeLog{}
	return IntakeState{Store: st, Log: lg, Now: func() time.Time { return time.Unix(1700000000, 0) }}, st, lg
}

func codeDefect(id, area string) IntakeDefect {
	return IntakeDefect{ID: id, Title: "t " + id, Project: "p", Area: area, Kind: "bug", Files: []string{"internal/x/y.go"}}
}

func TestDefectIntake_AddsToOpenBatch(t *testing.T) {
	state, st, _ := intakeFixture(OpenBatch{ID: "b1", Project: "p", Area: "ui", Status: BatchStatusDraft, Size: 3})
	dec, err := Intake(codeDefect("d1", "ui"), state, IntakeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Route != IntakeRouteOpenBatch || dec.BatchID != "b1" {
		t.Fatalf("got %+v", dec)
	}
	if !strings.Contains(dec.Reason, "3/5") || !strings.Contains(dec.Reason, "ui") {
		t.Errorf("reason %q", dec.Reason)
	}
	if len(st.added["b1"]) != 1 {
		t.Error("defect not added")
	}
}

func TestDefectIntake_StartsNewBatchWhenFull(t *testing.T) {
	state, st, _ := intakeFixture(
		OpenBatch{ID: "b1", Project: "p", Area: "ui", Status: BatchStatusDraft, Size: 5},
		OpenBatch{ID: "b2", Project: "p", Area: "ui", Status: "running", Size: 1},
	)
	dec, _ := Intake(codeDefect("d1", "ui"), state, IntakeConfig{})
	if dec.Route != IntakeRouteNewBatch || dec.BatchID != "batch-new-d1" {
		t.Fatalf("got %+v", dec)
	}
	if !strings.Contains(dec.Reason, "full") || len(st.created) != 1 {
		t.Errorf("reason %q created %d", dec.Reason, len(st.created))
	}
}

func TestDefectIntake_StartsNewBatchWhenAreaDiffers(t *testing.T) {
	state, _, _ := intakeFixture(OpenBatch{ID: "b1", Project: "p", Area: "api", Status: BatchStatusDraft, Size: 1})
	dec, _ := Intake(codeDefect("d1", "ui"), state, IntakeConfig{})
	if dec.Route != IntakeRouteNewBatch || !strings.Contains(dec.Reason, `no open batch for area "ui"`) {
		t.Fatalf("got %+v", dec)
	}
}

func TestDefectIntake_RoutesTinyFix(t *testing.T) {
	state, st, _ := intakeFixture(OpenBatch{ID: "b1", Project: "p", Area: "ui", Status: BatchStatusDraft, Size: 1})
	d := IntakeDefect{ID: "d1", Project: "p", Area: "ui", Kind: config.DefectKindTestOnly,
		Files: []string{"a_test.go", "b_test.go"}}
	dec, err := Intake(d, state, IntakeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Route != IntakeRouteTinyFix || dec.BatchID != "tiny-d1" || len(st.tiny) != 1 {
		t.Fatalf("got %+v", dec)
	}
	if dec.Reason == "" || len(st.added) != 0 {
		t.Errorf("reason %q added %v", dec.Reason, st.added)
	}
}

func TestDefectIntake_TinyFixRefusalFallsBack(t *testing.T) {
	state, st, lg := intakeFixture(OpenBatch{ID: "b1", Project: "p", Area: "ui", Status: BatchStatusDraft, Size: 2})
	cfg := IntakeConfig{Recheck: func([]string, config.DefectQueueConfig, string) TinyFixRefusal {
		return TinyFixRefusal{Refused: true, Reason: "not a tiny fix: operator-visible path(s) changed: x"}
	}}
	d := IntakeDefect{ID: "d1", Project: "p", Area: "ui", Kind: config.DefectKindTestOnly, Files: []string{"a_test.go"}}
	dec, _ := Intake(d, state, cfg)
	if dec.Route != IntakeRouteOpenBatch || dec.TinyFixRefusal == "" || len(st.tiny) != 0 {
		t.Fatalf("got %+v", dec)
	}
	if !strings.Contains(dec.Reason, "tiny-fix refused, sent to the normal path") {
		t.Errorf("reason %q", dec.Reason)
	}
	r := lg.recs[0]
	if r.Route != IntakeRouteOpenBatch || r.TinyFixRefusal == "" {
		t.Errorf("log record %+v", r)
	}
}

func TestDefectIntake_LogsEveryDecision(t *testing.T) {
	state, _, lg := intakeFixture()
	_, _ = Intake(codeDefect("d1", "ui"), state, IntakeConfig{})
	_, _ = Intake(IntakeDefect{ID: "d2", Project: "p", Kind: config.DefectKindTestOnly, Files: []string{"a_test.go"}}, state, IntakeConfig{})
	if len(lg.recs) != 2 {
		t.Fatalf("records %d", len(lg.recs))
	}
	if lg.recs[0].DefectID != "d1" || lg.recs[0].Route != IntakeRouteNewBatch || lg.recs[0].BatchID != "batch-new-d1" ||
		lg.recs[0].Reason == "" || lg.recs[0].At.IsZero() {
		t.Errorf("rec0 %+v", lg.recs[0])
	}
	if lg.recs[1].Route != IntakeRouteTinyFix {
		t.Errorf("rec1 %+v", lg.recs[1])
	}

	// A failed apply is still logged with the error.
	state2, st2, lg2 := intakeFixture(OpenBatch{ID: "b1", Project: "p", Area: "ui", Status: BatchStatusDraft, Size: 1})
	st2.failAdd = errors.New("boom")
	if _, err := Intake(codeDefect("d3", "ui"), state2, IntakeConfig{}); err == nil {
		t.Error("want apply error")
	}
	if len(lg2.recs) != 1 || lg2.recs[0].ApplyError != "boom" {
		t.Errorf("recs %+v", lg2.recs)
	}

	// File log appends JSONL.
	p := filepath.Join(t.TempDir(), "sub", "log.jsonl")
	fl := NewFileIntakeLog(p)
	for i := 0; i < 2; i++ {
		if err := fl.Append(IntakeRecord{DefectID: "x", Route: IntakeRouteNewBatch}); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(p)
	if n := strings.Count(string(b), "\n"); n != 2 {
		t.Errorf("lines %d", n)
	}
}
