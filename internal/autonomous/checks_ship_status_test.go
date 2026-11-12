package autonomous

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func statusFixture() []*TriageRecord {
	t0 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	return []*TriageRecord{
		{PRDID: "p1", Notes: []TriageNote{
			{At: t0, Source: afterDoneTriageSource, Text: "after-done checklist (shadow): one"},
			{At: t0.Add(time.Hour), Source: shipTriageSource, Text: "ship step (on): git push origin main"},
			{At: t0.Add(2 * time.Hour), Source: "alert", Text: "unrelated"},
		}},
		{PRDID: "p2", Notes: []TriageNote{
			{At: t0.Add(3 * time.Hour), Source: registryTriageSource, Text: "registry mismatch"},
			{At: t0.Add(4 * time.Hour), Source: shipCIWatchTriageSource, Text: "v1.0.0: success"},
		}},
	}
}

func TestCollectStatusFindings_FiltersBySourceAndAutomaton(t *testing.T) {
	recs := statusFixture()
	all := CollectStatusFindings(recs, "", ChecksFindingSources)
	if len(all) != 2 || all[0].PRDID != "p2" || all[1].PRDID != "p1" {
		t.Fatalf("checks findings (newest first) = %+v", all)
	}
	one := CollectStatusFindings(recs, "p1", ShipFindingSources)
	if len(one) != 1 || one[0].Source != shipTriageSource {
		t.Fatalf("ship findings for p1 = %+v", one)
	}
	if got := CollectStatusFindings(recs, "nope", ShipFindingSources); got == nil || len(got) != 0 {
		t.Fatalf("an unknown automaton must give an empty, non-nil list: %#v", got)
	}
}

func TestBuildStatus_ReflectsInstalledPolicy(t *testing.T) {
	SetChecksConfig(config.AutonomousChecksConfig{AfterDone: "shadow"})
	SetShipConfig(config.AutonomousShipConfig{Push: "off", Remote: "up"})
	t.Cleanup(func() {
		SetChecksConfig(config.AutonomousChecksConfig{})
		SetShipConfig(config.AutonomousShipConfig{})
	})
	cs := BuildChecksStatus(statusFixture(), "", "")
	if cs.Policy.AfterDone != "shadow" || cs.Policy.PreStartVerifier != "on" || !cs.Policy.BlockWhenMissing {
		t.Fatalf("checks policy = %+v", cs.Policy)
	}
	ss := BuildShipStatus(statusFixture(), "", "")
	if ss.Policy.Push != "off" || ss.Policy.Remote != "up" || ss.Policy.CIWorkflows == nil {
		t.Fatalf("ship policy = %+v", ss.Policy)
	}
	if !strings.Contains(RenderChecksStatusText(cs), "after_done=shadow") ||
		!strings.Contains(RenderShipStatusText(ss), "push=off") {
		t.Fatal("rendered text lacks the policy")
	}
	if !strings.Contains(RenderShipStatusText(BuildShipStatus(nil, "", "")), "no findings recorded") {
		t.Fatal("empty findings must say so")
	}
}
