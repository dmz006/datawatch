package supervisorloop

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type fakeCtxReader struct {
	size int
	err  error
}

func (f *fakeCtxReader) ContextSize(context.Context, string) (int, error) { return f.size, f.err }

type fakeRestartSessions struct {
	started    []string
	ended      []string
	startN     int
	startErr   error
	confirmErr error
	endErr     error
	models     []string
	handovers  []string
	log        []string
}

func (f *fakeRestartSessions) Start(_ context.Context, _, model, handover string) (string, error) {
	if f.startErr != nil {
		return "", f.startErr
	}
	f.startN++
	id := fmt.Sprintf("new-%d", f.startN)
	f.started = append(f.started, id)
	f.models = append(f.models, model)
	f.handovers = append(f.handovers, handover)
	f.log = append(f.log, "start:"+id)
	return id, nil
}
func (f *fakeRestartSessions) Confirm(_ context.Context, id string) error {
	f.log = append(f.log, "confirm:"+id)
	return f.confirmErr
}
func (f *fakeRestartSessions) End(_ context.Context, id string) error {
	if f.endErr != nil {
		return f.endErr
	}
	f.ended = append(f.ended, id)
	f.log = append(f.log, "end:"+id)
	return nil
}

type fakeHandoverText struct{ err error }

func (f fakeHandoverText) Handover(p string) (string, error) { return "handover for " + p, f.err }

type fakeRestartStore struct {
	rec     *RestartRecord
	saveErr error
}

func (s *fakeRestartStore) Load(string) (*RestartRecord, error) {
	if s.rec == nil {
		return nil, nil
	}
	c := *s.rec
	return &c, nil
}
func (s *fakeRestartStore) Save(r RestartRecord) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.rec = &r
	return nil
}
func (s *fakeRestartStore) Clear(string) error { s.rec = nil; return nil }

type restartClock struct{ t time.Time }

func (c *restartClock) Now() time.Time { return c.t }

var restartEpoch = time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)

func newRestarter() (*Restarter, *fakeCtxReader, *fakeRestartSessions, *fakeRestartStore, *restartClock) {
	rd, ss, st, ck := &fakeCtxReader{size: 100}, &fakeRestartSessions{}, &fakeRestartStore{}, &restartClock{t: restartEpoch}
	return &Restarter{Settings: DefaultSupervisorSettings(), Reader: rd, Sessions: ss, Handover: fakeHandoverText{}, Store: st, Clock: ck}, rd, ss, st, ck
}

func cur() SupervisorSession { return SupervisorSession{ID: "old", StartedAt: restartEpoch} }

func TestRestartNotDue(t *testing.T) {
	r, _, ss, _, ck := newRestarter()
	ck.t = restartEpoch.Add(time.Hour)
	res, err := r.Check(context.Background(), "p", cur())
	if err != nil || res.Outcome != RestartNone || res.SessionID != "old" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(ss.log) != 0 {
		t.Fatalf("unexpected calls %v", ss.log)
	}
}

func TestRestartBySizeOrdersSteps(t *testing.T) {
	r, rd, ss, st, _ := newRestarter()
	rd.size = r.Settings.ContextLimit + 1
	res, err := r.Check(context.Background(), "p", cur())
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != RestartDone || res.Reason != ReasonContextSize || res.SessionID != "new-1" || res.Resumed {
		t.Fatalf("res=%+v", res)
	}
	want := []string{"start:new-1", "confirm:new-1", "end:old"}
	if fmt.Sprint(ss.log) != fmt.Sprint(want) {
		t.Fatalf("order %v want %v", ss.log, want)
	}
	if ss.models[0] != r.Settings.Model || ss.handovers[0] != "handover for p" {
		t.Fatalf("model/handover %v %v", ss.models, ss.handovers)
	}
	if st.rec != nil {
		t.Fatal("record not cleared")
	}
}

func TestRestartByAgeUsesInjectedClock(t *testing.T) {
	r, _, ss, _, ck := newRestarter()
	ck.t = restartEpoch.Add(r.Settings.ContextAgeLimit) // exactly at limit: not due
	if res, _ := r.Check(context.Background(), "p", cur()); res.Outcome != RestartNone {
		t.Fatalf("at limit: %+v", res)
	}
	ck.t = restartEpoch.Add(r.Settings.ContextAgeLimit + time.Second)
	res, err := r.Check(context.Background(), "p", cur())
	if err != nil || res.Outcome != RestartDone || res.Reason != ReasonContextAge {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(ss.ended) != 1 || ss.ended[0] != "old" {
		t.Fatalf("ended %v", ss.ended)
	}
}

func TestRestartConfirmFailureKeepsOld(t *testing.T) {
	r, rd, ss, st, _ := newRestarter()
	rd.size = r.Settings.ContextLimit + 1
	ss.confirmErr = errors.New("silent")
	_, err := r.Check(context.Background(), "p", cur())
	if !errors.Is(err, ErrRestartNotConfirmed) {
		t.Fatalf("err=%v", err)
	}
	if len(ss.ended) != 1 || ss.ended[0] != "new-1" {
		t.Fatalf("old must stay, new must end: %v", ss.ended)
	}
	if st.rec != nil {
		t.Fatal("record should be cleared")
	}
	// Next check starts afresh.
	ss.confirmErr = nil
	res, err := r.Check(context.Background(), "p", cur())
	if err != nil || res.SessionID != "new-2" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestRestartResumeAfterDeathBeforeConfirm(t *testing.T) {
	r, _, ss, st, _ := newRestarter()
	st.rec = &RestartRecord{Project: "p", OldID: "old", NewID: "new-9", Step: RestartStarted, Reason: ReasonContextSize}
	// Context now small: a recorded restart still finishes.
	res, err := r.Check(context.Background(), "p", cur())
	if err != nil || res.Outcome != RestartDone || !res.Resumed || res.SessionID != "new-9" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if ss.startN != 0 {
		t.Fatal("must not start another session")
	}
	want := []string{"confirm:new-9", "end:old"}
	if fmt.Sprint(ss.log) != fmt.Sprint(want) {
		t.Fatalf("order %v", ss.log)
	}
}

func TestRestartResumeAfterConfirmSkipsConfirm(t *testing.T) {
	r, _, ss, st, _ := newRestarter()
	st.rec = &RestartRecord{Project: "p", OldID: "old", NewID: "new-9", Step: RestartConfirmed, Reason: ReasonContextAge}
	ss.confirmErr = errors.New("would fail")
	res, err := r.Check(context.Background(), "p", cur())
	if err != nil || res.SessionID != "new-9" || res.Reason != ReasonContextAge {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if fmt.Sprint(ss.log) != "[end:old]" {
		t.Fatalf("log %v", ss.log)
	}
}

func TestRestartEndOldFailsThenRepeatIsSafe(t *testing.T) {
	r, rd, ss, st, _ := newRestarter()
	rd.size = r.Settings.ContextLimit + 1
	ss.endErr = errors.New("boom")
	if _, err := r.Check(context.Background(), "p", cur()); err == nil {
		t.Fatal("want error")
	}
	if st.rec == nil || st.rec.Step != RestartConfirmed {
		t.Fatalf("record %+v", st.rec)
	}
	ss.endErr = nil
	res, err := r.Check(context.Background(), "p", cur())
	if err != nil || !res.Resumed || res.SessionID != "new-1" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if ss.startN != 1 {
		t.Fatalf("started %d sessions", ss.startN)
	}
	if len(ss.ended) != 1 || ss.ended[0] != "old" {
		t.Fatalf("ended %v", ss.ended)
	}
}

func TestRestartStartAndReaderErrors(t *testing.T) {
	r, rd, ss, st, _ := newRestarter()
	rd.err = errors.New("no read")
	if _, err := r.Check(context.Background(), "p", cur()); err == nil {
		t.Fatal("reader error expected")
	}
	rd.err = nil
	rd.size = r.Settings.ContextLimit + 1
	ss.startErr = errors.New("no start")
	if _, err := r.Check(context.Background(), "p", cur()); err == nil || st.rec != nil {
		t.Fatalf("start error: %v rec=%v", err, st.rec)
	}
	ss.startErr = nil
	st.saveErr = errors.New("disk")
	if _, err := r.Check(context.Background(), "p", cur()); err == nil {
		t.Fatal("save error expected")
	}
	if len(ss.ended) != 1 || ss.ended[0] != "new-1" {
		t.Fatalf("orphan new session must end: %v", ss.ended)
	}
}

func TestRestartNeedsWiringAndValidSettings(t *testing.T) {
	r := &Restarter{}
	if _, err := r.Check(context.Background(), "p", cur()); err == nil {
		t.Fatal("want wiring error")
	}
	r, _, _, _, _ = newRestarter()
	r.Settings.Model = "claude-opus-x"
	if _, err := r.Check(context.Background(), "p", cur()); err == nil {
		t.Fatal("opus must be refused")
	}
}
