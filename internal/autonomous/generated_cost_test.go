package autonomous

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/spendledger"
)

type fakeEntries []spendledger.Entry

func (f fakeEntries) Entries() []spendledger.Entry { return f }

func TestLedgerCostAttach(t *testing.T) {
	now := time.Now()
	l := NewSpendLedgerCost(fakeEntries{
		{SessionID: "s1", AutomatonID: "a1", Role: "worker", StartedAt: now.Add(-time.Hour), EndedAt: now, TokensIn: 100, TokensOut: 50, ToolCalls: 4},
		{SessionID: "s2", AutomatonID: "other", Role: "worker", StartedAt: now, TokensIn: 999},
	}, func() config.CostReportConfig { return config.CostReportConfig{} }, nil, nil)
	rec := &changerecord.ChangeRecord{AutomatonID: "a1"}
	if err := changerecord.AttachCost(rec, l); err != nil {
		t.Fatal(err)
	}
	c := rec.Cost
	if c == nil || c.Sessions != 1 || c.SessionIDs[0] != "s1" || c.TokensIn != 100 || c.TokensOut != 50 || c.ToolCalls != 4 {
		t.Fatalf("unexpected cost %+v", c)
	}
}
