// task_templates_defaults.go — the built-in task-template pack.
//
// The pack is data, not Go literals: the standard-tasks pack folder
// (internal/packs/builtinpacks/builtin) holds each template's id, title,
// kind, files, applies_when and report path in pack.yaml, and its spec
// text in the task's prompt file. BuiltInTaskTemplates resolves that pack
// with packs.ResolveTaskList over the built-in pack alone, and maps each
// resolved task to a config.TaskTemplateDef — the same type an operator
// writes in autonomous.task_templates.templates. EffectiveTaskTemplates
// (internal/autonomous/task_templates.go) merges this pack with the
// operator's templates — an operator template with the same id
// replaces the built-in at its original position, a new id is appended
// in config order — so the pack is the floor every planning run starts
// from and the operator's config the ceiling over it.
//
// Two hard properties the pack's text is written to hold for every
// feature name a story can carry:
//
//   - No blocking decompose-lint finding: every rendered spec/title is
//     checked against the built-in default pack (DefaultDecomposeLintR
//     ules) in the task-template tests, so a template task appended to
//     a fresh decomposition cannot tripped a block-severity rule
//     (version bumps, hosting-CLI calls, daemon starts, operator
//     dot-folder reads, read-only-but-writes, tracker ids).
//   - No hosting-CLI side effects: the parity-issue template writes a
//     file under docs/plans/parity-issues/ and never runs gh; the
//     version note is a no-edit note — the release version is the next
//     patch above main, taken at merge time, and it never touches a
//     version field.
package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/packs"
)

// Built-in task-template ids (operators reference templates by id).
const (
	// TemplateChangelogEntry is the changelog-entry template.
	TemplateChangelogEntry = "changelog_entry"
	// TemplateParityIssueDraft is the parity-issue-draft template
	// (writes a file under docs/plans/parity-issues/; never hosts).
	TemplateParityIssueDraft = "parity_issue_draft"
	// TemplateDocsSet is the docs-set template.
	TemplateDocsSet = "docs_set"
	// TemplateFinalGate is the read-only final-gate template.
	TemplateFinalGate = "final_gate"
	// TemplateVersionBumpLeftToMerge is the no-edit version note.
	TemplateVersionBumpLeftToMerge = "version_bump_left_to_merge"
	// TemplateBoundedLoadStep is the bounded-load-step posture note; the
	// same rule text the decompose prompt and the decompose-lint rule
	// carry (see load_step_rules.go).
	TemplateBoundedLoadStep = "bounded_load_step"
)

// builtInTaskTemplateKind is the Automaton kind the built-in pack is
// resolved for. The pack lists every Automaton kind, so any kind yields
// the same list; the feature kind is the one a story is planned under.
const builtInTaskTemplateKind = PRDKindFeature

// builtInTaskTemplatesOnce guards the one-time resolution of the pack.
// The resolved defs are cached: the prompt files are read once, so a
// later removal of the extracted pack folder cannot break a planning run.
var (
	builtInTaskTemplatesOnce  sync.Once
	builtInTaskTemplatesCache []config.TaskTemplateDef
	builtInTaskTemplatesErr   error
)

// BuiltInTaskTemplates returns the built-in task-template pack fresh
// (a new slice and fresh defs each call, so no caller can mutate the
// pack). The defs are resolved by packs.ResolveTaskList over the
// built-in pack alone. Slots ({{feature}}, {{files}}, {{files_list}})
// are left in the text; RenderTaskTemplateDef fills them at apply time.
//
// The built-in pack is embedded in the binary, so a failure to load it
// is a build defect and panics rather than returning a partial pack.
func BuiltInTaskTemplates() []config.TaskTemplateDef {
	builtInTaskTemplatesOnce.Do(func() {
		builtInTaskTemplatesCache, builtInTaskTemplatesErr = resolveBuiltInTaskTemplates()
	})
	if builtInTaskTemplatesErr != nil {
		panic(fmt.Sprintf("autonomous: built-in task templates: %v", builtInTaskTemplatesErr))
	}
	out := make([]config.TaskTemplateDef, len(builtInTaskTemplatesCache))
	copy(out, builtInTaskTemplatesCache)
	return out
}

// resolveBuiltInTaskTemplates loads the built-in pack, resolves its task
// list for builtInTaskTemplateKind, and maps each resolved task to a
// template definition.
func resolveBuiltInTaskTemplates() ([]config.TaskTemplateDef, error) {
	pack, err := packs.LoadBuiltin()
	if err != nil {
		return nil, err
	}
	resolved, err := packs.ResolveTaskList(builtInTaskTemplateKind, []*packs.Pack{pack})
	if err != nil {
		return nil, err
	}
	defs := make([]config.TaskTemplateDef, 0, len(resolved))
	for _, rt := range resolved {
		spec, err := packPromptSpec(pack.Dir, rt.Prompt)
		if err != nil {
			return nil, fmt.Errorf("task %q: %w", rt.Title, err)
		}
		defs = append(defs, config.TaskTemplateDef{
			ID:                       rt.ID,
			Title:                    rt.Title,
			Spec:                     spec,
			Files:                    rt.Files,
			Kind:                     rt.Kind,
			ReadOnly:                 rt.ReadOnly,
			ReportPath:               rt.ReportPath,
			VerificationScriptReason: rt.VerificationScriptReason,
			AppliesWhen:              rt.AppliesWhen,
		})
	}
	return defs, nil
}

// packPromptSpec reads a task's prompt file from the pack folder dir and
// returns its spec text.
func packPromptSpec(dir, prompt string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(prompt)))
	if err != nil {
		return "", fmt.Errorf("prompt %q: %w", prompt, err)
	}
	return promptSpecText(string(b)), nil
}

// promptSpecText returns the spec carried by a prompt file: the body
// after the leading "# " heading lines and blank lines, with surrounding
// whitespace trimmed. The heading is the template's human label and is
// not part of the spec.
func promptSpecText(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) && (strings.TrimSpace(lines[i]) == "" || strings.HasPrefix(lines[i], "# ")) {
		i++
	}
	return strings.TrimSpace(strings.Join(lines[i:], "\n"))
}
