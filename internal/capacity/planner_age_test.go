package capacity

import (
	"testing"
	"time"
)

// plannerAgeOrder returns the holders of the given waiters in grant order,
// where each waiter has been queued `age` before the fake clock's now.
func plannerAgeOrder(opt Options, specs []struct {
	req Request
	age time.Duration
}) []string {
	clk := newFakeClock()
	opt.Clock = clk
	l := New(opt)
	now := clk.Now()
	var ws []*waiter
	for i, s := range specs {
		ws = append(ws, &waiter{req: s.req, seq: uint64(i), since: now.Add(-s.age)})
	}
	var out []string
	for _, w := range l.orderListLocked(ws, now) {
		out = append(out, w.req.Holder)
	}
	return out
}

type ageSpec = struct {
	req Request
	age time.Duration
}

func equalOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAgedPlannerCompetesWithWorkersByPriority(t *testing.T) {
	got := plannerAgeOrder(Options{}, []ageSpec{
		{Request{Holder: "low", PRDID: "L", Priority: 0, Role: RoleWorker}, time.Minute},
		{Request{Holder: "plan", PRDID: "P", Priority: 5, Role: RolePlanner}, 11 * time.Minute},
		{Request{Holder: "high", PRDID: "H", Priority: 9, Role: RoleWorker}, time.Minute},
	})
	if want := []string{"high", "plan", "low"}; !equalOrder(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestYoungPlannerStillWaitsBehindWorker(t *testing.T) {
	got := plannerAgeOrder(Options{}, []ageSpec{
		{Request{Holder: "plan", PRDID: "P", Priority: 9, Role: RolePlanner}, 9 * time.Minute},
		{Request{Holder: "work", PRDID: "W", Priority: 0, Role: RoleWorker}, time.Minute},
	})
	if want := []string{"work", "plan"}; !equalOrder(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestVerifierStillBeforeAgedPlanner(t *testing.T) {
	got := plannerAgeOrder(Options{}, []ageSpec{
		{Request{Holder: "plan", PRDID: "P", Priority: 9, Role: RolePlanner}, time.Hour},
		{Request{Holder: "verify", PRDID: "V", Priority: 0, Role: RoleVerifier}, time.Minute},
	})
	if want := []string{"verify", "plan"}; !equalOrder(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestNegativePlannerAgeAfterKeepsOldOrder(t *testing.T) {
	got := plannerAgeOrder(Options{PlannerAgeAfter: -1}, []ageSpec{
		{Request{Holder: "plan", PRDID: "P", Priority: 9, Role: RolePlanner}, time.Hour},
		{Request{Holder: "work", PRDID: "W", Priority: 0, Role: RoleWorker}, time.Minute},
	})
	if want := []string{"work", "plan"}; !equalOrder(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestPlannerAgeAfterCustomValue(t *testing.T) {
	got := plannerAgeOrder(Options{PlannerAgeAfter: time.Minute}, []ageSpec{
		{Request{Holder: "work", PRDID: "W", Priority: 0, Role: RoleWorker}, time.Second},
		{Request{Holder: "plan", PRDID: "P", Priority: 5, Role: RolePlanner}, 2 * time.Minute},
	})
	if want := []string{"plan", "work"}; !equalOrder(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
