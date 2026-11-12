package mergepipeline

import (
	"bytes"
	"encoding/json"
	"errors"
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
	// ReleaseApprovalInEffect is the approval mode acted on for each release
	// size, resolved for the project: its override, else the global value,
	// else the built-in default. There is always a mode for each size.
	ReleaseApprovalInEffect ReleaseApprovalInEffect `json:"release_approval_in_effect"`
	// ConflictConfirmation says how many of the project's first model-resolved
	// conflicts still need the operator's confirmation.
	ConflictConfirmation ConflictConfirmationView `json:"conflict_confirmation"`
}

// ConflictConfirmationView is the conflict_confirmation block of the merge
// pipeline view. Mode and ConfirmFirstN are the values in effect for the
// project. Counted and Remaining are pointers so that a counter that could not
// be read is null, never a zero count; Error then says why.
type ConflictConfirmationView struct {
	// Mode is "shadow" or "on": what a model proposal that passes the gate does.
	Mode string `json:"mode"`
	// ConfirmFirstN is how many of the project's first proposals need
	// confirmation; 0 means confirmation is off.
	ConfirmFirstN int `json:"confirm_first_n"`
	// Counted is the number of model-resolved conflicts applied for the project.
	Counted *int `json:"counted"`
	// Remaining is ConfirmFirstN minus Counted, never below zero, zero when
	// ConfirmFirstN is 0.
	Remaining *int `json:"remaining"`
	// Enabled is true only when Mode is "on" and ConfirmFirstN is above 0.
	Enabled bool `json:"enabled"`
	// Error is the problem reading the counter, when there is one.
	Error string `json:"error,omitempty"`
}

// BuildConflictConfirmation fills the conflict_confirmation block for project.
// counter is the project's conflict counter; openErr is the error that
// opening it gave, if any. Either one being unusable leaves Counted and
// Remaining null and reports the problem in Error.
func BuildConflictConfirmation(project, mode string, confirmFirstN int, counter ConflictCounterStore, openErr error) ConflictConfirmationView {
	v := ConflictConfirmationView{
		Mode:          mode,
		ConfirmFirstN: confirmFirstN,
		Enabled:       mode == ConflictModeOn && confirmFirstN > 0,
	}
	var err error
	switch {
	case openErr != nil:
		err = openErr
	case counter == nil:
		err = errors.New("conflict counter is not available")
	default:
		var n int
		n, err = counter.Count(project)
		if err == nil {
			counted := n
			v.Counted = &counted
			remaining := 0
			if confirmFirstN > 0 && confirmFirstN > n {
				remaining = confirmFirstN - n
			}
			v.Remaining = &remaining
		}
	}
	if err != nil {
		v.Error = "conflict counter unreadable: " + err.Error()
	}
	return v
}

// ReleaseApprovalInEffect is the approval mode in effect for each release
// size. Every field is always set: an unset key resolves to its default.
type ReleaseApprovalInEffect struct {
	Patch string `json:"patch"`
	Minor string `json:"minor"`
	Major string `json:"major"`
}

// ViewSettings are the resolution settings in effect for the project.
type ViewSettings struct {
	FileName          string   `json:"file_name"`
	SourceOrder       []string `json:"source_order"`
	FallbackOnBadFile string   `json:"fallback_on_bad_file"`
	VersionFiles      []string `json:"version_files"`
	// The release settings in effect for the project.
	ReleaseSizeMap map[string]string `json:"release_size_map"`
	MajorTrigger   string            `json:"major_trigger"`
	ReleaseCadence string            `json:"release_cadence"`
	VersionScheme  string            `json:"version_scheme"`
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

// PostShipDetail is the post-ship hook state the daemon's settings block
// (autonomous.merge_pipeline.post_ship) reports: the switch, each hook as
// defined (its name, the built-in it runs or its command), and the targets
// the built-ins use. The block is global, so it is the same for every
// project.
type PostShipDetail struct {
	Enabled             bool           `json:"enabled"`
	Hooks               []PostShipHook `json:"hooks"`
	RegistryFile        string         `json:"registry_file"`
	RegistryRowPattern  string         `json:"registry_row_pattern"`
	CompanionRepository string         `json:"companion_repository"`
	IssueTitleTemplate  string         `json:"issue_title_template"`
	NotifyTemplate      string         `json:"notify_template"`
}

// ParsePostShipDetail reads one post-ship block as the daemon's settings
// report it.
func ParsePostShipDetail(b []byte) (PostShipDetail, error) {
	var d PostShipDetail
	if err := json.Unmarshal(b, &d); err != nil {
		return d, fmt.Errorf("the daemon's post-ship block could not be read: %w", err)
	}
	return d, nil
}

// PostShipDetailFromBody finds the autonomous.merge_pipeline.post_ship block
// in a daemon settings body and returns its raw JSON. It returns nil when the
// body carries no block.
func PostShipDetailFromBody(body []byte) ([]byte, error) {
	var envelope struct {
		Autonomous struct {
			MergePipeline struct {
				PostShip json.RawMessage `json:"post_ship"`
			} `json:"merge_pipeline"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("the daemon's settings reply could not be read: %w", err)
	}
	b := bytes.TrimSpace(envelope.Autonomous.MergePipeline.PostShip)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return nil, nil
	}
	return b, nil
}

// RenderPostShipDetailText writes the post-ship hook state in plain words:
// the switch, each hook (its built-in or its command, and whether it would
// run), and the targets the built-ins use when the block defines them.
func RenderPostShipDetailText(d PostShipDetail) string {
	var b strings.Builder
	if d.Enabled {
		b.WriteString("Post-ship hooks: on\n")
	} else {
		b.WriteString("Post-ship hooks: off (defined hooks would be skipped)\n")
	}
	if len(d.Hooks) == 0 {
		b.WriteString("  (no hook defined)\n")
	}
	for i := range d.Hooks {
		h := d.Hooks[i]
		what := "the command " + h.Command
		if h.Builtin != "" {
			what = "the built-in " + h.Builtin
		}
		state := "would run"
		if !h.Enabled {
			state = "skipped (disabled)"
		}
		fmt.Fprintf(&b, "  %d. %s: %s, %s\n", i+1, h.Name, what, state)
	}
	targets := []struct{ k, v string }{
		{"registry file", d.RegistryFile},
		{"registry row pattern", d.RegistryRowPattern},
		{"companion repository", d.CompanionRepository},
		{"issue title template", d.IssueTitleTemplate},
		{"notify template", d.NotifyTemplate},
	}
	anySet := false
	for _, t := range targets {
		if t.v != "" {
			anySet = true
			break
		}
	}
	if anySet {
		b.WriteString("Built-in targets:\n")
		for _, t := range targets {
			if t.v == "" {
				continue
			}
			fmt.Fprintf(&b, "  %s: %s\n", t.k, t.v)
		}
	}
	return b.String()
}

// RenderPostShipText writes the post-ship hook state as the merge-pipeline
// read keeps it in one line. The block is global, so it is the same for
// every project.
func RenderPostShipText(p ViewPostShip) string {
	var b strings.Builder
	switch {
	case p.Enabled:
		if len(p.EnabledHooks) == 0 {
			fmt.Fprintf(&b, "Post-ship hooks: on, %d defined, none to run\n", p.HookCount)
		} else {
			fmt.Fprintf(&b, "Post-ship hooks: on, %d of %d defined would run: %s\n", len(p.EnabledHooks), p.HookCount, strings.Join(p.EnabledHooks, ", "))
		}
	case p.HookCount > 0:
		fmt.Fprintf(&b, "Post-ship hooks: off (%d defined)\n", p.HookCount)
	default:
		b.WriteString("Post-ship hooks: off (none defined)\n")
	}
	return b.String()
}

func RenderViewText(v View) string {
	return renderViewText(v, RenderPostShipText(v.PostShip))
}

// RenderViewWithPostShip is the view text with the post-ship hook block
// rendered with the full detail (each hook, what it runs, the targets the
// built-ins use) when the daemon reported it; the one-line summary is
// replaced, so it is not written twice.
func RenderViewWithPostShip(v View, d PostShipDetail) string {
	return renderViewText(v, RenderPostShipDetailText(d))
}

// renderViewText writes the pipeline in effect for a project in plain words.
// postShipBlock is the post-ship block, already rendered.
func renderViewText(v View, postShipBlock string) string {
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
	fmt.Fprintf(&b, "Release size by kind: %s\n", sizeMapText(v.Settings.ReleaseSizeMap))
	fmt.Fprintf(&b, "Major release trigger: %s\n", dash(v.Settings.MajorTrigger))
	fmt.Fprintf(&b, "Release cadence: %s\n", dash(v.Settings.ReleaseCadence))
	fmt.Fprintf(&b, "Version scheme: %s\n", dash(v.Settings.VersionScheme))
	b.WriteString(postShipBlock)
	fmt.Fprintf(&b, "Merge conflict policy: %s", dash(v.ConflictPolicy))
	if v.ConflictPolicyInEffect != "" && v.ConflictPolicyInEffect != v.ConflictPolicy {
		fmt.Fprintf(&b, " (acting as %s", v.ConflictPolicyInEffect)
		if v.ConflictDegradedReason != "" {
			fmt.Fprintf(&b, ": %s", v.ConflictDegradedReason)
		}
		b.WriteString(")")
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Release approval: patch %s, minor %s, major %s\n",
		dash(v.ReleaseApprovalInEffect.Patch), dash(v.ReleaseApprovalInEffect.Minor), dash(v.ReleaseApprovalInEffect.Major))
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

// sizeMapText writes a kind-to-size map as kind=size pairs, sorted by kind.
// An empty map reads as (none).
func sizeMapText(m map[string]string) string {
	if len(m) == 0 {
		return "(none)"
	}
	kinds := make([]string, 0, len(m))
	for k := range m {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	pairs := make([]string, 0, len(kinds))
	for _, k := range kinds {
		pairs = append(pairs, k+"="+m[k])
	}
	return strings.Join(pairs, ", ")
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
