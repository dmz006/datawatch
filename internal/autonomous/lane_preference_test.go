package autonomous

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

func newLanePreferenceManager(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	return m, prd
}

func TestLanePreferenceNormalize(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "", true},
		{"any", "", true},
		{" ANY ", "", true},
		{"gpu", "gpu", true},
		{" GPU ", "gpu", true},
		{"Claude", "claude", true},
		{"cpu", "cpu", false},
		{"gpu,claude", "gpu,claude", false},
	} {
		got, ok := NormalizeLanePreference(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("NormalizeLanePreference(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLanePreferenceSetAndClear(t *testing.T) {
	m, prd := newLanePreferenceManager(t)
	revBefore, _ := m.Store().PRDRev(prd.ID)

	if err := m.SetPRDLanePreference(prd.ID, " GPU "); err != nil {
		t.Fatalf("SetPRDLanePreference: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.LanePreference != LanePreferenceGPU {
		t.Errorf("LanePreference = %q, want gpu", got.LanePreference)
	}
	if rev, _ := m.Store().PRDRev(prd.ID); rev != revBefore+1 {
		t.Errorf("Rev = %d, want %d", rev, revBefore+1)
	}

	if err := m.SetPRDLanePreference(prd.ID, "claude"); err != nil {
		t.Fatalf("set claude: %v", err)
	}
	got, _ = m.Store().GetPRD(prd.ID)
	if got.LanePreference != LanePreferenceClaude {
		t.Errorf("LanePreference = %q, want claude", got.LanePreference)
	}

	for _, clear := range []string{"any", ""} {
		if err := m.SetPRDLanePreference(prd.ID, "gpu"); err != nil {
			t.Fatal(err)
		}
		if err := m.SetPRDLanePreference(prd.ID, clear); err != nil {
			t.Fatalf("clear with %q: %v", clear, err)
		}
		got, _ = m.Store().GetPRD(prd.ID)
		if got.LanePreference != "" {
			t.Errorf("LanePreference after %q = %q, want empty", clear, got.LanePreference)
		}
	}
}

func TestLanePreferenceRecordsDecision(t *testing.T) {
	m, prd := newLanePreferenceManager(t)
	if err := m.SetPRDLanePreference(prd.ID, "gpu"); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if d := lastDecision(got, "set_lane_preference"); d == nil || d.Actor != "operator" || d.Note != "lane preference any -> gpu" {
		t.Errorf("decision = %+v", d)
	}
	if err := m.SetPRDLanePreference(prd.ID, "any"); err != nil {
		t.Fatal(err)
	}
	got, _ = m.Store().GetPRD(prd.ID)
	if d := lastDecision(got, "set_lane_preference"); d == nil || d.Note != "lane preference gpu -> any" {
		t.Errorf("clear decision = %+v", d)
	}
}

func TestLanePreferenceRefusesUnknownValue(t *testing.T) {
	m, prd := newLanePreferenceManager(t)
	revBefore, _ := m.Store().PRDRev(prd.ID)
	err := m.SetPRDLanePreference(prd.ID, "cpu")
	if err == nil {
		t.Fatal("expected an error for an unknown lane preference")
	}
	for _, word := range []string{"gpu", "claude", "any"} {
		if !strings.Contains(err.Error(), word) {
			t.Errorf("error %q does not name %q", err, word)
		}
	}
	if rev, _ := m.Store().PRDRev(prd.ID); rev != revBefore {
		t.Errorf("a refused value must not save: Rev %d -> %d", revBefore, rev)
	}
	if err := m.SetPRDLanePreference("nope", "gpu"); err == nil {
		t.Fatal("expected an error for an unknown id")
	}
}

// A writer that moves the revision between the read and the commit makes the
// commit stale; the setter retries and lands the change without losing the
// other writer's change.
func TestLanePreferenceStaleRevisionRetry(t *testing.T) {
	m, prd := newLanePreferenceManager(t)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = m.Store().CommitPRDIfRev(prd.ID, func() int { r, _ := m.Store().PRDRev(prd.ID); return r }(), func(p *PRD) bool {
				p.Decisions = append(p.Decisions, Decision{Kind: "other_writer"})
				return true
			})
		}
	}()
	var err error
	for i := 0; i < 50 && err == nil; i++ {
		pref := "gpu"
		if i%2 == 1 {
			pref = "claude"
		}
		err = m.SetPRDLanePreference(prd.ID, pref)
	}
	close(stop)
	wg.Wait()
	if err != nil && !errors.Is(err, ErrPRDStale) {
		t.Fatalf("unexpected error: %v", err)
	}

	// Deterministic part: a commit against an old revision is stale, and the
	// setter, which re-reads the revision, still succeeds afterwards.
	oldRev, _ := m.Store().PRDRev(prd.ID)
	if err := m.Store().CommitPRDIfRev(prd.ID, oldRev, func(p *PRD) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if err := m.Store().CommitPRDIfRev(prd.ID, oldRev, func(p *PRD) bool { return true }); !errors.Is(err, ErrPRDStale) {
		t.Fatalf("old revision commit = %v, want ErrPRDStale", err)
	}
	if err := m.SetPRDLanePreference(prd.ID, "gpu"); err != nil {
		t.Fatalf("setter after revision moved: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.LanePreference != "gpu" {
		t.Errorf("LanePreference = %q, want gpu", got.LanePreference)
	}
}

func TestLanePreferenceJSONRoundTrip(t *testing.T) {
	raw, err := json.Marshal(PRD{ID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "lane_preference") {
		t.Errorf("empty preference must be omitted: %s", raw)
	}
	raw, err = json.Marshal(PRD{ID: "p1", LanePreference: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"lane_preference":"claude"`) {
		t.Errorf("preference missing from %s", raw)
	}
	var back PRD
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.LanePreference != "claude" {
		t.Errorf("round trip = %q, want claude", back.LanePreference)
	}
}

func TestLanePreferenceOldRecordLoadsAsAny(t *testing.T) {
	var old PRD
	if err := json.Unmarshal([]byte(`{"id":"old1","spec":"s","kind":"bug"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.LanePreference != "" {
		t.Fatalf("LanePreference = %q, want empty", old.LanePreference)
	}
	if p, ok := NormalizeLanePreference(old.LanePreference); p != "" || !ok {
		t.Errorf("old record normalizes to %q, %v; want any", p, ok)
	}
}

func TestLanePreferenceTemplateInstantiateKeepsIt(t *testing.T) {
	m, prd := newLanePreferenceManager(t)
	tmpl, _ := m.Store().GetPRD(prd.ID)
	tmpl.IsTemplate = true
	tmpl.LanePreference = LanePreferenceGPU
	if err := m.Store().SavePRD(tmpl); err != nil {
		t.Fatal(err)
	}
	inst, err := m.InstantiateTemplate(prd.ID, nil, "operator")
	if err != nil {
		t.Fatalf("InstantiateTemplate: %v", err)
	}
	if inst.LanePreference != LanePreferenceGPU {
		t.Errorf("instantiated LanePreference = %q, want gpu", inst.LanePreference)
	}
}

func TestLanePreferenceAPISetter(t *testing.T) {
	m, prd := newLanePreferenceManager(t)
	a := &API{M: m}
	out, err := a.SetPRDLanePreference(prd.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := out.(*PRD); !ok || p.LanePreference != "claude" {
		t.Errorf("API result = %#v", out)
	}
	if _, err := a.SetPRDLanePreference(prd.ID, "cpu"); err == nil {
		t.Fatal("expected an error for an unknown lane preference")
	}
}

func TestLaneFillConfigDefaults(t *testing.T) {
	lf := DefaultConfig().LaneFill
	if lf.Mode != "shadow" || lf.EffectiveMode() != "shadow" {
		t.Fatalf("mode = %q / %q, want shadow", lf.Mode, lf.EffectiveMode())
	}
	if lf.EffectiveGPULanes() != 2 || lf.EffectiveClaudeLanes() != 3 || lf.EffectiveIdleAlertMinutes() != 30 ||
		lf.EffectiveDecomposeAheadMin() != 2 || lf.EffectivePollSeconds() != 60 {
		t.Fatalf("defaults wrong: %+v", lf)
	}
	if lf.EffectiveGPUBackend() != "pi-prompt" || lf.EffectiveClaudeBackend() != "claude-code" || lf.GPUModel != "" {
		t.Fatalf("backends wrong: %+v", lf)
	}
}

func TestLaneFillConfigEffectiveHelpers(t *testing.T) {
	var unset LaneFillConfig
	if unset.EffectiveMode() != "shadow" || unset.EffectivePollSeconds() != 60 ||
		unset.EffectiveGPUBackend() != "pi-prompt" || unset.EffectiveClaudeBackend() != "claude-code" {
		t.Fatalf("unset config resolves wrongly: %+v", unset)
	}
	if got := (LaneFillConfig{PollSeconds: 1}).EffectivePollSeconds(); got != 5 {
		t.Errorf("poll floor = %d, want 5", got)
	}
	if got := (LaneFillConfig{Mode: "ON"}).EffectiveMode(); got != "on" {
		t.Errorf("mode = %q, want on", got)
	}
	if got := (LaneFillConfig{ClaudeLanes: 0}).EffectiveClaudeLanes(); got != 0 {
		t.Errorf("explicit zero claude lanes = %d, want 0", got)
	}
}

func TestLaneFillConfigJSONAndLiveApply(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &API{M: m}
	if err := a.SetConfig(json.RawMessage(`{"lane_fill":{"mode":"on","claude_lanes":0,"gpu_lanes":4}}`)); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	lf := m.Config().LaneFill
	if lf.Mode != "on" || lf.ClaudeLanes != 0 || lf.GPULanes != 4 || lf.PollSeconds != 60 || lf.GPUBackend != "pi-prompt" {
		t.Fatalf("live lane_fill = %+v", lf)
	}
	raw, err := json.Marshal(m.Config())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"claude_lanes":0`) {
		t.Errorf("explicit zero missing from JSON: %s", raw)
	}
}
