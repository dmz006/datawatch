// generated_wrapping_lint.go — drops decomposed tasks whose whole purpose is
// an artefact the change record now generates (gate report, changelog entry,
// parity draft, testing-tracker rows, registry row).
package autonomous

import (
	"path"
	"regexp"
	"strconv"
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
		title:   genNounTitle(`gate[- ]reports?`),
		pathHit: func(p string) bool { return strings.HasPrefix(p, "docs/plans/reports/") },
	},
	genartifacts.ChangelogDraft: {
		noun: "the changelog entry", key: "changelog_draft",
		title:   genNounTitle(`(?:changelog(?:\.md)?(?:\s+(?:entry|entries|draft|section|line))?)`),
		pathHit: func(p string) bool { return strings.EqualFold(p, "CHANGELOG.md") },
	},
	genartifacts.ParityDraft: {
		noun: "the datawatch-app parity draft", key: "parity_draft",
		title:   genNounTitle(`(?:datawatch-app\s+)?parity\s+(?:issues?|drafts?)(?:\s+text)?`),
		pathHit: func(p string) bool { return strings.HasPrefix(p, "docs/plans/parity-issues/") },
	},
	genartifacts.TrackerRows: {
		noun: "the testing-tracker rows", key: "tracker_rows",
		title:   genNounTitle(`(?:testing[- ]tracker(?:\.md)?\s+rows?|testing[- ]tracker|tracker\s+rows?)`),
		pathHit: func(p string) bool { return p == "docs/testing-tracker.md" },
	},
	genartifacts.RegistryRow: {
		noun: "the registry row", key: "registry_row",
		title:   genNounTitle(`(?:(?:plans?\s+)?registry\s+rows?|plans?\s+(?:registry|index)(?:\s+row)?)`),
		pathHit: func(p string) bool { return p == "docs/plans/README.md" },
	},
}

// genNounTitle builds an anchored whole-title matcher: a write verb, optional
// determiner/adjective, the artefact noun phrase, and at most a trailing
// "for ..." clause or an "in/to <path>" target. A title that continues with
// any other word ("changelog parser", "gate report renderer", "tracker rows
// to the status table") describes code work and does not match.
func genNounTitle(noun string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)^\s*[a-z]+\s+(?:(?:the|a|an|new|generated)\s+)*` + noun +
		`(?:\s+(?:for|of)\s+.+|\s+(?:in|to|into)\s+\S*(?:/|\.md)\S*)?\s*\.?\s*$`)
}

// genSpecCodeRe flags a spec that names source files: such a task also
// changes code and is not a whole-artefact task.
var genSpecCodeRe = regexp.MustCompile(`(?i)\b[\w./-]+\.(?:go|js|ts|tsx|py|rs|java|sh|css|html|ya?ml|json|sql)\b`)

func genTitleHasArtefactVerb(title string) bool {
	if titleStartsWithWriteVerb(title) {
		return true
	}
	f := strings.Fields(title)
	if len(f) == 0 {
		return false
	}
	switch strings.ToLower(f[0]) {
	case "draft", "generate", "record", "file", "append", "prepare":
		return true
	}
	return false
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
// A task qualifies only when its whole purpose is the artefact: the title is
// a write verb plus the artefact noun and nothing else, the spec names no
// source files, and every listed file (if any) is a target of that same kind.
// Anything else is part of a larger change and is not recognised.
func ClassifyGeneratedTask(title, spec string, files []string) (genartifacts.Kind, bool) {
	if !genTitleHasArtefactVerb(title) || genSpecCodeRe.MatchString(spec) {
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
	// Every listed file must be a target of the same kind the title names.
	for _, f := range files {
		k, ok := generatedKindOfPath(f)
		if !ok || k != found {
			return "", false
		}
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

// generatedKindOrder lists the kinds in the order the planner guidance names
// them.
var generatedKindOrder = []genartifacts.Kind{
	genartifacts.GateReport, genartifacts.ChangelogDraft, genartifacts.ParityDraft,
	genartifacts.TrackerRows, genartifacts.RegistryRow,
}

// GeneratedArtefactGuidance builds the planner instruction text from the
// effective settings. It names exactly the enabled kinds; a kind that is off
// is omitted so a task for it is allowed. The combined-documentation rule is
// included unless combined_docs_task is off. Empty when nothing applies.
func GeneratedArtefactGuidance(e config.EffectiveGeneratedArtefactsSettings) string {
	var nouns []string
	for _, k := range generatedKindOrder {
		if generatedKindEnabled(e, k) {
			nouns = append(nouns, generatedKindTable[k].noun)
		}
	}
	var b strings.Builder
	if len(nouns) > 0 {
		b.WriteString("Generated artefacts: " + strings.Join(nouns, ", ") +
			" — the daemon generates these from the change record. Do not plan a task for any of them.\n")
	}
	if e.CombinedDocsTask {
		b.WriteString("Documentation: write at most one combined documentation task per Automaton for " +
			"documentation that needs judgment (how-to, definition, operator docs). Its \"files\" list " +
			"must cover every doc it writes. Do not plan one documentation task per surface.\n")
	}
	return b.String()
}

// generatedArtefactPlanSuffix is appended to the planner prompt. Empty when
// the feature is not wired.
func (m *Manager) generatedArtefactPlanSuffix() string {
	eff, wired := m.generatedArtefactSettings()
	if !wired {
		return ""
	}
	g := GeneratedArtefactGuidance(eff)
	if g == "" {
		return ""
	}
	return "\n\n" + g
}

var genDocTitleRe = regexp.MustCompile(`(?i)\b(?:docs?|documentation|how-?tos?|readme|operator guide|definitions?)\b`)

func genIsDocTask(t Task) bool {
	if t.Kind == TaskKindSmoke || t.Kind == TaskKindFinalGate {
		return false
	}
	if _, gen := generatedKindOfPath(firstOr(t.FilesPlanned)); gen && len(t.FilesPlanned) > 0 {
		return false
	}
	if genDocTitleRe.MatchString(t.Title) {
		return true
	}
	if len(t.FilesPlanned) == 0 {
		return false
	}
	for _, f := range t.FilesPlanned {
		n := normalizeGenPath(f)
		if !strings.HasPrefix(n, "docs/") && !strings.HasSuffix(strings.ToLower(n), ".md") {
			return false
		}
	}
	return true
}

func firstOr(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// LintCombinedDocs warns when a plan has more than one documentation task. It
// names the tasks and suggests the combined task; nothing is dropped.
func LintCombinedDocs(stories []Story) []string {
	var ids []string
	for _, st := range stories {
		for _, t := range st.Tasks {
			if genIsDocTask(t) {
				ids = append(ids, t.ID+" ("+t.Title+")")
			}
		}
	}
	if len(ids) < 2 {
		return nil
	}
	return []string{"plan has " + strconv.Itoa(len(ids)) + " documentation tasks: " + strings.Join(ids, ", ") +
		"; merge them into one combined documentation task whose files list covers every doc"}
}
