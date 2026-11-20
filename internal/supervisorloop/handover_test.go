package supervisorloop

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/planrecords"
)

type hoSession struct{ dir, profile map[string]string }

func (s hoSession) ProjectDir(p string) (string, error)        { return s.dir[p], nil }
func (s hoSession) SupervisorProfile(p string) (string, error) { return s.profile[p], nil }

type hoRules struct {
	def        string
	pack, file map[string]string
	err        error
}

func (r hoRules) DefaultPack() (string, error) { return r.def, r.err }
func (r hoRules) ProjectPack(p string) (string, bool, error) {
	t, ok := r.pack[p]
	return t, ok, nil
}
func (r hoRules) ProjectRulesFile(p string) (string, bool, error) {
	t, ok := r.file[p]
	return t, ok, nil
}

type hoPlans struct {
	plans   map[string][]planrecords.PlanDigest
	reasons map[string]string
}

func (f hoPlans) ProjectPlans(p string) ([]planrecords.PlanDigest, error) { return f.plans[p], nil }
func (f hoPlans) BlockedReason(_, id string) (string, error)              { return f.reasons[id], nil }

type hoClock struct{ t time.Time }

func (c hoClock) Now() time.Time { return c.t }

func digest(title, goal string, order int, autos ...planrecords.PlanAutomaton) planrecords.PlanDigest {
	d := planrecords.PlanDigest{Summary: planrecords.Summary{Title: title, Goal: goal, Order: order, State: planrecords.StateInProgress}, Automata: autos}
	for i := range autos {
		if autos[i].Run == planrecords.RunNext && d.Next == nil {
			d.Next = &d.Automata[i]
		}
	}
	return d
}

func newAssembler() *HandoverAssembler {
	return &HandoverAssembler{
		Session: hoSession{
			dir:     map[string]string{"alpha": "/work/alpha", "beta": "/work/beta"},
			profile: map[string]string{"alpha": "sup", "beta": "sup"},
		},
		Rules: hoRules{def: "DEFAULT PACK", pack: map[string]string{"alpha": "alpha pack zebra"}, file: map[string]string{"beta": "beta rules otter"}},
		Plans: hoPlans{
			plans: map[string][]planrecords.PlanDigest{
				"alpha": {digest("Alpha plan quartz", "ship quartz", 1,
					planrecords.PlanAutomaton{ID: "a1", Title: "build quartz", State: "pending", Run: planrecords.RunNext},
					planrecords.PlanAutomaton{ID: "a2", Title: "wait quartz", State: "blocked", Run: planrecords.RunBlocked})},
				"beta": {digest("Beta plan basalt", "ship basalt", 2)},
			},
			reasons: map[string]string{"a2": "needs the quartz key"},
		},
		Clock: hoClock{time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
	}
}

func TestAssembleHandoverLayers(t *testing.T) {
	h, err := newAssembler().AssembleHandover("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if h.Build.Model != DefaultSupervisorModel || strings.Contains(strings.ToLower(h.Build.Model), "opus") {
		t.Fatalf("model %q", h.Build.Model)
	}
	if len(h.Build.ToolServers) != 1 || h.Build.ToolServers[0] != ToolServerDatawatch || h.Build.ProjectDir != "/work/alpha" || h.Build.Profile != "sup" {
		t.Fatalf("build %+v", h.Build)
	}
	if h.Rules.DefaultPack != "DEFAULT PACK" || h.Rules.ProjectPack != "alpha pack zebra" {
		t.Fatalf("rules %+v", h.Rules)
	}
	if !h.AssembledAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("time %v", h.AssembledAt)
	}
	p := h.Direction.Plans[0]
	if p.Goal != "ship quartz" || p.NextWork != "build quartz" || !p.Work[1].Blocked || p.Work[1].BlockedReason != "needs the quartz key" {
		t.Fatalf("direction %+v", p)
	}
	out := h.Render()
	for _, want := range []string{"BLOCKED: needs the quartz key", "DEFAULT PACK", "2026-01-02T03:04:05Z"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
}

func TestAssembleHandoverProjectsIsolated(t *testing.T) {
	a := newAssembler()
	ha, err := a.AssembleHandover("alpha")
	if err != nil {
		t.Fatal(err)
	}
	hb, err := a.AssembleHandover("beta")
	if err != nil {
		t.Fatal(err)
	}
	ra, rb := ha.Render(), hb.Render()
	if ra == rb {
		t.Fatal("handovers are identical")
	}
	for _, s := range []string{"quartz", "zebra", "/work/alpha"} {
		if strings.Contains(rb, s) {
			t.Fatalf("beta contains alpha text %q", s)
		}
	}
	for _, s := range []string{"basalt", "otter", "/work/beta"} {
		if strings.Contains(ra, s) {
			t.Fatalf("alpha contains beta text %q", s)
		}
	}
}

func TestAssembleHandoverErrors(t *testing.T) {
	a := newAssembler()
	if _, err := a.AssembleHandover(" "); err == nil {
		t.Fatal("want project error")
	}
	a.Rules = hoRules{err: errors.New("boom")}
	if _, err := a.AssembleHandover("alpha"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err %v", err)
	}
	if _, err := (&HandoverAssembler{}).AssembleHandover("alpha"); err == nil {
		t.Fatal("want missing sources error")
	}
}
