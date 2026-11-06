package mergepipeline

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// This file holds the plain-language text of the merge pipeline surfaces. The
// MCP tools, the command line and the chat verbs all call these functions on
// the REST bodies, so the wording is written once and cannot drift between
// them.

// View is the body of GET /api/autonomous/merge-pipeline, as the text
// renderers read it.
type View struct {
	Project                string       `json:"project"`
	Definition             *Definition  `json:"definition"`
	Source                 string       `json:"source"`
	Reason                 string       `json:"reason"`
	FileError              string       `json:"file_error"`
	Settings               ViewSettings `json:"settings"`
	ConflictPolicy         string       `json:"conflict_policy"`
	ConflictPolicyInEffect string       `json:"conflict_policy_in_effect"`
	ConflictModel          string       `json:"conflict_model"`
	ConflictModelInEffect  string       `json:"conflict_model_in_effect"`
	ConflictDegradedReason string       `json:"conflict_degraded_reason"`
	// Overrides holds the per-project values; nil when the project has none.
	Overrides       map[string]json.RawMessage `json:"overrides"`
	RecordUpdatedAt string                     `json:"record_updated_at"`
	// PostShip is the post-ship hook state: on or off, how many hooks are
	// defined, and the names of the ones that would run. The block is global,
	// so it is the same for every project.
	PostShip ViewPostShip `json:"post_ship"`
}

// ViewSettings are the resolution settings in effect for the project.
type ViewSettings struct {
	FileName          string   `json:"file_name"`
	SourceOrder       []string `json:"source_order"`
	FallbackOnBadFile string   `json:"fallback_on_bad_file"`
	VersionFiles      []string `json:"version_files"`
}

// ViewPostShip is the post-ship hook state as the text shows it.
type ViewPostShip struct {
	Enabled      bool     `json:"enabled"`
	HookCount    int      `json:"hook_count"`
	EnabledHooks []string `json:"enabled_hooks"`
}

// ValidationReport is the body of POST /api/autonomous/merge-pipeline/validate.
type ValidationReport struct {
	Valid      bool        `json:"valid"`
	Errors     []string    `json:"errors"`
	Definition *Definition `json:"definition"`
	Source     string      `json:"source"`
	File       string      `json:"file"`
}

// RecordReport is the body of PUT /api/autonomous/merge-pipeline/record.
type RecordReport struct {
	Project    string     `json:"project"`
	Definition Definition `json:"definition"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// ParseView reads a merge-pipeline body. It refuses text that is not the
// expected object, naming what was wrong.
func ParseView(body []byte) (View, error) {
	var v View
	if err := json.Unmarshal(body, &v); err != nil {
		return View{}, fmt.Errorf("the daemon's merge pipeline reply could not be read: %w", err)
	}
	return v, nil
}

// ParseValidation reads a validate body.
func ParseValidation(body []byte) (ValidationReport, error) {
	var v ValidationReport
	if err := json.Unmarshal(body, &v); err != nil {
		return ValidationReport{}, fmt.Errorf("the daemon's validation reply could not be read: %w", err)
	}
	return v, nil
}

// ParseRecordReport reads a record-set body.
func ParseRecordReport(body []byte) (RecordReport, error) {
	var v RecordReport
	if err := json.Unmarshal(body, &v); err != nil {
		return RecordReport{}, fmt.Errorf("the daemon's record reply could not be read: %w", err)
	}
	return v, nil
}

// RenderViewText writes the pipeline in effect for a project in plain words.
func RenderViewText(v View) string {
	var b strings.Builder
	project := v.Project
	if project == "" {
		project = "(no project)"
	}
	fmt.Fprintf(&b, "Merge pipeline for %s\n", project)
	switch v.Source {
	case "file":
		b.WriteString("Source: the pipeline file in the repository\n")
	case "record":
		b.WriteString("Source: the pipeline record kept by the daemon")
		if v.RecordUpdatedAt != "" {
			fmt.Fprintf(&b, " (set %s)", v.RecordUpdatedAt)
		}
		b.WriteString("\n")
	case "default":
		b.WriteString("Source: the built-in pipeline\n")
	case "":
		b.WriteString("Source: none chosen\n")
	default:
		fmt.Fprintf(&b, "Source: %s\n", v.Source)
	}
	if v.Reason != "" {
		fmt.Fprintf(&b, "Why: %s\n", v.Reason)
	}
	if v.FileError != "" {
		fmt.Fprintf(&b, "Problem with the repository file: %s\n", v.FileError)
	}
	if v.Definition == nil {
		b.WriteString("No pipeline could be chosen, so a merge run would not start.\n")
	} else {
		b.WriteString(RenderDefinitionText(*v.Definition))
	}
	fmt.Fprintf(&b, "Pipeline file name: %s\n", dash(v.Settings.FileName))
	fmt.Fprintf(&b, "Order sources are tried in: %s\n", listOrNone(v.Settings.SourceOrder))
	fmt.Fprintf(&b, "When the repository file is unusable: %s\n", dash(v.Settings.FallbackOnBadFile))
	fmt.Fprintf(&b, "Files that hold the version: %s\n", listOrNone(v.Settings.VersionFiles))
	switch {
	case v.PostShip.Enabled:
		if len(v.PostShip.EnabledHooks) == 0 {
			fmt.Fprintf(&b, "Post-ship hooks: on, %d defined, none to run\n", v.PostShip.HookCount)
		} else {
			fmt.Fprintf(&b, "Post-ship hooks: on, %d of %d defined would run: %s\n", len(v.PostShip.EnabledHooks), v.PostShip.HookCount, strings.Join(v.PostShip.EnabledHooks, ", "))
		}
	case v.PostShip.HookCount > 0:
		fmt.Fprintf(&b, "Post-ship hooks: off (%d defined)\n", v.PostShip.HookCount)
	default:
		b.WriteString("Post-ship hooks: off (none defined)\n")
	}
	fmt.Fprintf(&b, "Merge conflict policy: %s", dash(v.ConflictPolicy))
	if v.ConflictPolicyInEffect != "" && v.ConflictPolicyInEffect != v.ConflictPolicy {
		fmt.Fprintf(&b, " (acting as %s", v.ConflictPolicyInEffect)
		if v.ConflictDegradedReason != "" {
			fmt.Fprintf(&b, ": %s", v.ConflictDegradedReason)
		}
		b.WriteString(")")
	}
	b.WriteString("\n")
	if v.ConflictModel != "" || v.ConflictModelInEffect != "" {
		fmt.Fprintf(&b, "Conflict model: %s", dash(v.ConflictModel))
		if v.ConflictModelInEffect != v.ConflictModel {
			fmt.Fprintf(&b, " (using %s)", dash(v.ConflictModelInEffect))
		}
		b.WriteString("\n")
	}
	if len(v.Overrides) == 0 {
		b.WriteString("Project overrides: none\n")
	} else {
		keys := make([]string, 0, len(v.Overrides))
		for k := range v.Overrides {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("Project overrides:\n")
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s = %s\n", k, compactJSON(v.Overrides[k]))
		}
	}
	return b.String()
}

// RenderDefinitionText lists the steps of a pipeline, one per line.
func RenderDefinitionText(d Definition) string {
	var b strings.Builder
	if len(d.Steps) == 0 {
		return "Steps: none\n"
	}
	fmt.Fprintf(&b, "Steps (%d):\n", len(d.Steps))
	for i, s := range d.Steps {
		what := s.Command
		if s.Action != "" {
			what = "built-in " + s.Action
		}
		fmt.Fprintf(&b, "  %d. %s [%s] %s", i+1, s.Name, s.Kind, what)
		var notes []string
		if s.Dir != "" {
			notes = append(notes, "in "+s.Dir)
		}
		if s.OnFailure != "" {
			notes = append(notes, "on failure "+string(s.OnFailure))
		}
		if s.RequiresMakeTarget != "" {
			notes = append(notes, "only if the Makefile has "+s.RequiresMakeTarget)
		}
		if s.Timeout > 0 {
			notes = append(notes, "limit "+s.Timeout.String())
		}
		if len(notes) > 0 {
			fmt.Fprintf(&b, " (%s)", strings.Join(notes, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// RenderValidationText says whether a pipeline is valid and lists every
// problem.
func RenderValidationText(v ValidationReport) string {
	what := "The pipeline you gave"
	if v.Source == "file" {
		what = "The repository pipeline file"
		if v.File != "" {
			what += " " + v.File
		}
	}
	var b strings.Builder
	if v.Valid {
		fmt.Fprintf(&b, "%s is valid.\n", what)
		if v.Definition != nil {
			b.WriteString(RenderDefinitionText(*v.Definition))
		}
		return b.String()
	}
	fmt.Fprintf(&b, "%s is not valid (%d %s):\n", what, len(v.Errors), plural(len(v.Errors), "problem"))
	for _, e := range v.Errors {
		fmt.Fprintf(&b, "  - %s\n", e)
	}
	return b.String()
}

// RenderRecordText confirms a stored record.
func RenderRecordText(r RecordReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Saved the merge pipeline record for %s (%d %s).\n", r.Project, len(r.Definition.Steps), plural(len(r.Definition.Steps), "step"))
	b.WriteString(RenderDefinitionText(r.Definition))
	return b.String()
}

// RenderRecordClearedText confirms a removed record.
func RenderRecordClearedText(project string) string {
	return fmt.Sprintf("Removed the merge pipeline record for %s. The repository file or the built-in pipeline applies again.\n", project)
}

func dash(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func listOrNone(l []string) string {
	if len(l) == 0 {
		return "(none)"
	}
	return strings.Join(l, ", ")
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func compactJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	switch t := v.(type) {
	case string:
		return t
	case []any:
		parts := make([]string, 0, len(t))
		for _, it := range t {
			parts = append(parts, fmt.Sprint(it))
		}
		return strings.Join(parts, ", ")
	}
	return string(raw)
}
