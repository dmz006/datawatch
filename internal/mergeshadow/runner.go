package mergeshadow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
)

// scratchDirName is the directory under the daemon data directory that holds
// the trial worktrees. It sits outside every project tree.
const scratchDirName = "merge-shadow-scratch"

// defaultBaseRef is the base ref used when the input names none.
const defaultBaseRef = "main"

// dateLayout is the date form used in the README release line.
const dateLayout = "2006-01-02"

// GateFunc runs the build, lint and test gates in dir, each bounded by
// timeout. RunGates is the production value; tests substitute a fake.
type GateFunc func(ctx context.Context, dir string, timeout time.Duration) []GateResult

// Input is everything the Runner reads about one finished Automaton. The
// caller copies it out of the Automaton; the Runner reads nothing else.
type Input struct {
	// AutomatonID names the plan file and the registry row.
	AutomatonID string
	// Project is the project key the merge pipeline settings are read for.
	Project string
	// RepoPath is the project's repository (its working directory).
	RepoPath string
	// Branch is the work branch the Automaton merges from.
	Branch string
	// BaseRef is the default branch the merge would land on; empty means main.
	BaseRef string
	// DataDir is the daemon data directory: the plan store and the scratch
	// directory for trial worktrees both live under it.
	DataDir string
	// Pipeline is the merge pipeline settings: version files and registry file.
	Pipeline config.MergePipelineConfig
	// ChangeRecord is the Automaton's change record, nil when it has none.
	ChangeRecord *changerecord.ChangeRecord
	// ChangelogTasks are the Automaton's tasks, carrying their output, so the
	// changelog entry can be read from the entry task.
	ChangelogTasks []ChangelogTask
	// GateTimeout bounds each gate. Zero or less uses the config default.
	GateTimeout time.Duration
	// Review is the review verdict on the work branch, nil when no review was
	// recorded. A non-nil verdict must name the branch head (CheckReviewHead).
	Review *ReviewVerdict
}

// Runner builds a shadow merge plan for one finished Automaton and stores it.
// Its zero value uses RunGates and the wall clock.
type Runner struct {
	// Gates runs the build, lint and test gates. nil means RunGates.
	Gates GateFunc
	// Now returns the current time. nil means time.Now.
	Now func() time.Time
}

// Run performs the shadow merge steps in order and stores the result:
//
//  0. list the Automaton's task branches and block the plan on any that holds
//     commits the work branch does not (CheckUnmergedTaskBranches), and, when
//     a review verdict is given, block it when the verdict names a commit other
//     than the branch head (CheckReviewHead); when any blocks, steps 1 to 3
//     are not run;
//  1. build the trial merge of the work branch into the base ref, in a
//     throwaway worktree (TrialMergeWith);
//  2. classify each conflicted path by file class;
//  3. run the gates in that worktree, unless the merge has conflicts, in
//     which case every gate is recorded as skipped;
//  4. take the changelog entry from the entry task;
//  5. derive the README line, registry row and parity-issue flag, and the
//     release size (the planned version against the base ref's version);
//  6. write the plan with Applied false.
//
// A failed trial merge is not an error: the plan is still written, with a
// blocking finding. Nothing is applied to the repository. A cancelled context
// and a failure to read the base ref or write the plan are errors.
func (r Runner) Run(ctx context.Context, in Input) (*MergePlan, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	base := in.BaseRef
	if base == "" {
		base = defaultBaseRef
	}
	gates := r.Gates
	if gates == nil {
		gates = RunGates
	}
	now := r.now().UTC()
	timeout := in.GateTimeout
	if timeout <= 0 {
		timeout = config.DefaultMergeShadowGateTimeout
	}
	cls := NewClassifier(in.Pipeline, in.Project)

	plan := &MergePlan{
		AutomatonID: in.AutomatonID,
		Project:     in.Project,
		Branch:      in.Branch,
		BaseRef:     base,
		CreatedAt:   now,
		Applied:     false,
	}

	// Step 0: a task branch of the Automaton with commits the Automaton branch
	// lacks would be dropped by the merge, so it blocks the plan. The trial
	// merge and the gates are not run against such a branch.
	taskFindings, err := CheckUnmergedTaskBranches(ctx, in.RepoPath, in.Branch)
	if err != nil {
		return nil, err
	}
	// A review that does not name the branch head is stale and blocks the
	// plan the same way. It is checked only when a verdict was recorded.
	if in.Review != nil {
		reviewFindings, err := CheckReviewHead(ctx, in.RepoPath, in.Branch, in.Review.Commit)
		if err != nil {
			return nil, err
		}
		taskFindings = append(taskFindings, reviewFindings...)
	}

	// Steps 1 to 3: the trial merge, its conflicts and the gates on it.
	var conflicts []Conflict
	var gateList []Gate
	var trialErr error
	if len(taskFindings) > 0 {
		conflicts = []Conflict{}
		gateList = skippedGates("not run: the Automaton branch is blocked by step 0 findings")
	} else {
		_, trialErr = TrialMergeWith(ctx, in.RepoPath, base, in.Branch,
			filepath.Join(in.DataDir, scratchDirName),
			func(wt string, paths []string) error {
				conflicts = classifyConflicts(cls, paths)
				if len(paths) > 0 {
					gateList = skippedGates("not run: the trial merge has conflicts")
					return nil
				}
				gateList = toGates(gates(ctx, wt, timeout))
				return nil
			})
		if cerr := ctx.Err(); cerr != nil {
			return nil, cerr
		}
		if trialErr != nil {
			conflicts = []Conflict{}
			gateList = skippedGates("not run: the trial merge did not complete")
		}
	}

	// Step 4: the changelog entry.
	entry := ReadChangelogEntry(in.ChangelogTasks)
	plan.ChangelogSource = entry.Source
	plan.ChangelogHeading = entry.Heading
	plan.ChangelogBody = entry.Body
	planned := versionFromHeading(entry.Heading)
	plan.PlannedVersion = planned

	// Step 5: the release size and the derived lines.
	plan.ReleaseSize = ReleaseSizeNone
	if planned != "" {
		landed, err := ReadLanded(ctx, in.RepoPath, base, LandedQuery{
			Project:     in.Project,
			AutomatonID: in.AutomatonID,
			Version:     planned,
			Pipeline:    in.Pipeline,
		})
		if err != nil {
			return nil, fmt.Errorf("mergeshadow: read base version: %w", err)
		}
		plan.ReleaseSize = releaseSizeBetween(landedVersion(landed, in.Pipeline, in.Project), planned)
	}
	d, err := Derive(in.ChangeRecord, planned, now.Format(dateLayout), "")
	if err != nil {
		return nil, err
	}
	if planned != "" {
		plan.ReadmeLine = d.ReadmeLine
	}
	plan.RegistryRow = d.RegistryRow
	plan.ParityIssueNeeded = d.ParityIssueNeeded

	// Step 6: the record itself.
	plan.Conflicts = conflicts
	plan.Gates = gateList
	plan.Findings = append(taskFindings, findingsFor(trialErr, conflicts, gateList, entry.Source)...)
	if err := Save(in.DataDir, in.AutomatonID, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func (r Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// validate reports the first required input that is missing.
func (in Input) validate() error {
	switch {
	case in.AutomatonID == "":
		return errors.New("mergeshadow: automaton id is required")
	case in.RepoPath == "":
		return errors.New("mergeshadow: repository path is required")
	case in.Branch == "":
		return errors.New("mergeshadow: work branch is required")
	case in.DataDir == "":
		return errors.New("mergeshadow: data directory is required")
	}
	return nil
}

// classifyConflicts gives each conflicted path its file class. A mechanical
// path is resolved by a fixed rewrite; every other path stays unresolved, and
// no resolution is guessed for it.
func classifyConflicts(cls Classifier, paths []string) []Conflict {
	out := make([]Conflict, 0, len(paths))
	for _, p := range paths {
		c := Conflict{Path: p, FileClass: string(cls.Classify(p))}
		if cls.Classify(p) == FileClassMechanical {
			c.Resolution = "mechanical rewrite"
			c.ResolvedBy = "mechanical"
		} else {
			c.Resolution = "unresolved"
		}
		out = append(out, c)
	}
	return out
}

// toGates converts gate results into the plan's gate records.
func toGates(results []GateResult) []Gate {
	out := make([]Gate, 0, len(results))
	for _, r := range results {
		out = append(out, Gate{Name: r.Name, Status: string(r.Status), Duration: r.Duration, Detail: gateDetail(r)})
	}
	return out
}

// skippedGates records every gate as skipped with the reason why it did not run.
func skippedGates(reason string) []Gate {
	out := make([]Gate, 0, len(gateNames))
	for _, name := range gateNames {
		out = append(out, Gate{Name: name, Status: string(GateSkipped), Detail: reason})
	}
	return out
}

// findingsFor lists the blocking problems of a plan: a trial merge that did
// not complete, each unresolved conflict, each gate that failed or timed out,
// and a missing changelog entry.
func findingsFor(trialErr error, conflicts []Conflict, gates []Gate, changelogSource string) []Finding {
	var out []Finding
	if trialErr != nil {
		out = append(out, Finding{Code: "trial_merge_failed", Message: trialErr.Error(), Blocking: true})
	}
	for _, c := range conflicts {
		if c.ResolvedBy == "" {
			out = append(out, Finding{Code: "conflict_unresolved", Message: c.Path + " (" + c.FileClass + ")", Blocking: true})
		}
	}
	for _, g := range gates {
		if g.Status == string(GateFail) || g.Status == string(GateTimeout) {
			out = append(out, Finding{Code: "gate_" + g.Status, Message: g.Name + ": " + g.Detail, Blocking: true})
		}
	}
	if changelogSource == ChangelogSourceMissing {
		out = append(out, Finding{Code: "changelog_missing", Message: "no task produced a usable changelog entry", Blocking: true})
	}
	return out
}

// landedVersion returns the first version the base ref holds in the project's
// version files, in the configured order, or "" when none is found.
func landedVersion(l Landed, cfg config.MergePipelineConfig, project string) string {
	for _, vf := range cfg.EffectiveVersionFiles(project) {
		if v, ok := l.Versions[vf]; ok && v.Found {
			return v.Value
		}
	}
	return ""
}

// versionFromHeading returns the version of a changelog heading of the form
// "## [version] - YYYY-MM-DD", or "" for any other text.
func versionFromHeading(heading string) string {
	m := changelogHeadingRe.FindStringSubmatch(heading)
	if m == nil {
		return ""
	}
	return m[1]
}

// releaseSizeBetween names the size of the step from the base version to the
// planned one. Anything that is not a plain X.Y.Z version on both sides, or a
// planned version that is not above the base, is none: nothing is inferred.
func releaseSizeBetween(from, to string) ReleaseSize {
	fv, ok1 := parseSemver(from)
	tv, ok2 := parseSemver(to)
	if !ok1 || !ok2 {
		return ReleaseSizeNone
	}
	switch {
	case !semverAbove(tv, fv):
		return ReleaseSizeNone
	case tv[0] > fv[0]:
		return ReleaseSizeMajor
	case tv[1] > fv[1]:
		return ReleaseSizeMinor
	default:
		return ReleaseSizePatch
	}
}

// parseSemver reads "X.Y.Z" or "vX.Y.Z" into its three numbers.
func parseSemver(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// semverAbove reports whether a is a higher version than b.
func semverAbove(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// gateDetail is the Detail recorded on the plan's Gate. A load_sensitive gate
// also carries its finding, the first failing lines, so the plan keeps them.
func gateDetail(r GateResult) string {
	detail := r.Detail
	if r.Name == "test" && r.Status == GateFail {
		detail = joinDetail(detail, failingTestsDetail(r.Output))
	}
	if r.Status != GateLoadSensitive || r.Finding == "" {
		return detail
	}
	return joinDetail(detail, "finding: "+strings.ReplaceAll(r.Finding, "\n", " | "))
}
