package usagecalc

import (
	"encoding/json"
	"testing"
)

// The planning figure per lane count comes from the queue estimate and the reset alone. The
// measured burn keeps coming from the recent readings, and a typed remaining work does not
// change the planning figure.

func TestAdviseLaneRowsCarryPlanningBurnFromQueue(t *testing.T) {
	in := workedExample(t)
	in.RemainingWork = RemainingWork{QueueTasks: 6, CostPerTask: 3.33, Typed: f64(5)}
	a := Advise(in)
	if a.RemainingWork.EstimatePoints == nil {
		t.Fatalf("estimate is nil; want 6 tasks at 3.33 points")
	}
	if len(a.ByLanes) != 4 {
		t.Fatalf("by_lanes has %d rows, want 4", len(a.ByLanes))
	}
	perLane := 19.98 / (3 * a.DaysLeft)
	for l, row := range a.ByLanes {
		near(t, "planning burn", row.PlanningBurnPer24h, a.BaselinePer24h+float64(l)*perLane, 1e-9)
	}
	near(t, "measured burn at 3 lanes", a.ByLanes[3].BurnPer24h, 24.08, 0.01)
}

func TestAdviseLaneRowsPlanningIsBaselineWithoutQueue(t *testing.T) {
	cases := map[string]RemainingWork{
		"typed figure only":    {Typed: f64(20)},
		"queue without a cost": {QueueTasks: 6},
		"nothing entered":      {},
	}
	for name, work := range cases {
		in := workedExample(t)
		in.RemainingWork = work
		a := Advise(in)
		for l, row := range a.ByLanes {
			if row.PlanningBurnPer24h != a.BaselinePer24h {
				t.Errorf("%s: lane %d planning %v; want the idle baseline %v", name, l, row.PlanningBurnPer24h, a.BaselinePer24h)
			}
		}
	}
}

func TestPlanningPerLaneGuards(t *testing.T) {
	cases := []struct {
		name     string
		days     float64
		maxLanes int
		est      *float64
		want     float64
	}{
		{"no estimate", 2, 3, nil, 0},
		{"no lanes", 2, 0, f64(12), 0},
		{"no time left", 0, 3, f64(12), 0},
		{"queue shared over lanes and days", 2, 3, f64(12), 2},
	}
	for _, c := range cases {
		a := Advice{DaysLeft: c.days, RemainingWork: WorkResult{EstimatePoints: c.est}}
		if got := a.planningPerLane(c.maxLanes); got != c.want {
			t.Errorf("%s: planningPerLane = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAdviseLaneRowPlanningJSON(t *testing.T) {
	in := workedExample(t)
	in.RemainingWork = RemainingWork{QueueTasks: 6, CostPerTask: 3.33}
	raw, err := json.Marshal(Advise(in).ByLanes[1])
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["planning_burn_per_24h"]; !ok {
		t.Errorf("lane row JSON is missing planning_burn_per_24h: %s", raw)
	}
}
