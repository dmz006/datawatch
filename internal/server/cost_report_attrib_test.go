// Attribution and shipped-state tests for the cost-report server adapters:
// managerCostSource (the Records populating and the ShippedUnits) and the
// release pick that owns a record, with the session and autonomous managers
// built in temp dirs like the daemon tests do.

package server

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/costreport"
	"github.com/dmz006/datawatch/internal/spendledger"
)

// TestCostReportSourceRecordsPopulateOwner — the session→owner map drives
// Record.AutomatonID/AutomationID/Release: the owned session carries all
// three, the unowned one stays blank (the engine then keys it under
// "unattributed", not a blank key).
func TestCostReportSourceRecordsPopulateOwner(t *testing.T) {
	now := time.Now()
	led := fakeSpendLedger{
		{SessionID: "abcd1234", Backend: "claude-code", StartedAt: now, TokensIn: 1000, TokensOut: 500},
		{SessionID: "ef012345", Backend: "ollama", StartedAt: now, TokensIn: 100, TokensOut: 50},
	}
	src := managerCostSource{
		ledger: led,
		owner:  map[string]costOwner{"abcd1234": {automaton: "prd-1", automation: "auto-1", release: "v1.2.0"}},
	}
	res, err := src.Records(costreport.SourceOptions{})
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(res.Records) != 2 {
		t.Fatalf("want 2 records, got %d", len(res.Records))
	}
	owned, unowned := false, false
	for _, r := range res.Records {
		switch r.SessionID {
		case "abcd1234":
			owned = true
			if r.AutomatonID != "prd-1" || r.AutomationID != "auto-1" || r.Release != "v1.2.0" {
				t.Errorf("owned record not populated: %+v", r)
			}
		case "ef012345":
			unowned = true
			if r.AutomatonID != "" || r.AutomationID != "" || r.Release != "" {
				t.Errorf("unowned record must stay blank: %+v", r)
			}
		}
	}
	if !owned || !unowned {
		t.Fatalf("owned=%v unowned=%v — both sessions expected", owned, unowned)
	}
}

// TestCostReportUnattributedRowViaEngine — an unowned session groups under
// the explicit "unattributed" key (never a blank one) when the report is
// assembled, alongside the owned key.
func TestCostReportUnattributedRowViaEngine(t *testing.T) {
	now := time.Now()
	led := fakeSpendLedger{
		{SessionID: "abcd1234", Backend: "claude-code", StartedAt: now, TokensIn: 1000, TokensOut: 500},
		{SessionID: "ef012345", Backend: "ollama", StartedAt: now, TokensIn: 100, TokensOut: 50},
	}
	src := managerCostSource{
		ledger: led,
		owner:  map[string]costOwner{"abcd1234": {automaton: "prd-1"}},
	}
	rep, err := costreport.Build(
		config.CostReportConfig{},
		costreport.Request{GroupBy: costreport.GroupByAutomaton, Until: now.Add(time.Second)},
		src, nil, nil, nil, now,
	)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	keys := map[string]bool{}
	for _, row := range rep.Rows {
		if row.Key == "" {
			t.Errorf("blank row key is forbidden")
		}
		keys[row.Key] = true
	}
	want := []string{"prd-1", costreport.UnattributedKey}
	for _, k := range want {
		if !keys[k] {
			t.Errorf("row %q missing; got keys %v", k, keys)
		}
	}
}

// TestCostReportShippedUnitsFromManager — the ShippedSource reads the
// shipped state (shipped.go: release + merged_at) of PRDs and Automations
// and excludes unshipped work.
func TestCostReportShippedUnitsFromManager(t *testing.T) {
	am, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	now := time.Now()
	prd, err := am.CreatePRD("owned spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := am.RecordPRDShipped(prd.ID, "v1.2.0", now, "c1", autonomous.ShippedSourceMergeStep); err != nil {
		t.Fatalf("RecordPRDShipped: %v", err)
	}
	auto, err := am.CreateAutomation(autonomous.AutomationInput{Name: "nightly"}, "test")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	if _, err := am.RecordAutomationShipped(auto.ID, "v2.0.0", now, "c2", autonomous.ShippedSourceMergeStep); err != nil {
		t.Fatalf("RecordAutomationShipped: %v", err)
	}
	// An unshipped PRD must not surface as a unit.
	if _, err := am.CreatePRD("unshipped spec", t.TempDir(), "", "", ""); err != nil {
		t.Fatalf("CreatePRD(unshipped): %v", err)
	}

	src := managerCostSource{auto: am}
	res, err := src.ShippedUnits()
	if err != nil {
		t.Fatalf("ShippedUnits: %v", err)
	}
	if len(res.Units) != 2 {
		t.Fatalf("want 2 units, got %d: %+v", len(res.Units), res.Units)
	}
	byKind := map[string]costreport.ShippedUnit{}
	for _, u := range res.Units {
		byKind[u.Kind] = u
	}
	if u := byKind["prd"]; u.UnitID != prd.ID || u.Release != "v1.2.0" {
		t.Errorf("prd unit = %+v, want id %s release v1.2.0", u, prd.ID)
	}
	if u := byKind["automation"]; u.UnitID != auto.ID || u.Release != "v2.0.0" {
		t.Errorf("automation unit = %+v, want id %s release v2.0.0", u, auto.ID)
	}
}

// TestCostReportShippedUnitsEmptyWhenNotWired — no autonomous manager is
// not an error: the release grouping then reports only "unreleased".
func TestCostReportShippedUnitsEmptyWhenNotWired(t *testing.T) {
	res, err := managerCostSource{}.ShippedUnits()
	if err != nil {
		t.Fatalf("ShippedUnits: %v", err)
	}
	if res == nil || res.Units == nil {
		t.Fatalf("want non-nil empty result, got %v", res)
	}
	if len(res.Units) != 0 {
		t.Errorf("want 0 units, got %d", len(res.Units))
	}
}

// TestEarliestShippedRelease — the release pick between a PRD's own
// shipped state and its automation's: earliest shipment wins, a tie goes
// to the PRD, unshipped owners contribute nothing.
func TestEarliestShippedRelease(t *testing.T) {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	at := func(mo time.Duration) *time.Time { t := base.Add(mo); return &t }

	mk := func(shipped bool, release string, at *time.Time) *autonomous.PRD {
		p := &autonomous.PRD{ID: "p"}
		if shipped {
			p.MergedInRelease = release
			p.MergedAt = at
			p.MergeCommit = "c"
		}
		return p
	}
	mka := func(shipped bool, release string, at *time.Time) *autonomous.Automation {
		a := &autonomous.Automation{ID: "a"}
		if shipped {
			a.MergedInRelease = release
			a.MergedAt = at
			a.MergeCommit = "c"
		}
		return a
	}

	cases := []struct {
		name     string
		prd      *autonomous.PRD
		auto     *autonomous.Automation
		expected string
	}{
		{"neither shipped", mk(false, "", nil), mka(false, "", nil), ""},
		{"prd only", mk(true, "v1", at(0)), mka(false, "", nil), "v1"},
		{"automation only", mk(false, "", nil), mka(true, "v2", at(0)), "v2"},
		{"prd shipped first", mk(true, "v1", at(0)), mka(true, "v2", at(time.Hour)), "v1"},
		{"automation shipped first", mk(true, "v1", at(time.Hour)), mka(true, "v2", at(0)), "v2"},
		{"tie goes to the prd", mk(true, "v1", at(0)), mka(true, "v2", at(0)), "v1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := earliestShippedRelease(tc.prd, tc.auto); got != tc.expected {
				t.Errorf("earliestShippedRelease = %q, want %q", got, tc.expected)
			}
		})
	}
}

// fakeSpendLedger is a fixed in-memory spend ledger.
type fakeSpendLedger []spendledger.Entry

func (f fakeSpendLedger) Entries() []spendledger.Entry { return f }

// TestCostReportLedgerCountsDeletedSessions — an Automaton whose sessions
// no longer exist anywhere still gets a row from its ledger entries, and
// "unattributed" holds only entries with no automaton id.
func TestCostReportLedgerCountsDeletedSessions(t *testing.T) {
	now := time.Now()
	led := fakeSpendLedger{
		{SessionID: "gone0001", AutomatonID: "prd-gone", TaskID: "t1", Role: spendledger.RoleWorker, Backend: "claude-code", StartedAt: now, EndedAt: now, TokensIn: 700, TokensOut: 300, CacheRead: 50, ToolCalls: 4},
		{SessionID: "gone0002", AutomatonID: "prd-gone", TaskID: "t1", Role: spendledger.RoleVerification, Backend: "claude-code", StartedAt: now, EndedAt: now, TokensIn: 100, TokensOut: 20},
		{SessionID: "loose001", Role: spendledger.RoleOther, Backend: "ollama", StartedAt: now, EndedAt: now, TokensIn: 10, TokensOut: 5},
	}
	src := managerCostSource{ledger: led}
	rep, err := costreport.Build(config.CostReportConfig{},
		costreport.Request{GroupBy: costreport.GroupByAutomaton, Until: now.Add(time.Second)},
		src, nil, nil, nil, now)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	rows := map[string]costreport.Row{}
	for _, r := range rep.Rows {
		rows[r.Key] = r
	}
	gone, ok := rows["prd-gone"]
	if !ok {
		t.Fatalf("no row for the Automaton with deleted sessions; rows=%v", rows)
	}
	if gone.TokensIn != 800 || gone.TokensOut != 320 {
		t.Errorf("prd-gone tokens = %d/%d, want 800/320", gone.TokensIn, gone.TokensOut)
	}
	un, ok := rows[costreport.UnattributedKey]
	if !ok {
		t.Fatalf("no unattributed row; rows=%v", rows)
	}
	if un.TokensIn != 10 || un.TokensOut != 5 {
		t.Errorf("unattributed = %d/%d, want only the no-automaton entry 10/5", un.TokensIn, un.TokensOut)
	}
}
