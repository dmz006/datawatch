// Defect-queue batching: group queued defects into batches, build the planning
// record for a batch, and split a failed story out into its own Automaton.
//
// Grouping is driven only by the autonomous.defect_queue settings, read
// through the DefectQueueSource interface so tests can supply fakes (the
// real config.DefectQueueConfig satisfies it). Defects of different
// projects never share a batch: each project is grouped with its own
// resolved settings.
//
// Decompose hook note: BatchTemplateText and BatchGroupingHint return the
// text the decompose prompt builder should include for a defect batch. The
// manager appends per-PRD suffixes (rulesContextSuffix, verificationScriptSuffix)
// at its decompose prompt assembly, but the PRD model has no defect-batch kind
// to key a suffix on, so these are exported rather than wired in; a caller that
// knows the PRD is a defect batch appends both to the spec it hands to the
// decomposer. The defect reference of each story lives on PlanStory.DefectID
// because the Story model has no such field.
package autonomous

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// DefectQueueSource resolves the defect queue settings in force for a
// project. config.DefectQueueConfig implements it.
type DefectQueueSource interface {
	EffectiveForProject(project string) config.DefectQueueSettings
}

// Task kinds of the shared tasks a defect batch plan carries.
const (
	DefectTaskKindGate      = "defect_gate"
	DefectTaskKindDocs      = "defect_docs"
	DefectTaskKindChangelog = "defect_changelog"
	DefectTaskKindParity    = "defect_parity"
	DefectTaskKindFix       = "defect_fix"
	DefectTaskKindTest      = "defect_test"
)

// QueueDefect is one queued defect. A higher Priority is more urgent.
type QueueDefect struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Priority    int      `json:"priority"`
	Project     string   `json:"project"`
	Area        string   `json:"area,omitempty"`
	Files       []string `json:"files,omitempty"`
	Surfaces    []string `json:"surfaces,omitempty"`
}

// DefectBatch is a group of related defects.
type DefectBatch struct {
	ID           string        `json:"id"`
	Project      string        `json:"project"`
	Defects      []QueueDefect `json:"defects"`
	Priority     int           `json:"priority"`
	PriorityRule string        `json:"priority_rule"`
	// related is the grouping evidence used while building the batch.
	keys []string
	// unrelated is true when allow_unrelated let defects with no shared key in.
	allowUnrelated bool
}

// RecomputePriority sets Priority from the members by the batch's rule.
func (b *DefectBatch) RecomputePriority() {
	if len(b.Defects) == 0 {
		b.Priority = 0
		return
	}
	if b.PriorityRule == config.DefectPriorityFirstMember {
		b.Priority = b.Defects[0].Priority
		return
	}
	max := b.Defects[0].Priority
	for _, d := range b.Defects[1:] {
		if d.Priority > max {
			max = d.Priority
		}
	}
	b.Priority = max
}

// GroupDefects assigns defects to batches by the configured grouping keys.
// A defect joins the first open batch (below size_cap, same project) it is
// related to; with allow_unrelated it joins the first open batch whatever
// its relation. Batches come out in order of their first member, and
// members keep input order.
func GroupDefects(defects []QueueDefect, cfg DefectQueueSource) []DefectBatch {
	var out []DefectBatch
	var projects []string
	byProject := map[string][]QueueDefect{}
	for _, d := range defects {
		if _, ok := byProject[d.Project]; !ok {
			projects = append(projects, d.Project)
		}
		byProject[d.Project] = append(byProject[d.Project], d)
	}
	for _, p := range projects {
		s := cfg.EffectiveForProject(p).Batch
		capN := s.SizeCap
		if capN < 1 {
			capN = 1
		}
		var batches []*DefectBatch
		for _, d := range byProject[p] {
			var target *DefectBatch
			for _, b := range batches {
				if len(b.Defects) >= capN {
					continue
				}
				if s.AllowUnrelated || defectRelatedToBatch(d, b, s.GroupingKeys) {
					target = b
					break
				}
			}
			if target == nil {
				target = &DefectBatch{
					ID:             "batch-" + d.ID,
					Project:        p,
					PriorityRule:   s.PriorityRule,
					keys:           s.GroupingKeys,
					allowUnrelated: s.AllowUnrelated,
				}
				batches = append(batches, target)
			}
			target.Defects = append(target.Defects, d)
			target.RecomputePriority()
		}
		for _, b := range batches {
			out = append(out, *b)
		}
	}
	return out
}

func defectRelatedToBatch(d QueueDefect, b *DefectBatch, keys []string) bool {
	for _, m := range b.Defects {
		if defectsRelated(d, m, keys) {
			return true
		}
	}
	return false
}

func defectsRelated(a, b QueueDefect, keys []string) bool {
	for _, k := range keys {
		switch k {
		case config.DefectGroupProjectArea:
			if a.Project == b.Project && a.Area != "" && a.Area == b.Area {
				return true
			}
		case config.DefectGroupSharedFiles:
			if sharesEntry(a.Files, b.Files) {
				return true
			}
		case config.DefectGroupSharedSurfaces:
			if sharesEntry(a.Surfaces, b.Surfaces) {
				return true
			}
		}
	}
	return false
}

func sharesEntry(a, b []string) bool {
	set := make(map[string]struct{}, len(a))
	for _, x := range a {
		set[x] = struct{}{}
	}
	for _, y := range b {
		if _, ok := set[y]; ok {
			return true
		}
	}
	return false
}

// PlanStory is one story of a batch plan with the defect it fixes.
type PlanStory struct {
	// DefectID is the defect reference: which defect this story fixes.
	DefectID string `json:"defect_id"`
	Story    Story  `json:"story"`
}

// BatchPlan is the planning record for one batch: one story per defect and
// the shared tasks added once for the whole batch.
type BatchPlan struct {
	Batch   DefectBatch `json:"batch"`
	Stories []PlanStory `json:"stories"`
	// Shared holds the once-per-batch tasks: verification gate,
	// documentation, changelog entry, parity-or-locale.
	Shared []Task `json:"shared"`
}

func defectStoryID(defectID string) string { return "story-" + defectID }

// BuildBatchPlan produces the planning record for a batch.
func BuildBatchPlan(b DefectBatch) *BatchPlan {
	p := &BatchPlan{Batch: b}
	var coreIDs []string
	for _, d := range b.Defects {
		ps, ids := buildDefectStory(b.ID, d)
		p.Stories = append(p.Stories, ps)
		coreIDs = append(coreIDs, ids...)
	}
	p.Shared = sharedBatchTasks(b.ID, "shared", "this batch", coreIDs, true)
	return p
}

func buildDefectStory(prdID string, d QueueDefect) (PlanStory, []string) {
	sid := defectStoryID(d.ID)
	fix := Task{
		ID: "task-" + d.ID + "-fix", StoryID: sid, PRDID: prdID, Status: TaskPending,
		Kind:  DefectTaskKindFix,
		Title: "Fix: " + d.Title,
		Spec:  fmt.Sprintf("Fix defect %s: %s\n\n%s", d.ID, d.Title, d.Description),
	}
	tasks := []Task{fix}
	if len(d.Files) > 0 {
		tasks = append(tasks, Task{
			ID: "task-" + d.ID + "-test", StoryID: sid, PRDID: prdID, Status: TaskPending,
			Kind:      DefectTaskKindTest,
			Title:     "Regression test: " + d.Title,
			Spec:      "Add a regression test that fails without the fix for defect " + d.ID + " in or near: " + strings.Join(d.Files, ", "),
			DependsOn: []string{fix.ID},
		})
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	return PlanStory{
		DefectID: d.ID,
		Story: Story{
			ID: sid, PRDID: prdID, Status: StoryPending,
			Title:        fmt.Sprintf("Fix defect %s: %s", d.ID, d.Title),
			Description:  d.Description,
			Tasks:        tasks,
			FilesPlanned: append([]string(nil), d.Files...),
		},
	}, ids
}

// sharedBatchTasks builds the gate, docs, changelog and (optionally) parity
// tasks. They depend on coreIDs so they run after the fixes. idTag keeps IDs
// unique between a batch and a split-out automaton.
func sharedBatchTasks(prdID, idTag, scope string, coreIDs []string, parity bool) []Task {
	mk := func(suffix, kind, title, spec string, deps []string) Task {
		return Task{
			ID: "task-" + idTag + "-" + suffix, PRDID: prdID, Status: TaskPending,
			Kind: kind, Title: title, Spec: spec,
			DependsOn: append([]string(nil), deps...),
		}
	}
	gate := mk("gate", DefectTaskKindGate, "Verification gate",
		"Run the project's build and test suite for "+scope+" and report counts only (passed, failed, skipped); no logs or output bodies.", coreIDs)
	docs := mk("docs", DefectTaskKindDocs, "Update documentation",
		"Update the documentation affected by the fixes in "+scope+".", []string{gate.ID})
	chg := mk("changelog", DefectTaskKindChangelog, "Add changelog entry",
		"Add one changelog entry covering the fixes in "+scope+".", []string{docs.ID})
	out := []Task{gate, docs, chg}
	if parity {
		out = append(out, mk("parity", DefectTaskKindParity, "Parity and locale check",
			"Check parity across surfaces and locale strings for the fixes in "+scope+"; make any needed updates.", []string{docs.ID}))
	}
	return out
}

// SplitOutStory moves the story for storyID out of plan into its own new
// plan (a new Automaton). The new plan holds only that story, its core
// tasks and its own gate, docs and changelog tasks. The original keeps its
// other stories and shared tasks, with the dependencies on the moved tasks
// removed so they stay runnable, and its batch priority is recomputed.
func SplitOutStory(plan *BatchPlan, storyID string) (*BatchPlan, error) {
	idx := -1
	for i, s := range plan.Stories {
		if s.Story.ID == storyID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("story %q not in batch %s", storyID, plan.Batch.ID)
	}
	moved := plan.Stories[idx]
	var def QueueDefect
	di := -1
	for i, d := range plan.Batch.Defects {
		if d.ID == moved.DefectID {
			def, di = d, i
			break
		}
	}
	if di < 0 {
		return nil, fmt.Errorf("defect %q of story %q not in batch %s", moved.DefectID, storyID, plan.Batch.ID)
	}

	// New plan: a batch of one. Its ID is distinct from the original's even
	// when the split defect was the first member (which names the original).
	nb := DefectBatch{
		ID: "batch-split-" + def.ID, Project: plan.Batch.Project, Defects: []QueueDefect{def},
		PriorityRule: plan.Batch.PriorityRule, keys: plan.Batch.keys, allowUnrelated: plan.Batch.allowUnrelated,
	}
	nb.RecomputePriority()
	story := moved.Story
	story.PRDID = nb.ID
	story.Tasks = append([]Task(nil), moved.Story.Tasks...)
	coreIDs := make([]string, len(story.Tasks))
	for i := range story.Tasks {
		story.Tasks[i].PRDID = nb.ID
		coreIDs[i] = story.Tasks[i].ID
	}
	split := &BatchPlan{
		Batch:   nb,
		Stories: []PlanStory{{DefectID: def.ID, Story: story}},
		Shared:  sharedBatchTasks(nb.ID, "split-"+def.ID, "defect "+def.ID, coreIDs, false),
	}

	// Original plan: drop the story and defect, unhook shared dependencies.
	gone := map[string]bool{}
	for _, t := range moved.Story.Tasks {
		gone[t.ID] = true
	}
	plan.Stories = append(plan.Stories[:idx:idx], plan.Stories[idx+1:]...)
	plan.Batch.Defects = append(plan.Batch.Defects[:di:di], plan.Batch.Defects[di+1:]...)
	plan.Batch.RecomputePriority()
	for i := range plan.Shared {
		var keep []string
		for _, dep := range plan.Shared[i].DependsOn {
			if !gone[dep] {
				keep = append(keep, dep)
			}
		}
		plan.Shared[i].DependsOn = keep
	}
	return split, nil
}

// BatchTemplateText is the text describing the shape of a defect batch plan,
// for the decompose prompt builder.
func BatchTemplateText() string {
	return strings.Join([]string{
		"This Automaton fixes a batch of related defects. Shape the plan as follows:",
		"- One story per defect. The story title names the defect it fixes.",
		"- Each story has one or two core tasks: the fix, and a regression test when files are known.",
		"- Add these shared tasks exactly once for the whole batch, never per story:",
		"  1. a verification gate that runs the build and tests and reports counts only;",
		"  2. a documentation task;",
		"  3. a changelog entry task;",
		"  4. a parity-or-locale task.",
		"- Shared tasks depend on the core tasks of every story.",
		"- Keep stories independent so one failed story can be split out without blocking the rest.",
	}, "\n")
}

// BatchGroupingHint tells the decomposer how the batch was grouped, from the
// resolved batch settings.
func BatchGroupingHint(s config.DefectQueueBatchSettings) string {
	rel := "Only defects related by " + strings.Join(s.GroupingKeys, ", ") + " share a batch"
	if s.AllowUnrelated {
		rel = "Defects may share a batch even when unrelated (grouping keys: " + strings.Join(s.GroupingKeys, ", ") + ")"
	}
	return fmt.Sprintf("%s. A batch holds at most %d defects. Batch priority rule: %s.", rel, s.SizeCap, s.PriorityRule)
}

// batchSortedIDs is a small helper for stable test and log output.
func batchSortedIDs(b DefectBatch) []string {
	ids := make([]string, len(b.Defects))
	for i, d := range b.Defects {
		ids[i] = d.ID
	}
	sort.Strings(ids)
	return ids
}
