package autonomous

import (
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

type fakeDormantRecords struct {
	seen        map[string]string
	comparisons int
}

func (f fakeDormantRecords) Exercised(key string) (bool, string, string) {
	ev, ok := f.seen[key]
	return ok, "2026-11-01T00:00:00Z", ev
}
func (f fakeDormantRecords) MergeShadowComparisons() int { return f.comparisons }

func dormantFixture() *config.Config {
	cfg := &config.Config{}
	cfg.Autonomous.Scan.FixLoopEnabled = false
	cfg.Autonomous.Scan.RulesGraderEnabled = false
	cfg.Autonomous.ScopeBreadthEnabled = false
	cfg.Autonomous.DefaultQualityGates.Enabled = false
	cfg.Autonomous.RemoteReapEnabled = false
	cfg.Autonomous.Notify.Forwarding.Enabled = false
	cfg.Autonomous.SupervisorLoop = config.SupervisorLoopConfig{
		"supervisor_loop.run_merge_until_conflict.enabled": false}
	return cfg
}

var dormantSeven = []string{DormantKeyScanFixLoop, DormantKeyScanRulesGrader, DormantKeyScopeBreadth,
	DormantKeyQualityGates, DormantKeyRemoteReap, DormantKeyNotifyForward, DormantKeyMergeUntil}

func byKey(l []DormantFeature) map[string]DormantFeature {
	m := map[string]DormantFeature{}
	for _, f := range l {
		m[f.Key] = f
	}
	return m
}

func TestDormantListsAllSeven(t *testing.T) {
	m := byKey(ListDormantFeatures(dormantFixture(), nil))
	for _, k := range dormantSeven {
		f, ok := m[k]
		if !ok {
			t.Fatalf("%s not listed", k)
		}
		if f.Purpose == "" || f.Default == "" || f.State != DormantStateOff || f.Current != "off" {
			t.Errorf("%s bad row %+v", k, f)
		}
		if f.EverExercised || f.Evidence == "" {
			t.Errorf("%s should read not exercised: %+v", k, f)
		}
	}
	if m[DormantKeyScanFixLoop].Default != "off" {
		t.Errorf("fix loop default %q", m[DormantKeyScanFixLoop].Default)
	}
	if m[DormantKeyMergeUntil].Evidence != "no record found; no merge shadow comparison exists" {
		t.Errorf("merge evidence %q", m[DormantKeyMergeUntil].Evidence)
	}
}

func TestDormantTurningOneOnRemovesIt(t *testing.T) {
	cfg := dormantFixture()
	cfg.Autonomous.ScopeBreadthEnabled = true
	if _, ok := byKey(ListDormantFeatures(cfg, nil))[DormantKeyScopeBreadth]; ok {
		t.Fatal("enabled feature still listed")
	}
	cfg = dormantFixture()
	cfg.Autonomous.SupervisorLoop["supervisor_loop.run_merge_until_conflict.enabled"] = true
	if _, ok := byKey(ListDormantFeatures(cfg, nil))[DormantKeyMergeUntil]; ok {
		t.Fatal("enabled loop step still listed")
	}
}

func TestDormantShadowMode(t *testing.T) {
	cfg := dormantFixture()
	cfg.Autonomous.Automation.MergePendingMode = config.MergePendingModeShadow
	if f := byKey(ListDormantFeatures(cfg, nil))[DormantKeyMergePending]; f.State != DormantStateShadow {
		t.Fatalf("want shadow, got %+v", f)
	}
}

func TestDormantLeavesConfigIdentical(t *testing.T) {
	cfg := dormantFixture()
	before := dormantFixture()
	ListDormantFeatures(cfg, fakeDormantRecords{})
	if !reflect.DeepEqual(cfg, before) {
		t.Fatal("config changed")
	}
}

func TestDormantRecordFlipsEverExercised(t *testing.T) {
	rec := fakeDormantRecords{seen: map[string]string{DormantKeyScanFixLoop: "audit scan_fixloop"}, comparisons: 2}
	m := byKey(ListDormantFeatures(dormantFixture(), rec))
	f := m[DormantKeyScanFixLoop]
	if !f.EverExercised || f.Evidence != "audit scan_fixloop" || f.LastExercised == "" {
		t.Fatalf("not flipped: %+v", f)
	}
	if m[DormantKeyScopeBreadth].EverExercised {
		t.Fatal("unrelated feature flipped")
	}
}
