package autonomous

import "testing"

func TestResolvePlannerResume(t *testing.T) {
	if a, _ := ResolvePlannerResume(true); a != PlannerResumeReattach {
		t.Fatalf("alive: action = %s, want %s", a, PlannerResumeReattach)
	}
	if a, _ := ResolvePlannerResume(false); a != PlannerResumeRestart {
		t.Fatalf("gone: action = %s, want %s", a, PlannerResumeRestart)
	}
}
