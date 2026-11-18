package genartifacts

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/changerecord"
)

// TestRenderEveryKindHostFree renders every artefact kind from a record whose
// session ids carry a fake host prefix and asserts the host never leaks while
// the bare ids still show in the gate report.
func TestRenderEveryKindHostFree(t *testing.T) {
	const host = "fakehost-test"
	rec := fullRecord()
	rec.Cost = &changerecord.CostSummary{
		Sessions:   2,
		SessionIDs: []string{host + "-aa11", host + "-bb22"},
		TokensIn:   100,
		TokensOut:  20,
	}
	for _, k := range Kinds() {
		got, _, err := Render(k, rec, Options{Root: t.TempDir()})
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		if strings.Contains(got, host) {
			t.Errorf("%s output contains host name %q:\n%s", k, host, got)
		}
		if k == GateReport {
			for _, id := range []string{"aa11", "bb22"} {
				if !strings.Contains(got, id) {
					t.Errorf("gate report missing bare id %q:\n%s", id, got)
				}
			}
		}
	}
}
