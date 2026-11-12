// Tiny-fix lane for the defect queue: classification of a small change, the
// one-task Automaton shape it runs as, and the re-check that refuses a change
// which turned out to be operator-visible.
//
// Everything here is a pure function of its arguments. Nothing reads the
// filesystem, the clock or a store, and nothing mutates the config or the
// change it is given.
package autonomous

import (
	"fmt"
	"path"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// TinyFixChange describes a proposed or finished change: the files it
// touches and the kind it was declared as.
type TinyFixChange struct {
	// Files are repo-relative paths touched by the change.
	Files []string
	// Kind is the declared change kind (one of the config.DefectKind* values).
	Kind string
}

// TinyFixDecision is the classifier's answer. Reason is always set and reads
// as a sentence a person can act on.
type TinyFixDecision struct {
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason"`
}

// TinyFixRefusal is the result of CheckOperatorVisible. Refused is false when
// the change may stay on the tiny-fix lane.
type TinyFixRefusal struct {
	Refused bool `json:"refused"`
	// Paths are the changed paths that caused the refusal.
	Paths []string `json:"paths,omitempty"`
	// Reason is human readable, names every offending path and, when Refused,
	// routes the change to the normal path.
	Reason string `json:"reason,omitempty"`
}

// tinyFixNormalPath is the routing instruction attached to every refusal.
const tinyFixNormalPath = "send this change to the normal path: a normal single-defect Automaton or a defect batch"

// ClassifyTinyFix decides whether change qualifies for the tiny-fix lane.
// Criteria come from cfg resolved for project: the per-project override wins
// over the block-level value, which wins over the built-in default. The
// change is eligible only when tiny fixes are enabled, it touches between 1
// and max_files files, every path matches allowed_path_globs, no path is
// operator-visible, and the declared kind is in allowed_kinds.
func ClassifyTinyFix(change TinyFixChange, cfg config.DefectQueueConfig, project string) TinyFixDecision {
	set := cfg.EffectiveForProject(project)
	crit := set.TinyFix
	no := func(format string, a ...interface{}) TinyFixDecision {
		return TinyFixDecision{Reason: fmt.Sprintf(format, a...)}
	}
	if !set.Enabled {
		return no("the defect queue is disabled, so no change is a tiny fix")
	}
	if !crit.Enabled {
		return no("tiny-fix handling is disabled for this project")
	}
	files := cleanChangePaths(change.Files)
	if len(files) == 0 {
		return no("the change lists no files, so it cannot be classified as a tiny fix")
	}
	if len(files) > crit.MaxFiles {
		return no("the change touches %d files, more than the tiny-fix limit of %d", len(files), crit.MaxFiles)
	}
	kind := strings.ToLower(strings.TrimSpace(change.Kind))
	if !tinyFixHasString(crit.AllowedKinds, kind) {
		return no("change kind %q is not an allowed tiny-fix kind (allowed: %s)", change.Kind, strings.Join(crit.AllowedKinds, ", "))
	}
	if visible := matchingPaths(files, crit.OperatorVisiblePathGlobs); len(visible) > 0 {
		return no("the change touches operator-visible path(s) %s", strings.Join(visible, ", "))
	}
	var outside []string
	for _, f := range files {
		if !matchesAnyGlob(f, crit.AllowedPathGlobs) {
			outside = append(outside, f)
		}
	}
	if len(outside) > 0 {
		return no("path(s) %s are outside the allowed tiny-fix paths", strings.Join(outside, ", "))
	}
	return TinyFixDecision{
		Eligible: true,
		Reason:   fmt.Sprintf("%d file(s), kind %q, all paths allowed", len(files), kind),
	}
}

// TinyFixAutomatonSpec carries what the one-task template needs.
type TinyFixAutomatonSpec struct {
	Title string
	// Spec is the worker's task description.
	Spec string
	// ProjectDir is the working directory of the Automaton.
	ProjectDir string
	// VerificationScript is the repo-relative path of the one verification
	// script. Required.
	VerificationScript string
}

// BuildTinyFixAutomaton returns the one-task Automaton for a tiny fix: one
// story holding one task with one verification script. There is no separate
// gate or documentation task; the change is merged through the normal
// review. The returned PRD is a value only; nothing is stored or started.
func BuildTinyFixAutomaton(spec TinyFixAutomatonSpec) (*PRD, error) {
	script := strings.TrimSpace(spec.VerificationScript)
	if script == "" {
		return nil, fmt.Errorf("tiny-fix automaton needs a verification script path")
	}
	title := strings.TrimSpace(spec.Title)
	if title == "" {
		return nil, fmt.Errorf("tiny-fix automaton needs a title")
	}
	return &PRD{
		Title:      title,
		Spec:       spec.Spec,
		ProjectDir: spec.ProjectDir,
		Status:     PRDDraft,
		Story: []Story{{
			Title:  title,
			Status: StoryPending,
			Tasks: []Task{{
				Title:              title,
				Spec:               spec.Spec,
				Status:             TaskPending,
				VerificationScript: script,
			}},
		}},
	}, nil
}

// CheckOperatorVisible is the re-check run when the tiny-fix lane finishes or
// is re-checked, against the files actually changed. It refuses when any
// changed path matches operator_visible_path_globs, or falls outside
// allowed_path_globs. It never mutates cfg or its inputs.
func CheckOperatorVisible(filesActuallyChanged []string, cfg config.DefectQueueConfig, project string) TinyFixRefusal {
	crit := cfg.EffectiveForProject(project).TinyFix
	files := cleanChangePaths(filesActuallyChanged)
	visible := matchingPaths(files, crit.OperatorVisiblePathGlobs)
	var outside []string
	for _, f := range files {
		if !matchesAnyGlob(f, crit.AllowedPathGlobs) && !tinyFixHasString(visible, f) {
			outside = append(outside, f)
		}
	}
	if len(visible) == 0 && len(outside) == 0 {
		return TinyFixRefusal{}
	}
	var parts []string
	if len(visible) > 0 {
		parts = append(parts, "operator-visible path(s) changed: "+strings.Join(visible, ", "))
	}
	if len(outside) > 0 {
		parts = append(parts, "path(s) outside the allowed tiny-fix set changed: "+strings.Join(outside, ", "))
	}
	return TinyFixRefusal{
		Refused: true,
		Paths:   append(append([]string(nil), visible...), outside...),
		Reason:  "not a tiny fix: " + strings.Join(parts, "; ") + "; " + tinyFixNormalPath,
	}
}

// cleanChangePaths trims, slash-normalises and de-duplicates paths, dropping blanks.
func cleanChangePaths(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range in {
		f = strings.TrimPrefix(path.Clean(strings.ReplaceAll(strings.TrimSpace(f), "\\", "/")), "./")
		if f == "" || f == "." || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

func matchingPaths(files, globs []string) []string {
	var out []string
	for _, f := range files {
		if matchesAnyGlob(f, globs) {
			out = append(out, f)
		}
	}
	return out
}

func tinyFixHasString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func matchesAnyGlob(p string, globs []string) bool {
	for _, g := range globs {
		if globMatchPath(g, p) {
			return true
		}
	}
	return false
}

// globMatchPath matches a slash path against a glob where "**" spans any
// number of path segments (including none) and other segments use path.Match
// rules. A pattern path.Match rejects never matches.
func globMatchPath(pattern, p string) bool {
	return globSegments(strings.Split(strings.TrimPrefix(pattern, "./"), "/"), strings.Split(p, "/"))
}

func globSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			if len(rest) == 0 {
				return len(segs) > 0 // trailing ** needs at least one segment
			}
			for i := 0; i <= len(segs); i++ {
				if globSegments(rest, segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		ok, err := path.Match(pat[0], segs[0])
		if err != nil || !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}
