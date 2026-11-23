package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/capacity"
)

func TestOverLimitFindingRaisedOnceAndDeduped(t *testing.T) {
	var got []audit.Entry
	a := NewCapacityAudit(func(e audit.Entry) { got = append(got, e) })
	over := []capacity.OverLimit{{Pool: "node:gpu1", Kind: "node", Name: "gpu1", Held: 3, Limit: 1}}
	a.OverLimitFinding(over)
	a.OverLimitFinding(over)
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	a.OverLimitFinding(nil) // back under the cap: forgotten
	a.OverLimitFinding(over)
	if len(got) != 2 {
		t.Fatalf("want re-raise after recovery, got %d", len(got))
	}
}
