// task_templates.go — applying the task templates to a decomposed
// story set.
//
// Templates are data (config.TaskTemplateDef): this file holds the
// built-in default pack in task_templates_defaults.go, and this file
// holds the merge-and-apply logic. ApplyTaskTemplates appends, for
// every story, the template tasks the planner did not already produce
// — matched by the template-id marker embedded in a task's title or
// spec, or by title similarity against the rendered template title.
// Slots ({{feature}}, {{files}}, {{files_list}}) are filled from the
// feature name and the union of the story's task files. A template
// task's depends_on is the story's other tasks (plus any template
// tasks its template depends on). The result is idempotent: a second
// run detects the tasks a first run added (by marker) and adds
// nothing.
package autonomous

import (
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/dmz006/datawatch/internal/config"
)

// TaskTemplateMarkerPrefix is the id marker embedded in a template
// task's title and spec ("template:<id>"). A task whose title or spec
// contains the marker counts as the planner's own version of that
// template, and a second apply run detects the tasks a first run
// added through it — this is what makes ApplyTaskTemplates idempotent.
const TaskTemplateMarkerPrefix = "template:"

// TaskTemplateIDMarker renders the marker for one template id.
func TaskTemplateIDMarker(id string) string {
	return TaskTemplateMarkerPrefix + strings.TrimSpace(id)
}

// taskTemplateSlotsFor fills the slot values for one story: the
// feature name, the union of the story's task files (comma-separated
// for {{files}}) and the same union as a bullet list (for
// {{files_list}}).
func taskTemplateSlotsFor(story Story, feature string) config.TaskTemplateSlots {
	seen := make(map[string]bool)
	var files []string
	for _, t := range story.Tasks {
		for _, f := range t.FilesPlanned {
			f = strings.TrimSpace(f)
			if f != "" && !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	list := make([]string, 0, len(files))
	for _, f := range files {
		list = append(list, "- "+f)
	}
	return config.TaskTemplateSlots{
		Feature:   strings.TrimSpace(feature),
		Files:     strings.Join(files, ", "),
		FilesList: strings.Join(list, "\n"),
	}
}

// RenderTaskTemplateDef renders a template's slots (title, spec and
// files) with the supplied slot values, returning the definition with
// the rendered text. Unknown {{...}} tokens are left untouched (see
// config.RenderTaskTemplateSpec).
func RenderTaskTemplateDef(def config.TaskTemplateDef, slots config.TaskTemplateSlots) config.TaskTemplateDef {
	def.Title = config.RenderTaskTemplateSpec(def.Title, slots)
	def.Spec = config.RenderTaskTemplateSpec(def.Spec, slots)
	def.Files = config.RenderTaskTemplateSpec(def.Files, slots)
	// The report path is a file name, so the feature is slugged.
	pathSlots := slots
	pathSlots.Feature = slugTemplateFeature(slots.Feature)
	def.ReportPath = config.RenderTaskTemplateSpec(def.ReportPath, pathSlots)
	return def
}

// slugTemplateFeature turns a feature name into a file-name-safe slug
// (lower-case letters and digits joined by single dashes).
func slugTemplateFeature(feature string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(feature) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if s == "" {
		return "feature"
	}
	return s
}

// splitTemplateFiles splits a template's files field (plain or
// comma-separated) into a trimmed, empty-filtered list.
func splitTemplateFiles(files string) []string {
	var out []string
	for _, part := range strings.Split(files, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// templateTaskID is the id of the task ApplyTaskTemplates appends for
// one template on one story.
func templateTaskID(storyID, templateID string) string {
	return storyID + "-tpl-" + templateID
}

// normalizeTemplateName folds a task or template title to its
// similarity key: lowercase, with every non-alphanumeric character
// removed. "Changelog entry for X" and "changelog_entry_for_X" both
// fold to "changelogentryforx".
func normalizeTemplateName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// storyAlreadyHasTemplate reports whether a story already carries a
// task for the template — either a planner task the operator marked
// with the template-id marker ("template:<id>" in its title or spec)
// or a task whose title is similar to the rendered template title
// (identical, or containing the rendered title, after normalization).
func storyAlreadyHasTemplate(story Story, rendered config.TaskTemplateDef) bool {
	wantMarker := TaskTemplateIDMarker(rendered.ID)
	normWant := normalizeTemplateName(rendered.Title)
	for _, t := range story.Tasks {
		if strings.Contains(t.Title, wantMarker) || strings.Contains(t.Spec, wantMarker) {
			return true
		}
		if normWant != "" {
			normGot := normalizeTemplateName(t.Title)
			if normGot == normWant || strings.Contains(normGot, normWant) || (normGot != "" && normWant != "" && strings.Contains(normWant, normGot)) {
				return true
			}
		}
	}
	return false
}

// taskForTemplate builds the concrete task a rendered template def
// produces on a story. The marker is embedded in both the title and the spec so a
// later apply run detects this exact task (idempotence) and so an
// operator can see which template a task came from.
func taskForTemplate(story Story, def config.TaskTemplateDef, baseDepends []string, templateTaskIDs map[string]string) Task {
	now := time.Now()
	title := strings.TrimSpace(def.Title)
	if title == "" {
		title = def.ID
	}
	spec := def.Spec
	if m := TaskTemplateIDMarker(def.ID); !strings.Contains(spec, m) {
		if spec != "" {
			spec += " "
		}
		spec += "(" + m + ")"
	}
	id := templateTaskID(story.ID, def.ID)
	depends := append([]string(nil), baseDepends...)
	for _, depID := range def.DependsOn {
		depID = strings.TrimSpace(depID)
		if other, ok := templateTaskIDs[depID]; ok && other != id {
			depends = append(depends, other)
		}
	}
	return Task{
		ID:                       id,
		StoryID:                  story.ID,
		PRDID:                    story.PRDID,
		Title:                    title,
		Spec:                     spec,
		Status:                   TaskQueued,
		DependsOn:                uniqueStrings(depends),
		Kind:                     def.Kind,
		ReadOnly:                 def.ReadOnly,
		VerificationScript:       def.VerificationScript,
		VerificationScriptReason: def.VerificationScriptReason,
		ReportPath:               reportPathForTemplate(def),
		FilesPlanned:             splitTemplateFiles(def.Files),
		CreatedAt:                now,
		UpdatedAt:                now,
	}
}

// reportPathForTemplate returns the rendered report path a template
// task carries: only a read-only template has one.
func reportPathForTemplate(def config.TaskTemplateDef) string {
	if !def.ReadOnly {
		return ""
	}
	return strings.TrimSpace(def.ReportPath)
}

// uniqueStrings removes duplicate strings, preserving first-seen
// order.
func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// EffectiveTaskTemplates resolves the template set active for a
// project: the built-in pack overlaid with the operator's
// autonomous.task_templates.templates — an operator template with the
// same id replaces the built-in at its original position, a new id is
// appended in config order. A nil config behaves like the shipped
// default (built-in pack, no operator overlays).
func EffectiveTaskTemplates(cfg *config.Config) []config.TaskTemplateDef {
	builtIn := BuiltInTaskTemplates()
	out := make([]config.TaskTemplateDef, len(builtIn))
	copy(out, builtIn)
	if cfg == nil {
		return out
	}
	for _, op := range cfg.Autonomous.TaskTemplates.Templates {
		id := strings.TrimSpace(op.ID)
		if id == "" {
			continue
		}
		replace := false
		for i := range out {
			if strings.TrimSpace(out[i].ID) == id {
				out[i] = op
				replace = true
				break
			}
		}
		if !replace {
			out = append(out, op)
		}
	}
	return out
}

// ApplyTaskTemplates appends the template tasks the planner did not
// already produce to every story (the final_gate template is the
// exception: it is applied once, to the last story, depending on every
// task of the Automaton; any final_gate task on another story is
// dropped), and returns the updated stories
// plus one "<story>/<template>" entry per applied template task
// (nil/empty when nothing was applied).
//
// Behavior:
//   - Honours autonomous.task_templates.enabled and the per-project
//     override keyed by projectDir (config.AutonomousConfig.
//     EffectiveTaskTemplatesEnabledForProject); disabled → stories
//     back, zero applied.
//   - For each story and each effective template not already covered
//     by a story task (marker or title similarity), appends one task:
//     the id is "<story-id>-tpl-<template-id>", the title/spec/files
//     are the slots rendered from the feature name and the union of
//     the story's task files, the kind/read-only flags come from the
//     template, and depends_on is the story's other tasks plus the
//     tasks of templates this template depends on.
//   - Idempotent: applied tasks carry the "template:<id>" marker in
//     their title and spec, so a second run recognizes them and adds
//     nothing (applied comes back empty).
//
// The input stories are not mutated: the returned slice is a copy
// with the new tasks appended.
func ApplyTaskTemplates(stories []Story, feature, projectDir string, cfg *config.Config) ([]Story, []string) {
	if !taskTemplatesEnabledForProject(cfg, projectDir) {
		return append([]Story(nil), stories...), nil
	}
	templates := EffectiveTaskTemplates(cfg)
	perStory := make([]config.TaskTemplateDef, 0, len(templates))
	var finalGate []config.TaskTemplateDef
	for _, def := range templates {
		if strings.TrimSpace(def.ID) == TemplateFinalGate {
			finalGate = append(finalGate, def)
			continue
		}
		perStory = append(perStory, def)
	}
	out := dropMisplacedFinalGates(stories)
	var applied []string
	for i := range out {
		story := out[i]
		newStory, added := applyTemplatesToStory(story, feature, perStory, cfg)
		out[i] = newStory
		for _, id := range added {
			applied = append(applied, TaskTemplateAppliedLabel(story.ID, id))
		}
	}
	if len(out) > 0 && len(finalGate) > 0 {
		last := len(out) - 1
		if added := applyFinalGate(&out[last], out, feature, finalGate, cfg); added {
			applied = append(applied, TaskTemplateAppliedLabel(out[last].ID, TemplateFinalGate))
		}
	}
	return out, applied
}

// dropMisplacedFinalGates returns a copy of stories in which every
// final_gate task on a story other than the last is removed (and
// scrubbed from the other tasks' depends_on): the Automaton has one
// final gate, at the end.
func dropMisplacedFinalGates(stories []Story) []Story {
	out := append([]Story(nil), stories...)
	dropped := make(map[string]bool)
	for i := 0; i < len(out)-1; i++ {
		kept := make([]Task, 0, len(out[i].Tasks))
		for _, t := range out[i].Tasks {
			if t.Kind == TaskKindFinalGate {
				dropped[t.ID] = true
				continue
			}
			kept = append(kept, t)
		}
		out[i].Tasks = kept
	}
	if len(dropped) == 0 {
		return out
	}
	for i := range out {
		tasks := append([]Task(nil), out[i].Tasks...)
		for j := range tasks {
			var deps []string
			for _, d := range tasks[j].DependsOn {
				if !dropped[d] {
					deps = append(deps, d)
				}
			}
			tasks[j].DependsOn = deps
		}
		out[i].Tasks = tasks
	}
	return out
}

// applyFinalGate appends the one final gate to the last story: its
// depends_on covers every task of every story, and its slots are
// rendered from the union of all task files. It adds nothing when the
// last story already carries a final gate (planner-written or from an
// earlier run). Reports whether a gate was added.
func applyFinalGate(last *Story, all []Story, feature string, defs []config.TaskTemplateDef, cfg *config.Config) bool {
	whole := *last
	whole.Tasks = nil
	for _, s := range all {
		whole.Tasks = append(whole.Tasks, s.Tasks...)
	}
	for _, t := range whole.Tasks {
		if t.Kind == TaskKindFinalGate {
			return false
		}
	}
	n := len(whole.Tasks)
	merged, added := applyTemplatesToStory(whole, feature, defs, cfg)
	if len(added) == 0 {
		return false
	}
	last.Tasks = append(append([]Task(nil), last.Tasks...), merged.Tasks[n:]...)
	return true
}

// applyTemplatesToStory appends the applicable template tasks to one
// story, returning the story (with a fresh Tasks slice when anything
// was added) and the applied template ids in template order.
//
// Each template's mode (cfg.Autonomous.EffectiveTaskTemplateMode, shipped
// defaults when cfg is nil) decides who does the task: off adds no task,
// daemon adds a task with DaemonRun set, llm adds the usual task.
func applyTemplatesToStory(story Story, feature string, templates []config.TaskTemplateDef, cfg *config.Config) (Story, []string) {
	var auto config.AutonomousConfig
	if cfg != nil {
		auto = cfg.Autonomous
	}
	var applied []string
	slots := taskTemplateSlotsFor(story, feature)
	newTasks := make([]Task, 0, len(story.Tasks)+len(templates))
	newTasks = append(newTasks, story.Tasks...)
	addedIDs := make(map[string]string, len(templates))
	for _, def := range templates {
		if strings.TrimSpace(def.ID) == "" {
			continue
		}
		if auto.EffectiveTaskTemplateMode(strings.TrimSpace(def.ID)) == config.TaskTemplateModeOff {
			continue
		}
		rendered := RenderTaskTemplateDef(def, slots)
		// A template whose report path is illegal is refused: it is
		// never instantiated into a task that could not be verified.
		if err := validateReportPath(reportPathForTemplate(rendered)); err != nil {
			continue
		}
		// Likewise a template whose verification_script is a command
		// string rather than a repo-relative script file.
		if err := validateTemplateVerificationScript(def); err != nil {
			continue
		}
		if storyAlreadyHasTemplate(story, rendered) {
			continue
		}
		addedIDs[def.ID] = templateTaskID(story.ID, def.ID)
	}
	if len(addedIDs) == 0 {
		story.Tasks = newTasks
		return story, nil
	}
	// depends_on = the story's other (already present) tasks, plus the
	// task of any template this one depends on (resolved through
	// addedIDs: a template that the story already had a task for simply
	// contributes no extra dependency — that existing task is already
	// in the base list).
	baseDepends := make([]string, 0, len(story.Tasks))
	for _, t := range story.Tasks {
		baseDepends = append(baseDepends, t.ID)
	}
	for _, def := range templates {
		id := strings.TrimSpace(def.ID)
		if _, ok := addedIDs[id]; !ok || id == "" {
			continue
		}
		task := taskForTemplate(story, RenderTaskTemplateDef(def, slots), baseDepends, addedIDs)
		if auto.EffectiveTaskTemplateMode(id) == config.TaskTemplateModeDaemon {
			task.DaemonRun = true
			task.ReadOnly = false
		}
		newTasks = append(newTasks, task)
		applied = append(applied, id)
	}
	story.Tasks = newTasks
	return story, applied
}

// taskTemplatesEnabledForProject resolves the enabled flag for the
// project: nil config → enabled (the shipped default enables the
// built-in pack).
func taskTemplatesEnabledForProject(cfg *config.Config, projectDir string) bool {
	if cfg == nil {
		return true
	}
	return cfg.Autonomous.EffectiveTaskTemplatesEnabledForProject(strings.TrimSpace(projectDir))
}

// TaskTemplateAppliedLabel formats one entry of ApplyTaskTemplates'
// applied label ("<story>/<template>").
func TaskTemplateAppliedLabel(storyID, templateID string) string {
	return storyID + "/" + templateID
}

// taskTemplateShellOperators are the characters that make a value a
// shell command rather than a file path: the operators (&&, ||, ;, |,
// >, <), expansion ($ and backtick), grouping and quoting.
const taskTemplateShellOperators = "&|;<>$`()'\"\\"

// verificationScriptLooksLikeCommand reports whether a template's
// verification_script value is a command string rather than a single
// repo-relative script file. It is one when it contains whitespace or a
// shell operator, or when it is not one repo-relative path: absolute, a
// path with a .. segment, or the project root itself. The empty value
// is not a command; it means the template carries no script.
func verificationScriptLooksLikeCommand(s string) bool {
	if s == "" {
		return false
	}
	if strings.IndexFunc(s, unicode.IsSpace) >= 0 {
		return true
	}
	if strings.ContainsAny(s, taskTemplateShellOperators) {
		return true
	}
	if strings.HasPrefix(s, "/") {
		return true
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return true
		}
	}
	return path.Clean(s) == "."
}

// validateTemplateVerificationScript rejects one template whose
// verification_script is a command string. The error names the template
// id and says a verification script must be a repo-relative script file.
// The value is checked untrimmed: it is copied verbatim onto the task,
// so surrounding whitespace would reach the verifier too.
func validateTemplateVerificationScript(def config.TaskTemplateDef) error {
	if !verificationScriptLooksLikeCommand(def.VerificationScript) {
		return nil
	}
	return fmt.Errorf("task template %q: verification_script %q is a command string; a verification script must be a repo-relative script file (for example scripts/verify.sh)", strings.TrimSpace(def.ID), def.VerificationScript)
}

// validateTaskTemplateVerificationScripts checks every template in a set
// and returns the first rejection, if any.
func validateTaskTemplateVerificationScripts(templates []config.TaskTemplateDef) error {
	for _, def := range templates {
		if err := validateTemplateVerificationScript(def); err != nil {
			return err
		}
	}
	return nil
}
