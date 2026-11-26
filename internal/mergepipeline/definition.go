package mergepipeline

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/tmuxscope"
)

// Kind classifies what a step is for.
type Kind string

// Step kinds.
const (
	KindTest         Kind = "test"
	KindLint         Kind = "lint"
	KindVersionBump  Kind = "version_bump"
	KindChangelog    Kind = "changelog"
	KindShip         Kind = "ship"
	KindTag          Kind = "tag"
	KindDeploy       Kind = "deploy"
	KindPostShipHook Kind = "post_ship_hook"
	KindCustom       Kind = "custom"
)

// Kinds lists every valid step kind.
func Kinds() []Kind {
	return []Kind{KindTest, KindLint, KindVersionBump, KindChangelog, KindShip,
		KindTag, KindDeploy, KindPostShipHook, KindCustom}
}

// OnFailure says what the pipeline does when a step fails.
type OnFailure string

// Failure actions.
const (
	// OnFailureStop ends the pipeline at the failed step. It is the default.
	OnFailureStop OnFailure = "stop"
	// OnFailureContinue records the failure and runs the next step.
	OnFailureContinue OnFailure = "continue"
)

// Built-in actions a step may name instead of a shell command.
const (
	ActionRebaseOntoMain       = "rebase_onto_main"
	ActionCheckBranchVersions  = "check_branch_versions"
	ActionMergeBranches        = "merge_branches"
	ActionSetReleaseVersion    = "set_release_version"
	ActionInsertChangelog      = "insert_changelog_heading"
	ActionSetReadmeReleaseLine = "set_readme_release_line"
	ActionCommitReleaseBump    = "commit_release_bump"
	ActionTagRelease           = "tag_release"
	ActionValidateReleaseBump  = "validate_release_bump"
	ActionPushRelease          = "push_release"
	ActionDeployLatest         = "deploy_latest"
)

// BuiltinActions lists every valid built-in action name.
func BuiltinActions() []string {
	return []string{ActionRebaseOntoMain, ActionCheckBranchVersions, ActionMergeBranches, ActionSetReleaseVersion,
		ActionInsertChangelog, ActionSetReadmeReleaseLine, ActionCommitReleaseBump, ActionTagRelease,
		ActionValidateReleaseBump, ActionPushRelease, ActionDeployLatest}
}

// Step is one stage of a merge pipeline. Exactly one of Command and Action is
// set: Command is a shell command line, Action names a built-in.
type Step struct {
	Name      string    `yaml:"name" json:"name"`
	Kind      Kind      `yaml:"kind" json:"kind"`
	Command   string    `yaml:"command,omitempty" json:"command,omitempty"`
	Action    string    `yaml:"action,omitempty" json:"action,omitempty"`
	Dir       string    `yaml:"dir,omitempty" json:"dir,omitempty"` // relative to the project; empty means its root
	OnFailure OnFailure `yaml:"on_failure,omitempty" json:"on_failure,omitempty"`
	// RequiresMakeTarget, when set, runs the step only if the project's
	// Makefile defines that target; otherwise the runner skips it.
	RequiresMakeTarget string        `yaml:"requires_make_target,omitempty" json:"requires_make_target,omitempty"`
	Timeout            time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"` // zero means the runner's default
	// TagSizes limits a tag_release step to releases of these sizes (patch,
	// minor, major). Empty tags every release.
	TagSizes []string `yaml:"tag_sizes,omitempty" json:"tag_sizes,omitempty"`
}

// Definition is an ordered list of steps.
type Definition struct {
	Steps []Step `yaml:"steps" json:"steps"`
	// ConflictRules say how a conflict in a matching file is settled. The
	// first matching rule wins; a file no rule matches keeps the built-in
	// handling.
	ConflictRules []ConflictRule `yaml:"conflict_rules,omitempty" json:"conflict_rules,omitempty"`
	// Shadow says what the shadow of a hand merge needs to know about the
	// project.
	Shadow *ShadowDefinition `yaml:"shadow,omitempty" json:"shadow,omitempty"`
}

// ShadowDefinition is the project's part of the shadow of a hand merge.
type ShadowDefinition struct {
	// PrivateFiles are files of the project's checkout that are never
	// committed and that its checks read, such as a private pattern list. The
	// shadow copies them into its own copy of the project.
	PrivateFiles []string `yaml:"private_files,omitempty" json:"private_files,omitempty"`
	// CheckFiles are the files and folders that decide what the checks do. A
	// change to one restarts the count of matched merges. None means the
	// scripts folder and the Makefile.
	CheckFiles []string `yaml:"check_files,omitempty" json:"check_files,omitempty"`
	// ReleaseText says whether the hand merge's own release wording counts:
	// strict (the default) allows only the dates to differ; ignore also leaves
	// the new changelog section and the wording of the README release line out
	// of the comparison.
	ReleaseText string `yaml:"release_text,omitempty" json:"release_text,omitempty"`
}

// The release_text values.
const (
	ReleaseTextStrict = "strict"
	ReleaseTextIgnore = "ignore"
)

// defaultCheckFiles are the check files of a pipeline that names none.
var defaultCheckFiles = []string{DefaultScriptDir, "Makefile"}

func (d Definition) checkFiles() []string {
	if d.Shadow != nil && len(d.Shadow.CheckFiles) > 0 {
		return append([]string(nil), d.Shadow.CheckFiles...)
	}
	return append([]string(nil), defaultCheckFiles...)
}

func (s *ShadowDefinition) problems() []string {
	if s == nil {
		return nil
	}
	var out []string
	for key, list := range map[string][]string{"private_files": s.PrivateFiles, "check_files": s.CheckFiles} {
		for _, f := range list {
			clean := path.Clean(f)
			if f == "" || clean != strings.TrimSuffix(f, "/") || clean == "." || path.IsAbs(f) || clean == ".." ||
				strings.HasPrefix(clean, "../") || strings.HasPrefix(f, "-") || strings.HasPrefix(f, ":") ||
				strings.ContainsAny(f, "*?[\\\n") || clean == ".git" || strings.HasPrefix(clean, ".git/") {
				out = append(out, fmt.Sprintf("shadow.%s: %q is not a plain path inside the repository", key, f))
			}
		}
	}
	switch s.ReleaseText {
	case "", ReleaseTextStrict, ReleaseTextIgnore:
	default:
		out = append(out, fmt.Sprintf("shadow.release_text: %q is not strict or ignore", s.ReleaseText))
	}
	sort.Strings(out)
	return out
}

// ConflictRule is one per-file conflict rule of a project.
type ConflictRule struct {
	// Path is a repository-relative file path, or a pattern for one folder
	// level as path.Match reads it.
	Path string `yaml:"path" json:"path"`
	// Rule is keep_main, keep_both or ask.
	Rule string `yaml:"rule" json:"rule"`
}

// The conflict rules.
const (
	ConflictRuleKeepMain = "keep_main"
	ConflictRuleKeepBoth = "keep_both"
	ConflictRuleAsk      = "ask"
)

// ValidationError reports every problem found in a definition.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid merge pipeline: " + strings.Join(e.Problems, "; ")
}

// unscopedTmuxKill matches a tmux kill-server, or a kill-session -a, which
// would reach the operator's live tmux server unless scoped.
// CallsUnscopedTmuxKill reports whether any simple command in command kills
// tmux without an explicit throwaway socket or a scrubbed environment on that
// same command. The check lives in internal/tmuxscope so internal/config can
// share it without an import cycle.
func CallsUnscopedTmuxKill(command string) bool {
	return tmuxscope.CallsUnscopedTmuxKill(command)
}

var makeTarget = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

// DefaultFileName is the conventional pipeline file in a project's repository.
// Callers pass it (or their own setting) to LoadFile.
const DefaultFileName = ".datawatch/merge-pipeline.yaml"

// Validate checks the definition and returns a *ValidationError listing every
// problem, or nil.
func (d Definition) Validate() error {
	var problems []string
	if len(d.Steps) == 0 {
		problems = append(problems, "no steps")
	}
	kinds := map[Kind]bool{}
	for _, k := range Kinds() {
		kinds[k] = true
	}
	actions := map[string]bool{}
	for _, a := range BuiltinActions() {
		actions[a] = true
	}
	seen := map[string]bool{}
	for i, s := range d.Steps {
		label := fmt.Sprintf("step %d", i+1)
		if strings.TrimSpace(s.Name) == "" {
			problems = append(problems, label+": empty name")
		} else {
			label = fmt.Sprintf("step %d (%s)", i+1, s.Name)
			if seen[s.Name] {
				problems = append(problems, label+": duplicate name")
			}
			seen[s.Name] = true
		}
		if !kinds[s.Kind] {
			problems = append(problems, fmt.Sprintf("%s: unknown kind %q", label, s.Kind))
		}
		hasCmd := strings.TrimSpace(s.Command) != ""
		hasAct := strings.TrimSpace(s.Action) != ""
		switch {
		case hasCmd && hasAct:
			problems = append(problems, label+": set command or action, not both")
		case !hasCmd && !hasAct:
			problems = append(problems, label+": needs a command or an action")
		case hasAct && !actions[s.Action]:
			problems = append(problems, fmt.Sprintf("%s: unknown action %q", label, s.Action))
		}
		if hasCmd && CallsUnscopedTmuxKill(s.Command) {
			problems = append(problems, label+": command calls an unscoped tmux kill")
		}
		if t := s.RequiresMakeTarget; t != "" && !makeTarget.MatchString(t) {
			problems = append(problems, fmt.Sprintf("%s: invalid requires_make_target %q", label, t))
		}
		if err := checkDir(s.Dir); err != nil {
			problems = append(problems, label+": "+err.Error())
		}
		if len(s.TagSizes) > 0 && s.Action != ActionTagRelease {
			problems = append(problems, label+": tag_sizes is only for the tag_release action")
		}
		for _, size := range s.TagSizes {
			switch ReleaseSize(size) {
			case ReleaseSizePatch, ReleaseSizeMinor, ReleaseSizeMajor:
			default:
				problems = append(problems, fmt.Sprintf("%s: unknown release size %q in tag_sizes", label, size))
			}
		}
		switch s.OnFailure {
		case "", OnFailureStop, OnFailureContinue:
		default:
			problems = append(problems, fmt.Sprintf("%s: unknown on_failure %q", label, s.OnFailure))
		}
		if s.Timeout < 0 {
			problems = append(problems, label+": negative timeout")
		}
	}
	problems = append(problems, d.Shadow.problems()...)
	for i, r := range d.ConflictRules {
		label := fmt.Sprintf("conflict rule %d", i+1)
		if strings.TrimSpace(r.Path) == "" {
			problems = append(problems, label+": empty path")
		} else if err := checkDir(r.Path); err != nil {
			problems = append(problems, label+": "+strings.ReplaceAll(err.Error(), "dir", "path"))
		} else if _, err := path.Match(r.Path, "x"); err != nil {
			problems = append(problems, fmt.Sprintf("%s: invalid pattern %q", label, r.Path))
		}
		switch r.Rule {
		case ConflictRuleKeepMain, ConflictRuleKeepBoth, ConflictRuleAsk:
		default:
			problems = append(problems, fmt.Sprintf("%s: unknown rule %q; use %s, %s or %s", label, r.Rule,
				ConflictRuleKeepMain, ConflictRuleKeepBoth, ConflictRuleAsk))
		}
	}
	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

// windowsDrive matches a drive-letter prefix such as C:\\ or C:/.
var windowsDrive = regexp.MustCompile(`^[A-Za-z]:([/\\]|$)`)

func checkDir(dir string) error {
	if dir == "" {
		return nil
	}
	if path.IsAbs(dir) || strings.HasPrefix(dir, "/") || strings.HasPrefix(dir, `\`) || windowsDrive.MatchString(dir) {
		return errors.New("dir must be relative to the project")
	}
	for _, part := range strings.FieldsFunc(dir, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return errors.New("dir must not contain '..'")
		}
	}
	return nil
}

// EffectiveOnFailure returns the step's failure action, defaulting to stop.
func (s Step) EffectiveOnFailure() OnFailure {
	if s.OnFailure == "" {
		return OnFailureStop
	}
	return s.OnFailure
}
