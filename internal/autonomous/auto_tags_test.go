package autonomous

// Tests for the per-Automation auto-tagging override, the tag rendering it
// uses, and the system-tag protection.

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func autoTagsDaemonCfg() config.AutoTagsConfig {
	return config.AutoTagsConfig{}
}

func TestResolveAutoTags_NilOverrideKeepsTheDaemonBlock(t *testing.T) {
	r := ResolveAutoTags(autoTagsDaemonCfg(), nil)
	if !r.Enabled || r.AutomationTemplate != "automation:{automation}" ||
		r.StageTemplate != "stage:{stage}" || r.KindTemplate != "kind:{kind}" {
		t.Fatalf("nil override: %+v", r)
	}
	if r.KindLabels["other"] != "maintenance" {
		t.Fatalf("nil override lost the default kind labels: %v", r.KindLabels)
	}
}

func TestResolveAutoTags_OperativeFalseWins(t *testing.T) {
	daemon := autoTagsDaemonCfg()
	off := false
	daemon.Enabled = &off
	if ResolveAutoTags(daemon, nil).Enabled {
		t.Fatal("a daemon-level enabled: false must turn auto tagging off")
	}
	daemon.Enabled = nil
	over := &AutoTagsOverride{Enabled: &off}
	if ResolveAutoTags(daemon, over).Enabled {
		t.Fatal("an override enabled: false must win over the daemon's on")
	}
	on := true
	if got := ResolveAutoTags(daemon, &AutoTagsOverride{Enabled: &on}); !got.Enabled {
		t.Fatal("an override enabled: true must win over the daemon's off")
	}
}

func TestResolveAutoTags_UnsetFieldsInherit(t *testing.T) {
	daemon := autoTagsDaemonCfg()
	if daemon.KindTemplate == "" {
		daemon.KindTemplate = "kind:{kind}"
	}
	over := &AutoTagsOverride{StageTemplate: "phase:{stage}"}
	r := ResolveAutoTags(daemon, over)
	if r.StageTemplate != "phase:{stage}" {
		t.Fatalf("override stage_template = %q", r.StageTemplate)
	}
	if r.AutomationTemplate != "automation:{automation}" || r.KindTemplate != "kind:{kind}" {
		t.Fatalf("unset fields must inherit the daemon's: %+v", r)
	}
}

func TestValidateAutoTagsOverride(t *testing.T) {
	if err := validateAutoTagsOverride(nil); err != nil {
		t.Fatalf("nil override: %v", err)
	}
	if err := validateAutoTagsOverride(&AutoTagsOverride{StageTemplate: "phase:{stage}"}); err != nil {
		t.Fatalf("a good template: %v", err)
	}
	if err := validateAutoTagsOverride(&AutoTagsOverride{StageTemplate: "stage:{owner}"}); err == nil {
		t.Fatal("an unknown placeholder must be refused")
	}
}

func TestRenderAutoTag(t *testing.T) {
	r := ResolveAutoTags(autoTagsDaemonCfg(), nil)
	vars := func(automation, stage, kind string) map[string]string {
		return map[string]string{"automation": automation, "stage": stage, "kind": kind}
	}
	if got, err := RenderAutoTag(r, "automation", vars("A1", "", "")); err != nil || got != "automation:A1" {
		t.Fatalf("automation tag = %q, %v", got, err)
	}
	if got, err := RenderAutoTag(r, "stage", vars("", "build", "")); err != nil || got != "stage:build" {
		t.Fatalf("stage tag = %q, %v", got, err)
	}
	// The kind vocabulary never leaks: other renders as the kind label.
	if got, err := RenderAutoTag(r, "kind", vars("", "", "other")); err != nil || got != "kind:maintenance" {
		t.Fatalf("kind tag for other = %q, %v (want kind:maintenance)", got, err)
	}
	if got, err := RenderAutoTag(r, "kind", vars("", "", "feature")); err != nil || got != "kind:feature" {
		t.Fatalf("kind tag for feature = %q, %v", got, err)
	}
	// A daemon kind_labels entry renames {kind}.
	daemon := autoTagsDaemonCfg()
	daemon.KindLabels = map[string]string{"other": "support"}
	rs := ResolveAutoTags(daemon, nil)
	if got, _ := RenderAutoTag(rs, "kind", vars("", "", "other")); got != "kind:support" {
		t.Fatalf("kind tag for other with kind_labels = %q, want kind:support", got)
	}
	// A template that renders to nothing with the concrete value missing is refused.
	rEmpty := ResolveAutoTags(autoTagsDaemonCfg(), &AutoTagsOverride{StageTemplate: "{stage}"})
	if _, err := RenderAutoTag(rEmpty, "stage", map[string]string{}); err == nil {
		t.Fatal("an empty rendering must be refused")
	}
	if _, err := RenderAutoTag(r, "what", vars("a", "b", "c")); err == nil {
		t.Fatal("an unknown what must be refused")
	}
}

func TestRenderAutoTag_DisabledIsARefusal(t *testing.T) {
	off := false
	daemon := config.AutoTagsConfig{Enabled: &off}
	if _, err := RenderAutoTag(ResolveAutoTags(daemon, nil), "automation", map[string]string{"automation": "A1"}); err == nil {
		t.Fatal("auto tagging off must refuse the tag")
	}
}

func TestTagSystem_RefusedRenameReparentAndDelete(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	tg := mustTag(t, m, "automation:A1", "")
	tg.System = true
	if err := st.SaveTag(tg); err != nil {
		t.Fatalf("mark the tag system: %v", err)
	}
	if _, err := m.RenameTag(tg.ID, "elsewhere"); !errors.Is(err, ErrTagSystem) {
		t.Fatalf("RenameTag on a system tag: %v, want ErrTagSystem", err)
	}
	if _, err := m.SetTagParent(tg.ID, "somewhere"); !errors.Is(err, ErrTagSystem) {
		t.Fatalf("SetTagParent on a system tag: %v, want ErrTagSystem", err)
	}
	if err := m.DeleteTag(tg.ID); !errors.Is(err, ErrTagSystem) {
		t.Fatalf("DeleteTag on a system tag: %v, want ErrTagSystem", err)
	}
	// Color and description stay editable.
	if _, err := m.SetTagColor(tg.ID, "#112233"); err != nil {
		t.Fatalf("SetTagColor on a system tag: %v", err)
	}
	if _, err := m.SetTagDescription(tg.ID, "created by datawatch"); err != nil {
		t.Fatalf("SetTagDescription on a system tag: %v", err)
	}
	// The tag is still there, still named, and still a root.
	got, _ := m.requireTag(tg.ID)
	if got.Name != "automation:A1" || got.ParentID != "" || !got.System || got.Color != "#112233" || got.Description != "created by datawatch" {
		t.Fatalf("system tag after the attempts: %+v", got)
	}
}

func TestAutomationInput_AutoTagsOverrideSurvives(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := makeAutomationMemberPRDs(t, m, 1)[0]
	over := &AutoTagsOverride{StageTemplate: "phase:{stage}"}
	a, err := m.CreateAutomation(AutomationInput{
		Name:     "tagged",
		Members:  []AutomationMember{{PRDID: p.ID}},
		AutoTags: over,
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	if a.AutoTags == nil || a.AutoTags.StageTemplate != "phase:{stage}" {
		t.Fatalf("created automation lost its auto_tags override: %+v", a.AutoTags)
	}
	// An update that does not name the override keeps the stored one.
	rerun := []AutomationMember{{PRDID: p.ID}}
	updated, err := m.UpdateAutomation(a.ID, a.Version, AutomationInput{
		Name:          "tagged",
		EntryMemberID: a.EntryMemberID,
		Members:       rerun,
	}, "operator")
	if err != nil {
		t.Fatalf("UpdateAutomation: %v", err)
	}
	if updated.AutoTags == nil || updated.AutoTags.StageTemplate != "phase:{stage}" {
		t.Fatalf("an update without auto_tags lost the stored override: %+v", updated.AutoTags)
	}
}

// --- the engine side: syncAutoTags keeps the system tags in step with the
// Automation ---

// autoTagsManager builds a Manager with the daemon-level auto_tags block
// fixed at the given enabled state, read through the standard supplier.
func autoTagsManager(t *testing.T, on bool) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	m.SetAutoTagsDaemon(func() config.AutoTagsConfig { return config.AutoTagsConfig{Enabled: &on} })
	return m
}

// autoTagsPRD creates a PRD with the given title (feature unless the title
// carries a bug keyword) and returns it.
func autoTagsPRD(t *testing.T, m *Manager, title string) *PRD {
	t.Helper()
	prd, err := m.store.CreatePRD(title, "/p", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD %q: %v", title, err)
	}
	return prd
}

// autoTagsEngineCount counts the engine-owned tags (marker in the
// description).
func autoTagsEngineCount(m *Manager) int {
	n := 0
	for _, tg := range m.ListTags() {
		if strings.HasPrefix(tg.Description, autoTagMarkerPrefix) {
			n++
		}
	}
	return n
}

// mustAutoTag finds the engine tag by marker prefix (automation tag markers
// carry the Automation id, so match by prefix, not full description).
func mustAutoTag(t *testing.T, m *Manager, marker string) *Tag {
	t.Helper()
	want := autoTagMarkerPrefix + marker
	for _, tg := range m.ListTags() {
		if strings.HasPrefix(tg.Description, want) {
			return tg
		}
	}
	t.Fatalf("engine tag with marker %q not found (tags: %v)", marker, m.ListTags())
	return nil
}

// mustNotHaveAutoTag fails when an engine tag with the marker prefix exists.
func mustNotHaveAutoTag(t *testing.T, m *Manager, marker string) {
	t.Helper()
	want := autoTagMarkerPrefix + marker
	for _, tg := range m.ListTags() {
		if strings.HasPrefix(tg.Description, want) {
			t.Fatalf("engine tag with marker %q still exists: %q", marker, tg.Name)
		}
	}
}

// mustAutoTagStage finds the engine tag of one of a's stages: its marker is
// "stage <automation id> <stage>" (autoTagMarkerStage).
func mustAutoTagStage(t *testing.T, m *Manager, a *Automation, stage string) *Tag {
	t.Helper()
	return mustAutoTag(t, m, "stage "+a.ID+" "+stage)
}

// autoTagsPRDIDs returns the PRD's tag IDs from the store (fresh read).
func autoTagsPRDIDs(t *testing.T, m *Manager, prdID string) []string {
	t.Helper()
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		t.Fatalf("prd %q vanished", prdID)
	}
	return prd.TagIDs
}

// autoTagsHas fails unless want occurs exactly once in got.
func autoTagsHas(t *testing.T, label string, got []string, want string) {
	t.Helper()
	n := 0
	for _, id := range got {
		if id == want {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%s: tag %q occurs %d time(s) in %v, want exactly once", label, want, n, got)
	}
}

// autoTagsLacks fails if want occurs in got.
func autoTagsLacks(t *testing.T, label string, got []string, want string) {
	t.Helper()
	for _, id := range got {
		if id == want {
			t.Fatalf("%s: tag %q should not be in %v", label, want, got)
		}
	}
}

func TestSyncAutoTags_JoinCreatesSystemTagsPerMember(t *testing.T) {
	m := autoTagsManager(t, true)
	op := mustTag(t, m, "operator-note", "")
	feat := autoTagsPRD(t, m, "Ship the dark mode toggle")
	sec := autoTagsPRD(t, m, "Review the audit trail policy")
	for _, prd := range []*PRD{feat, sec} {
		prd.TagIDs = []string{op.ID}
		if err := m.store.SavePRD(prd); err != nil {
			t.Fatal(err)
		}
	}

	a, err := m.CreateAutomation(AutomationInput{
		Name:    "Alpha",
		Members: []AutomationMember{{PRDID: feat.ID, Stage: "draft"}, {PRDID: sec.ID, Stage: "review"}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}

	autoTag := mustAutoTag(t, m, "automation ")
	draftTag := mustAutoTagStage(t, m, a, "draft")
	reviewTag := mustAutoTagStage(t, m, a, "review")
	kindTag := mustAutoTag(t, m, "kind feature")
	for _, tg := range []*Tag{autoTag, draftTag, reviewTag, kindTag} {
		if !tg.System || tg.ParentID != "" {
			t.Fatalf("%q must be a system tag under the root: %+v", tg.Name, tg)
		}
	}
	if autoTag.Name != "automation:Alpha" || draftTag.Name != "stage:draft" ||
		reviewTag.Name != "stage:review" || kindTag.Name != "kind:feature" {
		t.Fatalf("tag names: %+v %+v %+v %+v", autoTag, draftTag, reviewTag, kindTag)
	}

	fIDs := autoTagsPRDIDs(t, m, feat.ID)
	sIDs := autoTagsPRDIDs(t, m, sec.ID)
	for _, c := range []struct {
		label string
		ids   []string
		stage *Tag
	}{{"feature member", fIDs, draftTag}, {"policy member", sIDs, reviewTag}} {
		autoTagsHas(t, c.label, c.ids, op.ID)
		autoTagsHas(t, c.label, c.ids, autoTag.ID)
		autoTagsHas(t, c.label, c.ids, c.stage.ID)
		autoTagsHas(t, c.label, c.ids, kindTag.ID)
	}
	autoTagsLacks(t, "feature member", fIDs, reviewTag.ID)
	autoTagsLacks(t, "policy member", sIDs, draftTag.ID)

	// Idempotence: re-writing the same membership moves nothing and adds
	// no duplicate assignments.
	before := autoTagsEngineCount(m)
	updated, err := m.UpdateAutomation(a.ID, a.Version, AutomationInput{
		Name:          "Alpha",
		EntryMemberID: a.EntryMemberID,
		Members:       []AutomationMember{{PRDID: feat.ID, Stage: "draft"}, {PRDID: sec.ID, Stage: "review"}},
	}, "operator")
	if err != nil {
		t.Fatalf("UpdateAutomation: %v", err)
	}
	if autoTagsEngineCount(m) != before || len(updated.Members) != 2 {
		t.Fatalf("a repeat sync changed the fleet: %d engine tag(s), %d member(s)",
			autoTagsEngineCount(m), len(updated.Members))
	}
	fIDs = autoTagsPRDIDs(t, m, feat.ID)
	autoTagsHas(t, "after repeat", fIDs, autoTag.ID)
	autoTagsHas(t, "after repeat", fIDs, draftTag.ID)
	autoTagsHas(t, "after repeat", fIDs, kindTag.ID)
	autoTagsHas(t, "after repeat", fIDs, op.ID)
}

func TestSyncAutoTags_MemberMoveDropsTheOrphanedStageTag(t *testing.T) {
	m := autoTagsManager(t, true)
	op := mustTag(t, m, "operator-note", "")
	prd := autoTagsPRD(t, m, "Ship the dark mode toggle")
	prd.TagIDs = []string{op.ID}
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	a, err := m.CreateAutomation(AutomationInput{
		Name:    "Alpha",
		Members: []AutomationMember{{PRDID: prd.ID, Stage: "draft"}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	draftTag := mustAutoTagStage(t, m, a, "draft")
	autoTag := mustAutoTag(t, m, "automation ")
	kindTag := mustAutoTag(t, m, "kind feature")

	updated, err := m.UpdateAutomation(a.ID, a.Version, AutomationInput{
		Name:          "Alpha",
		EntryMemberID: a.EntryMemberID,
		Members:       []AutomationMember{{PRDID: prd.ID, Stage: "review"}},
	}, "operator")
	if err != nil {
		t.Fatalf("UpdateAutomation: %v", err)
	}
	_ = updated

	mustNotHaveAutoTag(t, m, "stage "+a.ID+" draft") // zero members: reclaimed
	reviewTag := mustAutoTagStage(t, m, a, "review")
	ids := autoTagsPRDIDs(t, m, prd.ID)
	autoTagsHas(t, "moved", ids, op.ID)
	autoTagsHas(t, "moved", ids, autoTag.ID)
	autoTagsHas(t, "moved", ids, reviewTag.ID)
	autoTagsHas(t, "moved", ids, kindTag.ID)
	autoTagsLacks(t, "moved", ids, draftTag.ID)
}

func TestSyncAutoTags_RenameStagePreservesTheTagID(t *testing.T) {
	m := autoTagsManager(t, true)
	prd := autoTagsPRD(t, m, "Ship the dark mode toggle")
	a, err := m.CreateAutomation(AutomationInput{
		Name:    "Alpha",
		Members: []AutomationMember{{PRDID: prd.ID, Stage: "draft"}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	draftTag := mustAutoTagStage(t, m, a, "draft")

	updated, err := m.RenameAutomationStage(a.ID, "draft", "review", a.Version)
	if err != nil {
		t.Fatalf("RenameAutomationStage: %v", err)
	}
	got := mustAutoTagStage(t, m, a, "review")
	if got.ID != draftTag.ID || got.Name != "stage:review" {
		t.Fatalf("stage tag was re-created, not migrated: %+v, want id %s", got, draftTag.ID)
	}
	mustNotHaveAutoTag(t, m, "stage "+a.ID+" draft")
	ids := autoTagsPRDIDs(t, m, prd.ID)
	autoTagsHas(t, "renamed", ids, draftTag.ID) // the same tag ID stays assigned
	if len(updated.Members) != 1 || updated.Members[0].Stage != "review" {
		t.Fatalf("member after rename: %+v", updated.Members)
	}
}

func TestSyncAutoTags_RenameStageRefusesAnOperatorTagWithThatName(t *testing.T) {
	m := autoTagsManager(t, true)
	prd := autoTagsPRD(t, m, "Ship the dark mode toggle")
	opTag, err := m.CreateTag("stage:review", "", "#123456", "my stage") // operator-owned name
	if err != nil {
		t.Fatalf("CreateTag stage:review: %v", err)
	}
	a, err := m.CreateAutomation(AutomationInput{
		Name:    "Alpha",
		Members: []AutomationMember{{PRDID: prd.ID, Stage: "draft"}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	draftTag := mustAutoTagStage(t, m, a, "draft")

	if _, err := m.RenameAutomationStage(a.ID, "draft", "review", a.Version); err == nil {
		t.Fatal("renaming onto an operator tag name must be refused")
	}
	// Loud refusal: the operator tag is untouched and the engine tag kept
	// its old identity.
	fresh, _ := m.store.GetTag(opTag.ID)
	if fresh == nil || fresh.Name != "stage:review" || fresh.Description != "my stage" || fresh.System {
		t.Fatalf("the operator tag changed: %+v", fresh)
	}
	if got := mustAutoTagStage(t, m, a, "draft"); got.ID != draftTag.ID {
		t.Fatalf("the engine stage tag changed on refusal: %+v", got)
	}
}

func TestSyncAutoTags_RenameAutomationKeepsTheTagIDs(t *testing.T) {
	m := autoTagsManager(t, true)
	op := mustTag(t, m, "operator-note", "")
	prd := autoTagsPRD(t, m, "Ship the dark mode toggle")
	prd.TagIDs = []string{op.ID}
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	a, err := m.CreateAutomation(AutomationInput{
		Name:    "Alpha",
		Members: []AutomationMember{{PRDID: prd.ID, Stage: "draft"}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	autoTag := mustAutoTag(t, m, "automation ")
	draftTag := mustAutoTagStage(t, m, a, "draft")
	before := autoTagsPRDIDs(t, m, prd.ID)

	updated, err := m.UpdateAutomation(a.ID, a.Version, AutomationInput{
		Name:          "Omega",
		EntryMemberID: a.EntryMemberID,
		Members:       []AutomationMember{{PRDID: prd.ID, Stage: "draft"}},
	}, "operator")
	if err != nil {
		t.Fatalf("UpdateAutomation: %v", err)
	}
	if updated.Name != "Omega" {
		t.Fatalf("renamed automation: %q", updated.Name)
	}
	got := mustAutoTag(t, m, "automation ")
	if got.ID != autoTag.ID || got.Name != "automation:Omega" {
		t.Fatalf("automation tag not re-rendered in place: %+v, want id %s", got, autoTag.ID)
	}
	if gotStage := mustAutoTagStage(t, m, a, "draft"); gotStage.ID != draftTag.ID {
		t.Fatalf("stage tag id changed: %+v, want id %s", gotStage, draftTag.ID)
	}
	beforeIDs := append([]string(nil), before...)
	sort.Strings(beforeIDs)
	nowIDs := append([]string(nil), autoTagsPRDIDs(t, m, prd.ID)...)
	sort.Strings(nowIDs)
	if !reflect.DeepEqual(nowIDs, beforeIDs) {
		t.Fatalf("PRD tag ids changed on a rename: %v, want %v", nowIDs, beforeIDs)
	}
}

func TestSyncAutoTags_KindChangeSwapsTheSharedKindTag(t *testing.T) {
	m := autoTagsManager(t, true)
	feat := autoTagsPRD(t, m, "Ship the dark mode toggle")
	bug := autoTagsPRD(t, m, "Fix the flaky login race") // a bug keyword: kind bug
	a, err := m.CreateAutomation(AutomationInput{
		Name:    "Alpha",
		Members: []AutomationMember{{PRDID: feat.ID, Stage: "draft"}, {PRDID: bug.ID, Stage: "draft"}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	featureTag := mustAutoTag(t, m, "kind feature")
	bugTag := mustAutoTag(t, m, "kind bug")
	if bugTag.ID == featureTag.ID {
		t.Fatal("feature and bug must be distinct shared tags")
	}
	autoTagsHas(t, "bug member", autoTagsPRDIDs(t, m, bug.ID), bugTag.ID)
	autoTagsHas(t, "feature member", autoTagsPRDIDs(t, m, feat.ID), featureTag.ID)

	// The explicit kind of the first member moves to bug: the shared tags
	// swap, the records survive (shared vocabulary is never re-created per
	// move).
	if err := m.SetPRDKind(feat.ID, PRDKindBug); err != nil {
		t.Fatalf("SetPRDKind: %v", err)
	}
	autoTagsHas(t, "re-kinded", autoTagsPRDIDs(t, m, feat.ID), bugTag.ID)
	autoTagsLacks(t, "re-kinded", autoTagsPRDIDs(t, m, feat.ID), featureTag.ID)
	mustAutoTag(t, m, "kind feature") // the record stays for the vocabulary

	// Other renders as its default label, maintenance.
	if err := m.SetPRDKind(bug.ID, PRDKindOther); err != nil {
		t.Fatalf("SetPRDKind other: %v", err)
	}
	maintTag := mustAutoTag(t, m, "kind maintenance")
	if maintTag.Name != "kind:maintenance" {
		t.Fatalf("kind:other rendered as %q, want kind:maintenance", maintTag.Name)
	}
	autoTagsHas(t, "kind other", autoTagsPRDIDs(t, m, bug.ID), maintTag.ID)
	autoTagsLacks(t, "kind other", autoTagsPRDIDs(t, m, bug.ID), bugTag.ID)
	if got, ok := m.GetAutomation(a.ID); !ok || got == nil {
		t.Fatal("automation vanished")
	}
}

func TestSyncAutoTags_MemberLeaveKeepsOperatorAndSharedTags(t *testing.T) {
	m := autoTagsManager(t, true)
	op := mustTag(t, m, "operator-note", "")
	feat := autoTagsPRD(t, m, "Ship the dark mode toggle")
	keeper := autoTagsPRD(t, m, "Review the audit trail policy")
	for _, prd := range []*PRD{feat, keeper} {
		prd.TagIDs = []string{op.ID}
		if err := m.store.SavePRD(prd); err != nil {
			t.Fatal(err)
		}
	}
	a, err := m.CreateAutomation(AutomationInput{
		Name:    "Alpha",
		Members: []AutomationMember{{PRDID: feat.ID, Stage: "draft"}, {PRDID: keeper.ID, Stage: "review"}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	autoTag := mustAutoTag(t, m, "automation ")
	draftTag := mustAutoTagStage(t, m, a, "draft")
	reviewTag := mustAutoTagStage(t, m, a, "review")
	kindTag := mustAutoTag(t, m, "kind feature")

	// The second member leaves; the entry member keeps the Automation alive.
	_, err = m.UpdateAutomation(a.ID, a.Version, AutomationInput{
		Name:          "Alpha",
		EntryMemberID: a.EntryMemberID,
		Members:       []AutomationMember{{PRDID: feat.ID, Stage: "draft"}},
	}, "operator")
	if err != nil {
		t.Fatalf("UpdateAutomation: %v", err)
	}
	if kept, ok := m.GetAutomation(a.ID); !ok || kept == nil {
		t.Fatal("automation vanished after a member left")
	}

	gone := autoTagsPRDIDs(t, m, keeper.ID)
	autoTagsHas(t, "leaver", gone, op.ID)      // operator tag survives
	autoTagsHas(t, "leaver", gone, kindTag.ID) // shared kind tag survives
	autoTagsLacks(t, "leaver", gone, autoTag.ID)
	autoTagsLacks(t, "leaver", gone, reviewTag.ID)

	// The Automation and its remaining member keep their engine tags.
	autoTagsHas(t, "keeper", autoTagsPRDIDs(t, m, feat.ID), autoTag.ID)
	autoTagsHas(t, "keeper", autoTagsPRDIDs(t, m, feat.ID), draftTag.ID)
	autoTagsHas(t, "keeper", autoTagsPRDIDs(t, m, feat.ID), kindTag.ID)

	mustNotHaveAutoTag(t, m, "stage "+a.ID+" review") // nobody on review: reclaimed
	mustAutoTagStage(t, m, a, "draft")                // still in use: kept
	mustAutoTag(t, m, "automation ")
}

func TestSyncAutoTags_OverrideOffThenOn(t *testing.T) {
	m := autoTagsManager(t, true) // daemon on; the override decides.
	op := mustTag(t, m, "operator-note", "")
	prd := autoTagsPRD(t, m, "Ship the dark mode toggle")
	prd.TagIDs = []string{op.ID}
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	off := false
	a, err := m.CreateAutomation(AutomationInput{
		Name:     "Alpha",
		Members:  []AutomationMember{{PRDID: prd.ID, Stage: "draft"}},
		AutoTags: &AutoTagsOverride{Enabled: &off},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	if n := autoTagsEngineCount(m); n != 0 {
		t.Fatalf("override off: %d engine tag(s) created, want 0", n)
	}
	ids := autoTagsPRDIDs(t, m, prd.ID)
	autoTagsHas(t, "override off", ids, op.ID)
	if len(ids) != 1 {
		t.Fatalf("override off: prd tags = %v, want the operator tag only", ids)
	}

	// A per-Automation flip back on re-creates the system tags.
	updated, err := m.UpdateAutomation(a.ID, a.Version, AutomationInput{
		Name:          "Alpha",
		EntryMemberID: a.EntryMemberID,
		Members:       []AutomationMember{{PRDID: prd.ID, Stage: "draft"}},
		AutoTags:      &AutoTagsOverride{}, // inherit the daemon's on
	}, "operator")
	if err != nil {
		t.Fatalf("UpdateAutomation: %v", err)
	}
	_ = updated
	if autoTagsEngineCount(m) != 3 {
		t.Fatalf("flip on: engine tags = %d, want 3", autoTagsEngineCount(m))
	}
	autoTagsHas(t, "flip on", autoTagsPRDIDs(t, m, prd.ID), op.ID)
	autoTagsHas(t, "flip on", autoTagsPRDIDs(t, m, prd.ID), mustAutoTag(t, m, "automation ").ID)
}

func TestSyncAutoTags_DaemonOffRecomputesAndReclaims(t *testing.T) {
	m := autoTagsManager(t, true)
	op := mustTag(t, m, "operator-note", "")
	prd := autoTagsPRD(t, m, "Ship the dark mode toggle")
	prd.TagIDs = []string{op.ID}
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	a, err := m.CreateAutomation(AutomationInput{
		Name:    "Alpha",
		Members: []AutomationMember{{PRDID: prd.ID, Stage: "draft"}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	if n := autoTagsEngineCount(m); n != 3 {
		t.Fatalf("engine tag count after create = %d, want 3", n)
	}

	// A live daemon flip off re-syncs every Automation.
	m.SetAutoTagsDaemon(func() config.AutoTagsConfig { off := false; return config.AutoTagsConfig{Enabled: &off} })
	m.SyncAutoTagsAll()

	// The shared kind tag is a fleet vocabulary: it stays after the daemon
	// flip; only this Automation's own tags were reclaimed.
	if n := autoTagsEngineCount(m); n != 1 {
		t.Fatalf("daemon off: %d engine tag(s) remain, want 1 (the shared kind tag)", n)
	}
	ids := autoTagsPRDIDs(t, m, prd.ID)
	autoTagsHas(t, "daemon off", ids, op.ID) // operator tag survives
	mustNotHaveAutoTag(t, m, "automation")
	mustNotHaveAutoTag(t, m, "stage "+a.ID+" draft")
	mustAutoTag(t, m, "kind feature") // the shared vocabulary record stays
	for _, id := range ids {
		tg, _ := m.store.GetTag(id)
		if tg == nil {
			continue
		}
		if strings.HasPrefix(tg.Description, autoTagMarkerPrefix) &&
			!strings.HasPrefix(tg.Description, autoTagMarkerPrefix+"kind") {
			t.Fatalf("daemon off: prd still carries engine tag %q", tg.Name)
		}
	}
}

func TestSyncAutoTags_DeleteClearsAndReclaims(t *testing.T) {
	m := autoTagsManager(t, true)
	op := mustTag(t, m, "operator-note", "")
	prd := autoTagsPRD(t, m, "Ship the dark mode toggle")
	prd.TagIDs = []string{op.ID}
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	a, err := m.CreateAutomation(AutomationInput{
		Name:    "Alpha",
		Members: []AutomationMember{{PRDID: prd.ID, Stage: "draft"}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	autoTag := mustAutoTag(t, m, "automation ")
	kindTag := mustAutoTag(t, m, "kind feature")

	if err := m.DeleteAutomation(a.ID); err != nil {
		t.Fatalf("DeleteAutomation: %v", err)
	}

	ids := autoTagsPRDIDs(t, m, prd.ID)
	autoTagsHas(t, "deleted", ids, op.ID)      // operator tag survives
	autoTagsHas(t, "deleted", ids, kindTag.ID) // shared kind tag survives
	autoTagsLacks(t, "deleted", ids, autoTag.ID)
	mustNotHaveAutoTag(t, m, "automation") // no Automation: its record is reclaimed
	mustNotHaveAutoTag(t, m, "stage "+a.ID+" draft")
}

func TestSyncAutoTags_RefusesToHijackAnOperatorTag(t *testing.T) {
	m := autoTagsManager(t, true)
	operatorTag := mustTag(t, m, "automation:Alpha", "") // an operator owns this name
	prd := autoTagsPRD(t, m, "Ship the dark mode toggle")

	_, err := m.CreateAutomation(AutomationInput{
		Name:    "Alpha",
		Members: []AutomationMember{{PRDID: prd.ID, Stage: "draft"}},
	}, "operator")
	if err == nil {
		t.Fatal("creating an Automation whose tag name an operator owns must be refused")
	}
	// Loud refusal: the operator tag is untouched and no engine tag exists.
	fresh, _ := m.store.GetTag(operatorTag.ID)
	if fresh == nil || fresh.Name != "automation:Alpha" || fresh.System || fresh.Description != "" {
		t.Fatalf("the operator tag changed: %+v", fresh)
	}
	if n := autoTagsEngineCount(m); n != 0 {
		t.Fatalf("%d engine tag(s) created on refusal, want 0", n)
	}
	if list := m.ListAutomations(); len(list) != 0 {
		t.Fatalf("%d Automation(s) persisted behind the refused tags, want 0", len(list))
	}
}
