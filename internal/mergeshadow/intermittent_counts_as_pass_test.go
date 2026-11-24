package mergeshadow

import (
	"strings"
	"testing"
	"time"
)

// flakeOutput is go test -json output of one failing test.
const flakeOutput = `{"Action":"output","Package":"p","Test":"TestFlaky","Output":"    flaky_test.go:9: race\\n"}
{"Action":"fail","Package":"p","Test":"TestFlaky"}`

// An intermittent test gate counts as a pass, and the plan's gate detail names
// the flaky test so the flake is visible next to the pass.
func TestIntermittentGateCountsAsPassWithFlakeNamed(t *testing.T) {
	res := GateResult{
		Name: "test", Status: GateIntermittent, Detail: intermittentDetail,
		Output: flakeOutput, Finding: leadingLines(flakeOutput, findingLines),
	}
	g := toGates([]GateResult{res})[0]
	if !GateStatusCountsAsPass(g.Status) {
		t.Errorf("status %q does not count as pass", g.Status)
	}
	if !strings.Contains(g.Detail, "flaky tests: TestFlaky") {
		t.Errorf("detail %q does not name the flaky test", g.Detail)
	}
	if f := findingsFor(nil, nil, []Gate{g}, "source"); len(f) != 0 {
		t.Errorf("blocking findings %v for an intermittent gate", f)
	}
}

func TestGateStatusCountsAsPass(t *testing.T) {
	for status, want := range map[string]bool{
		string(GatePass): true, string(GateIntermittent): true,
		string(GateFail): false, string(GateTimeout): false, string(GateSkipped): false,
	} {
		if got := GateStatusCountsAsPass(status); got != want {
			t.Errorf("GateStatusCountsAsPass(%q) = %v, want %v", status, got, want)
		}
	}
}

// The supervisor's manual gate record says pass; a plan whose test gate was
// intermittent agrees with it, and a failed gate still differs.
func TestCompareIntermittentGateAgreesWithManualPass(t *testing.T) {
	manual := []GateOutcome{
		{Name: "build", Status: "pass"}, {Name: "lint", Status: "pass"}, {Name: "test", Status: "pass"},
	}
	plan := compareSamplePlan()
	plan.Gates[2] = Gate{Name: "test", Status: string(GateIntermittent), Duration: 2 * time.Second}
	c, err := Compare(plan, matchingLanded(), true, manual)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Differences) != 0 {
		t.Errorf("differences = %+v, want none", c.Differences)
	}

	plan.Gates[2].Status = string(GateFail)
	c, err = Compare(plan, matchingLanded(), true, manual)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Differences) != 1 || c.Differences[0].Field != "gates" {
		t.Errorf("differences = %+v, want one gates difference", c.Differences)
	}
}
