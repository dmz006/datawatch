package autonomous

// Tests for the per-Automation auto-tagging override, the tag rendering it
// uses, and the system-tag protection.

import (
	"errors"
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
