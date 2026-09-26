package autonomous

import (
	"testing"
)

func TestBootResume_PlanningPRDKillsItsOrphanDecomposeSession(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	other, _ := m.CreatePRD("s2", "/w/proj", "opencode", "", EffortNormal)
	for _, id := range []string{prd.ID, other.ID} {
		p, _ := m.Store().GetPRD(id)
		p.Status = PRDPlanning
		_ = m.Store().SavePRD(p)
	}
	live := map[string][]string{prd.ID: {"dec-1"}, other.ID: {"dec-2", "dec-3"}}
	var killed []string
	m.SetDecomposeSessionsFn(func(id string) []string { return live[id] })
	m.SetSessionKillerFn(func(id string) error { killed = append(killed, id); return nil })

	api.resumeRunningPRDs()

	if len(killed) != 3 {
		t.Fatalf("want all 3 orphan decompose sessions killed, got %v", killed)
	}
	for _, id := range []string{prd.ID, other.ID} {
		p, _ := m.Store().GetPRD(id)
		if p.Status != PRDDraft {
			t.Fatalf("prd %s should be reset to draft, got %s", id, p.Status)
		}
	}
}

func TestDecomposeRequest_CarriesPRDID(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	var got string
	m.decompose = func(r DecomposeRequest) (string, error) {
		got = r.PRDID
		return `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`, nil
	}
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatal(err)
	}
	if got != prd.ID {
		t.Fatalf("decompose request PRDID = %q, want %q", got, prd.ID)
	}
}
