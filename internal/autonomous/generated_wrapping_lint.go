// generated_wrapping_lint.go — drops decomposed tasks whose whole purpose is
// an artefact the change record now generates (gate report, changelog entry,
// parity draft, testing-tracker rows, registry row).
package autonomous

import (
	"path"
	"regexp"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

// DroppedTask records one task removed by DropGeneratedTasks.
type DroppedTask struct {
	TaskID  string
	StoryID string
	Title   string
	Kind    genartifacts.Kind
	Reason  string
}

// generatedKindInfo — per kind: the human noun, config key, title pattern and
// the target-path matcher.
type generatedKindInfo struct {
	noun    string
	key     string
	title   *regexp.Regexp
	pathHit func(p string) bool
}

var generatedKindTable = map[genartifacts.Kind]generatedKindInfo{
	genartifacts.GateReport: {
		noun: "the gate report", key: "gate_report",
		title:   regexp.MustCompile(`(?i)\bgate[- ]report\b`),
		pathHit: func(p string) bool { return strings.HasPrefix(p, "docs/plans/reports/") },
	},
	genartifacts.ChangelogDraft: {
		noun: "the changelog entry", key: "changelog_draft",
		title:   regexp.MustCompile(`(?i)\bchangelog\b`),
		pathHit: func(p string) bool { return strings.EqualFold(p, "CHANGELOG.md") },
	},
	genartifacts.ParityDraft: {
		noun: "the datawatch-app parity draft", key: "parity_draft",
		title:   regexp.MustCompile(`(?i)\bparity[- ](?:issue|draft)s?\b|\bdatawatch-app parity\b`),
		pathHit: func(p string) bool { return strings.HasPrefix(p, "docs/plans/parity-issues/") },
	},
	genartifacts.TrackerRows: {
		noun: "the testing-tracker rows", key: "tracker_rows",
		title:   regexp.MustCompile(`(?i)\btesting[- ]tracker\b|\btracker rows?\b`),
		pathHit: func(p string) bool { return p == "docs/testing-tracker.md" },
	},
	genartifacts.RegistryRow: {
		noun: "the registry row", key: "registry_row",
		title:   regexp.MustCompile(`(?i)\bregistry rows?\b|\bplans? registry\b|\bplans? index\b`),
		pathHit: func(p string) bool { return p == "docs/plans/README.md" },
	},
}

func normalizeGenPath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	p = strings.TrimPrefix(path.Clean(p), "./")
	return p
}

// generatedKindOfPath returns the artefact kind a path belongs to.
func generatedKindOfPath(p string) (genartifacts.Kind, bool) {
	p = normalizeGenPath(p)
	for _, k := range genartifacts.Kinds() {
		if info, ok := generatedKindTable[k]; ok && info.pathHit(p) {
			return k, true
		}
	}
	return "", false
}

// ClassifyGeneratedTask reports the artefact kind a task exists to produce.
// A task qualifies only when its whole purpose is the artefact: every listed
// file is a target of one kind, or (no files listed) the title is a write verb
// naming that artefact. A task that lists any other file, or targets several
// kinds, is part of a larger change and is not recognised.
func ClassifyGeneratedTask(title, spec string, files []string) (genartifacts.Kind, bool) {
	if len(files) > 0 {
		var kind genartifacts.Kind
		for i, f := range files {
			k, ok := generatedKindOfPath(f)
			if !ok {
				return "", false
			}
			if i > 0 && k != kind {
				return "", false
			}
			kind = k
		}
		return kind, true
	}
	if !titleStartsWithWriteVerb(title) {
		return "", false
	}
	var found genartifacts.Kind
	n := 0
	for _, k := range genartifacts.Kinds() {
		if info, ok := generatedKindTable[k]; ok && info.title.MatchString(title) {
			found = k
			n++
		}
	}
	if n != 1 {
		return "", false
	}
	return found, true
}

func generatedKindEnabled(e config.EffectiveGeneratedArtefactsSettings, k genartifacts.Kind) bool {
	switch k {
	case genartifacts.GateReport:
		return e.GateReport
	case genartifacts.ChangelogDraft:
		return e.ChangelogDraft
	case genartifacts.ParityDraft:
		return e.ParityDraft
	case genartifacts.TrackerRows:
		return e.TrackerRows
	case genartifacts.RegistryRow:
		return e.RegistryRow
	}
	return false
}

// DropGeneratedTasks removes recognised generated-artefact tasks whose kind is
// enabled in flags. Smoke and final_gate tasks are never dropped. Dependencies
// on a dropped task are replaced by that task's own dependencies, and a story
// left with no tasks is removed (its dependants inherit its dependencies).
func DropGeneratedTasks(stories []Story, flags config.GeneratedArtefacts) ([]Story, []DroppedTask) {
	eff := flags.Effective()
	var dropped []DroppedTask
	droppedDeps := map[string][]string{} // dropped task/story id -> its deps

	// Pass 1: find drops.
	for _, st := range stories {
		for _, t := range st.Tasks {
			if t.Kind == TaskKindSmoke || t.Kind == TaskKindFinalGate {
				continue
			}
			k, ok := ClassifyGeneratedTask(t.Title, t.Spec, t.FilesPlanned)
			if !ok || !generatedKindEnabled(eff, k) {
				continue
			}
			info := generatedKindTable[k]
			dropped = append(dropped, DroppedTask{
				TaskID: t.ID, StoryID: st.ID, Title: t.Title, Kind: k,
				Reason: "dropped: " + info.noun + " is generated from the change record (set autonomous.generated_artefacts." + info.key + "=false to keep the task)",
			})
			droppedDeps[t.ID] = t.DependsOn
		}
	}
	if len(dropped) == 0 {
		return stories, nil
	}
	isDropped := map[string]bool{}
	for _, d := range dropped {
		isDropped[d.TaskID] = true
	}

	// Pass 2: remove tasks, then emptied stories.
	kept := make([]Story, 0, len(stories))
	for _, st := range stories {
		ns := st
		ns.Tasks = nil
		for _, t := range st.Tasks {
			if !isDropped[t.ID] {
				ns.Tasks = append(ns.Tasks, t)
			}
		}
		if len(st.Tasks) > 0 && len(ns.Tasks) == 0 {
			droppedDeps[st.ID] = st.DependsOn
			isDropped[st.ID] = true
			continue
		}
		kept = append(kept, ns)
	}

	// Pass 3: repair dependencies.
	var resolve func(ids []string, seen map[string]bool) []string
	resolve = func(ids []string, seen map[string]bool) []string {
		var out []string
		for _, id := range ids {
			if !isDropped[id] {
				out = append(out, id)
				continue
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, resolve(droppedDeps[id], seen)...)
		}
		return out
	}
	repair := func(self string, deps []string) []string {
		if len(deps) == 0 {
			return deps
		}
		hit := false
		for _, d := range deps {
			if isDropped[d] {
				hit = true
				break
			}
		}
		if !hit {
			return deps
		}
		var out []string
		seen := map[string]bool{self: true}
		for _, d := range resolve(deps, map[string]bool{}) {
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
		return out
	}
	for si := range kept {
		kept[si].DependsOn = repair(kept[si].ID, kept[si].DependsOn)
		for ti := range kept[si].Tasks {
			t := &kept[si].Tasks[ti]
			t.DependsOn = repair(t.ID, t.DependsOn)
		}
	}
	return kept, dropped
}

// dropGeneratedPlanTasks applies DropGeneratedTasks to a freshly decomposed
// plan using the wired settings. It returns the (possibly shorter) stories and
// one warning line per drop. Dormant when no settings provider is wired.
func (m *Manager) dropGeneratedPlanTasks(stories []Story) ([]Story, []string) {
	eff, wired := m.generatedArtefactSettings()
	if !wired {
		return stories, nil
	}
	b := func(v bool) *bool { return &v }
	flags := config.GeneratedArtefacts{
		ChangeRecord: b(eff.ChangeRecord), GateReport: b(eff.GateReport),
		ChangelogDraft: b(eff.ChangelogDraft), ParityDraft: b(eff.ParityDraft),
		TrackerRows: b(eff.TrackerRows), RegistryRow: b(eff.RegistryRow),
		CombinedDocsTask: b(eff.CombinedDocsTask),
	}
	kept, dropped := DropGeneratedTasks(stories, flags)
	var notes []string
	for _, d := range dropped {
		notes = append(notes, "task "+d.TaskID+" ("+d.Title+") "+d.Reason)
	}
	return kept, notes
}
