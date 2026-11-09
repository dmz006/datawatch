package mergepipeline

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
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
}

// Definition is an ordered list of steps.
type Definition struct {
	Steps []Step `yaml:"steps" json:"steps"`
}

// ValidationError reports every problem found in a definition.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid merge pipeline: " + strings.Join(e.Problems, "; ")
}

// unscopedTmuxKill matches a tmux kill-server, or a kill-session -a, which
// would reach the operator's live tmux server unless scoped.
var unscopedTmuxKill = regexp.MustCompile(`tmux([ \t]+[^|;&]*)?[ \t]+(kill-server|kill-session([ \t]+[^|;&]*)?[ \t]-[A-Za-z]*a)`)

// tmuxKillScoped matches the accepted scopings within one simple command: an
// explicit socket on the tmux call, or a scrubbed environment prefixing it.
var tmuxKillScoped = regexp.MustCompile(`tmux[ \t]+(-[A-Za-z]*S[ \t]|-L[ \t])|env[ \t]+(-u[ \t]+TMUX[ \t]+-u[ \t]+TMUX_TMPDIR|-u[ \t]+TMUX_TMPDIR[ \t]+-u[ \t]+TMUX)[ \t][^|;&\n]*tmux`)

// commandSeparators splits a command line into simple commands.
var commandSeparators = regexp.MustCompile(`[;&|\n]+`)

// CallsUnscopedTmuxKill reports whether any simple command in command kills
// tmux without an explicit throwaway socket or a scrubbed environment on that
// same command. A scoping marker on another command does not count.
func CallsUnscopedTmuxKill(command string) bool {
	for _, seg := range commandSeparators.Split(command, -1) {
		if unscopedTmuxKill.MatchString(seg) && !tmuxKillScoped.MatchString(seg) {
			return true
		}
	}
	return false
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
		switch s.OnFailure {
		case "", OnFailureStop, OnFailureContinue:
		default:
			problems = append(problems, fmt.Sprintf("%s: unknown on_failure %q", label, s.OnFailure))
		}
		if s.Timeout < 0 {
			problems = append(problems, label+": negative timeout")
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
