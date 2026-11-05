package supervisorloop

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const goodSpec = `{"stories":[{"name":"Add parser","tasks":[{"name":"Write parser","spec":"do it","files":["a.go"]}]}]}`

func modelJobs(c *fakeClient, backend, model string, style Style) (*ModelJobs, *fakeEscalator) {
	e, esc, _ := newEngine(c)
	for _, n := range ModelJobNames {
		e.Settings.Jobs[n] = JobSetting{Style: style, Backend: backend, Model: model}
	}
	return &ModelJobs{Engine: e}, esc
}

func TestSupervisorLoopModelPromptContents(t *testing.T) {
	c := &fakeClient{replies: []string{goodSpec, "infra", "pi"}}
	m, _ := modelJobs(c, "ollama", "qwen", StyleOneshot)
	ctx := context.Background()
	if _, err := m.DraftSpec(ctx, "ship the parser\nplease"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ClassifyCIFailure(ctx, "runner lost connection"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PickBackend(ctx, "mechanical", []string{"pi", "claude"}); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"> ship the parser\n> please", "runner lost connection", "pi, claude"} {
		if !strings.Contains(c.reqs[i].Prompt, want) {
			t.Errorf("prompt %d missing %q: %s", i, want, c.reqs[i].Prompt)
		}
	}
	if !strings.Contains(c.reqs[1].Prompt, "flaky_test") {
		t.Error("class list missing")
	}
	if c.reqs[0].Backend != "ollama" || c.reqs[0].Model != "qwen" {
		t.Errorf("settings not used: %+v", c.reqs[0])
	}
}

func TestSupervisorLoopModelValidators(t *testing.T) {
	if err := ValidateSpecDraft(goodSpec); err != nil {
		t.Fatal(err)
	}
	bad := []string{
		"nope",
		goodSpec + " trailing junk",
		goodSpec + goodSpec,
		`{"stories":[]}`,
		`{"stories":[{"name":"s","tasks":[{"name":"t","spec":"","files":["a"]}]}]}`,
		`{"stories":[{"name":"s","tasks":[{"name":"t","spec":"x","files":[]}]}]}`,
		`{"stories":[{"name":"Fix BL123","tasks":[{"name":"t","spec":"x","files":["a"]}]}]}`,
		`{"stories":[{"name":"s","tasks":[{"name":"GH#4 thing","spec":"x","files":["a"]}]}]}`,
	}
	for _, b := range bad {
		if ValidateSpecDraft(b) == nil {
			t.Errorf("accepted %s", b)
		}
	}
	c := &fakeClient{replies: []string{"flaky_test"}}
	m, _ := modelJobs(c, "b", "m", StyleOneshot)
	if got, err := m.ClassifyCIFailure(context.Background(), "x"); err != nil || got != "flaky_test" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestSupervisorLoopModelReaskThenEscalate(t *testing.T) {
	c := &fakeClient{replies: []string{"bogus", "also bogus", "third"}}
	m, esc := modelJobs(c, "b", "m", StyleOneshot)
	_, err := m.ClassifyCIFailure(context.Background(), "x")
	if !errors.Is(err, ErrEscalated) {
		t.Fatalf("err = %v", err)
	}
	if len(c.reqs) != 2 || len(esc.got) != 1 || len(esc.got[0].Attempts) != 2 {
		t.Fatalf("reqs=%d esc=%+v", len(c.reqs), esc.got)
	}
	c2 := &fakeClient{replies: []string{"gh", "pi"}}
	m2, _ := modelJobs(c2, "b", "m", StyleOneshot)
	if got, err := m2.PickBackend(context.Background(), "r", []string{"pi", "claude"}); err != nil || got != "pi" || len(c2.reqs) != 2 {
		t.Fatalf("re-ask: %q %v %d", got, err, len(c2.reqs))
	}
}

func TestSupervisorLoopModelNoModelRefusal(t *testing.T) {
	c := &fakeClient{replies: []string{"infra"}}
	m, esc := modelJobs(c, "", "", StyleOneshot)
	_, err := m.ClassifyCIFailure(context.Background(), "x")
	if !errors.Is(err, ErrEscalated) || !errors.Is(err, ErrNoModel) {
		t.Fatalf("err = %v", err)
	}
	if len(c.reqs) != 0 {
		t.Fatal("model was called")
	}
	if len(esc.got) != 1 {
		t.Fatal("not escalated")
	}
}

func TestSupervisorLoopModelStyleRouting(t *testing.T) {
	for _, st := range []Style{StyleOneshot, StyleSession} {
		c := &fakeClient{replies: []string{"infra"}}
		m, _ := modelJobs(c, "b", "m", st)
		if _, err := m.ClassifyCIFailure(context.Background(), "x"); err != nil {
			t.Fatal(err)
		}
		if c.reqs[0].Style != st {
			t.Errorf("style %q sent as %q", st, c.reqs[0].Style)
		}
	}
}

func TestSupervisorLoopModelRefusesPartialSetting(t *testing.T) {
	for _, bm := range [][2]string{{"ollama", ""}, {"", "qwen"}} {
		c := &fakeClient{replies: []string{"infra"}}
		m, esc := modelJobs(c, bm[0], bm[1], StyleOneshot)
		if _, err := m.ClassifyCIFailure(context.Background(), "log"); !errors.Is(err, ErrNoModel) {
			t.Fatalf("%v: got %v", bm, err)
		}
		if len(c.reqs) != 0 || len(esc.got) != 1 {
			t.Fatalf("%v: called=%d escalations=%d", bm, len(c.reqs), len(esc.got))
		}
	}
}

func TestSupervisorLoopModelLiveSettings(t *testing.T) {
	c := &fakeClient{replies: []string{"infra"}}
	m, _ := modelJobs(c, "", "", StyleOneshot)
	m.SettingsFn = func() Settings {
		s := m.Engine.Settings
		s.Jobs = map[string]JobSetting{"classify_ci_failure": {Style: StyleSession, Backend: "pi", Model: "q"}}
		return s
	}
	if _, err := m.ClassifyCIFailure(context.Background(), "log"); err != nil {
		t.Fatal(err)
	}
	if c.reqs[0].Style != StyleSession || c.reqs[0].Backend != "pi" {
		t.Fatalf("%+v", c.reqs[0])
	}
}
