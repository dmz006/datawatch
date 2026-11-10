package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// standardTaskTestConfig returns a config with the shipped defaults
// and the given task-template block applied.
func standardTaskTestConfig(tt config.TaskTemplatesConfig) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Autonomous.TaskTemplates = tt
	return cfg
}

func TestStandardTaskBuiltInIDsAppearOnceInConfigOrder(t *testing.T) {
	rows := ListStandardTasks(config.DefaultConfig(), "/work/proj")
	builtIn := BuiltInTaskTemplates()
	if len(rows) != len(builtIn) {
		t.Fatalf("rows = %d, want %d (one per built-in)", len(rows), len(builtIn))
	}
	seen := map[string]int{}
	for i, row := range rows {
		seen[row.ID]++
		if row.ID != builtIn[i].ID {
			t.Errorf("row %d id = %q, want %q (config order)", i, row.ID, builtIn[i].ID)
		}
	}
	for _, def := range builtIn {
		if seen[def.ID] != 1 {
			t.Errorf("built-in %q appears %d times, want once", def.ID, seen[def.ID])
		}
	}
}

func TestStandardTaskOperatorOverrideReplacesRow(t *testing.T) {
	op := config.TaskTemplateDef{
		ID:          TemplateDocsSet,
		Title:       "Operator docs for {{feature}}",
		Kind:        "docs-custom",
		AppliesWhen: "Only when the operator says so.",
	}
	cfg := standardTaskTestConfig(config.TaskTemplatesConfig{
		Enabled:   true,
		Templates: []config.TaskTemplateDef{op},
	})
	rows := ListStandardTasks(cfg, "/work/proj")

	count := 0
	var got StandardTask
	var idx int
	for i, row := range rows {
		if row.ID == TemplateDocsSet {
			count++
			got = row
			idx = i
		}
	}
	if count != 1 {
		t.Fatalf("docs_set rows = %d, want 1", count)
	}
	// The override keeps the built-in's position.
	builtInIdx := -1
	for i, def := range BuiltInTaskTemplates() {
		if def.ID == TemplateDocsSet {
			builtInIdx = i
		}
	}
	if idx != builtInIdx {
		t.Errorf("override row at %d, want built-in position %d", idx, builtInIdx)
	}
	if got.Title != op.Title {
		t.Errorf("title = %q, want operator title %q", got.Title, op.Title)
	}
	if got.Kind != op.Kind {
		t.Errorf("kind = %q, want %q", got.Kind, op.Kind)
	}
	if got.OfferedWhen != op.AppliesWhen {
		t.Errorf("offered_when = %q, want %q", got.OfferedWhen, op.AppliesWhen)
	}
	if got.Summary != standardTaskSummaries[TemplateDocsSet] {
		t.Errorf("summary = %q, want built-in summary for the id", got.Summary)
	}
}

func TestStandardTaskOperatorTemplateWithoutSummaryUsesTitle(t *testing.T) {
	op := config.TaskTemplateDef{ID: "custom_lint", Title: "Custom lint for {{feature}}", Kind: "test"}
	cfg := standardTaskTestConfig(config.TaskTemplatesConfig{
		Enabled:   true,
		Templates: []config.TaskTemplateDef{op},
	})
	rows := ListStandardTasks(cfg, "/work/proj")
	last := rows[len(rows)-1]
	if last.ID != "custom_lint" {
		t.Fatalf("last row id = %q, want custom_lint (appended)", last.ID)
	}
	if last.Summary != op.Title {
		t.Errorf("summary = %q, want title %q", last.Summary, op.Title)
	}
}

func TestStandardTaskProjectOverrideDisablesForThatProjectOnly(t *testing.T) {
	cfg := standardTaskTestConfig(config.TaskTemplatesConfig{
		Enabled: true,
		Projects: map[string]config.TaskTemplateProjectOverride{
			"/work/off": {Enabled: boolPtr(false)},
		},
	})

	off := ListStandardTasks(cfg, "/work/off")
	for _, row := range off {
		if row.Enabled {
			t.Errorf("project /work/off row %q enabled = true, want false", row.ID)
		}
		if row.ProjectOverride == nil || *row.ProjectOverride {
			t.Errorf("project /work/off row %q override = %v, want false", row.ID, row.ProjectOverride)
		}
	}

	other := ListStandardTasks(cfg, "/work/other")
	for _, row := range other {
		if !row.Enabled {
			t.Errorf("project /work/other row %q enabled = false, want true", row.ID)
		}
		if row.ProjectOverride != nil {
			t.Errorf("project /work/other row %q override = %v, want nil", row.ID, *row.ProjectOverride)
		}
	}
}

func TestStandardTaskEnabledMatchesApplyTaskTemplates(t *testing.T) {
	cfg := standardTaskTestConfig(config.TaskTemplatesConfig{
		Enabled: false,
		Projects: map[string]config.TaskTemplateProjectOverride{
			"/work/on": {Enabled: boolPtr(true)},
		},
	})
	for _, dir := range []string{"/work/on", "/work/none"} {
		rows := ListStandardTasks(cfg, dir)
		want := taskTemplatesEnabledForProject(cfg, dir)
		for _, row := range rows {
			if row.Enabled != want {
				t.Errorf("dir %s row %q enabled = %v, want %v (ApplyTaskTemplates check)", dir, row.ID, row.Enabled, want)
			}
		}
	}
}

func TestStandardTaskModeAndRunnerMatchResolvedDefaults(t *testing.T) {
	cfg := standardTaskTestConfig(config.TaskTemplatesConfig{Enabled: true})
	rows := ListStandardTasks(cfg, "/work/proj")
	if len(rows) == 0 {
		t.Fatal("no rows")
	}
	wantMode := map[string]string{
		TemplateChangelogEntry:         "daemon",
		TemplateParityIssueDraft:       "daemon",
		TemplateBoundedLoadStep:        "daemon",
		TemplateVersionBumpLeftToMerge: "daemon",
		TemplateDocsSet:                "llm",
		TemplateFinalGate:              "llm",
	}
	wantRunner := map[string]string{
		TemplateChangelogEntry:         "daemon",
		TemplateParityIssueDraft:       "daemon",
		TemplateBoundedLoadStep:        "daemon",
		TemplateVersionBumpLeftToMerge: "daemon",
		TemplateDocsSet:                "session",
		TemplateFinalGate:              "script gate",
	}
	for _, row := range rows {
		if wantMode[row.ID] == "" {
			continue
		}
		if row.Mode != wantMode[row.ID] {
			t.Errorf("row %q mode = %q, want %q", row.ID, row.Mode, wantMode[row.ID])
		}
		if row.Runner != wantRunner[row.ID] {
			t.Errorf("row %q runner = %q, want %q", row.ID, row.Runner, wantRunner[row.ID])
		}
	}
}

func TestStandardTaskModeOffYieldsRunnerNone(t *testing.T) {
	cfg := standardTaskTestConfig(config.TaskTemplatesConfig{
		Enabled: true,
		Mode:    map[string]string{TemplateDocsSet: "off"},
	})
	rows := ListStandardTasks(cfg, "/work/proj")
	var got StandardTask
	found := false
	for _, row := range rows {
		if row.ID == TemplateDocsSet {
			got = row
			found = true
		}
	}
	if !found {
		t.Fatal("docs_set row not found")
	}
	if got.Mode != "off" {
		t.Errorf("docs_set mode = %q, want off", got.Mode)
	}
	if got.Runner != "none" {
		t.Errorf("docs_set runner = %q, want none", got.Runner)
	}
}

func TestStandardTaskNilConfigUsesShippedPack(t *testing.T) {
	rows := ListStandardTasks(nil, "")
	if len(rows) != len(BuiltInTaskTemplates()) {
		t.Fatalf("nil config rows = %d, want %d", len(rows), len(BuiltInTaskTemplates()))
	}
	for _, row := range rows {
		if !row.Enabled || row.ProjectOverride != nil {
			t.Errorf("nil config row %q enabled=%v override=%v, want enabled and nil override", row.ID, row.Enabled, row.ProjectOverride)
		}
	}
}
