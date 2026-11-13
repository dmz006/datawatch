// Task classification for the task-type routing table.
//
// ClassifyTask names the task type of one task, which the routing table
// (internal/config/task_routing.go) then maps to a worker and verification
// LLM. The order is fixed:
//
//  1. The task's own kind: Task.Kind (smoke and final_gate both map to
//     final_gate) and the template id embedded in a template task's spec
//     (changelog, parity draft, docs set, version note, load step, final gate).
//  2. Deterministic keyword rules over the title, then over the title and
//     spec together, for free tasks. The rules live in one ordered table
//     (taskTypeRules) so the order is testable.
//  3. Otherwise daemon_backend_code.
//
// A task that lists non-test Go files under internal/ is never classified as
// a mechanical type: the tie resolves to the code type.
package autonomous

import (
	"regexp"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// Values of TaskRoutingDecision.ClassifiedBy.
const (
	TaskClassifiedByTemplateKind = "template_kind"
	TaskClassifiedByTitleSpec    = "title_spec"
	TaskClassifiedByDefault      = "default"
)

// taskTypeRule is one keyword rule. Title patterns are matched against the
// title alone first and may be broad; spec patterns are matched against the
// title and spec together and are kept narrow because specs mention many
// words in passing.
type taskTypeRule struct {
	taskType string
	title    []*regexp.Regexp
	spec     []*regexp.Regexp
}

func rx(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		out[i] = regexp.MustCompile(`(?i)` + p)
	}
	return out
}

// taskTypeRules is the ordered keyword table. The first rule whose pattern
// matches wins. Code-shaped rules come before the mechanical ones that share
// words with them (tests, docs), so a task that touches a surface and adds
// tests is classified by the surface.
var taskTypeRules = []taskTypeRule{
	{
		taskType: config.TaskTypeMergeConflictRepair,
		title:    rx(`merge[- ]conflicts?`, `resolve (the )?conflicts?`, `conflict markers?`),
		spec:     rx(`merge[- ]conflicts?`, `conflict markers?`, `<<<<<<<`),
	},
	{
		taskType: config.TaskTypeReviewRepair,
		title:    rx(`review (finding|block|feedback)s?`, `rules[- ]review`, `fix (the )?review`, `address (the )?review`),
		spec:     rx(`review finding`, `rules[- ]review (fix|finding|block)`, `review blocked`),
	},
	{
		taskType: config.TaskTypeBoundedLoadStep,
		title:    rx(`\bload (test|step|run)s?\b`, `\bstress\b`, `\bsoak\b`),
		spec:     rx(`\bsoak\b`, `\bstress test`, `\bload step\b`, `\bload test`),
	},
	{
		taskType: config.TaskTypeLocaleStrings,
		title:    rx(`\blocales?\b`, `\btranslations?\b`, `\bi18n\b`, `\bl10n\b`, `\bstring keys?\b`),
		spec:     rx(`locales?/[\w.-]+\.json`, `\btranslation strings?\b`, `\bi18n\b`),
	},
	{
		taskType: config.TaskTypeFormattingCleanup,
		title:    rx(`\bgofmt\b`, `\blint(ing)?\b`, `\brename\b`, `\bformat(ting)?\b`, `\breformat\b`, `\bwhitespace\b`, `\btypos?\b`),
		spec:     rx(`\bgofmt\b`, `\bgolangci-lint\b`, `\bformatting[- ]only\b`),
	},
	{
		taskType: config.TaskTypeMCPAPISurface,
		title:    rx(`internal/mcp`, `\bmcp\b`, `\bopenapi\b`, `\brest (api|endpoint|handler|route|surface)s?\b`, `/api/`, `\bapi (endpoint|surface|route|handler)s?\b`, `\bendpoints?\b`),
		spec:     rx(`internal/mcp`, `\bopenapi\b`, `\brest (api|endpoint|handler|route|surface)s?\b`, `/api/[a-z]`, `\bmcp tools?\b`),
	},
	{
		taskType: config.TaskTypeUICode,
		title:    rx(`app\.js`, `style\.css`, `\bweb (app|ui|page|tab)\b`, `\bpwa\b`, `\bfrontend\b`, `\bweb/`),
		spec:     rx(`app\.js`, `style\.css`, `internal/server/web`, `\bweb app\b`, `\bpwa\b`),
	},
	{
		taskType: config.TaskTypeDocsSet,
		title:    rx(`\bdocs?\b`, `\bdocumentation\b`, `how-?tos?\b`, `\bdefinitions?\b`, `\breadme\b`, `docs/`),
		spec:     rx(`docs/[\w./-]+\.md`, `\bhow-?to (doc|page|guide)s?\b`, `\bdefinitions? (doc|page)\b`),
	},
	{
		taskType: config.TaskTypeTests,
		title:    rx(`\b(unit|regression|integration) tests?\b`, `\btests?\b`, `_test\.go`),
		spec:     rx(`\b(unit|regression) tests?\b`, `_test\.go`),
	},
}

// templateTaskTypes maps a template id to its task type (step 1).
var templateTaskTypes = map[string]string{
	TemplateChangelogEntry:         config.TaskTypeChangelogEntry,
	TemplateParityIssueDraft:       config.TaskTypeParityIssueDraft,
	TemplateDocsSet:                config.TaskTypeDocsSet,
	TemplateFinalGate:              config.TaskTypeFinalGate,
	TemplateVersionBumpLeftToMerge: config.TaskTypeVersionNote,
	TemplateBoundedLoadStep:        config.TaskTypeBoundedLoadStep,
}

// mechanicalTaskTypes are the types a code task must never be silently
// classified as.
var mechanicalTaskTypes = map[string]bool{
	config.TaskTypeDocsSet:           true,
	config.TaskTypeChangelogEntry:    true,
	config.TaskTypeParityIssueDraft:  true,
	config.TaskTypeLocaleStrings:     true,
	config.TaskTypeTests:             true,
	config.TaskTypeFormattingCleanup: true,
	config.TaskTypeVersionNote:       true,
	config.TaskTypeBoundedLoadStep:   true,
}

var internalGoPathRe = regexp.MustCompile(`internal/[A-Za-z0-9_./-]+\.go\b`)

// listsInternalCodeFile reports whether the task names a non-test Go file
// under internal/ in its planned files, title or spec, and returns the first.
func listsInternalCodeFile(task *Task) (string, bool) {
	check := func(p string) bool {
		return strings.Contains(p, "internal/") && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go")
	}
	for _, f := range task.FilesPlanned {
		if check(f) {
			return f, true
		}
	}
	for _, text := range []string{task.Title, task.Spec} {
		for _, p := range internalGoPathRe.FindAllString(text, -1) {
			if check(p) {
				return p, true
			}
		}
	}
	return "", false
}

// codeTypeForPath picks the code type for a listed internal Go path.
func codeTypeForPath(path string) string {
	if strings.Contains(path, "internal/mcp/") || strings.Contains(path, "internal/server/") {
		return config.TaskTypeMCPAPISurface
	}
	return config.TaskTypeDaemonBackendCode
}

// ClassifyTask returns the task type of task and how it was decided
// (template_kind, title_spec or default). story is accepted for symmetry
// with the apply path and for story-level hints; a nil story is fine.
func ClassifyTask(task *Task, story *Story) (taskType, classifiedBy string) {
	if task == nil {
		return config.TaskTypeDaemonBackendCode, TaskClassifiedByDefault
	}
	switch task.Kind {
	case TaskKindSmoke, TaskKindFinalGate:
		return config.TaskTypeFinalGate, TaskClassifiedByTemplateKind
	}
	if id := taskTemplateIDOfSpec(task.Spec); id != "" {
		if tt, ok := templateTaskTypes[id]; ok {
			return tt, TaskClassifiedByTemplateKind
		}
	}
	if tt, ok := classifyByKeywords(task); ok {
		if mechanicalTaskTypes[tt] {
			if path, listed := listsInternalCodeFile(task); listed {
				return codeTypeForPath(path), TaskClassifiedByTitleSpec
			}
		}
		return tt, TaskClassifiedByTitleSpec
	}
	return config.TaskTypeDaemonBackendCode, TaskClassifiedByDefault
}

// classifyByKeywords runs the ordered rule table: first over the title, then
// over the title and spec together.
func classifyByKeywords(task *Task) (string, bool) {
	for _, r := range taskTypeRules {
		if anyMatch(r.title, task.Title) {
			return r.taskType, true
		}
	}
	both := task.Title + "\n" + task.Spec
	for _, r := range taskTypeRules {
		if anyMatch(r.spec, both) {
			return r.taskType, true
		}
	}
	return "", false
}

func anyMatch(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
