package autonomous

import (
	"fmt"
	"regexp"
	"strings"
)

// Smoke/live-check config lint ("smoke_config_lint").
//
// A smoke task IS the live verification of the operator's installation.
// Historically such specs have instructed the worker to bring the operator's
// real configuration or data into the repository — copying
// $HOME/.config, datawatch.yaml, secret material, or database files into
// the repo tree so the daemon "runs against the real thing". That puts
// operator secrets and private data under version control, defeats the
// point of a smoke test, and the scope boundary treats the project
// directory as the only place a worker may write. Synthetic, throwaway
// configuration in a temporary directory or a scratch folder outside the
// repository is the intended substitute.
//
// The rule is a decompose-path lint: it only looks at a task's title, spec
// and kind (the same inputs the planner produced), and it is wired into
// LintPlanScope next to the scope lint, so it honours the existing
// scope_lint mode (off / warn / enforce) exactly like LintPlanScope does.

// smokeTaskRe marks a task as a smoke or live-check task when its title or
// spec says so: the word "smoke" ("smoke test", "smoke run", ...), or the
// phrase "live check" in any of its hyphen/underscore/space spellings.
var smokeTaskRe = regexp.MustCompile(`(?i)(\bsmoke\b)|(\blive[-_ ]?check\b)`)

// copyMoveVerbRe matches a word that, applied to operator configuration or
// data, would land it in the repository: cp, copy, rsync, symlink, "ln -s",
// snapshot, and their inflections.
var copyMoveVerbRe = regexp.MustCompile(`(?i)\b(?:cp|copy|copies|copying|rsync|symlink|symlinks|snapshot|snapshots|snapshotting)\b`)

// lnSRe matches the shell form "ln -s ..." / "ln -rs ...".
var lnSRe = regexp.MustCompile(`(?i)\bln\s+-[a-z]*s\b`)

// operatorDataRe is the list of operator-configuration / operator-data
// targets a smoke spec must not move into the repository. A spec is flagged
// only when BOTH a copy verb and one of these targets are present, so
// specs that merely mention configuration or secrets (reading about them,
// checking that they load) stay clean.
var operatorDataPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{name: "home config directory", re: regexp.MustCompile(`(?i)\.config\b|(?i)\bhome\s+config\b|(?i)\bconfig\s+(?:dir|directory|folder|files)\b`)},
	{name: "datawatch.yaml", re: regexp.MustCompile(`(?i)datawatch\.ya?ml`)},
	{name: "secrets", re: regexp.MustCompile(`(?i)\bsecrets?\b`)},
	{name: "databases", re: regexp.MustCompile(`(?i)\bdatabases?\b|(?i)\bdatabase\s+(?:dir|directory|files|folder)\b|(?i)\.(?:sqlite3?|db)\b`)},
	{name: "operator config/data", re: regexp.MustCompile(`(?i)\boperator(?:'s|\s+config)\s+(?:config|data|secrets?)\b`)},
}

// isSmokeTask reports whether a decomposed task is a smoke or live-check
// task: its kind is "smoke" (see TaskKindSmoke), or its title/spec names
// the smoke test or the live check outright.
func isSmokeTask(title, spec, kind string) bool {
	if strings.EqualFold(kind, TaskKindSmoke) {
		return true
	}
	return smokeTaskRe.MatchString(title) || smokeTaskRe.MatchString(spec)
}

// LintSmokeConfig returns problems with a decomposed task that asks the
// worker to copy, cp, rsync, symlink or snapshot operator configuration or
// data (the home config directory, datawatch.yaml, secrets, databases)
// into the repository. Non-smoke tasks are ignored (nil). A smoke task is
// flagged only when the spec combines a copy/snapshot verb with a named
// operator data target; clean specs that build synthetic configuration in a
// temp directory or a scratch folder read nil back.
func LintSmokeConfig(title, spec, kind string) []string {
	if !isSmokeTask(title, spec, kind) {
		return nil
	}
	var verbs []string
	if v := copyMoveVerbRe.FindString(spec); v != "" {
		verbs = append(verbs, strings.ToLower(v))
	}
	if lnSRe.MatchString(spec) {
		verbs = append(verbs, "ln -s")
	}
	var targets []string
	for _, p := range operatorDataPatterns {
		if p.re.MatchString(spec) {
			targets = append(targets, p.name)
		}
	}
	if len(verbs) == 0 || len(targets) == 0 {
		return nil
	}
	label := "smoke task"
	if k := strings.TrimSpace(kind); k != "" && !strings.EqualFold(k, TaskKindSmoke) {
		label = "live-check task (kind " + k + ")"
	}
	return []string{
		fmt.Sprintf("%s: spec instructs the worker to %s operator %s into the repository; a smoke/live-check task must synthesize throwaway configuration and data in a temporary directory or a scratch folder outside the repository instead",
			label, strings.Join(verbs, "/"), strings.Join(targets, ", ")),
	}
}
