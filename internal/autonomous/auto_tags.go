package autonomous

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// AutoTagsOverride is the per-Automation override of the daemon-level
// autonomous.auto_tags block. A nil override on an Automation means the
// daemon's block applies whole; when set, each unset field inherits from the
// daemon's. kind_labels stay daemon-global: kind labels are a shared
// vocabulary (feature → feature, bug → bug, other → maintenance) that should
// not differ between Automations, so the override carries only the three
// templates and the enable flag.

// AutoTagsOverride — the per-Automation override (see AutoTagsOverride above).
type AutoTagsOverride struct {
	// Enabled — when false, auto tagging is off for this Automation no matter
	// the daemon's block; nil (unset) inherits the daemon's setting.
	Enabled *bool `json:"enabled,omitempty"`
	// AutomationTemplate — override for the daemon's automation_template.
	AutomationTemplate string `json:"automation_template,omitempty"`
	// StageTemplate — override for the daemon's stage_template.
	StageTemplate string `json:"stage_template,omitempty"`
	// KindTemplate — override for the daemon's kind_template.
	KindTemplate string `json:"kind_template,omitempty"`
}

// ResolvedAutoTags is a fully-resolved auto-tagging setting: the daemon's
// autonomous.auto_tags block with an Automation's override layered on top.
type ResolvedAutoTags struct {
	Enabled            bool
	AutomationTemplate string
	StageTemplate      string
	KindTemplate       string
	KindLabels         map[string]string
}

// ResolveAutoTags merges the daemon's autonomous.auto_tags block and an
// Automation's override: an override that is nil keeps the daemon's block,
// and each of its unset fields (empty template, nil Enabled) inherits the
// daemon's. kind_labels are always the daemon's.
func ResolveAutoTags(daemonCfg config.AutoTagsConfig, override *AutoTagsOverride) ResolvedAutoTags {
	d := daemonCfg.Effective()
	out := ResolvedAutoTags{
		Enabled:            d.On(),
		AutomationTemplate: d.AutomationTemplate,
		StageTemplate:      d.StageTemplate,
		KindTemplate:       d.KindTemplate,
		KindLabels:         d.KindLabels,
	}
	if override == nil {
		return out
	}
	if override.Enabled != nil {
		out.Enabled = *override.Enabled
	}
	if override.AutomationTemplate != "" {
		out.AutomationTemplate = override.AutomationTemplate
	}
	if override.StageTemplate != "" {
		out.StageTemplate = override.StageTemplate
	}
	if override.KindTemplate != "" {
		out.KindTemplate = override.KindTemplate
	}
	if out.KindLabels == nil {
		out.KindLabels = map[string]string{}
	}
	return out
}

// validateAutoTagsOverride reports the override's problems; called from
// validateAutomation for the Automation and its members. An empty template
// is fine (it inherits the daemon's); a template that is present must be a
// usable one, so a typo in the placeholder is stored-loud, not applied-quiet.
func validateAutoTagsOverride(o *AutoTagsOverride) error {
	if o == nil {
		return nil
	}
	for field, tpl := range map[string]string{
		"automation_template": o.AutomationTemplate,
		"stage_template":      o.StageTemplate,
		"kind_template":       o.KindTemplate,
	} {
		if tpl == "" {
			continue
		}
		if err := config.ValidateAutoTagsTemplate(tpl); err != nil {
			return fmt.Errorf("auto_tags.%s: %v", field, err)
		}
	}
	return nil
}

// autoTagKindLabel is the tag value {kind} renders as for an Automaton kind:
// the daemon's kind_labels for that kind, then the kind's default label
// (feature → feature, bug → bug, other → maintenance), then the kind itself.
func autoTagKindLabel(resolved ResolvedAutoTags, kind string) string {
	if v, ok := resolved.KindLabels[kind]; ok {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	if v, ok := config.AutoTagsKindDefaultLabels[kind]; ok {
		return v
	}
	return kind
}

// RenderAutoTag renders one auto tag: what is "automation" (the Automation),
// "stage" or "kind". vars supplies the concrete automation/stage/kind names;
// a missing variable renders as the empty string, and the rendered name must
// then still be non-empty and newline-free (validated with the same rules as
// the config block, so a template that only renders empty when the concrete
// value is empty is stored-loud here as well).
func RenderAutoTag(resolved ResolvedAutoTags, what string, vars map[string]string) (string, error) {
	if !resolved.Enabled {
		return "", fmt.Errorf("auto tagging is not enabled")
	}
	if vars == nil {
		vars = map[string]string{}
	}
	var tpl string
	switch what {
	case "automation":
		tpl = resolved.AutomationTemplate
	case "stage":
		tpl = resolved.StageTemplate
	case "kind":
		tpl = resolved.KindTemplate
		vars["kind"] = autoTagKindLabel(resolved, vars["kind"])
	default:
		return "", fmt.Errorf("%q is not an auto tag (use automation, stage or kind)", what)
	}
	return config.RenderAutoTagsTemplate(tpl, vars)
}

// Engine sync. The Manager side of this feature: syncAutoTags materializes
// the three tags RenderAutoTag names — one per Automation, one per stage its
// members occupy, one per member's kind — as system tags (Tag.System,
// neutral color, parented under root) and keeps each member PRD carrying
// exactly its Automation's engine tags on top of whatever operator-owned
// tags it already carries. Renames move the tag record in place, so tag IDs
// and existing assignments survive; an Automation leaving the picture (or
// the feature turning off) removes the engine's footprints and reclaims the
// tag records nobody references anymore. Operator-owned tags are never
// written by the engine.

// autoTagMarkerPrefix marks a tag owned by the auto-tagging engine, stored
// in Tag.Description. It is followed by the slot: "automation <id>",
// "stage <id> <stage>" or "kind <label>". A tag without the prefix is
// operator-owned and the engine never rewrites it.
const autoTagMarkerPrefix = "datawatch:auto-tag:"

func autoTagMarkerAutomation(automationID string) string {
	return autoTagMarkerPrefix + "automation " + automationID
}

func autoTagMarkerStage(automationID, stage string) string {
	return autoTagMarkerPrefix + "stage " + automationID + " " + stage
}

func autoTagMarkerStagePrefix(automationID string) string {
	return autoTagMarkerPrefix + "stage " + automationID + " "
}

func autoTagMarkerKind(label string) string {
	return autoTagMarkerPrefix + "kind " + label
}

// SetAutoTagsDaemon wires the supplier of the daemon-level
// autonomous.auto_tags block that syncAutoTags resolves against. Read at
// call time, so a live config change takes effect on the next sync.
func (m *Manager) SetAutoTagsDaemon(f func() config.AutoTagsConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoTagsDaemon = f
}

func (m *Manager) autoTagsDaemonCfg() config.AutoTagsConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.autoTagsDaemon == nil {
		return config.AutoTagsConfig{}
	}
	return m.autoTagsDaemon()
}

// SyncAutoTagsAll re-syncs the engine tags of every Automation. Called when
// the daemon-level autonomous.auto_tags block changes live, so a
// flip-off/flip-on applies to existing Automations at once instead of
// waiting for their next membership edit. Best-effort: a per-Automation
// failure is logged and the rest still sync.
func (m *Manager) SyncAutoTagsAll() {
	if m.automations == nil {
		return
	}
	for _, a := range m.ListAutomations() {
		if err := m.syncAutoTags(a); err != nil {
			log.Printf("auto-tag sync %q: %v", a.Name, err)
		}
	}
}

// syncAutoTags reconciles one Automation's engine tags and writes them onto
// each of its member Automata's PRDs. A member PRD ends up with its
// operator tags plus this Automation's automation tag, its stage tag (when
// the member occupies one), and its kind tag. Renaming the Automation, one
// of its stages, or a kind's rendered label moves the tag record in place
// (same tag ID, same assignments). Turning the feature off for the
// Automation strips the engine footprints and reclaims the un-referenced
// records; the shared kind tags stay.
func (m *Manager) syncAutoTags(a *Automation) error {
	if a == nil {
		return nil
	}
	resolved := ResolveAutoTags(m.autoTagsDaemonCfg(), a.AutoTags)
	if !resolved.Enabled {
		return m.clearAutoTagsFor(a)
	}
	type taggedMember struct {
		prd   *PRD
		stage string
	}
	var members []taggedMember
	stageSet := map[string]bool{}
	kindSet := map[string]bool{}
	for i := range a.Members {
		mm := a.Members[i]
		if mm.kind() != MemberKindPRD || mm.PRDID == "" {
			continue
		}
		prd, ok := m.store.GetPRD(mm.PRDID)
		if !ok {
			continue // membership sync already refuses a missing Automaton
		}
		members = append(members, taggedMember{prd: prd, stage: mm.Stage})
		if mm.Stage != "" {
			stageSet[mm.Stage] = true
		}
		kindSet[autoTagKindLabel(resolved, EffectiveKind(prd, nil))] = true
	}
	// Slot one: the Automation tag, stable across renames.
	autoName, err := RenderAutoTag(resolved, "automation", map[string]string{
		"automation": a.Name,
	})
	if err != nil {
		return err
	}
	autoTag, err := m.ensureAutoTag(autoName, autoTagMarkerAutomation(a.ID))
	if err != nil {
		return err
	}
	// Slot two: one tag per occupied stage. A renamed stage's record is
	// migrated in place by the rename itself, so by the time we get here
	// each occupied stage already has its record or none at all.
	stageTags := map[string]*Tag{}
	for _, stage := range sortedBoolKeys(stageSet) {
		stageName, err := RenderAutoTag(resolved, "stage", map[string]string{
			"stage":      stage,
			"automation": a.Name,
		})
		if err != nil {
			return err
		}
		tag, err := m.ensureAutoTag(stageName, autoTagMarkerStage(a.ID, stage))
		if err != nil {
			return err
		}
		stageTags[stage] = tag
	}
	// Slot three: one tag per kind. Kind tags are a shared vocabulary — no
	// adoption, and they are never reclaimed here.
	kindTags := map[string]*Tag{}
	for _, label := range sortedBoolKeys(kindSet) {
		kindName, err := RenderAutoTag(resolved, "kind", map[string]string{
			"kind": label,
		})
		if err != nil {
			return err
		}
		tag, err := m.ensureAutoTag(kindName, autoTagMarkerKind(label))
		if err != nil {
			return err
		}
		kindTags[label] = tag
	}
	for _, mem := range members {
		desired := []string{autoTag.ID}
		if mem.stage != "" {
			if tag, ok := stageTags[mem.stage]; ok {
				desired = append(desired, tag.ID)
			}
		}
		if tag, ok := kindTags[autoTagKindLabel(resolved, EffectiveKind(mem.prd, nil))]; ok {
			desired = append(desired, tag.ID)
		}
		if err := m.applyAutoTags(mem.prd, a.ID, desired); err != nil {
			return err
		}
	}
	if len(members) == 0 {
		// The Automation carries no PRD member: its tag is no one's engine
		// tag anymore and goes with it, unless still carried elsewhere.
		if err := m.deleteOrphanAutoStageTags(a.ID, nil); err != nil {
			return err
		}
		return m.deleteUnusedAutoTag(autoTagMarkerAutomation(a.ID))
	}
	return m.deleteOrphanAutoStageTags(a.ID, stageSet)
}

// render... helpers below are the small shared pieces of the engine sync.

// sortedBoolKeys returns the map's keys in a deterministic order.
func sortedBoolKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sameStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// tagByMarker finds the engine tag owning exactly marker among all.
func tagByMarker(all []*Tag, marker string) *Tag {
	for _, t := range all {
		if t.Description == marker {
			return t
		}
	}
	return nil
}

// ensureAutoTag returns the engine tag that owns marker, renaming the
// record in place when it now renders under a different name (renames move
// the tag, they do not recreate it, so its ID and assignments survive).
// When no tag owns the marker yet, a tag that already carries the rendered
// name is a conflict and refuses the sync — the engines' system tags and
// operator tags are two trees that must not share a name; otherwise a fresh
// record is created.
func (m *Manager) ensureAutoTag(name, marker string) (*Tag, error) {
	all := m.ListTags()
	if t := tagByMarker(all, marker); t != nil {
		if t.Name != name {
			t.Name = name
			t.UpdatedAt = time.Now()
			if err := m.store.SaveTag(t); err != nil {
				return nil, err
			}
			m.emitTagEvent(TagEvent{Kind: TagEventRenamed, TagID: t.ID})
		}
		return t, nil
	}
	for _, t := range all {
		if t.Name == name {
			return nil, fmt.Errorf("auto-tags: tag name %q already exists and is not free for auto-tagging (rename the tag or change the auto_tags templates)", name)
		}
	}
	return m.createAutoTag(name, marker)
}

// createAutoTag writes the engine's own system tag record — neutral color,
// parented under root. The engine writes its records directly; the
// operator-side rule (ErrTagSystem) stays on the operator API.
func (m *Manager) createAutoTag(name, marker string) (*Tag, error) {
	now := time.Now()
	t := &Tag{
		ID:          newID(),
		Name:        name,
		Description: marker,
		System:      true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := m.store.SaveTag(t); err != nil {
		return nil, err
	}
	m.emitTagEvent(TagEvent{Kind: TagEventCreated, TagID: t.ID})
	return t, nil
}

// applyAutoTags makes prd's engine tagging right for this sync: keeper
// tags stay (in their order), engine tags from other Automations stay
// (their own sync owns them), engine kind tags outside desired are
// stripped (the kind slot is the shared vocabulary, and the PRD carries
// one kind only), and the desired set is added. It is a no-op when the
// result equals prd.TagIDs, and it goes through SetPRDTagsAndPrimary so
// the assignment event fires like any operator tagging does.
func (m *Manager) applyAutoTags(prd *PRD, automationID string, desired []string) error {
	if prd == nil {
		return nil
	}
	desiredSet := map[string]bool{}
	for _, id := range desired {
		desiredSet[id] = true
	}
	kindMarker := autoTagMarkerPrefix + "kind "
	autoMarker := autoTagMarkerAutomation(automationID)
	stagePrefix := autoTagMarkerStagePrefix(automationID)
	var kept []string
	var out []string
	for _, id := range prd.TagIDs {
		t, _ := m.store.GetTag(id)
		if t != nil && !desiredSet[id] && strings.HasPrefix(t.Description, autoTagMarkerPrefix) &&
			(strings.HasPrefix(t.Description, kindMarker) ||
				t.Description == autoMarker ||
				strings.HasPrefix(t.Description, stagePrefix)) {
			continue // an engine tag this sync no longer renders for this PRD
		}
		kept = append(kept, id)
	}
	out = append(kept, desired...)
	if sameStringSlices(out, prd.TagIDs) {
		return nil
	}
	return m.SetPRDTagsAndPrimary(prd.ID, out, nil)
}

// stripAutomationAutoTags removes one Automation's engine tags — its
// automation tag and its stage tags — from prd. The shared kind tag and
// every operator-owned tag stay put.
func (m *Manager) stripAutomationAutoTags(automationID string, prd *PRD) error {
	if prd == nil {
		return nil
	}
	drop := map[string]bool{}
	for _, t := range m.ListTags() {
		switch {
		case t.Description == autoTagMarkerAutomation(automationID),
			strings.HasPrefix(t.Description, autoTagMarkerStagePrefix(automationID)):
			drop[t.ID] = true
		}
	}
	var kept []string
	changed := false
	for _, id := range prd.TagIDs {
		if drop[id] {
			changed = true
			continue
		}
		kept = append(kept, id)
	}
	if !changed {
		return nil
	}
	return m.SetPRDTagsAndPrimary(prd.ID, kept, nil)
}

// migrateAutoStageTag moves the record the stage from used (marker
// ".stage <id> <from>") to the stage to it renames to: same tag ID, same
// assignments, new name and marker — so the PRDs that carry it keep it
// without a single write. Refused when another tag already has the
// rendered name to. A missing record (the feature was off, or the stage
// was never tagged) is a no-op: the next sync creates it.
func (m *Manager) migrateAutoStageTag(automationID, from, to, toName string) error {
	t := tagByMarker(m.ListTags(), autoTagMarkerStage(automationID, from))
	if t == nil {
		return nil
	}
	for _, other := range m.ListTags() {
		if other.ID != t.ID && other.Name == toName {
			return fmt.Errorf("auto-tags: tag name %q already exists and is not free for auto-tagging (rename the tag or change the auto_tags templates)", toName)
		}
	}
	t.Name = toName
	t.Description = autoTagMarkerStage(automationID, to)
	t.UpdatedAt = time.Now()
	if err := m.store.SaveTag(t); err != nil {
		return err
	}
	m.emitTagEvent(TagEvent{Kind: TagEventRenamed, TagID: t.ID})
	return nil
}

// clearAutoTagsFor strips the Automation's engine tags from its members
// (the Automation is deleted, or auto tagging turned off for it, or off
// daemon-wide) and reclaims its un-referenced tag records. The shared kind
// tags are a fleet vocabulary and stay, as does every operator-owned tag.
func (m *Manager) clearAutoTagsFor(a *Automation) error {
	if a == nil {
		return nil
	}
	for i := range a.Members {
		mm := a.Members[i]
		if mm.PRDID == "" {
			continue
		}
		prd, ok := m.store.GetPRD(mm.PRDID)
		if !ok {
			continue
		}
		if err := m.stripAutomationAutoTags(a.ID, prd); err != nil {
			return err
		}
	}
	if err := m.deleteUnusedAutoTag(autoTagMarkerAutomation(a.ID)); err != nil {
		return err
	}
	return m.deleteOrphanAutoStageTags(a.ID, nil)
}

// deleteUnusedAutoTag deletes the tag owning marker when nothing references
// it anymore; a tag still carried by a live assignment (a PRD or a session
// that stores it as its own tag) keeps its record — the engine never
// deletes a tag out from under a live use.
func (m *Manager) deleteUnusedAutoTag(marker string) error {
	t := tagByMarker(m.ListTags(), marker)
	return m.deleteAutoTagRecord(t)
}

// deleteOrphanAutoStageTags deletes a's stage tags for stages no member
// occupies anymore, keeping the ones named in keep and the ones still in
// use elsewhere.
func (m *Manager) deleteOrphanAutoStageTags(automationID string, keep map[string]bool) error {
	prefix := autoTagMarkerStagePrefix(automationID)
	for _, t := range m.ListTags() {
		if !strings.HasPrefix(t.Description, prefix) {
			continue
		}
		stage := strings.TrimPrefix(t.Description, prefix)
		if keep[stage] {
			continue
		}
		if err := m.deleteAutoTagRecord(t); err != nil {
			return err
		}
	}
	return nil
}

// deleteAutoTagRecord deletes one engine tag record when it has neither
// children nor assignments; nil is a no-op.
func (m *Manager) deleteAutoTagRecord(t *Tag) error {
	if t == nil {
		return nil
	}
	if len(m.TagDescendants(t.ID)) > 0 {
		return nil // something grew underneath; the record stays
	}
	if m.tagInUseCount(t.ID) > 0 {
		return nil
	}
	if err := m.store.DeleteTag(t.ID); err != nil {
		return err
	}
	m.emitTagEvent(TagEvent{Kind: TagEventDeleted, TagID: t.ID})
	return nil
}
