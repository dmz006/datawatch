package router

import (
	"strings"
	"testing"
)

func TestPlanVerbs_RenameSendsAPatchAndReports(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"PATCH /api/plans/ship": {0, `{"plan":{"title":"Ship it now","id":"ship"},"goal":""}`},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan rename ship Ship it now"), "\n")
	last := (*seen)[len(*seen)-1]
	if last.method != "PATCH" || last.path != "/api/plans/ship" || last.body != `{"title":"Ship it now"}` {
		t.Errorf("rename sent %+v", last)
	}
	if !strings.Contains(got, "Renamed plan ship to Ship it now") {
		t.Errorf("rename reply %q", got)
	}
	// "arcs" is the alias of "plan" for the new verbs too.
	r.HandleTestMessage("autonomous arcs rename ship Again")
	if last = (*seen)[len(*seen)-1]; last.body != `{"title":"Again"}` {
		t.Errorf("arcs rename sent %+v", last)
	}
}

func TestPlanVerbs_RenameConflictNamesTheOtherPlan(t *testing.T) {
	_, port := planCommDaemon(t, map[string]planCommRoute{
		"PATCH /api/plans/beta": {409, `{"error":"the title \"Alpha\" is already used in project acme by \"Alpha\" (alpha)"}`},
		"PATCH /api/plans/long": {400, `{"error":"a plan title is at most 100 characters"}`},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan rename beta Alpha"), "\n")
	if !strings.Contains(got, "failed") || !strings.Contains(got, `by "Alpha" (alpha)`) || strings.Contains(got, "HTTP 409") {
		t.Errorf("conflict reply %q", got)
	}
	got = strings.Join(r.HandleTestMessage("autonomous plan rename long "+strings.Repeat("x", 101)), "\n")
	if !strings.Contains(got, "100 characters") {
		t.Errorf("validation reply %q", got)
	}
}

func TestPlanVerbs_EditGoal(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"PATCH /api/plans/ship": {0, `{"plan":{"title":"Ship","id":"ship"},"goal":"get it out the door"}`},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan edit ship goal get it out the door"), "\n")
	last := (*seen)[len(*seen)-1]
	if last.method != "PATCH" || last.path != "/api/plans/ship" || last.body != `{"goal":"get it out the door"}` {
		t.Errorf("edit sent %+v", last)
	}
	if !strings.Contains(got, "Set the goal of plan Ship [ship]: get it out the door") {
		t.Errorf("edit reply %q", got)
	}
}

func TestPlanVerbs_EditAndRenameRefuseBadShapes(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{})
	r := setupPeerRouter(t, port)
	for msg, want := range map[string]string{
		"autonomous plan rename ship":          "usage:",
		"autonomous plan rename":               "usage:",
		"autonomous plan edit ship":            "usage:",
		"autonomous plan edit ship title x":    "usage:",
		"autonomous plan edit ship goal":       "give the goal text",
		"autonomous plan edit ghost goal text": "no plan record matches",
	} {
		got := strings.Join(r.HandleTestMessage(msg), "\n")
		if !strings.Contains(got, want) {
			t.Errorf("%q -> %q, want %q", msg, got, want)
		}
	}
	// Only the last call, an unknown plan, reaches the daemon.
	if len(*seen) != 1 {
		t.Errorf("bad shapes reached the daemon: %+v", *seen)
	}
}

func TestPlanVerbs_HelpListsRenameAndEdit(t *testing.T) {
	for _, want := range []string{"plan rename <plan> <new title...>", "plan edit <plan> goal <text...>"} {
		if !strings.Contains(planVerbUsage, want) {
			t.Errorf("plan usage lacks %q", want)
		}
	}
	_, port := planCommDaemon(t, map[string]planCommRoute{})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous help"), "\n")
	for _, want := range []string{"plan rename <plan> <new title...>", "plan edit <plan> goal <text...>"} {
		if !strings.Contains(got, want) {
			t.Errorf("autonomous help lacks %q", want)
		}
	}
}
