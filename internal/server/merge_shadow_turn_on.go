// The turn-on report: what the operator reads before turning datawatch's
// merging on. It is built from the stored shadows of hand merges, the merge
// selection rule and the pipeline in effect. It only reads.

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/git"
	"github.com/dmz006/datawatch/internal/mergepipeline"
	"github.com/dmz006/datawatch/internal/mergeshadow"
	"github.com/dmz006/datawatch/internal/proclimit"
)

const (
	turnOnTimeout = 2 * time.Minute
	// turnOnMergeLimit is how many shadowed merges the report lists.
	turnOnMergeLimit = 20
	turnOnListLimit  = 10
)

type turnOnMerge struct {
	AutomatonID string            `json:"automaton_id"`
	Name        string            `json:"name,omitempty"`
	HandCommit  string            `json:"hand_commit"`
	At          time.Time         `json:"at"`
	Verdict     string            `json:"verdict"`
	Rows        []mergeshadow.Row `json:"rows"`
}

type mergeTurnOn struct {
	Project string `json:"project"`
	// MergingOn is the merge run and deploy run switch.
	MergingOn bool                             `json:"merging_on"`
	Target    int                              `json:"target"`
	Met       bool                             `json:"met"`
	Streak    mergeshadow.StreakResult         `json:"streak"`
	Merges    []turnOnMerge                    `json:"merges"`
	Eligible  []autonomous.MergeSelectionEntry `json:"eligible"`
	Excluded  []autonomous.MergeSelectionEntry `json:"excluded"`
	NextPush  []string                         `json:"next_push"`
	// Actions is everything a merge run does that reaches outside it.
	Actions       []string `json:"actions"`
	StillToApply  []string `json:"still_to_apply"`
	PlantedFaults []string `json:"planted_faults"`
	TurnOff       string   `json:"turn_off"`
	Problems      []string `json:"problems,omitempty"`
	// Lines is the whole report as text, the same on every surface.
	Lines []string `json:"lines"`
}

// plantedFaults is what the product's own sandbox tests plant in a shadow and
// require to be held. The running daemon plants nothing.
var plantedFaults = []string{
	"a fake secret in the work: held by the leak result",
	"a private-list word in a commit message: held by the leak result",
	"a missing private pattern list, and a project with no local check: held by the leak result",
	"failed, blocked and cancelled work, and an acceptance or review recorded after the merge: held by the selection result",
	"a failing check, a skipped check, a daemon pass where the hand check failed: held by the checks result",
	"a check that pushes, tags, moves a branch, writes into the project or leaves a process running: held by the side effects result",
	"a push to the real remote by its own address: not prevented, reported by the side effects result",
	"a changed pipeline record, merge setting or check script: restarts the count",
}

const turnOffText = "datawatch config set " + config.MergeDeployRunKey + " false (a run in progress is refused before its push)"

func gitLines(ctx context.Context, repo string, args ...string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- fixed binary, arguments built here
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// mainHoldsMore says why main may hold a hand merge that came after commit:
// "" when neither main here nor its remote-tracking copy holds anything that
// commit does not. It asks the remote nothing.
func mainHoldsMore(ctx context.Context, repo, commit string) string {
	const cannot = "whether a hand merge that got no shadow is among them cannot be told"
	if commit == "" || strings.HasPrefix(commit, "-") {
		return "the commit it recorded is not known, so whether a hand merge with no shadow followed cannot be told"
	}
	for _, ref := range []string{"refs/heads/main", "refs/remotes/origin/main"} {
		tip, err := gitLines(ctx, repo, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
		if err != nil && ref != "refs/heads/main" {
			continue
		}
		if err != nil || len(tip) != 1 {
			return "main cannot be read, so whether a hand merge with no shadow followed cannot be told"
		}
		more, err := gitLines(ctx, repo, "rev-list", "--count", commit+".."+tip[0])
		if err != nil || len(more) != 1 {
			return "what " + ref + " holds after it cannot be read, so whether a hand merge with no shadow followed cannot be told"
		}
		if more[0] != "0" {
			return fmt.Sprintf("%s holds %s commit(s) it does not; %s", ref, more[0], cannot)
		}
	}
	return ""
}

// streakScope says what the count covers.
const streakScope = "What the count covers: stored shadows of hand merges only. It runs only where each shadowed merge starts at the very commit " +
	"the one before it recorded, and only while main here and its remote-tracking copy hold nothing after the newest; anything else on main " +
	"ends it, because it cannot be told whether that was a hand merge with no shadow. So record each hand merge after its release's last " +
	"commit on main. Commits on the remote that this checkout has not fetched are not seen. A check that skips itself and exits successfully counts as passed."

// mergeTurnOnReport builds the report for the merge repository; nil when none
// is configured or the Manager cannot answer.
func (s *Server) mergeTurnOnReport(ctx context.Context, threshold int) *mergeTurnOn {
	src, ok := s.autonomousMgr.(mergeShadowSource)
	if !ok || src.MergeBaseRepo() == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, turnOnTimeout)
	defer cancel()
	repo := src.MergeBaseRepo()
	project := filepath.Base(repo)
	cfg := s.mergePipelineConfig()
	dataDir := s.statePath()
	rep := &mergeTurnOn{
		Project: project, Target: threshold, Merges: []turnOnMerge{}, NextPush: []string{}, Actions: []string{},
		StillToApply: []string{}, PlantedFaults: plantedFaults, TurnOff: turnOffText,
		MergingOn: s.cfg != nil && s.cfg.Autonomous.MergeDeployRunEnabled,
	}
	problem := func(format string, a ...any) { rep.Problems = append(rep.Problems, fmt.Sprintf(format, a...)) }

	replays, err := listHandShadows(dataDir)
	if err != nil {
		problem("the stored shadows cannot be read: %v", err)
	}
	runner := newMergeRunner(src, cfg, repo, project, dataDir, nil)
	settings, serr := json.Marshal(cfg)
	hash := ""
	if serr == nil {
		hash, serr = mergepipeline.SettingsFingerprint(runner, settings)
	}
	if serr != nil {
		problem("the merge settings in effect cannot be read: %v", serr)
	}
	rep.Streak = mergeshadow.ReplayStreak(replays, project, hash, func(files []string) string {
		h, err := mergepipeline.CheckFilesFingerprint(ctx, repo, "refs/heads/main", files)
		if err != nil {
			return ""
		}
		return h
	}, func(commit string) string { return mainHoldsMore(ctx, repo, commit) })
	rep.Met = rep.Streak.Length >= threshold && len(rep.Problems) == 0
	var lastPush []mergeshadow.ReplayPushRef
	for _, r := range replays {
		if r.Project != project {
			continue
		}
		if lastPush == nil && len(r.Daemon.Push) > 0 {
			lastPush = r.Daemon.Push
		}
		if len(rep.Merges) < turnOnMergeLimit {
			rep.Merges = append(rep.Merges, turnOnMerge{AutomatonID: r.AutomatonID, Name: r.Name,
				HandCommit: r.Hand.Commit, At: r.Hand.At, Verdict: r.Verdict(), Rows: r.Rows()})
		}
	}

	sel := src.MergeSelection(ctx, "")
	rep.Eligible, rep.Excluded = sel.Eligible, sel.Excluded

	var def mergepipeline.Definition
	if res, err := runner.Resolver.Resolve(project); err != nil {
		problem("no merge pipeline can be chosen: %v", err)
	} else {
		def = res.Definition
	}
	rep.NextPush = nextPushLines(ctx, repo, def, sel, lastPush)
	rep.Actions = s.turnOnActions(def, cfg, project, runner)
	rep.StillToApply = turnOnStillToApply(rep, def, cfg, project, runner, replays)
	rep.Lines = turnOnLines(rep, sel.Note)
	return rep
}

func hasTagStep(def mergepipeline.Definition) bool {
	for _, st := range def.Steps {
		if st.Action == mergepipeline.ActionTagRelease || (st.Action == "" && st.Kind == mergepipeline.KindTag) {
			return true
		}
	}
	return false
}

func shortID(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

// nextPushLines says what the next runs would push. It reads the
// remote-tracking copy of main and asks the remote nothing.
func nextPushLines(ctx context.Context, repo string, def mergepipeline.Definition, sel autonomous.MergeSelection, last []mergeshadow.ReplayPushRef) []string {
	tag := "no tag"
	if hasTagStep(def) {
		tag = "and a tag (the pipeline has a tag step)"
	}
	base := "refs/remotes/origin/main"
	if _, err := gitLines(ctx, repo, "rev-parse", "--verify", "--quiet", base); err != nil {
		base = "refs/heads/main"
	}
	out := []string{}
	for _, e := range sel.Eligible {
		commits, err := gitLines(ctx, repo, "rev-list", "--abbrev-commit", base+".."+e.Tip)
		if err != nil {
			out = append(out, fmt.Sprintf("%s (%s): the commits it adds cannot be read", e.AutomatonID, e.Branch))
			continue
		}
		listed := strings.Join(commits, " ")
		if len(commits) > turnOnListLimit {
			listed = strings.Join(commits[:turnOnListLimit], " ") + fmt.Sprintf(" and %d more", len(commits)-turnOnListLimit)
		}
		out = append(out, fmt.Sprintf("%s (%s): one run would push main only, %s, with %d commit(s) of the branch (%s) and the merge and release commits the run makes",
			e.AutomatonID, e.Branch, tag, len(commits), listed))
	}
	if len(sel.Eligible) == 0 {
		out = append(out, "nothing: no branch is eligible now")
	}
	for _, p := range last {
		out = append(out, fmt.Sprintf("the newest shadow's push list: %s to %s, %d commit(s)", p.Ref, shortID(p.New), p.Commits))
	}
	return out
}

func (s *Server) turnOnActions(def mergepipeline.Definition, cfg config.MergePipelineConfig, project string, runner *mergepipeline.Runner) []string {
	out := []string{}
	add := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	var checks []string
	for _, st := range def.Steps {
		switch {
		case st.Action == mergepipeline.ActionPushRelease:
			add("push main to the project's remote (step %s), after the outgoing check and the secret scan", st.Name)
		case st.Action == mergepipeline.ActionTagRelease:
			sizes := "every release"
			if len(st.TagSizes) > 0 {
				sizes = strings.Join(st.TagSizes, ", ") + " releases"
			}
			add("create a tag and push it for %s (step %s)", sizes, st.Name)
		case st.Action == mergepipeline.ActionDeployLatest:
			if s.deployConfig().IsZero() {
				add("deploy: not configured, so step %s is skipped", st.Name)
			} else {
				add("deploy: build, install and restart as autonomous.deploy says (step %s)", st.Name)
			}
		case st.Action == mergepipeline.ActionRebaseOntoMain:
			add("fetch main from the project's remote (step %s)", st.Name)
		case st.Action != "":
		case st.Kind == mergepipeline.KindLint || st.Kind == mergepipeline.KindTest:
			checks = append(checks, st.Name)
		default:
			add("run the command of step %s (%s): %s", st.Name, st.Kind, st.Command)
		}
	}
	if len(checks) > 0 {
		add("run the checks %s: commands from the work being merged, in a scratch tree, not sandboxed (they can reach what the daemon's user can)", strings.Join(checks, ", "))
	}
	add("move local main and the rebased branch to what was pushed, only once the remote shows the commit")
	add("record the Automaton landed, completed and shipped once the remote shows the commit")
	for _, h := range runner.PostShip.Hooks {
		if runner.PostShip.Enabled && h.Enabled {
			what := h.Builtin
			if what == "" {
				what = "command " + h.Command
			}
			add("after a confirmed push, run the post-ship hook %s (%s)", h.Name, what)
		}
	}
	size := []string{config.ReleaseSizePatch, config.ReleaseSizeMinor, config.ReleaseSizeMajor}
	var approvals []string
	for _, sz := range size {
		approvals = append(approvals, sz+" "+cfg.EffectiveReleaseApproval(project, sz))
	}
	add("release approval: %s; a conflict nobody settles is put to the operator", strings.Join(approvals, ", "))
	if runner.ConflictSettings != nil {
		if set := runner.ConflictSettings(); set.Policy == mergepipeline.PolicyMechanicalAndModel {
			add("send a conflict to the model %s (mode %s)", cfg.EffectiveConflictModel(project), set.Mode)
		}
	}
	return out
}

func turnOnStillToApply(rep *mergeTurnOn, def mergepipeline.Definition, cfg config.MergePipelineConfig, project string, runner *mergepipeline.Runner, replays []*mergeshadow.Replay) []string {
	out := []string{}
	add := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	if hasTagStep(def) {
		add("the pipeline has a tag step: every release it covers is tagged. Remove it, or limit it with tag_sizes, before turning merging on")
	}
	lint, test := false, false
	for _, st := range def.Steps {
		if st.Action == "" {
			lint = lint || st.Kind == mergepipeline.KindLint
			test = test || st.Kind == mergepipeline.KindTest
		}
	}
	if !lint {
		add("the pipeline has no lint step")
	}
	if !test {
		add("the pipeline has no test step, so no merge can match and none is checked by tests")
	}
	if def.Shadow == nil || len(def.Shadow.PrivateFiles) == 0 {
		add("the pipeline names no shadow.private_files: a check that needs an uncommitted file, such as a private pattern list, cannot have it in a shadow")
	}
	if runner.ConflictSettings != nil && len(def.ConflictRules) > 0 && runner.ConflictSettings().Policy == mergepipeline.PolicyStopAndAsk {
		add("the pipeline's conflict_rules do nothing under the policy in effect (stop_and_ask)")
	}
	for _, r := range replays {
		if r.Project == rep.Project && r.State == mergeshadow.ReplayDone && r.Leak.Evaluated {
			if !r.Leak.ProjectCheckRan {
				add("the newest shadow found no check-outgoing target on the base commit")
			}
			break
		}
	}
	if left := def.StepsAShadowDoesNotRun(); len(left) > 0 {
		add("the pipeline has command step(s) a shadow does not run (%s): no hand merge can match while it does", strings.Join(left, ", "))
	}
	if !rep.Met {
		add("matched hand merges: %d of %d", rep.Streak.Length, rep.Target)
	}
	if !rep.MergingOn {
		add("%s is false: turning it on is the last step, and only on the operator's word", config.MergeDeployRunKey)
	}
	return out
}

func turnOnLines(rep *mergeTurnOn, selectionNote string) []string {
	var out []string
	add := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	section := func(title string, items []string) {
		add("")
		add("%s:", title)
		if len(items) == 0 {
			add("  none")
		}
		for _, i := range items {
			add("  %s", i)
		}
	}
	merging := "off"
	if rep.MergingOn {
		merging = "ON"
	}
	met := "not met"
	if rep.Met {
		met = "met"
	}
	add("Turn-on report for %s. Merging is %s.", rep.Project, merging)
	add("Matched hand merges in a row: %d of %d (%s). In the run: %d bug, %d feature, %d with a conflict; %d \"would ask\" passed over. %d shadowed.",
		rep.Streak.Length, rep.Target, met, rep.Streak.Bugs, rep.Streak.Features, rep.Streak.WithConflict, rep.Streak.Asked, rep.Streak.Total)
	if rep.Streak.StoppedBy != "" {
		add("The count stops at %s.", rep.Streak.StoppedBy)
	}
	add("%s", streakScope)
	add("A shadow starts its commands under: %s. This does not hold everything a command can start: one that finds a service outside it can run work with no such limit while the result still reads matched. Without that limit a shadow runs none of the project's commands.",
		shadowCommandLimit().Describe())
	add("One shadow runs at a time. A hand merge's, or a finish-time plan with gates, starts only with a command's limit and %d GiB more of memory available; this is not reserved. Git in a shadow's engine, copy and push checks has in addition %d GiB per process and %d minutes per command.",
		proclimit.MemoryMarginBytes>>30, git.ShadowProcessLimit.MemoryBytes>>30, int(git.ShadowProcessLimit.Time.Minutes()))
	var merges []string
	for _, m := range rep.Merges {
		merges = append(merges, fmt.Sprintf("%s %s at %s: %s", m.At.UTC().Format("2006-01-02 15:04"), m.AutomatonID, shortID(m.HandCommit), m.Verdict))
		for _, r := range m.Rows {
			line := fmt.Sprintf("  %d %s: %s", r.Number, r.Name, r.Result)
			if r.Detail != "" {
				line += " — " + r.Detail
			}
			merges = append(merges, line)
		}
	}
	section("Shadowed hand merges, newest first", merges)
	entry := func(es []autonomous.MergeSelectionEntry) []string {
		var l []string
		for _, e := range es {
			l = append(l, fmt.Sprintf("%s (%s): %s", e.AutomatonID, e.Branch, e.Summary()))
		}
		return l
	}
	section("Eligible now", entry(rep.Eligible))
	section("Excluded now, with why", entry(rep.Excluded))
	if selectionNote != "" {
		add("  %s", selectionNote)
	}
	section("The next push", rep.NextPush)
	section("What datawatch does outside a run once merging is on", rep.Actions)
	section("Settings and pipeline changes still to apply", rep.StillToApply)
	section("Planted faults the product's sandbox tests require to be held (this daemon plants none)", rep.PlantedFaults)
	if len(rep.Problems) > 0 {
		section("Problems building this report", rep.Problems)
	}
	add("")
	add("To turn it off: %s", rep.TurnOff)
	add("Go back to merging by hand at the first failed check, wrong version, or tag or push nobody asked for.")
	add("Earlier merge shadow plans and comparisons never count here.")
	return out
}
