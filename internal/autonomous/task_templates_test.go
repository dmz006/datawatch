package autonomous

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// testTaskTemplateStory builds the in-memory story the template tests
// work on: one story with two planner tasks carrying planned files.
func testTaskTemplateStory(t *testing.T) Story {
	t.Helper()
	now := time.Now()
	return Story{
		ID:     "a1000001-s1",
		PRDID:  "a1000001",
		Title:  "Implement task templates",
		Status: StoryPending,
		Tasks: []Task{
			{
				ID:           "a1000001-s1-t1",
				StoryID:      "a1000001-s1",
				PRDID:        "a1000001",
				Title:        "Implement the apply logic",
				Spec:         "Write the merge-and-apply logic.",
				Status:       TaskQueued,
				FilesPlanned: []string{"internal/autonomous/task_templates.go"},
				CreatedAt:    now,
				UpdatedAt:    now,
			},
			{
				ID:      "a1000001-s1-t2",
				StoryID: "a1000001-s1",
				PRDID:   "a1000001",
				Title:   "Write the tests",
				Spec:    "Cover the apply logic.",
				Status:  TaskQueued,
				FilesPlanned: []string{"internal/autonomous/task_templates_test.go",
					"internal/autonomous/task_templates.go"},
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}
}

// testTaskTemplateCfg builds a config with the shipped task-template
// defaults (enabled, no operator templates, no project overrides).
func testTaskTemplateCfg() *config.Config {
	return &config.Config{
		Autonomous: config.AutonomousConfig{
			TaskTemplates: config.TaskTemplatesConfig{Enabled: true},
		},
	}
}

// TestTaskTemplatesBuiltInPackPassesDefaultLintPack renders every
// built-in template into a concrete task the way ApplyTaskTemplates
// would (slots filled, marker embedded) and asserts the block
// severity of the built-in decompose-lint default pack is clean for
// all of them: no task a template creates can trip a block rule.
func TestTaskTemplatesBuiltInPackPassesDefaultLintPack(t *testing.T) {
	slots := config.TaskTemplateSlots{
		Feature:   "task templates",
		Files:     "internal/autonomous/task_templates.go, docs/howto/task-templates.md",
		FilesList: "- internal/autonomous/task_templates.go\n- docs/howto/task-templates.md",
	}
	story := Story{ID: "s1", PRDID: "p1", Status: StoryPending}
	for _, def := range BuiltInTaskTemplates() {
		if strings.TrimSpace(def.ID) == "" {
			t.Fatalf("built-in template with empty id: %#v", def)
		}
		rendered := RenderTaskTemplateDef(def, slots)
		spec := rendered.Spec
		if m := TaskTemplateIDMarker(def.ID); !strings.Contains(spec, m) {
			spec += " (" + m + ")"
		}
		story.Tasks = append(story.Tasks, Task{
			ID:                 templateTaskID(story.ID, def.ID),
			StoryID:            story.ID,
			PRDID:              story.PRDID,
			Title:              rendered.Title,
			Spec:               spec,
			Status:             TaskQueued,
			Kind:               def.Kind,
			ReadOnly:           def.ReadOnly,
			VerificationScript: def.VerificationScript,
			ReportPath:         reportPathForTemplate(rendered),
			FilesPlanned:       splitTemplateFiles(def.Files),
		})
	}
	findings := LintTasks([]Story{story}, DefaultDecomposeLintRules())
	for _, f := range findings {
		if f.Severity == config.DecomposeLintSeverityBlock {
			t.Errorf("block finding on template task %s: rule %s: %s (matched %q)",
				f.Task, f.RuleID, f.Message, f.Matched)
		}
	}
	if summary := SummaryOf(findings); summary.Block != 0 {
		t.Fatalf("default lint pack block summary = %d, want 0 (findings: %+v)", summary.Block, findings)
	}
}

// TestTaskTemplatesApplyAppendsMissingWithSlotsAndDeps asserts the
// core apply behavior: every built-in template not already covered is
// appended with slots filled from the feature name and the union of
// the story's task files, depends_on the story's other tasks, and the
// right flags (final gate read-only with no files; parity files
// rendered from the feature).
func TestTaskTemplatesApplyAppendsMissingWithSlotsAndDeps(t *testing.T) {
	story := testTaskTemplateStory(t)
	feature := "task templates"

	got, applied := ApplyTaskTemplates([]Story{story}, feature, "proj", testTaskTemplateCfg())
	if len(got) != 1 {
		t.Fatalf("stories = %d, want 1", len(got))
	}
	s := got[0]
	if len(applied) != len(BuiltInTaskTemplates()) {
		t.Fatalf("applied = %v, want %d entries", applied, len(BuiltInTaskTemplates()))
	}
	if len(s.Tasks) != 2+len(BuiltInTaskTemplates()) {
		t.Fatalf("tasks = %d, want %d", len(s.Tasks), 2+len(BuiltInTaskTemplates()))
	}
	// The original story is not mutated.
	if len(story.Tasks) != 2 {
		t.Fatalf("input story mutated: tasks = %d, want 2", len(story.Tasks))
	}

	byID := make(map[string]Task, len(s.Tasks))
	for _, task := range s.Tasks {
		byID[task.ID] = task
	}
	plugins := map[string]Task{
		TemplateChangelogEntry:         byID[templateTaskID(s.ID, TemplateChangelogEntry)],
		TemplateParityIssueDraft:       byID[templateTaskID(s.ID, TemplateParityIssueDraft)],
		TemplateDocsSet:                byID[templateTaskID(s.ID, TemplateDocsSet)],
		TemplateFinalGate:              byID[templateTaskID(s.ID, TemplateFinalGate)],
		TemplateVersionBumpLeftToMerge: byID[templateTaskID(s.ID, TemplateVersionBumpLeftToMerge)],
	}
	for id, task := range plugins {
		if task.ID == "" {
			t.Fatalf("template task %q missing from story", id)
		}
		if !strings.Contains(task.Spec, feature) {
			t.Errorf("%s: spec does not carry the feature name %q: %q", id, feature, task.Spec)
		}
		if strings.Contains(task.Title, "{{") || strings.Contains(task.Spec, "{{") {
			t.Errorf("%s: unrendered slot in title/spec: %q / %q", id, task.Title, task.Spec)
		}
		base := []string{"a1000001-s1-t1", "a1000001-s1-t2"}
		for _, dep := range base {
			if !taskTemplateContainsString(task.DependsOn, dep) {
				t.Errorf("%s: depends_on %v missing %q", id, task.DependsOn, dep)
			}
		}
		if taskTemplateContainsString(task.DependsOn, task.ID) {
			t.Errorf("%s: depends on itself", id)
		}
	}
	gate := plugins[TemplateFinalGate]
	if !gate.ReadOnly || gate.Kind != "final_gate" || len(gate.FilesPlanned) != 0 {
		t.Errorf("final_gate flags wrong: read_only=%v kind=%q files=%v", gate.ReadOnly, gate.Kind, gate.FilesPlanned)
	}
	parity := plugins[TemplateParityIssueDraft]
	found := false
	for _, f := range parity.FilesPlanned {
		if strings.HasPrefix(f, "docs/plans/parity-issues/") && strings.Contains(f, feature) {
			found = true
		}
	}
	if !found {
		t.Errorf("parity files %v: want a rendered file under docs/plans/parity-issues/", parity.FilesPlanned)
	}
	// files slot: union of both planner tasks' files, deduped.
	docs := plugins[TemplateDocsSet]
	wantFiles := []string{"internal/autonomous/task_templates.go", "internal/autonomous/task_templates_test.go"}
	if !reflect.DeepEqual(docs.FilesPlanned, wantFiles) {
		t.Errorf("docs_set files = %v, want %v", docs.FilesPlanned, wantFiles)
	}
	// applied labels are "<story>/<template>".
	wantLabel := TaskTemplateAppliedLabel(s.ID, TemplateChangelogEntry)
	foundLabel := false
	for _, a := range applied {
		if a == wantLabel {
			foundLabel = true
		}
	}
	if !foundLabel {
		t.Errorf("applied %v: missing %q", applied, wantLabel)
	}
}

// TestTaskTemplatesApplyIsIdempotent runs apply twice and asserts the
// second run adds nothing and the story is untouched.
func TestTaskTemplatesApplyIsIdempotent(t *testing.T) {
	story := testTaskTemplateStory(t)
	first, applied := ApplyTaskTemplates([]Story{story}, "task templates", "proj", testTaskTemplateCfg())
	if len(applied) == 0 {
		t.Fatalf("first run applied nothing")
	}
	second, applied2 := ApplyTaskTemplates(first, "task templates", "proj", testTaskTemplateCfg())
	if len(applied2) != 0 {
		t.Fatalf("second run applied %v, want nothing", applied2)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("second run changed the stories:\nfirst:  %+v\nsecond: %+v", first, second)
	}
	third, applied3 := ApplyTaskTemplates(second, "task templates", "proj", testTaskTemplateCfg())
	if len(applied3) != 0 || !reflect.DeepEqual(second, third) {
		t.Fatalf("third run not a no-op: applied=%v", applied3)
	}
}

// TestTaskTemplatesMarkerAndTitleSimilaritySkip the templates a
// planner task already covers: marker tasks (template:<id> in title or
// spec) by id, and similar-titled tasks ("Changelog entry for ..."
// style) by title.
func TestTaskTemplatesApplySkipsPlannerProducedTemplates(t *testing.T) {
	story := testTaskTemplateStory(t)
	story.Tasks = append(story.Tasks,
		Task{
			ID: "a1000001-s1-t3", StoryID: story.ID, PRDID: story.PRDID,
			Title:  "Write the changelog entry",
			Spec:   "Cover the changelog. (template:" + TemplateChangelogEntry + ")",
			Status: TaskQueued,
		},
		Task{
			ID: "a1000001-s1-t4", StoryID: story.ID, PRDID: story.PRDID,
			Title:  "Update the " + "docs set for task templates",
			Spec:   "Bring the docs into shape.",
			Status: TaskQueued,
		},
	)
	got, applied := ApplyTaskTemplates([]Story{story}, "task templates", "proj", testTaskTemplateCfg())
	s := got[0]
	ids := make([]string, 0, len(s.Tasks))
	for _, task := range s.Tasks {
		ids = append(ids, task.ID)
	}
	for _, dup := range []string{
		templateTaskID(s.ID, TemplateChangelogEntry),
		templateTaskID(s.ID, TemplateDocsSet),
	} {
		if taskTemplateContainsString(ids, dup) {
			t.Errorf("duplicate template task %q appended", dup)
		}
	}
	if len(applied) == 0 {
		t.Fatalf("applied = %v, want the remaining templates", applied)
	}
	if len(s.Tasks) != 4+len(BuiltInTaskTemplates())-2 {
		t.Errorf("tasks = %d, want %d", len(s.Tasks), 4+len(BuiltInTaskTemplates())-2)
	}
}

// TestTaskTemplatesOperatorTemplateReplacesBuiltInSameID asserts an
// operator template with a built-in id replaces the built-in (its
// text wins, count stays one per id) and a new id is appended.
func TestTaskTemplatesOperatorTemplateReplacesBuiltInSameID(t *testing.T) {
	cfg := testTaskTemplateCfg()
	cfg.Autonomous.TaskTemplates.Templates = []config.TaskTemplateDef{
		{
			ID:    TemplateChangelogEntry,
			Title: "Custom changelog entry for {{feature}}",
			Spec:  "Operator's own changelog entry for '{{feature}}'.",
			Files: "docs/CHANGELOG.md",
			Kind:  "docs",
		},
		{
			ID:        "review_notes",
			Title:     "Review notes for {{feature}}",
			Spec:      "Collect review notes for '{{feature}}' as a short checklist.",
			Kind:      "note",
			DependsOn: []string{TemplateChangelogEntry},
		},
	}
	story := testTaskTemplateStory(t)
	got, _ := ApplyTaskTemplates([]Story{story}, "task templates", "proj", cfg)
	s := got[0]
	byID := make(map[string]Task, len(s.Tasks))
	for _, task := range s.Tasks {
		byID[task.ID] = task
	}
	custom, ok := byID[templateTaskID(s.ID, TemplateChangelogEntry)]
	if !ok {
		t.Fatalf("custom changelog task missing")
	}
	if !strings.Contains(custom.Title, "Custom changelog entry") {
		t.Errorf("changelog title = %q, want the operator replacement", custom.Title)
	}
	// One task per template id, built-in + 1 custom = 6 total appended.
	if len(s.Tasks) != 2+len(BuiltInTaskTemplates())+1 {
		t.Errorf("tasks = %d, want %d", len(s.Tasks), 2+len(BuiltInTaskTemplates())+1)
	}
	// The operator's brand-new template is present too, and its
	// depends_on names the changelog template task (a per-story
	// template cannot depend on the final gate, which depends on it).
	gateID := templateTaskID(s.ID, TemplateChangelogEntry)
	notes, ok := byID[templateTaskID(s.ID, "review_notes")]
	if !ok {
		t.Fatalf("operator-only template task missing")
	}
	if notes.Kind != "note" {
		t.Errorf("review_notes kind = %q, want note", notes.Kind)
	}
	if !taskTemplateContainsString(notes.DependsOn, gateID) {
		t.Errorf("review_notes depends_on %v: missing changelog task %q", notes.DependsOn, gateID)
	}
	// Idempotence still holds with operator templates.
	again, applied2 := ApplyTaskTemplates(got, "task templates", "proj", cfg)
	if len(applied2) != 0 || !reflect.DeepEqual(got, again) {
		t.Errorf("second run with operator templates not a no-op: applied=%v", applied2)
	}
}

// TestTaskTemplatesDisabledGloballyAndPerProject honours
// autonomous.task_templates.enabled (global) and the per-project
// override keyed by the project directory.
func TestTaskTemplatesDisabledGloballyAndPerProject(t *testing.T) {
	story := testTaskTemplateStory(t)
	projectDir := "proj"

	// Global off: nothing applied for any project.
	cfg := testTaskTemplateCfg()
	cfg.Autonomous.TaskTemplates.Enabled = false
	got, applied := ApplyTaskTemplates([]Story{story}, "f", projectDir, cfg)
	if len(applied) != 0 {
		t.Errorf("global off: applied %v, want nothing", applied)
	}
	if len(got) != 1 || len(got[0].Tasks) != len(story.Tasks) {
		t.Errorf("global off: stories changed (tasks=%d)", len(got[0].Tasks))
	}

	// Global off + per-project on: applied.
	cfg = testTaskTemplateCfg()
	cfg.Autonomous.TaskTemplates.Enabled = false
	on := true
	cfg.Autonomous.TaskTemplates.Projects = map[string]config.TaskTemplateProjectOverride{
		projectDir: {Enabled: &on},
	}
	_, applied = ApplyTaskTemplates([]Story{story}, "f", projectDir, cfg)
	if len(applied) == 0 {
		t.Errorf("global off + project on: applied nothing, want the pack")
	}

	// Global on + per-project off (a real temp dir as the key): nothing.
	tmp := t.TempDir()
	off := false
	cfg = testTaskTemplateCfg()
	cfg.Autonomous.TaskTemplates.Projects = map[string]config.TaskTemplateProjectOverride{
		tmp: {Enabled: &off},
	}
	_, applied = ApplyTaskTemplates([]Story{story}, "f", tmp, cfg)
	if len(applied) != 0 {
		t.Errorf("project off: applied %v, want nothing", applied)
	}

	// Global on, other project untouched: applied.
	_, applied = ApplyTaskTemplates([]Story{story}, "f", "other-project", cfg)
	if len(applied) == 0 {
		t.Errorf("other project: applied nothing, want the pack")
	}

	// Override with enabled absent (nil): follows the global flag.
	cfg = testTaskTemplateCfg()
	cfg.Autonomous.TaskTemplates.Projects = map[string]config.TaskTemplateProjectOverride{
		projectDir: {},
	}
	got, applied = ApplyTaskTemplates([]Story{story}, "f", projectDir, cfg)
	if len(applied) == 0 {
		t.Errorf("nil override enabled: applied nothing, want the global default")
	}
	_ = got
}

// TestTaskTemplatesNilConfigUsesBuiltInPack: a nil config behaves
// like the shipped default (enabled, built-in pack only).
func TestTaskTemplatesNilConfigUsesBuiltInPack(t *testing.T) {
	story := testTaskTemplateStory(t)
	got, applied := ApplyTaskTemplates([]Story{story}, "f", "p", nil)
	if len(applied) != len(BuiltInTaskTemplates()) {
		t.Fatalf("applied = %v, want %d", applied, len(BuiltInTaskTemplates()))
	}
	if len(got) != 1 || len(got[0].Tasks) != len(story.Tasks)+len(BuiltInTaskTemplates()) {
		t.Fatalf("nil config apply wrong: %+v", got)
	}
}

// TestTaskTemplatesAppliedSpecsPassDefaultLintPack runs apply against
// a real story and lints the appended template tasks with the
// built-in default pack: no block-severity finding on any applied
// task (applied specs must pass the lint pack).
func TestTaskTemplatesAppliedSpecsPassDefaultLintPack(t *testing.T) {
	story := testTaskTemplateStory(t)
	got, _ := ApplyTaskTemplates([]Story{story}, "task templates", "p", testTaskTemplateCfg())
	rules := DefaultDecomposeLintRules()
	for _, f := range LintTasks(got, rules) {
		if f.Severity == config.DecomposeLintSeverityBlock {
			t.Errorf("block finding on applied task %s: rule %s (%s) matched %q",
				f.Task, f.RuleID, f.Message, f.Matched)
		}
	}
}

// containsString is the small slice helper the template tests use.
func taskTemplateContainsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// finalGateReportStory applies cfg's templates to the test story and
// returns the resulting stories.
func finalGateReportStory(t *testing.T, cfg *config.Config, feature string) []Story {
	t.Helper()
	out, _ := ApplyTaskTemplates([]Story{testTaskTemplateStory(t)}, feature, "", cfg)
	return out
}

func TestFinalGateTemplateReportPathReadOnlyDefaultsVerifiable(t *testing.T) {
	stories := finalGateReportStory(t, testTaskTemplateCfg(), "Final Gate: Feature X!")
	seen := 0
	for _, task := range stories[0].Tasks {
		if !task.ReadOnly {
			continue
		}
		seen++
		if task.ReportPath == "" && strings.TrimSpace(task.VerificationScript) == "" {
			t.Fatalf("read-only task %q has neither report path nor script", task.ID)
		}
		if task.ReportPath != "" {
			if err := validateReportPath(task.ReportPath); err != nil {
				t.Fatalf("task %q report path invalid: %v", task.ID, err)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no read-only template task instantiated")
	}
}

func TestFinalGateTemplateReportPathOnFinalGate(t *testing.T) {
	stories := finalGateReportStory(t, testTaskTemplateCfg(), "Final Gate: Feature X!")
	var gate *Task
	for i := range stories[0].Tasks {
		if strings.Contains(stories[0].Tasks[i].ID, TemplateFinalGate) {
			gate = &stories[0].Tasks[i]
		}
	}
	if gate == nil {
		t.Fatal("final gate task not instantiated")
	}
	if want := ".datawatch/reports/final-gate-feature-x-final-gate.txt"; gate.ReportPath != want {
		t.Fatalf("report path = %q, want %q", gate.ReportPath, want)
	}
	if !taskHasUsableReportPath(*gate) || strings.TrimSpace(gate.VerificationScript) != "" {
		t.Fatal("final gate must carry a usable report path and no verification script")
	}
	if !strings.Contains(gate.Spec, "only file") {
		t.Fatalf("spec must state the report is the only file written: %q", gate.Spec)
	}
	for _, f := range LintTasks(stories, EffectiveRules(config.DecomposeLintConfig{})) {
		if f.Task == gate.ID && strings.Contains(f.RuleID, "unverifiable") {
			t.Fatalf("final gate trips lint rule %s", f.RuleID)
		}
	}
}

// TestFinalGateTemplateHasReportAndNoCommandString asserts the built-in
// final-gate task, as taskForTemplate builds it, is read-only, carries a
// usable report path and has no VerificationScript. The gate used to carry
// a command string there, which the verifier prestart rejects as not a file.
func TestFinalGateTemplateHasReportAndNoCommandString(t *testing.T) {
	var def *config.TaskTemplateDef
	for _, d := range BuiltInTaskTemplates() {
		if d.ID == TemplateFinalGate {
			d := d
			def = &d
		}
	}
	if def == nil {
		t.Fatal("built-in final gate template missing")
	}
	story := testTaskTemplateStory(t)
	gate := taskForTemplate(story, *def, nil, nil)
	if !gate.ReadOnly {
		t.Fatal("final gate task must be read-only")
	}
	if !taskHasUsableReportPath(gate) {
		t.Fatalf("final gate task report path %q is not usable", gate.ReportPath)
	}
	if gate.VerificationScript != "" {
		t.Fatalf("final gate task must have no verification script, got %q", gate.VerificationScript)
	}
}

func TestFinalGateTemplateReportPathBadUserTemplateRefused(t *testing.T) {
	for _, bad := range []string{"/etc/passwd", "../x.txt", "docs/out.txt", ".datawatch/reports/../x"} {
		cfg := testTaskTemplateCfg()
		cfg.Autonomous.TaskTemplates.Templates = []config.TaskTemplateDef{{
			ID: "user_gate", Title: "User gate {{feature}}", Spec: "read only",
			ReadOnly: true, ReportPath: bad,
		}}
		for _, task := range finalGateReportStory(t, cfg, "feat")[0].Tasks {
			if strings.Contains(task.ID, "user_gate") {
				t.Fatalf("bad report path %q was instantiated", bad)
			}
		}
	}
	cfg := testTaskTemplateCfg()
	cfg.Autonomous.TaskTemplates.Templates = []config.TaskTemplateDef{{
		ID: "user_ok", Title: "User ok", Spec: "read only", ReadOnly: true,
		ReportPath: ".datawatch/reports/{{feature}}-ok.txt",
	}}
	found := false
	for _, task := range finalGateReportStory(t, cfg, "My Feat")[0].Tasks {
		if strings.Contains(task.ID, "user_ok") {
			found = true
			if task.ReportPath != ".datawatch/reports/my-feat-ok.txt" {
				t.Fatalf("report path = %q", task.ReportPath)
			}
		}
	}
	if !found {
		t.Fatal("valid user template not instantiated")
	}
}

// TestTaskTemplateCopiesVerificationScriptReason checks that a template's
// explicit verification-script exemption reason is copied onto the task it
// instantiates, and that a template without one leaves the task field empty.
func TestTaskTemplateCopiesVerificationScriptReason(t *testing.T) {
	story := testTaskTemplateStory(t)
	def := config.TaskTemplateDef{
		ID:                       "exempt-reason",
		Title:                    "Exempt task",
		Spec:                     "Write the release note.",
		VerificationScriptReason: "docs-only change; no script can verify prose",
	}
	task := taskForTemplate(story, def, nil, map[string]string{})
	if task.VerificationScriptReason != def.VerificationScriptReason {
		t.Fatalf("VerificationScriptReason = %q, want %q",
			task.VerificationScriptReason, def.VerificationScriptReason)
	}

	bare := config.TaskTemplateDef{ID: "no-reason", Spec: "Do the thing."}
	if got := taskForTemplate(story, bare, nil, map[string]string{}).VerificationScriptReason; got != "" {
		t.Fatalf("template without reason produced VerificationScriptReason = %q, want empty", got)
	}
}

// TestNoteTemplateExemptionReason asserts every built-in template of Kind
// note carries a non-empty B124 exemption reason, and that a task built
// from each one passes the structural plan check's verification rule
// (no missing_verification finding), since a note changes no files and
// has no script to verify.
func TestNoteTemplateExemptionReason(t *testing.T) {
	slots := config.TaskTemplateSlots{
		Feature:   "task templates",
		Files:     "internal/autonomous/task_templates.go",
		FilesList: "- internal/autonomous/task_templates.go",
	}
	story := Story{ID: "s1", PRDID: "p1", Status: StoryPending}
	noteCount := 0
	for _, def := range BuiltInTaskTemplates() {
		if def.Kind != "note" {
			continue
		}
		noteCount++
		if strings.TrimSpace(def.VerificationScriptReason) == "" {
			t.Errorf("note template %q has empty VerificationScriptReason", def.ID)
		}
		rendered := RenderTaskTemplateDef(def, slots)
		task := taskForTemplate(story, rendered, nil, map[string]string{})
		if task.VerificationScriptReason == "" {
			t.Errorf("note template %q: task lost VerificationScriptReason", def.ID)
		}
		story.Tasks = append(story.Tasks, task)
	}
	if noteCount == 0 {
		t.Fatal("expected at least one built-in note template")
	}
	report := CheckPlanStructure(&PRD{ID: "p1"}, []Story{story})
	for _, f := range report.Findings {
		if f.Kind == FindingMissingVerification {
			t.Errorf("unexpected missing_verification finding on note task %s: %s", f.TaskID, f.Detail)
		}
	}
}
