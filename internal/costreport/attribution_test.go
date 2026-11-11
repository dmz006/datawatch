package costreport

import (
	"testing"
	"time"
)

// fakeLedger builds ledger-shaped records: attributed, unattributed
// Automaton-launched (by task id and by role) and non-Automaton.
func fakeLedger(at time.Time) []Record {
	return []Record{
		{SessionID: "ok1", FromLedger: true, LedgerAutomatonID: "A1", TaskID: "t1", Role: "worker", At: at},
		{SessionID: "bad-task", FromLedger: true, TaskID: "t2", Role: "other", At: at},
		{SessionID: "bad-role", FromLedger: true, Role: "verification", At: at},
		{SessionID: "human", FromLedger: true, Role: "other", At: at},
		{SessionID: "blank", FromLedger: true, At: at},
		{SessionID: "notledger", TaskID: "t3", At: at},
	}
}

func TestAttributionCheck(t *testing.T) {
	at := time.Now()
	c := CheckAttribution(fakeLedger(at), at.Add(-time.Hour), at.Add(time.Hour), false)
	if c.AutomatonLaunched != 3 || c.Unattributed != 2 || c.OK {
		t.Fatalf("got %+v", c)
	}
	if len(c.SessionIDs) != 2 || c.SessionIDs[0] != "bad-task" || c.SessionIDs[1] != "bad-role" {
		t.Fatalf("ids %v", c.SessionIDs)
	}
}

func TestAttributionCheckClean(t *testing.T) {
	at := time.Now()
	c := CheckAttribution(fakeLedger(at)[:1], time.Time{}, time.Time{}, false)
	if !c.OK || c.Unattributed != 0 || c.AutomatonLaunched != 1 {
		t.Fatalf("got %+v", c)
	}
}

func TestAttributionCheckWindowAndPeer(t *testing.T) {
	at := time.Now()
	recs := []Record{
		{SessionID: "old", FromLedger: true, TaskID: "t", At: at.Add(-48 * time.Hour)},
		{SessionID: "peer", FromLedger: true, TaskID: "t", Peer: true, At: at},
	}
	c := CheckAttribution(recs, at.Add(-time.Hour), at.Add(time.Hour), false)
	if c.AutomatonLaunched != 0 {
		t.Fatalf("got %+v", c)
	}
}
