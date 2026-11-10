// standard_tasks.go — the read-only listing of every standard task the
// decomposer can add to a story.
//
// The list is not a second copy of the task pack: ListStandardTasks
// builds its rows from EffectiveTaskTemplates, the same definitions
// ApplyTaskTemplates uses, and takes its enabled flag from the same
// check (taskTemplatesEnabledForProject). The only data kept here is
// the one-line summary per built-in id, which the template definitions
// do not carry.
package autonomous

import (
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// StandardTaskModeModelSession is the mode every standard task row
// reports today. The per-task mode setting does not exist yet, so
// every row carries this constant.
const StandardTaskModeModelSession = "model session"

// StandardTask is one row of the standard-task listing: a task
// definition the decomposer can add, as resolved for one project.
type StandardTask struct {
	// ID is the template id (the built-in id, or the operator's own).
	ID string `json:"id"`
	// Title is the definition's title, slots left unrendered.
	Title string `json:"title"`
	// Kind is the definition's kind tag (free text, may be empty).
	Kind string `json:"kind"`
	// Summary is a one-line description. Built-in ids use the summary
	// map below; an operator template with no entry falls back to its
	// title.
	Summary string `json:"summary"`
	// OfferedWhen is the definition's applies-when condition.
	OfferedWhen string `json:"offered_when"`
	// Enabled is whether the decomposer offers this task for the
	// project, by the same check ApplyTaskTemplates uses.
	Enabled bool `json:"enabled"`
	// ProjectOverride is the project's own enabled override, or nil
	// when the project has no override.
	ProjectOverride *bool `json:"project_override"`
	// Mode is how the task runs. It is StandardTaskModeModelSession on
	// every row.
	Mode string `json:"mode"`
}

// standardTaskSummaries maps each built-in template id to its one-line
// summary.
var standardTaskSummaries = map[string]string{
	TemplateChangelogEntry:         "Adds a changelog entry describing the slice's result.",
	TemplateParityIssueDraft:       "Drafts a parity issue file for behavior gaps the slice touches.",
	TemplateDocsSet:                "Updates the operator-facing docs the slice changes.",
	TemplateBoundedLoadStep:        "Posture note: keeps any load step bounded.",
	TemplateFinalGate:              "Read-only closing gate that checks the slice's build and tests.",
	TemplateVersionBumpLeftToMerge: "No-edit note: the release version is set at merge time.",
}

// ListStandardTasks returns one row per effective task template, in
// EffectiveTaskTemplates order (built-in pack in its own order, with
// operator overlays replacing a built-in at its position or appended
// in config order). projectDir selects the project whose override
// decides Enabled and ProjectOverride. A nil cfg behaves like the
// shipped default.
func ListStandardTasks(cfg *config.Config, projectDir string) []StandardTask {
	defs := EffectiveTaskTemplates(cfg)
	key := strings.TrimSpace(projectDir)
	enabled := taskTemplatesEnabledForProject(cfg, projectDir)
	override := standardTaskProjectOverride(cfg, key)
	rows := make([]StandardTask, 0, len(defs))
	for _, def := range defs {
		id := strings.TrimSpace(def.ID)
		if id == "" {
			continue
		}
		summary, ok := standardTaskSummaries[id]
		if !ok {
			summary = def.Title
		}
		rows = append(rows, StandardTask{
			ID:              id,
			Title:           def.Title,
			Kind:            def.Kind,
			Summary:         summary,
			OfferedWhen:     def.AppliesWhen,
			Enabled:         enabled,
			ProjectOverride: override,
			Mode:            StandardTaskModeModelSession,
		})
	}
	return rows
}

// standardTaskProjectOverride returns a copy of the project's enabled
// override, or nil when the config has no override with an enabled
// flag for the key.
func standardTaskProjectOverride(cfg *config.Config, key string) *bool {
	if cfg == nil {
		return nil
	}
	ov, ok := cfg.Autonomous.TaskTemplates.Projects[key]
	if !ok || ov.Enabled == nil {
		return nil
	}
	v := *ov.Enabled
	return &v
}
