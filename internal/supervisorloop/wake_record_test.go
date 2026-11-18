package supervisorloop

import (
	"testing"
	"time"
)

type recordClock struct{ t time.Time }

func (c *recordClock) Now() time.Time { return c.t }

func TestWakeRecordCounts(t *testing.T) {
	c := &recordClock{t: time.Unix(1000, 0)}
	r := NewWakeRecord(c, time.Minute)
	r.MarkEvent()
	c.t = c.t.Add(10 * time.Second)
	r.MarkScheduledCheck()
	r.MarkScheduledCheck()
	s := r.Status()
	if s.EventCount != 1 || s.ScheduledCount != 2 {
		t.Fatalf("counts = %d/%d", s.EventCount, s.ScheduledCount)
	}
	if !s.LastScheduledCheck.Equal(c.t) || s.LastEvent.Equal(c.t) {
		t.Fatalf("times wrong: %+v", s)
	}
	if s.EventsSilentFor != "" {
		t.Fatalf("unexpected silence %q", s.EventsSilentFor)
	}
}

func TestWakeRecordSilentEventPath(t *testing.T) {
	c := &recordClock{t: time.Unix(1000, 0)}
	r := NewWakeRecord(c, time.Minute)
	r.MarkEvent()
	c.t = c.t.Add(5 * time.Minute)
	if got := r.Status().EventsSilentFor; got != "5m0s" {
		t.Fatalf("silent for = %q", got)
	}
	r.MarkEvent()
	if got := r.Status().EventsSilentFor; got != "" {
		t.Fatalf("silence should clear, got %q", got)
	}
}

func TestWakeRecordNoEventYet(t *testing.T) {
	c := &recordClock{t: time.Unix(1000, 0)}
	r := NewWakeRecord(c, 0)
	if r.Status().EventsSilentFor != "" {
		t.Fatal("fresh record must not be silent")
	}
	r.MarkScheduledCheck()
	c.t = c.t.Add(2 * time.Hour)
	if r.Status().EventsSilentFor == "" {
		t.Fatal("checks without events should report silence")
	}
}

func TestWakeRecordEngineRunMarksCheck(t *testing.T) {
	e, _, _ := newEngine(&fakeClient{replies: []string{"good"}})
	e.Wakes = NewWakeRecord(&recordClock{t: time.Unix(5, 0)}, 0)
	_, _ = e.Run(t.Context(), Job{Name: "j", Kind: KindModel, Validator: okOnly}, "in")
	if e.Wakes.Status().ScheduledCount != 1 {
		t.Fatal("Run did not mark a scheduled check")
	}
}
