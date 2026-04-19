// BL28 — quality-gate wiring on Executor.

package pipeline

import (
	"testing"
)

// stubStarter is a pure in-memory SessionStarter.
type stubStarter struct {
	idx   int
	state string
}

func (s *stubStarter) StartSession(task, projectDir, backend string) (string, error) {
	s.idx++
	return "sess-xx", nil
}
func (s *stubStarter) GetSessionState(sessionID string) string {
	if s.state == "" {
		return "running"
	}
	return s.state
}

func TestBL28_SetQualityGates(t *testing.T) {
	e := NewExecutor(&stubStarter{}, "claude-code")
	cfg := QualityGateConfig{
		Enabled:           true,
		TestCommand:       "echo PASS",
		Timeout:           5,
		BlockOnRegression: true,
	}
	e.SetQualityGates(cfg)
	if !e.qualityGates.Enabled || e.qualityGates.TestCommand != "echo PASS" {
		t.Errorf("config not applied: %+v", e.qualityGates)
	}
}

func TestBL28_CompareResults_SummaryFormat(t *testing.T) {
	// Regression summary mentions new failure count and both before
	// and after values, so the operator sees the delta at a glance.
	base := TestResult{PassCount: 10, FailCount: 2}
	curr := TestResult{PassCount: 10, FailCount: 4}
	r := CompareResults(base, curr)
	if !r.Regression {
		t.Fatal("expected regression")
	}
	if r.NewFailures != 2 {
		t.Errorf("new failures=%d want 2", r.NewFailures)
	}
	if r.Summary == "" {
		t.Error("summary should not be empty")
	}
}
