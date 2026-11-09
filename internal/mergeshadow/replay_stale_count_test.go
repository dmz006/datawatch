package mergeshadow

import (
	"os"
	"strings"
	"testing"
)

// staleCountFixture is a recorded failing go test run: a test that counts
// settings keys against an expected number that is out of date.
const staleCountFixture = "../../testdata/mergeshadow/stale-count-test/go-test.json"

const staleCountTestName = "TestSettingsKeyCount"

func TestParseGoTestFailuresStaleCount(t *testing.T) {
	data, err := os.ReadFile(staleCountFixture) // #nosec G304 -- fixed fixture path
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	names, first := ParseGoTestFailures(string(data))
	if len(names) != 1 || names[0] != staleCountTestName {
		t.Fatalf("names = %v, want [%s]", names, staleCountTestName)
	}
	if first != "settings_count_test.go:14: settings keys = 41, want 40" {
		t.Errorf("first assertion = %q", first)
	}
	if n, f := ParseGoTestFailures("not json\n"); len(n) != 0 || f != "" {
		t.Errorf("non-JSON input gave %v %q", n, f)
	}
}

// TestReplayStaleCountNamedInPlanTestGate builds the plan's gate records from
// the recorded failing run and checks the failing test is named in the test
// gate's detail.
func TestReplayStaleCountNamedInPlanTestGate(t *testing.T) {
	data, err := os.ReadFile(staleCountFixture) // #nosec G304 -- fixed fixture path
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	gates := toGates([]GateResult{
		{Name: "build", Status: GatePass},
		{Name: "test", Status: GateFail, Detail: "exit status 1", Output: string(data)},
	})
	var testGate *Gate
	for i := range gates {
		if gates[i].Name == "test" {
			testGate = &gates[i]
		}
	}
	if testGate == nil {
		t.Fatal("plan has no test gate")
	}
	if !strings.Contains(testGate.Detail, staleCountTestName) {
		t.Errorf("test gate detail %q does not name %s", testGate.Detail, staleCountTestName)
	}
	if !strings.Contains(testGate.Detail, "settings keys = 41, want 40") {
		t.Errorf("test gate detail %q lacks the assertion line", testGate.Detail)
	}
	if !strings.Contains(testGate.Detail, "exit status 1") {
		t.Errorf("test gate detail %q lost the original detail", testGate.Detail)
	}
}
