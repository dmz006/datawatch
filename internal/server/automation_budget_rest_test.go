// REST tests for the set_budget control path under /api/automations/{id}/, the
// computed budget view on the Automation record, and the budget fields of
// /api/autonomous/status, driven through the real autonomous manager.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// budgetNow is the fixed clock of these tests: Thursday 2026-10-22 12:00 UTC,
// inside the week that started on Monday the 19th.
var budgetNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

// wireBudget gives mgr a budget evaluator on a fixed clock that reports spent
// dollars for every Automation.
func wireBudget(mgr *autonomous.Manager, spent float64) {
	b := autonomous.NewAutomationBudget(nil, nil)
	b.Clock = func() time.Time { return budgetNow }
	b.Spend = func(*autonomous.Automation, *autonomous.AutomationMember, time.Time, string) float64 { return spent }
	b.Resets = b.ResetFor
	mgr.SetAutomationBudget(b)
}

// budgetWeekBody is a complete $40-a-week policy request.
func budgetWeekBody(extra map[string]any) map[string]any {
	body := map[string]any{
		"ceiling_type": "dollar", "ceiling_value": 40, "window": "week", "pacing": "even",
		"on_breach": "pause", "backend": "claude", "reset_zone": "UTC",
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

// budgetRecord is the part of the Automation wire record these tests read.
type budgetRecord struct {
	ID           string `json:"id"`
	Version      int64  `json:"version"`
	Status       string `json:"status"`
	BudgetPolicy *struct {
		CeilingType  string  `json:"ceiling_type"`
		CeilingValue float64 `json:"ceiling_value"`
		Window       string  `json:"window"`
		Pacing       string  `json:"pacing"`
		OnBreach     string  `json:"on_breach"`
		Backend      string  `json:"backend"`
	} `json:"budget_policy"`
	Members []struct {
		ID               string  `json:"id"`
		EstimatedCostUSD float64 `json:"estimated_cost_usd"`
		BudgetPolicy     *struct {
			CeilingValue float64 `json:"ceiling_value"`
			Window       string  `json:"window"`
		} `json:"budget_policy"`
	} `json:"members"`
	Budget *budgetViewWire `json:"budget"`
}

type budgetViewWire struct {
	Scope          string     `json:"scope"`
	CeilingUSD     float64    `json:"ceiling_usd"`
	SpentUSD       float64    `json:"spent_usd"`
	RemainingUSD   float64    `json:"remaining_usd"`
	Window         string     `json:"window"`
	WindowStart    *time.Time `json:"window_start"`
	WindowEnd      *time.Time `json:"window_end"`
	Pacing         string     `json:"pacing"`
	CeilingReached bool       `json:"ceiling_reached"`
	ResetAt        *time.Time `json:"reset_at"`
	ResetKnown     bool       `json:"reset_known"`
	ResetSource    string     `json:"reset_source"`
	Allocation     *struct {
		Estimated bool   `json:"estimated"`
		Basis     string `json:"basis"`
	} `json:"allocation_estimate"`
}

func decodeBudgetRecord(t *testing.T, body string) budgetRecord {
	t.Helper()
	var r budgetRecord
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("decode record: %v (body=%q)", err, body)
	}
	return r
}

// setBudget posts a set_budget request at the record's current version unless
// the body names one, and decodes the record on success.
func setBudget(t *testing.T, s *Server, a ctlAutomation, body map[string]any, want int) budgetRecord {
	t.Helper()
	if _, set := body["expected_version"]; !set {
		body["expected_version"] = a.Version
	}
	out := doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/set_budget", body, want)
	if want != http.StatusOK {
		return budgetRecord{}
	}
	return decodeBudgetRecord(t, out)
}

func (r budgetRecord) asCtl() ctlAutomation {
	return ctlAutomation{ID: r.ID, Version: r.Version, Status: r.Status}
}

func TestAutomationRESTSetBudget(t *testing.T) {
	s, mgr, al, a := newActiveAutomation(t)
	wireBudget(mgr, 12.40)

	got := setBudget(t, s, a, budgetWeekBody(map[string]any{"actor": "alice", "reason": "weekly cap"}), http.StatusOK)
	if got.Status != "active" || got.Version != a.Version+1 {
		t.Fatalf("set_budget: status=%s version=%d (was %d)", got.Status, got.Version, a.Version)
	}
	p := got.BudgetPolicy
	if p == nil || p.CeilingType != "dollar" || p.CeilingValue != 40 || p.Window != "week" || p.Pacing != "even" || p.OnBreach != "pause" || p.Backend != "claude" {
		t.Fatalf("policy %+v", p)
	}
	if got.Budget == nil || got.Budget.SpentUSD != 12.40 || got.Budget.CeilingUSD != 40 {
		t.Fatalf("the response carries the computed view: %+v", got.Budget)
	}
	wantAudit(t, al, "automation_set_budget", a.ID, map[string]any{
		"actor": "alice", "reason": "weekly cap", "expected_version": float64(a.Version),
		"ceiling_type": "dollar", "ceiling_value": float64(40), "window": "week", "clear": false,
	})

	// A member override and a declared cost.
	over := setBudget(t, s, got.asCtl(), map[string]any{"member_id": "m1", "ceiling_type": "dollar", "ceiling_value": 3, "window": "day", "estimated_cost_usd": 1.5}, http.StatusOK)
	var m1 = -1
	for i, m := range over.Members {
		if m.ID == "m1" {
			m1 = i
		}
	}
	if m1 < 0 || over.Members[m1].BudgetPolicy == nil || over.Members[m1].BudgetPolicy.CeilingValue != 3 || over.Members[m1].EstimatedCostUSD != 1.5 {
		t.Fatalf("member m1 %+v", over.Members)
	}
	if over.BudgetPolicy == nil || over.BudgetPolicy.CeilingValue != 40 {
		t.Fatalf("the Automation policy changed: %+v", over.BudgetPolicy)
	}
	wantAudit(t, al, "automation_set_budget", a.ID, map[string]any{"member_id": "m1", "window": "day"})

	// Clearing the member override, then the Automation policy.
	cleared := setBudget(t, s, over.asCtl(), map[string]any{"member_id": "m1", "clear": true}, http.StatusOK)
	for _, m := range cleared.Members {
		if m.ID == "m1" && m.BudgetPolicy != nil {
			t.Fatalf("override still set: %+v", m)
		}
	}
	none := setBudget(t, s, cleared.asCtl(), map[string]any{"clear": true, "reason": "done"}, http.StatusOK)
	if none.BudgetPolicy != nil || none.Budget != nil || none.Status != "active" {
		t.Fatalf("after clear: policy=%+v budget=%+v status=%s", none.BudgetPolicy, none.Budget, none.Status)
	}
	wantAudit(t, al, "automation_set_budget", a.ID, map[string]any{"clear": true, "reason": "done"})

	// The ops are POST only.
	doAutomation(t, s, http.MethodGet, "/api/automations/"+a.ID+"/set_budget", nil, http.StatusMethodNotAllowed)
}

func TestAutomationRESTSetBudgetRejectsBadInput(t *testing.T) {
	s, mgr, _, a := newActiveAutomation(t)
	wireBudget(mgr, 0)

	for name, body := range map[string]map[string]any{
		"no fields":            {},
		"zero ceiling":         {"ceiling_type": "dollar", "window": "day"},
		"unknown ceiling type": {"ceiling_type": "euros", "ceiling_value": 5, "window": "day"},
		"unknown window":       {"ceiling_type": "dollar", "ceiling_value": 5, "window": "fortnight"},
		"unknown pacing":       {"ceiling_type": "dollar", "ceiling_value": 5, "window": "day", "pacing": "fast"},
		"unknown on_breach":    {"ceiling_type": "dollar", "ceiling_value": 5, "window": "day", "on_breach": "explode"},
		"weekday on a day":     {"ceiling_type": "dollar", "ceiling_value": 5, "window": "day", "reset_weekday": "monday"},
		"reset day too big":    {"ceiling_type": "dollar", "ceiling_value": 5, "window": "month", "reset_day": 31},
		"clear with fields":    {"clear": true, "ceiling_value": 5},
		"estimate no member":   {"estimated_cost_usd": 2},
		"negative estimate":    {"member_id": "m1", "estimated_cost_usd": -2},
	} {
		out := doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/set_budget", withVersion(body, a.Version), http.StatusBadRequest)
		if strings.Contains(out, "BL") {
			t.Errorf("%s: message carries a backlog identifier: %q", name, out)
		}
	}
	// A body that is not JSON.
	req := httptest.NewRequest(http.MethodPost, "/api/automations/"+a.ID+"/set_budget", strings.NewReader("{not json"))
	rr := httptest.NewRecorder()
	s.handleAutomations(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: %d", rr.Code)
	}

	// Unknown Automation and unknown member are 404.
	doAutomation(t, s, http.MethodPost, "/api/automations/nope0000/set_budget", withVersion(budgetWeekBody(nil), 1), http.StatusNotFound)
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/set_budget", withVersion(budgetWeekBody(map[string]any{"member_id": "ghost"}), a.Version), http.StatusNotFound)

	// A stale or missing version is a 409 and nothing was written.
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/set_budget", withVersion(budgetWeekBody(nil), a.Version+7), http.StatusConflict)
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/set_budget", budgetWeekBody(nil), http.StatusConflict)
	if cur, _ := mgr.GetAutomation(a.ID); cur.Version != a.Version || cur.BudgetPolicy != nil {
		t.Fatalf("a refused request wrote: version %d policy %+v", cur.Version, cur.BudgetPolicy)
	}

	// A member that has finished cannot take an override; a cancelled Automation cannot take a policy.
	skipped := ctl(t, s, a, "skip_member", map[string]any{"member_id": "m0"}, http.StatusOK)
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/set_budget", withVersion(budgetWeekBody(map[string]any{"member_id": "m0"}), skipped.Version), http.StatusConflict)
	cancelled := ctl(t, s, skipped, "cancel", map[string]any{"cancel_in_flight": false}, http.StatusOK)
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/set_budget", withVersion(budgetWeekBody(nil), cancelled.Version), http.StatusConflict)
}

func withVersion(body map[string]any, v int64) map[string]any {
	out := make(map[string]any, len(body)+1)
	for k, val := range body {
		out[k] = val
	}
	out["expected_version"] = v
	return out
}

func TestAutomationRESTRecordCarriesBudgetState(t *testing.T) {
	s, mgr, _, a := newActiveAutomation(t)
	wireBudget(mgr, 12.40)

	// No policy: the record has no budget view.
	if rec := decodeBudgetRecord(t, doAutomation(t, s, http.MethodGet, "/api/automations/"+a.ID, nil, http.StatusOK)); rec.Budget != nil {
		t.Fatalf("a record with no policy carries a view: %+v", rec.Budget)
	}

	set := setBudget(t, s, a, budgetWeekBody(map[string]any{"allocation_estimate": true}), http.StatusOK)
	got := decodeBudgetRecord(t, doAutomation(t, s, http.MethodGet, "/api/automations/"+a.ID, nil, http.StatusOK))
	b := got.Budget
	if b == nil || b.Scope != "automation" || b.CeilingUSD != 40 || b.SpentUSD != 12.40 || b.RemainingUSD != 27.60 || b.Window != "week" || b.Pacing != "even" {
		t.Fatalf("budget view %+v", b)
	}
	if b.WindowStart == nil || !b.WindowStart.Equal(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)) ||
		b.WindowEnd == nil || !b.WindowEnd.Equal(time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("window %v..%v", b.WindowStart, b.WindowEnd)
	}
	if b.ResetAt == nil || !b.ResetAt.Equal(*b.WindowEnd) || !b.ResetKnown || b.ResetSource != "window" || b.CeilingReached {
		t.Fatalf("reset %v known=%v source=%q reached=%v", b.ResetAt, b.ResetKnown, b.ResetSource, b.CeilingReached)
	}
	// With no plan tier wired there is no estimate to show, and none is invented.
	if b.Allocation != nil && !b.Allocation.Estimated {
		t.Fatalf("allocation not labelled estimated: %+v", b.Allocation)
	}

	// The list carries it too.
	var list []budgetRecord
	if err := json.Unmarshal([]byte(doAutomation(t, s, http.MethodGet, "/api/automations", nil, http.StatusOK)), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Budget == nil || list[0].Budget.SpentUSD != 12.40 {
		t.Fatalf("list %+v", list)
	}

	// Reading never writes: the version is unchanged and the stored record holds no view.
	if got.Version != set.Version {
		t.Fatalf("version moved on read: %d -> %d", set.Version, got.Version)
	}
	stored, _ := mgr.GetAutomation(a.ID)
	raw, _ := json.Marshal(stored)
	if strings.Contains(string(raw), `"budget":`) {
		t.Fatalf("stored record carries the computed view: %s", raw)
	}
}

func TestAutonomousStatusRESTCarriesAutomationBudget(t *testing.T) {
	s, mgr, _, a := newActiveAutomation(t)
	wireBudget(mgr, 12.40)
	setBudget(t, s, a, budgetWeekBody(nil), http.StatusOK)

	rr := httptest.NewRecorder()
	s.handleAutonomousStatus(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var st struct {
		Automations []struct {
			ID             string          `json:"id"`
			SpentUSD       float64         `json:"spent_usd"`
			CeilingDisplay string          `json:"ceiling_display"`
			Budget         *budgetViewWire `json:"budget"`
		} `json:"automations"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status: %v (%s)", err, rr.Body.String())
	}
	if len(st.Automations) != 1 {
		t.Fatalf("automations %+v", st.Automations)
	}
	row := st.Automations[0]
	if row.ID != a.ID || row.SpentUSD != 12.40 || row.CeilingDisplay != "$12.40 of $40.00 (week)" {
		t.Fatalf("row %+v", row)
	}
	if row.Budget == nil || row.Budget.CeilingUSD != 40 || row.Budget.RemainingUSD != 27.60 || !row.Budget.ResetKnown {
		t.Fatalf("row budget %+v", row.Budget)
	}
}
