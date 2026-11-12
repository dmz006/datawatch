package audit

import "testing"

func TestBudgetEvents_NamesAndCEF(t *testing.T) {
	cases := []struct {
		action string
		want   string
		id     int
		name   string
		sev    int
	}{
		{ActionBudgetCeilingWait, "autonomous_budget_ceiling_wait", 35, "AutonomousBudgetCeilingWait", 4},
		{ActionBudgetWindowResumed, "autonomous_budget_window_resumed", 36, "AutonomousBudgetWindowResumed", 2},
	}
	for _, c := range cases {
		if c.action != c.want {
			t.Errorf("action constant = %q, want %q", c.action, c.want)
		}
		id, name, sev := cefSignature(c.action)
		if id != c.id || name != c.name || sev != c.sev {
			t.Errorf("cefSignature(%q) = %d %q %d, want %d %q %d", c.action, id, name, sev, c.id, c.name, c.sev)
		}
	}
}
