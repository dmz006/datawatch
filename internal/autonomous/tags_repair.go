package autonomous

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Tag repair: an Automaton or a session can point to a tag ID that has no
// record. RepairTags plans how to make each such ID good again and, when it is
// not a dry run, does it. It uses saved copies of tag records the caller gives
// it, and for auto tags the Automations on disk. It only adds records and moves
// assignments; it never deletes a record.

// Actions of a tag repair item.
const (
	// TagRepairRestore puts the record back under its own ID.
	TagRepairRestore = "restore"
	// TagRepairMerge moves the assignments of the missing ID to the tag that
	// has taken its place.
	TagRepairMerge = "merge"
	// TagRepairUnresolved leaves the ID as it is.
	TagRepairUnresolved = "unresolved"
)

// Sources of a tag repair item.
const (
	// TagRepairFromCopy is a saved copy of the record.
	TagRepairFromCopy = "copy"
	// TagRepairFromAutomations is an auto tag worked out from the Automation
	// whose members carry the ID. The name is the one the auto tag settings
	// give now, and the creation time is the time of the repair.
	TagRepairFromAutomations = "automations"
)

// TagRepairItem is what the repair does for one missing tag ID.
type TagRepairItem struct {
	TagID  string `json:"tag_id"`
	Action string `json:"action"`
	Source string `json:"source,omitempty"`
	Name   string `json:"name,omitempty"`
	// IntoTagID is the tag a merge moves the assignments to.
	IntoTagID string `json:"into_tag_id,omitempty"`
	// AutomatonIDs are the Automata that point to the ID.
	AutomatonIDs []string `json:"automaton_ids"`
	// Sessions counts the sessions that carry the ID as one of their own tags.
	Sessions int `json:"sessions"`
	// Reason says why an ID is unresolved.
	Reason string `json:"reason,omitempty"`
}

// TagRepairReport is the plan of a repair and, when DryRun is false, what was
// done.
type TagRepairReport struct {
	DryRun     bool `json:"dry_run"`
	CopiesRead int  `json:"copies_read"`
	// Missing counts the tag IDs with no record.
	Missing    int `json:"missing"`
	Restore    int `json:"restore"`
	Merge      int `json:"merge"`
	Unresolved int `json:"unresolved"`
	// Automata counts the Automata that point to a missing ID, and
	// AutomataLeft those that still do after the repair.
	Automata     int             `json:"automata"`
	AutomataLeft int             `json:"automata_left"`
	Items        []TagRepairItem `json:"items"`
	// UnusedCopies names the tags a copy holds that have no record and that
	// nothing points to. They are not restored.
	UnusedCopies []string `json:"unused_copies"`
}

// ParseTagCopies reads the tag records in a saved copy: the reply of the tag
// list, the registry's saved copy (both an object with a tags list), a JSON
// list of tags, or a tags file with one record on each line.
func ParseTagCopies(data []byte) ([]*Tag, error) {
	data = bytes.TrimSpace(data)
	var tags []*Tag
	var wrapped struct {
		Tags []*Tag `json:"tags"`
	}
	switch {
	case len(data) == 0:
		return nil, errors.New("tag copy is empty")
	case data[0] == '[':
		if err := json.Unmarshal(data, &tags); err != nil {
			return nil, fmt.Errorf("tag copy: %w", err)
		}
	case json.Unmarshal(data, &wrapped) == nil && wrapped.Tags != nil:
		tags = wrapped.Tags
	default:
		for n, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var t Tag
			if err := json.Unmarshal([]byte(line), &t); err != nil {
				return nil, fmt.Errorf("tag copy line %d: %w", n+1, err)
			}
			tags = append(tags, &t)
		}
	}
	for _, t := range tags {
		if t == nil || t.ID == "" || t.Name == "" {
			return nil, errors.New("tag copy: every tag needs an id and a name")
		}
	}
	if len(tags) == 0 {
		return nil, errors.New("tag copy holds no tags")
	}
	return tags, nil
}

// RepairTags plans the repair of every tag ID that an Automaton, a session or
// a tag's parent names and that has no record. With dryRun it changes nothing.
func (m *Manager) RepairTags(copies []*Tag, dryRun bool) (TagRepairReport, error) {
	m.tagRepairMu.Lock()
	defer m.tagRepairMu.Unlock()
	p := m.newTagRepairPlan(copies)
	p.fromCopies()
	p.fromAutomations()
	report := p.report(dryRun)
	if dryRun {
		return report, nil
	}
	return report, m.applyTagRepair(p, report)
}

// tagRepairPlan is the working state of one repair.
type tagRepairPlan struct {
	m          *Manager
	copiesRead int
	copies     map[string]*Tag
	present    map[string]*Tag
	// carriers maps a missing ID to the Automata that point to it. An ID only
	// sessions name has an empty set.
	carriers map[string]map[string]bool
	items    map[string]*TagRepairItem
	// restore holds the records the repair puts back, by ID.
	restore map[string]*Tag
}

func (m *Manager) newTagRepairPlan(copies []*Tag) *tagRepairPlan {
	p := &tagRepairPlan{m: m, copiesRead: len(copies), copies: map[string]*Tag{},
		present: map[string]*Tag{}, carriers: map[string]map[string]bool{},
		items: map[string]*TagRepairItem{}, restore: map[string]*Tag{}}
	for _, c := range copies {
		if c == nil || c.ID == "" || c.Name == "" {
			continue
		}
		// Of two copies of one record the one changed last wins.
		if have := p.copies[c.ID]; have == nil || c.UpdatedAt.After(have.UpdatedAt) {
			cp := *c
			p.copies[c.ID] = &cp
		}
	}
	for _, t := range m.ListTags() {
		p.present[t.ID] = t
	}
	need := func(id string) map[string]bool {
		if id == "" || p.present[id] != nil {
			return nil
		}
		if p.carriers[id] == nil {
			p.carriers[id] = map[string]bool{}
		}
		return p.carriers[id]
	}
	for _, prd := range m.store.ListPRDs() {
		for _, id := range append(append([]string(nil), prd.TagIDs...), prd.PrimaryTagID) {
			if set := need(id); set != nil {
				set[prd.ID] = true
			}
		}
	}
	for _, set := range m.sessionTagSets() {
		for _, id := range set {
			need(id)
		}
	}
	for _, t := range p.present {
		need(t.ParentID)
	}
	// The parent of a record a copy gives back is needed too.
	for added := true; added; {
		added = false
		for id := range p.carriers {
			c := p.copies[id]
			if c == nil || c.ParentID == "" || p.copies[c.ParentID] == nil {
				continue
			}
			if _, ok := p.carriers[c.ParentID]; !ok && need(c.ParentID) != nil {
				added = true
			}
		}
	}
	return p
}

// order returns the missing IDs, the one most Automata carry first.
func (p *tagRepairPlan) order() []string {
	ids := make([]string, 0, len(p.carriers))
	for id := range p.carriers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if a, b := len(p.carriers[ids[i]]), len(p.carriers[ids[j]]); a != b {
			return a > b
		}
		return ids[i] < ids[j]
	})
	return ids
}

// records returns the records the daemon holds and those the plan restores.
func (p *tagRepairPlan) records() []*Tag {
	out := make([]*Tag, 0, len(p.present)+len(p.restore))
	for _, t := range p.present {
		out = append(out, t)
	}
	for _, t := range p.restore {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// place decides what happens to missing ID id, which stands for the record
// rec: a merge when another tag has taken its place, else a restore.
func (p *tagRepairPlan) place(id, source string, rec *Tag) {
	item := &TagRepairItem{TagID: id, Source: source, Name: rec.Name}
	p.items[id] = item
	auto := IsAutoTagRecord(rec.Description)
	all := p.records()
	for _, t := range all {
		sameAuto := auto && t.Description == rec.Description
		sameOwn := !auto && !IsAutoTagRecord(t.Description) &&
			t.ParentID == rec.ParentID && strings.EqualFold(t.Name, rec.Name)
		if sameAuto || sameOwn {
			item.Action, item.IntoTagID, item.Name = TagRepairMerge, t.ID, t.Name
			return
		}
	}
	if auto {
		for _, t := range all {
			if t.Name == rec.Name {
				item.Action = TagRepairUnresolved
				item.Reason = fmt.Sprintf("another tag (%s) already has the name", t.ID)
				return
			}
		}
	}
	item.Action = TagRepairRestore
	p.restore[id] = rec
}

func (p *tagRepairPlan) fromCopies() {
	// Parents first, so a child whose parent merges can follow it.
	var ids []string
	done := map[string]bool{}
	var visit func(id string)
	visit = func(id string) {
		if done[id] {
			return
		}
		done[id] = true
		if _, missing := p.carriers[id]; !missing || p.copies[id] == nil {
			return
		}
		visit(p.copies[id].ParentID)
		ids = append(ids, id)
	}
	for _, id := range p.order() {
		visit(id)
	}
	for _, id := range ids {
		rec := *p.copies[id]
		if parent := rec.ParentID; parent != "" && p.present[parent] == nil && p.restore[parent] == nil {
			if it := p.items[parent]; it != nil && it.Action == TagRepairMerge {
				rec.ParentID = it.IntoTagID
			} else {
				rec.ParentID = "" // no record for the parent: the tag goes to the root
			}
		}
		p.place(id, TagRepairFromCopy, &rec)
	}
}

// tagSlot is the auto tag a missing ID stands for.
type tagSlot struct {
	automation *Automation // set for an automation or a stage tag
	stage      string      // set for a stage tag
	kind       string      // set for a kind tag
}

// tagFacts is what the Automations on disk say.
type tagFacts struct {
	automations []*Automation
	members     map[string]map[string]bool            // Automation ID -> member Automata
	stages      map[string]map[string]map[string]bool // Automation ID -> stage -> member Automata
	label       map[string]string                     // Automaton ID -> kind label
}

func (m *Manager) tagRepairFacts() tagFacts {
	f := tagFacts{members: map[string]map[string]bool{},
		stages: map[string]map[string]map[string]bool{}, label: map[string]string{}}
	if m.automations == nil {
		return f
	}
	resolved := ResolveAutoTags(m.autoTagsDaemonCfg(), nil)
	f.automations = m.ListAutomations()
	sort.Slice(f.automations, func(i, j int) bool { return f.automations[i].ID < f.automations[j].ID })
	for _, a := range f.automations {
		f.members[a.ID] = map[string]bool{}
		f.stages[a.ID] = map[string]map[string]bool{}
		for i := range a.Members {
			mm := a.Members[i]
			if mm.kind() != MemberKindPRD || mm.PRDID == "" {
				continue
			}
			f.members[a.ID][mm.PRDID] = true
			if mm.Stage != "" {
				if f.stages[a.ID][mm.Stage] == nil {
					f.stages[a.ID][mm.Stage] = map[string]bool{}
				}
				f.stages[a.ID][mm.Stage][mm.PRDID] = true
			}
		}
	}
	for _, prd := range m.store.ListPRDs() {
		f.label[prd.ID] = autoTagKindLabel(resolved, EffectiveKind(prd, nil))
	}
	return f
}

// idsWithin reports whether every ID of sub is in set.
func idsWithin(sub, set map[string]bool) bool {
	for k := range sub {
		if !set[k] {
			return false
		}
	}
	return true
}

func (f tagFacts) labels(set map[string]bool) []string {
	seen := map[string]bool{}
	for id := range set {
		if l := f.label[id]; l != "" {
			seen[l] = true
		}
	}
	return sortedBoolKeys(seen)
}

// automationTagCarriers returns, for Automation a, the Automata that carry its
// automation tag: a record the daemon holds, or an ID this plan has already
// taken for that tag. Each set is one tag ID.
func (p *tagRepairPlan) automationTagCarriers(a *Automation, taken map[string]tagSlot) []map[string]bool {
	var out []map[string]bool
	for id, slot := range taken {
		if slot.automation != nil && slot.automation.ID == a.ID && slot.stage == "" {
			out = append(out, p.carriers[id])
		}
	}
	marker := autoTagMarkerAutomation(a.ID)
	for _, t := range p.present {
		if t.Description != marker {
			continue
		}
		set := map[string]bool{}
		for _, prd := range p.m.store.ListPRDs() {
			for _, id := range prd.TagIDs {
				if id == t.ID {
					set[prd.ID] = true
				}
			}
		}
		out = append(out, set)
	}
	return out
}

// workOut returns the auto tag the missing ID id stands for, when the
// Automations leave only one answer. Otherwise it returns why not.
func (p *tagRepairPlan) workOut(f tagFacts, id string, taken map[string]tagSlot) (tagSlot, bool, string) {
	c := p.carriers[id]
	if len(c) == 0 {
		return tagSlot{}, false, "no Automaton carries it and no copy holds it"
	}
	var holders []*Automation
	spanned := map[string]bool{}
	for _, a := range f.automations {
		if idsWithin(c, f.members[a.ID]) {
			holders = append(holders, a)
		}
		for prd := range c {
			if f.members[a.ID][prd] {
				spanned[a.ID] = true
			}
		}
	}
	labels := f.labels(c)
	if len(holders) == 0 {
		if len(labels) == 1 && len(spanned) >= 2 {
			return tagSlot{kind: labels[0]}, true, ""
		}
		return tagSlot{}, false, "no one Automation holds every Automaton that carries it, and it is not a kind tag"
	}
	if len(holders) > 1 {
		return tagSlot{}, false, "more than one Automation holds every Automaton that carries it"
	}
	a := holders[0]
	stage := ""
	for _, s := range sortedStageKeys(f.stages[a.ID]) {
		set := f.stages[a.ID][s]
		if len(set) == len(c) && idsWithin(c, set) && len(c) < len(f.members[a.ID]) {
			stage = s
			break
		}
	}
	// A kind tag beside the automation tag: its Automata are exactly those of
	// one kind among the Automata that carry the automation tag.
	kind := ""
	identical := false
	for _, whole := range p.automationTagCarriers(a, taken) {
		if !idsWithin(c, whole) || len(labels) != 1 {
			continue
		}
		if len(whole) == len(c) {
			identical = true
			continue
		}
		same := 0
		for prd := range whole {
			if f.label[prd] == labels[0] {
				same++
			}
		}
		if same == len(c) {
			kind = labels[0]
		}
	}
	switch {
	case kind != "" && stage != "":
		return tagSlot{}, false, "it can be a stage tag or a kind tag"
	case kind != "":
		return tagSlot{kind: kind}, true, ""
	case identical:
		return tagSlot{}, false, "the same Automata carry the automation tag: it can be the kind tag or an older automation tag"
	case len(c) >= 2 && len(c) == len(f.members[a.ID]):
		return tagSlot{automation: a}, true, ""
	case len(labels) > 1 && stage != "":
		return tagSlot{automation: a, stage: stage}, true, ""
	case len(labels) > 1:
		return tagSlot{automation: a}, true, ""
	}
	return tagSlot{}, false, "every Automaton that carries it has one kind: it can be an automation, stage or kind tag"
}

func sortedStageKeys(m map[string]map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// record renders the auto tag of slot as the daemon would make it now.
func (p *tagRepairPlan) record(id string, slot tagSlot, now time.Time) (*Tag, error) {
	daemon := p.m.autoTagsDaemonCfg()
	var override *AutoTagsOverride
	if slot.automation != nil {
		override = slot.automation.AutoTags
	}
	resolved := ResolveAutoTags(daemon, override)
	resolved.Enabled = true // a lost record is named even where auto tags are off now
	var what, marker string
	vars := map[string]string{}
	switch {
	case slot.kind != "":
		what, marker = "kind", autoTagMarkerKind(slot.kind)
		vars["kind"] = slot.kind
	case slot.stage != "":
		what, marker = "stage", autoTagMarkerStage(slot.automation.ID, slot.stage)
		vars["stage"], vars["automation"] = slot.stage, slot.automation.Name
	default:
		what, marker = "automation", autoTagMarkerAutomation(slot.automation.ID)
		vars["automation"] = slot.automation.Name
	}
	var name string
	var err error
	if slot.kind != "" {
		// The label is already rendered, so the template takes it as it is.
		name, err = config.RenderAutoTagsTemplate(resolved.KindTemplate, vars)
	} else {
		name, err = RenderAutoTag(resolved, what, vars)
	}
	if err != nil {
		return nil, err
	}
	return &Tag{ID: id, Name: name, Description: marker, System: true, CreatedAt: now, UpdatedAt: now}, nil
}

func (p *tagRepairPlan) fromAutomations() {
	f := p.m.tagRepairFacts()
	taken := map[string]tagSlot{}
	reasons := map[string]string{}
	now := time.Now()
	// An answer can settle another ID, so go round until nothing changes.
	for changed := true; changed; {
		changed = false
		for _, id := range p.order() {
			if p.items[id] != nil {
				continue
			}
			slot, ok, why := p.workOut(f, id, taken)
			if !ok {
				reasons[id] = why
				continue
			}
			rec, err := p.record(id, slot, now)
			if err != nil {
				reasons[id] = err.Error()
				continue
			}
			taken[id] = slot
			p.place(id, TagRepairFromAutomations, rec)
			changed = true
		}
	}
	for _, id := range p.order() {
		if p.items[id] == nil {
			p.items[id] = &TagRepairItem{TagID: id, Action: TagRepairUnresolved, Reason: reasons[id]}
		}
	}
}

func (p *tagRepairPlan) report(dryRun bool) TagRepairReport {
	r := TagRepairReport{DryRun: dryRun, CopiesRead: p.copiesRead, Items: []TagRepairItem{}, UnusedCopies: []string{}}
	p.m.mu.Lock()
	counter := p.m.sessionTags
	p.m.mu.Unlock()
	all, left := map[string]bool{}, map[string]bool{}
	for _, id := range p.order() {
		item := *p.items[id]
		item.AutomatonIDs = sortedBoolKeys(p.carriers[id])
		if counter != nil {
			item.Sessions = counter.CountSessionsWithTag(id)
		}
		r.Missing++
		for _, prd := range item.AutomatonIDs {
			all[prd] = true
		}
		switch item.Action {
		case TagRepairRestore:
			r.Restore++
		case TagRepairMerge:
			r.Merge++
		default:
			r.Unresolved++
			for _, prd := range item.AutomatonIDs {
				left[prd] = true
			}
		}
		r.Items = append(r.Items, item)
	}
	r.Automata, r.AutomataLeft = len(all), len(left)
	for id, c := range p.copies {
		if _, missing := p.carriers[id]; !missing && p.present[id] == nil {
			r.UnusedCopies = append(r.UnusedCopies, c.Name+" ("+id+")")
		}
	}
	sort.Strings(r.UnusedCopies)
	return r
}

// applyTagRepair puts the records back, then moves the assignments of the
// merged IDs.
func (m *Manager) applyTagRepair(p *tagRepairPlan, r TagRepairReport) error {
	for _, item := range r.Items {
		if item.Action != TagRepairRestore {
			continue
		}
		if _, ok := m.store.GetTag(item.TagID); ok {
			continue
		}
		if err := m.store.SaveTag(p.restore[item.TagID]); err != nil {
			return fmt.Errorf("restore tag %q: %w", item.TagID, err)
		}
		m.emitTagEvent(TagEvent{Kind: TagEventCreated, TagID: item.TagID})
	}
	for _, item := range r.Items {
		if item.Action != TagRepairMerge {
			continue
		}
		if _, err := m.moveTagAssignmentsAt(item.TagID, item.IntoTagID, false); err != nil {
			return fmt.Errorf("merge tag %q into %q: %w", item.TagID, item.IntoTagID, err)
		}
		m.emitTagEvent(TagEvent{Kind: TagEventDeleted, TagID: item.TagID})
	}
	return nil
}
