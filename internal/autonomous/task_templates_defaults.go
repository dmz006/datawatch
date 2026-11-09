// task_templates_defaults.go — the built-in task-template pack.
//
// The pack is data: a slice of config.TaskTemplateDef values, defined
// with the same types an operator writes in autonomous.task_templates
// .templates (same id, title, spec, files, kind, read_only, depends_on
// , applies_when). EffectiveTaskTemplates
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
	"github.com/dmz006/datawatch/internal/config"
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

// noteTemplateExemptionReason is the B124 exemption carried by every
// built-in note template: a note task changes no files, so no script
// has anything to verify.
const noteTemplateExemptionReason = "note task: changes no files by design, so there is nothing for a script to verify"

// BuiltInTaskTemplates returns the built-in task-template pack fresh
// (a new slice and fresh defs each call, so no caller can mutate the
// pack). Slots ({{feature}}, {{files}}, {{files_list}}) are left in
// the text; RenderTaskTemplateDef fills them at apply time.
func BuiltInTaskTemplates() []config.TaskTemplateDef {
	return []config.TaskTemplateDef{
		{
			ID:    TemplateChangelogEntry,
			Title: "Changelog entry for {{feature}}",
			Spec:  "Write a concise changelog entry that describes '{{feature}}' in terms of the result the operator sees: what changed, how it shows up, and any migration step. Keep the entry factual and short; do not create or rename files outside the changelog. Story files involved: {{files_list}}.",
			Files: "CHANGELOG.md",
			Kind:  "docs",
			AppliesWhen: "Always offered: every feature slice that lands needs a " +
				"changelog entry so the changelog and the work stay in step.",
		},
		{
			ID:    TemplateParityIssueDraft,
			Title: "Parity issue draft for {{feature}}",
			Spec:  "Write an issue-draft file under docs/plans/parity-issues/ that captures the parity gaps this work leaves or closes for '{{feature}}': the affected area, the current behavior, the expected behavior, and the suggested next step as plain prose. This is a file write only. It never runs any hosting CLI (no gh, no issue creation on a tracker) and creates nothing on any host. Story files involved: {{files_list}}.",
			Files: "docs/plans/parity-issues/{{feature}}-parity-draft.md",
			Kind:  "docs",
			AppliesWhen: "Offered when the slice touches behavior whose gaps are " +
				"worth tracking as an issue draft without touching a tracker.",
		},
		{
			ID:    TemplateDocsSet,
			Title: "Docs set for {{feature}}",
			Spec:  "Bring the docs of '{{feature}}' into shape with the change: update the affected howto, operations and glossary entries so they describe the post-change behavior, keep existing links reachable, and flag anything the operator must still review. Work only on the story's docs: {{files_list}}.",
			Files: "{{files}}",
			Kind:  "docs",
			AppliesWhen: "Offered whenever the slice changes operator-visible " +
				"behavior that the docs already describe.",
		},
		{
			ID:    TemplateBoundedLoadStep,
			Title: "Bounded load-step rule for {{feature}}",
			Spec: "Record the bounded-load-step posture for '{{feature}}': " + boundedLoadStepRule +
				" This is a note, not a change: it writes no files and touches nothing else (no files).",
			Kind:                     "note",
			VerificationScriptReason: noteTemplateExemptionReason,
			AppliesWhen: "Always offered as a posture note: any story that could " +
				"carry a CPU-load or stress step must say its bounds up front (nice, a quarter of the cores, a hard timeout, a trap-cleanup, and never outlive the owning task).",
		},
		{
			ID:         TemplateFinalGate,
			Title:      "Final gate for {{feature}}",
			Spec:       "Read-only final gate for '{{feature}}'. Verify, with read and check only, that the slice is complete: the build is clean, the tests of the touched packages pass, and the touched files hang together ({{files_list}}). Write the verdict and the evidence to one report file, .datawatch/reports/{{feature}}-final-gate.txt (the feature slugged into the file name); that report is the only file this task writes. It makes no other edits and never runs a hosting CLI.",
			Kind:       "final_gate",
			ReadOnly:   true,
			ReportPath: ".datawatch/reports/{{feature}}-final-gate.txt",
			// No VerificationScript: the gate's own check is its report
			// file. A read-only task with a usable report path is verified
			// by the read-only report check, and the command text the gate
			// used to carry is not a file, so the verifier prestart rejects it.
			AppliesWhen: "Always offered as the closing task: a cheap, read-only " +
				"verdict before the story is considered done.",
		},
		{
			ID:                       TemplateVersionBumpLeftToMerge,
			Title:                    "Release version note for {{feature}}",
			Spec:                     "Record a plain note, for the operator's records only: the release version of '{{feature}}' is the next patch above main, resolved at merge time. It is never a fixed number written into the tree. This is a note, not an edit: it never writes a file and leaves version fields exactly as they are.",
			Kind:                     "note",
			VerificationScriptReason: noteTemplateExemptionReason,
			AppliesWhen: "Offered to make the versioning posture explicit in the " +
				"story without any task touching versioning itself.",
		},
	}
}
