package supervisorloop

import (
	"errors"
	"testing"
	"time"
)

type fakeMemory struct {
	pinned  map[string]string
	lessons []string
	saves   int
	err     error
}

func newFakeMemory() *fakeMemory { return &fakeMemory{pinned: map[string]string{}} }

func (m *fakeMemory) LoadPinned(k string) (string, bool, error) {
	if m.err != nil {
		return "", false, m.err
	}
	v, ok := m.pinned[k]
	return v, ok, nil
}
func (m *fakeMemory) SavePinned(k, t string) error {
	if m.err != nil {
		return m.err
	}
	m.saves++
	m.pinned[k] = t
	return nil
}
func (m *fakeMemory) Recall(string) ([]string, error) { return m.lessons, m.err }

type stepClock struct{ t time.Time }

func (c *stepClock) Now() time.Time { c.t = c.t.Add(time.Minute); return c.t }

func newRecs() (*StateRecords, *fakeMemory) {
	m := newFakeMemory()
	return NewStateRecords(m, &stepClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}), m
}

func TestStateRecordSetGetIdempotent(t *testing.T) {
	r, m := newRecs()
	e := StateEntry{ID: "a", Section: SectionWork, Text: "ship", Owner: "w1", NextStep: "test"}
	if err := r.SetEntry("p", "sup", e, "bob", "start"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEntry("p", "sup", e, "bob", "again"); err != nil {
		t.Fatal(err)
	}
	if m.saves != 1 {
		t.Fatalf("saves=%d", m.saves)
	}
	rec, _ := r.Get("p", "sup")
	if len(rec.Entries) != 1 || len(rec.Entries[0].Edits) != 1 {
		t.Fatalf("%+v", rec)
	}
	ed := rec.Entries[0].Edits[0]
	if ed.Who != "bob" || ed.Why != "start" || ed.When.Minute() != 1 {
		t.Fatalf("%+v", ed)
	}
	e.Text = "ship it"
	if err := r.SetEntry("p", "sup", e, "amy", "reword"); err != nil {
		t.Fatal(err)
	}
	rec, _ = r.Get("p", "sup")
	if len(rec.Entries[0].Edits) != 2 || rec.Entries[0].Text != "ship it" {
		t.Fatalf("%+v", rec)
	}
}

func TestStateRecordChangeRemove(t *testing.T) {
	r, m := newRecs()
	_ = r.SetEntry("p", "sup", StateEntry{ID: "b", Section: SectionDecision, Text: "x"}, "u", "w")
	_ = r.SetEntry("p", "sup", StateEntry{ID: "a", Section: SectionCorrection, Text: "y"}, "u", "w")
	txt, own := "z", "me"
	if err := r.ChangeEntry("p", "sup", "b", EntryChange{Text: &txt, Owner: &own, NextStep: &txt}, "u", "fix"); err != nil {
		t.Fatal(err)
	}
	n := m.saves
	if err := r.ChangeEntry("p", "sup", "b", EntryChange{Text: &txt}, "u", "same"); err != nil || m.saves != n {
		t.Fatal("not idempotent")
	}
	rec, _ := r.Get("p", "sup")
	if rec.Entries[0].ID != "a" || rec.Entries[1].Text != "z" || len(rec.Entries[1].Edits) != 2 {
		t.Fatalf("%+v", rec)
	}
	if err := r.ChangeEntry("p", "sup", "nope", EntryChange{}, "u", "w"); !errors.Is(err, ErrEntryNotFound) {
		t.Fatal(err)
	}
	if err := r.RemoveEntry("p", "sup", "a", "u", "done"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveEntry("p", "sup", "a", "u", "done"); err != nil {
		t.Fatal(err)
	}
	rec, _ = r.Get("p", "sup")
	if len(rec.Entries) != 1 {
		t.Fatalf("%+v", rec)
	}
}

func TestStateRecordValidationAndErrors(t *testing.T) {
	r, m := newRecs()
	bad := []StateEntry{{Section: SectionWork}, {ID: "a", Section: "nope"}}
	for _, e := range bad {
		if err := r.SetEntry("p", "s", e, "u", "w"); !errors.Is(err, ErrBadEntry) {
			t.Fatal(err)
		}
	}
	ok := StateEntry{ID: "a", Section: SectionChecked}
	if r.SetEntry("p", "s", ok, "", "w") == nil || r.SetEntry("p", "s", ok, "u", "") == nil {
		t.Fatal("who/why required")
	}
	if r.ChangeEntry("p", "s", "a", EntryChange{}, "", "") == nil || r.RemoveEntry("p", "s", "a", "", "") == nil {
		t.Fatal("who/why required")
	}
	if _, err := r.Get("", "s"); !errors.Is(err, ErrBadEntry) {
		t.Fatal(err)
	}
	m.pinned[StateKey("p", "s")] = "{bad"
	if _, err := r.Get("p", "s"); err == nil {
		t.Fatal("want decode error")
	}
	m.err = errors.New("down")
	if _, err := r.Get("p", "s"); err == nil {
		t.Fatal("want store error")
	}
	if _, err := r.Lessons("p", "s"); err == nil {
		t.Fatal("want store error")
	}
	if r.SetEntry("p", "s", ok, "u", "w") == nil || r.ChangeEntry("p", "s", "a", EntryChange{}, "u", "w") == nil || r.RemoveEntry("p", "s", "a", "u", "w") == nil {
		t.Fatal("want store errors")
	}
}

func TestStateRecordLessons(t *testing.T) {
	r, m := newRecs()
	m.lessons = []string{" one ", "", "one", "two"}
	got, err := r.Lessons("p", "s")
	if err != nil || len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("%v %v", got, err)
	}
}
