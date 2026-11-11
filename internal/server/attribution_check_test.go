package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/spendledger"
)

type fakeAttrLedger []spendledger.Entry

func (f fakeAttrLedger) Entries() []spendledger.Entry { return f }

func TestAttributionCheckStatus(t *testing.T) {
	l := fakeAttrLedger{
		{SessionID: "a", AutomatonID: "P1", TaskID: "t", Role: "worker"},
		{SessionID: "b", TaskID: "t2", Role: "worker"},
		{SessionID: "c", Role: "other"},
	}
	st := attributionStatusFrom(l)
	if st.AutomatonLaunched != 2 || st.Unattributed != 1 || st.OK || st.SessionIDs[0] != "b" {
		t.Fatalf("got %+v", st)
	}
}
