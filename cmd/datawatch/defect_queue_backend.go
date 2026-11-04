package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server"
)

// defectQueueBackend is the production server.DefectQueueBackend (and so
// autonomous.DefectIntakeStore) for the defect queue REST/MCP/CLI/comm
// surfaces. It keeps a batch as an ordinary draft Automaton whose first spec
// line carries the batch metadata (project and area), one story per defect
// and one shared story holding the gate, documentation, changelog and parity
// tasks. A tiny fix is an ordinary one-task draft Automaton. Settings are
// read live through queueCfg on every call, so a runtime config set applies
// with no restart.
//
// Batches and tiny fixes are created through the Store directly (not
// Manager.CreatePRD), because Manager.CreatePRD runs the kind-bug defect
// intake hook (autonomous.defect_intake_automation) and a defect-queue
// Automaton must not be offered to that Automation a second time.
type defectQueueBackend struct {
	amgr     *autonomouspkg.Manager
	queueCfg func() config.DefectQueueConfig

	mu sync.Mutex
}

// wireDefectQueue installs the production defect queue on the REST server:
// the backend over the autonomous manager plus the file decision log under
// the data dir. Settings are read live from cfg (the REST config handler
// mutates cfg in place), so a runtime config set needs no restart.
func wireDefectQueue(hs *server.HTTPServer, amgr *autonomouspkg.Manager, cfg *config.Config) {
	hs.SetDefectQueue(
		&defectQueueBackend{
			amgr:     amgr,
			queueCfg: func() config.DefectQueueConfig { return cfg.Autonomous.DefectQueue },
		},
		autonomouspkg.NewFileIntakeLog(autonomouspkg.DefaultIntakeLogPath(expandHome(cfg.DataDir))),
	)
}

const (
	// defectBatchHeader starts the first spec line of a batch Automaton; the
	// rest of the line is the JSON batch metadata.
	defectBatchHeader = "defect-queue-batch "
	// defectSharedBase is the plan-level ID of the shared-tasks story.
	defectSharedBase = "shared"
)

type defectBatchMeta struct {
	Project string `json:"project"`
	Area    string `json:"area,omitempty"`
}

var defectScriptSafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func parseDefectBatchMeta(spec string) (defectBatchMeta, bool) {
	line, _, _ := strings.Cut(spec, "\n")
	rest, ok := strings.CutPrefix(line, defectBatchHeader)
	if !ok {
		return defectBatchMeta{}, false
	}
	var m defectBatchMeta
	if err := json.Unmarshal([]byte(rest), &m); err != nil {
		return defectBatchMeta{}, false
	}
	return m, true
}

func defectPRDOpen(s autonomouspkg.PRDStatus) bool {
	switch s {
	case autonomouspkg.PRDCompleted, autonomouspkg.PRDRejected, autonomouspkg.PRDCancelled,
		autonomouspkg.PRDArchived, autonomouspkg.PRDFailed:
		return false
	}
	return true
}

// scopedStoryID / scopedTaskID give every story and task of a batch an ID
// that cannot collide with another Automaton's (the store keys tasks and
// stories globally).
func defectScoped(prdID, base string) string { return prdID + "-" + base }

// defectBase strips an Automaton's scope prefix from a stored ID.
func defectBase(prdID, id string) string { return strings.TrimPrefix(id, prdID+"-") }

// rescopeDefectStories rewrites story and task IDs (and the depends_on edges
// between them) with mapID. Edges to IDs outside the set are left alone.
func rescopeDefectStories(stories []autonomouspkg.Story, mapID func(string) string) {
	ids := map[string]string{}
	for i := range stories {
		ids[stories[i].ID] = mapID(stories[i].ID)
		for j := range stories[i].Tasks {
			ids[stories[i].Tasks[j].ID] = mapID(stories[i].Tasks[j].ID)
		}
	}
	remap := func(in []string) []string {
		if in == nil {
			return nil
		}
		out := make([]string, len(in))
		for k, d := range in {
			if n, ok := ids[d]; ok {
				out[k] = n
			} else {
				out[k] = d
			}
		}
		return out
	}
	for i := range stories {
		stories[i].ID = ids[stories[i].ID]
		stories[i].DependsOn = remap(stories[i].DependsOn)
		for j := range stories[i].Tasks {
			t := &stories[i].Tasks[j]
			t.ID = ids[t.ID]
			t.StoryID = stories[i].ID
			t.DependsOn = remap(t.DependsOn)
		}
	}
}

// setDefectStories stores stories for a draft Automaton and then applies the
// depends_on edges by task ID. Store.SetStories resolves depends_on against
// IDs already in the store (or sibling titles), so edges between IDs of the
// plan being stored are applied after the stories exist.
func setDefectStories(store *autonomouspkg.Store, prdID string, stories []autonomouspkg.Story) error {
	taskDeps := map[string][]string{}
	for i := range stories {
		for j := range stories[i].Tasks {
			t := &stories[i].Tasks[j]
			if len(t.DependsOn) > 0 {
				taskDeps[t.ID] = append([]string(nil), t.DependsOn...)
				t.DependsOn = nil
			}
		}
	}
	if err := store.SetStories(prdID, stories); err != nil {
		return err
	}
	for id, deps := range taskDeps {
		deps := deps
		store.MutateTask(id, func(tk *autonomouspkg.Task) { tk.DependsOn = deps })
	}
	return nil
}

func sharedStory(prdID string, tasks []autonomouspkg.Task) autonomouspkg.Story {
	return autonomouspkg.Story{
		ID:          defectSharedBase,
		PRDID:       prdID,
		Status:      autonomouspkg.StoryPending,
		Title:       "Shared batch tasks",
		Description: "Verification gate, documentation, changelog and parity tasks, run once for the whole batch.",
		Tasks:       tasks,
	}
}

func (b *defectQueueBackend) batchSettings(project string) config.DefectQueueBatchSettings {
	return b.queueCfg().EffectiveForProject(project).Batch
}

func renderDefectBatchSpec(meta defectBatchMeta, s config.DefectQueueBatchSettings, defects []autonomouspkg.QueueDefect) string {
	hdr, _ := json.Marshal(meta)
	var sb strings.Builder
	sb.WriteString(defectBatchHeader + string(hdr) + "\n\n")
	sb.WriteString(autonomouspkg.BatchTemplateText() + "\n")
	sb.WriteString(autonomouspkg.BatchGroupingHint(s) + "\n\n")
	sb.WriteString("Defects in this batch:\n")
	for _, d := range defects {
		fmt.Fprintf(&sb, "- %s: %s\n", d.ID, d.Title)
		if strings.TrimSpace(d.Description) != "" {
			fmt.Fprintf(&sb, "  %s\n", strings.ReplaceAll(strings.TrimSpace(d.Description), "\n", "\n  "))
		}
		if len(d.Files) > 0 {
			fmt.Fprintf(&sb, "  files: %s\n", strings.Join(d.Files, ", "))
		}
	}
	return sb.String()
}

func defectBatchTitle(meta defectBatchMeta) string {
	t := "Defect batch: " + meta.Project
	if meta.Area != "" {
		t += " / " + meta.Area
	}
	return t
}

func defectProjectDir(project string) string {
	if filepath.IsAbs(project) {
		return project
	}
	return ""
}

// storedDefects rebuilds the defects of a stored batch from its stories.
func storedDefects(prd *autonomouspkg.PRD, meta defectBatchMeta) []autonomouspkg.QueueDefect {
	var out []autonomouspkg.QueueDefect
	for _, st := range prd.Story {
		base := defectBase(prd.ID, st.ID)
		id, ok := strings.CutPrefix(base, "story-")
		if !ok {
			continue
		}
		title := strings.TrimPrefix(st.Title, "Fix defect "+id+": ")
		out = append(out, autonomouspkg.QueueDefect{
			ID: id, Title: title, Description: st.Description, Priority: prd.Priority,
			Project: meta.Project, Area: meta.Area, Files: append([]string(nil), st.FilesPlanned...),
		})
	}
	return out
}

func (b *defectQueueBackend) batchPRD(batchID string) (*autonomouspkg.PRD, defectBatchMeta, error) {
	prd, ok := b.amgr.Store().SnapshotPRD(batchID)
	if !ok {
		return nil, defectBatchMeta{}, fmt.Errorf("batch %q: %w", batchID, server.ErrDefectNotFound)
	}
	meta, isBatch := parseDefectBatchMeta(prd.Spec)
	if !isBatch {
		return nil, defectBatchMeta{}, fmt.Errorf("batch %q: %w", batchID, server.ErrDefectNotFound)
	}
	return prd, meta, nil
}

func defectBatchEditable(prd *autonomouspkg.PRD) error {
	switch prd.Status {
	case autonomouspkg.PRDDraft, autonomouspkg.PRDNeedsReview, autonomouspkg.PRDRevisionsAsked:
		return nil
	}
	return fmt.Errorf("batch %s is %s; only a draft, needs_review or revisions_asked batch can change", prd.ID, prd.Status)
}

func (b *defectQueueBackend) setPriority(prdID string, n int) {
	b.amgr.Store().MutatePRD(prdID, func(p *autonomouspkg.PRD) bool {
		p.Priority = n
		p.PrioritySource = autonomouspkg.PrioritySourceExplicit
		return true
	})
}

// ListOpenBatches implements autonomous.DefectIntakeStore.
func (b *defectQueueBackend) ListOpenBatches(project string) ([]autonomouspkg.OpenBatch, error) {
	var out []autonomouspkg.OpenBatch
	for _, prd := range b.amgr.Store().ListPRDs() {
		meta, ok := parseDefectBatchMeta(prd.Spec)
		if !ok || !defectPRDOpen(prd.Status) {
			continue
		}
		if project != "" && meta.Project != project {
			continue
		}
		size := 0
		for _, st := range prd.Story {
			if defectBase(prd.ID, st.ID) != defectSharedBase {
				size++
			}
		}
		out = append(out, autonomouspkg.OpenBatch{
			ID: prd.ID, Project: meta.Project, Area: meta.Area, Status: string(prd.Status), Size: size,
		})
	}
	return out, nil
}

// CreateBatch implements autonomous.DefectIntakeStore: a new draft batch
// Automaton holding the defect's story plus the shared tasks.
func (b *defectQueueBackend) CreateBatch(d autonomouspkg.QueueDefect) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	meta := defectBatchMeta{Project: d.Project, Area: d.Area}
	set := b.batchSettings(d.Project)
	store := b.amgr.Store()
	prd, err := store.CreatePRD(renderDefectBatchSpec(meta, set, []autonomouspkg.QueueDefect{d}),
		defectProjectDir(d.Project), "", "", "")
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		_ = store.DeletePRD(prd.ID)
		return "", err
	}
	if _, err := store.UpdatePRDFields(prd.ID, defectBatchTitle(meta), prd.Spec); err != nil {
		return fail(err)
	}
	plan := autonomouspkg.BuildBatchPlan(autonomouspkg.DefectBatch{
		ID: prd.ID, Project: d.Project, Defects: []autonomouspkg.QueueDefect{d}, PriorityRule: set.PriorityRule,
	})
	plan.Batch.RecomputePriority()
	var stories []autonomouspkg.Story
	for _, ps := range plan.Stories {
		stories = append(stories, ps.Story)
	}
	stories = append(stories, sharedStory(prd.ID, plan.Shared))
	rescopeDefectStories(stories, func(id string) string { return defectScoped(prd.ID, id) })
	if err := setDefectStories(store, prd.ID, stories); err != nil {
		return fail(err)
	}
	if err := b.amgr.SetPRDKind(prd.ID, autonomouspkg.PRDKindBug); err != nil {
		return fail(err)
	}
	b.setPriority(prd.ID, plan.Batch.Priority)
	return prd.ID, nil
}

// AddToBatch implements autonomous.DefectIntakeStore: one more story in an
// existing draft batch, with the shared gate taking the new core tasks as
// dependencies.
func (b *defectQueueBackend) AddToBatch(batchID string, d autonomouspkg.QueueDefect) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	prd, meta, err := b.batchPRD(batchID)
	if err != nil {
		return err
	}
	if prd.Status != autonomouspkg.PRDDraft {
		return fmt.Errorf("batch %s is %s; only a draft batch takes new defects", batchID, prd.Status)
	}
	existing := storedDefects(prd, meta)
	for _, e := range existing {
		if e.ID == d.ID {
			return fmt.Errorf("defect %s is already in batch %s", d.ID, batchID)
		}
	}
	set := b.batchSettings(meta.Project)
	plan := autonomouspkg.BuildBatchPlan(autonomouspkg.DefectBatch{
		ID: batchID, Project: meta.Project, Defects: []autonomouspkg.QueueDefect{d}, PriorityRule: set.PriorityRule,
	})
	ns := plan.Stories[0].Story
	var coreIDs []string
	for _, t := range ns.Tasks {
		coreIDs = append(coreIDs, defectScoped(batchID, t.ID))
	}
	one := []autonomouspkg.Story{ns}
	rescopeDefectStories(one, func(id string) string { return defectScoped(batchID, id) })

	stories := prd.Story
	var out []autonomouspkg.Story
	for _, st := range stories {
		if defectBase(batchID, st.ID) == defectSharedBase {
			out = append(out, one[0])
			for k := range st.Tasks {
				if st.Tasks[k].Kind == autonomouspkg.DefectTaskKindGate {
					st.Tasks[k].DependsOn = append(st.Tasks[k].DependsOn, coreIDs...)
				}
			}
		}
		out = append(out, st)
	}
	if len(out) == len(stories) { // no shared story found: append the story
		out = append(out, one[0])
	}
	all := append(existing, d)
	batch := autonomouspkg.DefectBatch{ID: batchID, Defects: all, PriorityRule: set.PriorityRule}
	batch.RecomputePriority()

	store := b.amgr.Store()
	if err := setDefectStories(store, batchID, out); err != nil {
		return err
	}
	if _, err := store.UpdatePRDFields(batchID, defectBatchTitle(meta), renderDefectBatchSpec(meta, set, all)); err != nil {
		return err
	}
	b.setPriority(batchID, batch.Priority)
	return nil
}

// CreateTinyFix implements autonomous.DefectIntakeStore: a one-task draft
// Automaton with one verification script.
func (b *defectQueueBackend) CreateTinyFix(d autonomouspkg.QueueDefect, change autonomouspkg.TinyFixChange) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	script := ".datawatch/verify/tinyfix-" + defectScriptSafe.ReplaceAllString(d.ID, "_") + ".sh"
	spec := fmt.Sprintf("Fix defect %s: %s\n\n%s\n\nTiny-fix lane (kind %s). Files: %s\nWrite the verification script at %s; it must fail without the fix and pass with it.",
		d.ID, d.Title, strings.TrimSpace(d.Description), change.Kind, strings.Join(change.Files, ", "), script)
	built, err := autonomouspkg.BuildTinyFixAutomaton(autonomouspkg.TinyFixAutomatonSpec{
		Title: "Tiny fix " + d.ID + ": " + d.Title, Spec: spec,
		ProjectDir: defectProjectDir(d.Project), VerificationScript: script,
	})
	if err != nil {
		return "", err
	}
	store := b.amgr.Store()
	prd, err := store.CreatePRD(built.Spec, built.ProjectDir, "", "", "")
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		_ = store.DeletePRD(prd.ID)
		return "", err
	}
	if _, err := store.UpdatePRDFields(prd.ID, built.Title, built.Spec); err != nil {
		return fail(err)
	}
	if err := store.SetStories(prd.ID, built.Story); err != nil {
		return fail(err)
	}
	if err := b.amgr.SetPRDKind(prd.ID, autonomouspkg.PRDKindBug); err != nil {
		return fail(err)
	}
	b.setPriority(prd.ID, d.Priority)
	return prd.ID, nil
}

// OpenBatchViews implements server.DefectQueueBackend.
func (b *defectQueueBackend) OpenBatchViews(project string) ([]server.DefectBatchView, error) {
	var out []server.DefectBatchView
	for _, prd := range b.amgr.Store().ListPRDs() {
		meta, ok := parseDefectBatchMeta(prd.Spec)
		if !ok || !defectPRDOpen(prd.Status) {
			continue
		}
		if project != "" && meta.Project != project {
			continue
		}
		v := server.DefectBatchView{ID: prd.ID, Project: meta.Project, Area: meta.Area, Status: string(prd.Status)}
		for _, st := range prd.Story {
			id, isDefect := strings.CutPrefix(defectBase(prd.ID, st.ID), "story-")
			if !isDefect {
				continue
			}
			v.Stories = append(v.Stories, server.DefectStoryRef{
				StoryID: st.ID, DefectID: id, Title: strings.TrimPrefix(st.Title, "Fix defect "+id+": "),
			})
		}
		v.Size = len(v.Stories)
		out = append(out, v)
	}
	return out, nil
}

// SplitStory implements server.DefectQueueBackend: the story moves into a new
// Automaton with its own gate, documentation and changelog tasks; the batch
// keeps its other stories with the dependencies on the moved tasks removed.
func (b *defectQueueBackend) SplitStory(batchID, storyID string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	prd, meta, err := b.batchPRD(batchID)
	if err != nil {
		return "", err
	}
	if err := defectBatchEditable(prd); err != nil {
		return "", err
	}
	set := b.batchSettings(meta.Project)
	defects := storedDefects(prd, meta)
	plan := &autonomouspkg.BatchPlan{Batch: autonomouspkg.DefectBatch{
		ID: batchID, Project: meta.Project, Defects: defects, PriorityRule: set.PriorityRule,
	}}
	for _, st := range prd.Story {
		if id, ok := strings.CutPrefix(defectBase(batchID, st.ID), "story-"); ok {
			plan.Stories = append(plan.Stories, autonomouspkg.PlanStory{DefectID: id, Story: st})
		} else if defectBase(batchID, st.ID) == defectSharedBase {
			plan.Shared = append(plan.Shared, st.Tasks...)
		}
	}
	split, err := autonomouspkg.SplitOutStory(plan, storyID)
	if err != nil {
		return "", fmt.Errorf("%v: %w", err, server.ErrDefectNotFound)
	}
	moved := split.Batch.Defects[0]

	store := b.amgr.Store()
	np, err := store.CreatePRD(renderDefectBatchSpec(meta, set, split.Batch.Defects), defectProjectDir(meta.Project), "", "", "")
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		_ = store.DeletePRD(np.ID)
		return "", err
	}
	if _, err := store.UpdatePRDFields(np.ID, defectBatchTitle(meta)+" (split "+moved.ID+")", np.Spec); err != nil {
		return fail(err)
	}
	var stories []autonomouspkg.Story
	for _, ps := range split.Stories {
		stories = append(stories, ps.Story)
	}
	stories = append(stories, sharedStory(np.ID, split.Shared))
	rescopeDefectStories(stories, func(id string) string {
		return defectScoped(np.ID, defectBase(batchID, id))
	})
	if err := setDefectStories(store, np.ID, stories); err != nil {
		return fail(err)
	}
	if err := b.amgr.SetPRDKind(np.ID, autonomouspkg.PRDKindBug); err != nil {
		return fail(err)
	}
	b.setPriority(np.ID, split.Batch.Priority)

	// The original batch: drop the moved story, unhook the shared
	// dependencies, refresh the spec and priority.
	if err := store.RemoveStory(batchID, storyID); err != nil {
		return "", fmt.Errorf("split created %s but removing the story from %s failed: %w", np.ID, batchID, err)
	}
	for _, t := range plan.Shared {
		deps := t.DependsOn
		store.MutateTask(t.ID, func(tk *autonomouspkg.Task) { tk.DependsOn = append([]string(nil), deps...) })
	}
	if _, err := store.UpdatePRDFields(batchID, defectBatchTitle(meta), renderDefectBatchSpec(meta, set, plan.Batch.Defects)); err != nil {
		return "", err
	}
	b.setPriority(batchID, plan.Batch.Priority)
	return np.ID, nil
}

var (
	_ server.DefectQueueBackend       = (*defectQueueBackend)(nil)
	_ autonomouspkg.DefectIntakeStore = (*defectQueueBackend)(nil)
)
