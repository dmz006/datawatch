package supervisorloop

import (
	"testing"
	"time"
)

func recordTexts(t *testing.T, c *Corrections, texts ...string) {
	t.Helper()
	for i, tx := range texts {
		corr := Correction{ID: string(rune('a' + i)), Text: tx, Class: ClassStandingRule}
		if err := c.Record("p", "r", corr, "op", "said so"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRepeatTwiceNoFinding(t *testing.T) {
	c, _, _, _ := newCorrections()
	recordTexts(t, c, "Use the dry run first", "use the dry run first!", "other")
	d := NewRepeatDetector(c, &stepClock{t: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)})
	got, err := d.Detect("p", "r")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestRepeatThreeRaisesFinding(t *testing.T) {
	c, _, _, _ := newCorrections()
	recordTexts(t, c, "Use the dry run first", "use  the dry-run FIRST.", "Use the dry run first!", "unrelated")
	clock := &stepClock{t: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}
	d := NewRepeatDetector(c, clock)
	got, err := d.Detect("p", "r")
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v %v", got, err)
	}
	f := got[0]
	if f.Kind != FindingRepeatCorrection || f.Count != 3 || f.Text != "use the dry run first" {
		t.Fatalf("finding %+v", f)
	}
	if want := []string{"a", "b", "c"}; len(f.IDs) != 3 || f.IDs[0] != want[0] || f.IDs[2] != want[2] {
		t.Fatalf("ids %v", f.IDs)
	}
	if f.At.IsZero() || !f.At.After(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("time not from clock: %v", f.At)
	}
}

func TestDetectRepeatsIgnoresBlank(t *testing.T) {
	list := []Correction{{ID: "1", Text: "  "}, {ID: "2", Text: "!!"}, {ID: "3", Text: ""}}
	if got := DetectRepeats(list, time.Time{}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestRepeatDetectorNeedsDeps(t *testing.T) {
	if _, err := NewRepeatDetector(nil, nil).Detect("p", "r"); err == nil {
		t.Fatal("expected error")
	}
}
