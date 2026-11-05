package supervisorloop

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func modelJob(style Style) Job {
	return Job{Name: "draft_spec", Kind: KindModel, Style: style, Backend: "b", Model: "m", Validator: okOnly}
}

func TestValidatorPassesFirstTry(t *testing.T) {
	c := &fakeClient{replies: []string{"good"}}
	e, esc, st := newEngine(c)
	out, err := e.Run(context.Background(), modelJob(StyleOneshot), "q")
	if err != nil || out != "good" {
		t.Fatal(out, err)
	}
	if len(esc.got) != 0 || st.recs[0].Outcome != OutcomeOK || st.recs[0].Attempts != 1 {
		t.Fatalf("%+v", st.recs[0])
	}
	if st.recs[0].Duration <= 0 {
		t.Fatal("duration")
	}
}

func TestReaskThenPass(t *testing.T) {
	c := &fakeClient{replies: []string{"bad", "good"}}
	e, esc, st := newEngine(c)
	e.Settings.EscalateAfter = 3
	out, err := e.Run(context.Background(), modelJob(StyleOneshot), "q")
	if err != nil || out != "good" || len(esc.got) != 0 {
		t.Fatal(out, err)
	}
	r := st.recs[0]
	if r.Attempts != 2 || r.Outcome != OutcomeOK || len(r.ValidatorErrors) != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestTwoFailuresEscalate(t *testing.T) {
	c := &fakeClient{replies: []string{"bad1", "bad2", "good"}}
	e, esc, st := newEngine(c)
	out, err := e.Run(context.Background(), modelJob(StyleOneshot), "q")
	if !errors.Is(err, ErrEscalated) || out != "" {
		t.Fatal(out, err)
	}
	if len(esc.got) != 1 || len(esc.got[0].Attempts) != 2 {
		t.Fatalf("%+v", esc.got)
	}
	a := esc.got[0].Attempts
	if a[0].Output != "bad1" || !strings.Contains(a[1].ValidatorError, "bad2") {
		t.Fatalf("%+v", a)
	}
	if !strings.Contains(esc.got[0].Summary(), "bad1") || st.recs[0].Outcome != OutcomeEscalated {
		t.Fatal("summary/outcome")
	}
	if len(c.replies) != 1 {
		t.Fatal("kept asking after escalation")
	}
}

func TestRetriesExhaustedWithoutEscalation(t *testing.T) {
	c := &fakeClient{replies: []string{"a", "b", "c"}}
	e, esc, st := newEngine(c)
	e.Settings.EscalateAfter = 10
	_, err := e.Run(context.Background(), modelJob(StyleOneshot), "q")
	if !errors.Is(err, ErrValidatorFailed) || len(esc.got) != 0 || st.recs[0].Outcome != OutcomeValidatorFailed || st.recs[0].Attempts != 3 {
		t.Fatal(err, st.recs[0])
	}
}

func TestStyleRouting(t *testing.T) {
	for _, s := range []Style{StyleSession, StyleOneshot} {
		c := &fakeClient{replies: []string{"good"}}
		e, _, st := newEngine(c)
		if _, err := e.Run(context.Background(), modelJob(s), "q"); err != nil {
			t.Fatal(err)
		}
		if len(c.reqs) != 1 || c.reqs[0].Style != s || c.reqs[0].Backend != "b" || c.reqs[0].Model != "m" || st.recs[0].Style != s {
			t.Fatalf("%v %+v", s, c.reqs)
		}
	}
}

func TestMechanicalAndErrors(t *testing.T) {
	e, _, st := newEngine(&fakeClient{})
	j := Job{Name: "m", Kind: KindMechanical, Exec: func(context.Context, string) (string, error) { return "good", nil }, Validator: okOnly}
	if out, err := e.Run(context.Background(), j, ""); err != nil || out != "good" {
		t.Fatal(out, err)
	}
	j.Exec = func(context.Context, string) (string, error) { return "bad", nil }
	if _, err := e.Run(context.Background(), j, ""); !errors.Is(err, ErrValidatorFailed) {
		t.Fatal(err)
	}
	j.Exec = nil
	if _, err := e.Run(context.Background(), j, ""); err == nil {
		t.Fatal("want error")
	}
	e.Client = nil
	if _, err := e.Run(context.Background(), modelJob(StyleOneshot), ""); err == nil {
		t.Fatal("want error")
	}
	if len(st.recs) != 4 {
		t.Fatal(len(st.recs))
	}
	if got := modelJob(StyleOneshot).WithSetting(JobSetting{Style: StyleSession, Model: "x"}); got.Style != StyleSession || got.Model != "x" {
		t.Fatal(got)
	}
}
