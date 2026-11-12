package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAutomationMilestoneJSONRoundTrip(t *testing.T) {
	in := Automation{ID: "a1", Name: "arc", Milestone: true}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"milestone":true`) {
		t.Fatalf("milestone missing from %s", b)
	}
	var out Automation
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Milestone {
		t.Fatal("milestone lost on round trip")
	}
	if !cloneAutomation(&out).Milestone {
		t.Fatal("milestone lost on clone")
	}
}

func TestAutomationMilestoneOmittedWhenFalse(t *testing.T) {
	b, err := json.Marshal(Automation{ID: "a1", Name: "arc"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "milestone") {
		t.Fatalf("milestone should be omitted: %s", b)
	}
}

func TestAutomationMilestoneKeptAcrossStoreReload(t *testing.T) {
	dir := t.TempDir()
	prds := map[string]*PRD{"prd-ms-1": {ID: "prd-ms-1"}}
	s1, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatal(err)
	}
	created, err := s1.Create(&Automation{
		Name:      "milestone arc",
		Milestone: true,
		Members:   []AutomationMember{storeMember("m1", "prd-ms-1", "", "", "")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Milestone {
		t.Fatal("milestone dropped by Create")
	}
	// An update that mutates something else keeps the flag.
	if _, err := s1.Update(created.ID, created.Version, func(a *Automation) error {
		a.Description = "changed"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s2, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Get(created.ID)
	if !ok {
		t.Fatal("automation not reloaded")
	}
	if !got.Milestone {
		t.Fatal("milestone lost across store write and reload")
	}
}
