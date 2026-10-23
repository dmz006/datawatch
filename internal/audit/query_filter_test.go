package audit

import (
	"testing"
	"time"
)

// seedQueryLog writes five entries one minute apart, oldest first.
func seedQueryLog(t *testing.T) (*Log, time.Time) {
	t.Helper()
	l, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	base := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	for i, e := range []Entry{
		{Actor: "operator", Action: "start", SessionID: "s1"},
		{Actor: "daemon", Action: ActionCapacityWait, Subject: "task-1"},
		{Actor: "daemon", Action: ActionBudgetCeilingWait, Subject: "claude"},
		{Actor: "daemon", Action: ActionBudgetWindowResumed, Subject: "claude"},
		{Actor: "daemon", Action: ActionCapacityAdmitted, Subject: "task-1"},
	} {
		e.Timestamp = base.Add(time.Duration(i) * time.Minute)
		if err := l.Write(e); err != nil {
			t.Fatal(err)
		}
	}
	return l, base
}

func actionsOf(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Action
	}
	return out
}

func sameStrings(a, b []string) bool {
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

func TestAuditQuery_SubjectAndActionPrefixFilters(t *testing.T) {
	l, base := seedQueryLog(t)

	cases := []struct {
		name   string
		filter QueryFilter
		want   []string // newest first
	}{
		{"no filter is newest first", QueryFilter{},
			[]string{ActionCapacityAdmitted, ActionBudgetWindowResumed, ActionBudgetCeilingWait, ActionCapacityWait, "start"}},
		{"exact action", QueryFilter{Action: ActionBudgetCeilingWait}, []string{ActionBudgetCeilingWait}},
		{"exact action does not match a longer name", QueryFilter{Action: "autonomous_budget"}, nil},
		{"prefix action lists a family", QueryFilter{Action: "autonomous_budget_*"},
			[]string{ActionBudgetWindowResumed, ActionBudgetCeilingWait}},
		{"broader prefix", QueryFilter{Action: "autonomous_*"},
			[]string{ActionCapacityAdmitted, ActionBudgetWindowResumed, ActionBudgetCeilingWait, ActionCapacityWait}},
		{"prefix with no member", QueryFilter{Action: "nothing_*"}, nil},
		{"subject exact", QueryFilter{Subject: "task-1"}, []string{ActionCapacityAdmitted, ActionCapacityWait}},
		{"subject is not a prefix match", QueryFilter{Subject: "task"}, nil},
		{"subject and prefix action", QueryFilter{Subject: "claude", Action: "autonomous_budget_*"},
			[]string{ActionBudgetWindowResumed, ActionBudgetCeilingWait}},
		{"subject and exact action", QueryFilter{Subject: "task-1", Action: ActionCapacityWait}, []string{ActionCapacityWait}},
		{"subject and action that never co-occur", QueryFilter{Subject: "claude", Action: ActionCapacityWait}, nil},
		{"actor and prefix action", QueryFilter{Actor: "daemon", Action: "autonomous_capacity_*"},
			[]string{ActionCapacityAdmitted, ActionCapacityWait}},
		{"since is inclusive", QueryFilter{Since: base.Add(3 * time.Minute)},
			[]string{ActionCapacityAdmitted, ActionBudgetWindowResumed}},
		{"until is exclusive", QueryFilter{Until: base.Add(2 * time.Minute)},
			[]string{ActionCapacityWait, "start"}},
		{"since and until window with prefix", QueryFilter{Since: base.Add(time.Minute), Until: base.Add(4 * time.Minute), Action: "autonomous_*"},
			[]string{ActionBudgetWindowResumed, ActionBudgetCeilingWait, ActionCapacityWait}},
		{"limit keeps the newest", QueryFilter{Limit: 2}, []string{ActionCapacityAdmitted, ActionBudgetWindowResumed}},
		{"limit applies after filtering", QueryFilter{Action: "autonomous_*", Limit: 1}, []string{ActionCapacityAdmitted}},
		{"limit larger than matches", QueryFilter{Subject: "claude", Limit: 50},
			[]string{ActionBudgetWindowResumed, ActionBudgetCeilingWait}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := l.Read(tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if !sameStrings(actionsOf(got), tc.want) {
				t.Errorf("actions = %v, want %v", actionsOf(got), tc.want)
			}
		})
	}
}

func TestAuditQuery_ParseQueryRejectsBadBounds(t *testing.T) {
	for _, name := range []string{"since", "until"} {
		_, err := ParseQuery(func(n string) string {
			if n == name {
				return "yesterday"
			}
			return ""
		})
		if err == nil {
			t.Errorf("%s=yesterday was accepted", name)
		}
	}
	f, err := ParseQuery(func(n string) string {
		return map[string]string{"subject": "t1", "action": "a_*", "limit": "7", "since": "2026-10-22T12:00:00Z"}[n]
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.Subject != "t1" || f.Action != "a_*" || f.Limit != 7 || f.Since.IsZero() {
		t.Errorf("parsed %+v", f)
	}
	if d, _ := ParseQuery(func(string) string { return "" }); d.Limit != DefaultQueryLimit {
		t.Errorf("default limit = %d", d.Limit)
	}
}
