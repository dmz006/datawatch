package mergeshadow

import (
	"fmt"
	"sort"
	"strings"
)

// The results of one row.
const (
	RowMatch       = "match"
	RowDiffers     = "differs"
	RowAsked       = "asked"
	RowWaiting     = "waiting"
	RowNotCompared = "not_compared"
)

// The verdicts of one replay.
const (
	VerdictMatch   = "match"
	VerdictDiffers = "differs"
	// VerdictAsked: the daemon would have asked about a conflict. It neither
	// counts toward the streak nor breaks it.
	VerdictAsked = "asked"
	// VerdictWaiting: everything compared matches, and the hand merge's check
	// results have not been recorded.
	VerdictWaiting = "waiting"
	// VerdictRunning: the shadow has not finished.
	VerdictRunning = "running"
)

// The numbers of the seven rows.
const (
	RowSelection = iota + 1
	RowContent
	RowVersion
	RowChecks
	RowConflicts
	RowLeak
	RowSideEffects
)

// Row is one of the seven results of a replay.
type Row struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	Result string `json:"result"`
	Detail string `json:"detail"`
}

// Rows works out the seven results from the stored facts. Nothing missing ever
// counts as a match.
func (r *Replay) Rows() []Row {
	names := []string{"selection", "content", "version", "checks", "conflicts", "leak", "side effects"}
	rows := make([]Row, len(names))
	why := ""
	switch r.State {
	case ReplayDone:
	case ReplayError:
		why = "the shadow could not be run: " + r.Error
	default:
		why = "the shadow has not finished"
	}
	for i, n := range names {
		rows[i] = Row{Number: i + 1, Name: n, Result: RowNotCompared, Detail: why}
	}
	if why != "" {
		return rows
	}
	set := func(n int, result, format string, a ...any) {
		rows[n-1].Result, rows[n-1].Detail = result, fmt.Sprintf(format, a...)
	}
	r.selectionRow(set)
	r.sideEffectRow(set)
	r.conflictRow(set)
	if r.Daemon.Asked {
		for _, n := range []int{RowContent, RowVersion, RowChecks, RowLeak} {
			set(n, RowNotCompared, "the run stopped where it would have asked about a conflict")
		}
		return rows
	}
	r.contentRow(set)
	r.versionRow(set)
	r.checksRow(set)
	r.leakRow(set)
	return rows
}

type rowSetter func(n int, result, format string, a ...any)

// stopped says why the shadow run produced no candidate to compare.
func (r *Replay) stopped() string {
	switch {
	case r.Daemon.StoppedAt != "":
		return "the shadow run stopped at " + r.Daemon.StoppedAt
	case r.Daemon.Reason != "":
		return "the shadow run did not finish: " + r.Daemon.Reason
	}
	return "the shadow run made no push list"
}

func (r *Replay) selectionRow(set rowSetter) {
	switch s := r.Selection; {
	case s == nil:
		set(RowSelection, RowNotCompared, "the selection rule gave no answer")
	case !s.Eligible:
		set(RowSelection, RowDiffers, "datawatch would not have merged this work: %s", strings.Join(s.Reasons, "; "))
	default:
		set(RowSelection, RowMatch, "eligible on commit %s (%s)", s.Tip, s.Basis)
	}
}

func (r *Replay) contentRow(set rowSetter) {
	switch c := r.Content; {
	case !c.Compared:
		set(RowContent, RowNotCompared, "the two trees were not compared: %s", r.stopped())
	case len(c.Differences) > 0:
		set(RowContent, RowDiffers, "%d file(s) differ: %s", len(c.Differences), strings.Join(c.Differences, ", "))
	case len(c.Allowed) > 0:
		set(RowContent, RowMatch, "same tree apart from what may differ in %s", strings.Join(c.Allowed, ", "))
	default:
		set(RowContent, RowMatch, "same tree")
	}
}

func (r *Replay) versionRow(set rowSetter) {
	d, h := r.Daemon, r.Hand
	var tags []string
	for _, p := range d.Push {
		if t, ok := strings.CutPrefix(p.Ref, "refs/tags/"); ok {
			tags = append(tags, t)
		}
	}
	got := fmt.Sprintf("datawatch %s (%s), hand %s (%s)", orNone(d.Version), orNone(string(d.Size)), orNone(h.Version), orNone(string(h.Size)))
	switch {
	case len(d.Push) == 0:
		set(RowVersion, RowNotCompared, "%s", r.stopped())
	case len(tags) > 0:
		set(RowVersion, RowDiffers, "datawatch would push a tag: %s", strings.Join(tags, ", "))
	case len(h.Tags) > 0:
		set(RowVersion, RowDiffers, "the hand merge made a tag: %s", strings.Join(h.Tags, ", "))
	case h.Version == "" || d.Version != h.Version || d.Size != h.Size:
		set(RowVersion, RowDiffers, "%s", got)
	default:
		set(RowVersion, RowMatch, "%s, no tag", got)
	}
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func (r *Replay) checksRow(set rowSetter) {
	if len(r.Daemon.NotRun) == 0 {
		r.checkResults(set)
		return
	}
	// A step that did not run never lets the result match. What differs in
	// the checks that did run is said first: a failed check is often why.
	result, detail := "", ""
	r.checkResults(func(_ int, res, format string, a ...any) { result, detail = res, fmt.Sprintf(format, a...) })
	var left []string
	for _, s := range r.Daemon.NotRun {
		left = append(left, fmt.Sprintf("%s (%s step): %s", s.Name, s.Kind, s.Why))
	}
	why := fmt.Sprintf("the shadow did not run %d step(s) of the pipeline, so nothing shows they would pass: %s", len(left), strings.Join(left, "; "))
	if result == RowDiffers {
		why = detail + "; " + why
	}
	set(RowChecks, RowDiffers, "%s", why)
}

// checkResults compares the lint and test steps with the hand merge's.
func (r *Replay) checkResults(set rowSetter) {
	lint, test := false, false
	daemon, why := map[string]string{}, map[string]string{}
	var order []string
	for _, c := range r.Daemon.Checks {
		lint, test = lint || c.Kind == "lint", test || c.Kind == "test"
		daemon[c.Name] = c.Result
		if c.Why != "" {
			why[c.Name] = " (" + c.Why + ")"
		}
		order = append(order, c.Name)
	}
	if !lint || !test {
		set(RowChecks, RowDiffers, "the pipeline needs at least one lint step and one test step to be compared; it ran %d check step(s)", len(order))
		return
	}
	if len(r.HandChecks) == 0 {
		var ran []string
		failed := false
		for _, n := range order {
			ran = append(ran, n+" "+daemon[n]+why[n])
			failed = failed || daemon[n] != "ok"
		}
		if failed {
			set(RowChecks, RowDiffers, "not every check ran and passed: %s", strings.Join(ran, ", "))
			return
		}
		set(RowChecks, RowWaiting, "record the hand merge's result for each of: %s", strings.Join(order, ", "))
		return
	}
	extra := make([]string, 0, len(r.HandChecks))
	for n := range r.HandChecks {
		if _, ok := daemon[n]; !ok {
			extra = append(extra, n)
		}
	}
	sort.Strings(extra)
	var misses, same []string
	for _, n := range order {
		d, h := daemon[n], r.HandChecks[n]
		switch {
		case h == "":
			misses = append(misses, n+": no hand result is recorded")
		case d == "ok" && h == HandCheckPass, d == "failed" && h == HandCheckFail:
			same = append(same, n+" "+h)
		case d == "ok" && h == HandCheckFail:
			misses = append(misses, n+": serious, datawatch passed what failed by hand")
		default:
			misses = append(misses, fmt.Sprintf("%s: datawatch %s%s, hand %s", n, d, why[n], h))
		}
	}
	for _, n := range extra {
		misses = append(misses, n+": checked by hand, and the pipeline has no step of that name")
	}
	if len(misses) > 0 {
		set(RowChecks, RowDiffers, "%s", strings.Join(misses, "; "))
		return
	}
	set(RowChecks, RowMatch, "same results: %s", strings.Join(same, ", "))
}

func (r *Replay) conflictRow(set rowSetter) {
	d, h := sortedCopy(r.Daemon.Conflicts), sortedCopy(r.Hand.Conflicts)
	switch {
	case r.Daemon.Asked:
		set(RowConflicts, RowAsked, "datawatch would ask: %s", r.Daemon.AskNote)
	case len(r.Daemon.Push) == 0:
		set(RowConflicts, RowNotCompared, "%s", r.stopped())
	case strings.Join(d, "\x00") != strings.Join(h, "\x00"):
		set(RowConflicts, RowDiffers, "conflicted files differ: datawatch [%s], hand [%s]", strings.Join(d, ", "), strings.Join(h, ", "))
	case len(h) == 0:
		set(RowConflicts, RowMatch, "no conflict on either side")
	case !r.Content.Compared || len(r.Content.Differences) > 0:
		set(RowConflicts, RowDiffers, "same conflicted files (%s), and the results are not shown to be equal", strings.Join(h, ", "))
	default:
		set(RowConflicts, RowMatch, "same conflicted files (%s) and the same result", strings.Join(h, ", "))
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func (r *Replay) leakRow(set rowSetter) {
	l := r.Leak
	var refs []string
	other := ""
	for _, p := range r.Daemon.Push {
		refs = append(refs, p.Ref)
		if p.Ref != "refs/heads/main" {
			other = p.Ref
		}
	}
	switch {
	case !l.Evaluated || len(refs) == 0:
		set(RowLeak, RowNotCompared, "the push list was not checked: %s", r.stopped())
	case other != "":
		set(RowLeak, RowDiffers, "the push list holds more than main: %s", other)
	case l.Refusal != "":
		set(RowLeak, RowDiffers, "the push would be refused: %s", l.Refusal)
	case !l.ProjectCheckRan || !l.ProjectCheckPassed:
		set(RowLeak, RowDiffers, "the project's local checks did not run and pass (it needs a check-outgoing make target on the base commit)")
	case l.PrivatePatterns <= 0:
		set(RowLeak, RowDiffers, "the project's check did not show a loaded private pattern list")
	case !l.SecretScanRan:
		set(RowLeak, RowDiffers, "the secret scan did not run")
	case l.SecretFindings > 0:
		set(RowLeak, RowDiffers, "the secret scan has %d secret finding(s)", l.SecretFindings)
	default:
		set(RowLeak, RowMatch, "only main, no tag; secret scan clean; local checks clean with %d private pattern(s) loaded", l.PrivatePatterns)
	}
}

func (r *Replay) sideEffectRow(set rowSetter) {
	e := r.SideEffects
	switch {
	case !e.Checked:
		set(RowSideEffects, RowNotCompared, "side effects were not checked")
	case len(e.Found) > 0:
		set(RowSideEffects, RowDiffers, "%s", strings.Join(e.Found, "; "))
	case len(e.Elsewhere) > 0:
		set(RowSideEffects, RowMatch, "none from the shadow; changed meanwhile by others: %s", strings.Join(e.Elsewhere, "; "))
	default:
		set(RowSideEffects, RowMatch, "no ref, worktree or process left behind")
	}
}

// Verdict is the replay's result as a whole.
func (r *Replay) Verdict() string {
	switch r.State {
	case ReplayDone:
	case ReplayError:
		return VerdictDiffers
	default:
		return VerdictRunning
	}
	rows := r.Rows()
	if r.Daemon.Asked {
		if rows[RowSelection-1].Result == RowMatch && rows[RowSideEffects-1].Result == RowMatch {
			return VerdictAsked
		}
		return VerdictDiffers
	}
	waiting := false
	for _, row := range rows {
		switch row.Result {
		case RowMatch:
		case RowWaiting:
			waiting = true
		default:
			return VerdictDiffers
		}
	}
	if waiting {
		return VerdictWaiting
	}
	return VerdictMatch
}

// StreakResult is the run of matched hand merges, newest first.
type StreakResult struct {
	Length int `json:"length"`
	// Total is how many replays are stored for the project.
	Total int `json:"total"`
	// Bugs, Features and WithConflict count the matched merges of the run.
	Bugs         int `json:"bugs"`
	Features     int `json:"features"`
	WithConflict int `json:"with_conflict"`
	// Asked counts the "would ask" merges passed over.
	Asked     int    `json:"asked"`
	StoppedBy string `json:"stopped_by,omitempty"`
}

// ReplayStreak counts the newest replays of one project that matched, with the
// settings and check files in effect now. "Would ask" is passed over. Anything
// else that is not a match ends the run, and so does a replay made with other
// merge settings or other check files: checksHash answers what the check files
// a replay lists hash to now.
//
// Only hand merges with a stored shadow are seen here, and one without a
// shadow must not be counted across. So the run goes on only where a shadowed
// merge starts at the very commit the one before it recorded, and it counts
// only while main holds nothing after the newest: sinceNewest answers why main
// may hold more than the commit given, "" when it holds nothing more. Whether
// what came between was a hand merge cannot be told, so anything ends the run.
func ReplayStreak(replays []*Replay, project, settingsHash string, checksHash func(files []string) string, sinceNewest func(commit string) string) StreakResult {
	out, newest := matchedRun(replays, project, settingsHash, checksHash)
	if out.Length == 0 {
		return out // nothing is claimed, and the reason found stands
	}
	if why := sinceNewest(newest.Hand.Commit); why != "" {
		return StreakResult{Total: out.Total, StoppedBy: fmt.Sprintf("Automaton %s (%s), the newest shadowed hand merge: %s",
			newest.AutomatonID, shortCommit(newest.Hand.Commit), why)}
	}
	return out
}

// matchedRun counts the run of matches among the stored shadows alone, and
// returns the newest of them.
func matchedRun(replays []*Replay, project, settingsHash string, checksHash func(files []string) string) (StreakResult, *Replay) {
	var mine []*Replay
	for _, r := range replays {
		if r.Project == project {
			mine = append(mine, r)
		}
	}
	SortReplays(mine)
	out := StreakResult{Total: len(mine)}
	if len(mine) == 0 {
		return out, nil
	}
	newest := mine[0]
	now := map[string]string{}
	for i, r := range mine {
		if i > 0 {
			if newer := mine[i-1]; r.Hand.Commit == "" || newer.Hand.Base != r.Hand.Commit {
				out.StoppedBy = fmt.Sprintf("a gap before Automaton %s (%s): its merge starts at %s, not at %s, which Automaton %s recorded; whether a hand merge that got no shadow is in between cannot be told",
					newer.AutomatonID, shortCommit(newer.Hand.Commit), orNone(shortCommit(newer.Hand.Base)), orNone(shortCommit(r.Hand.Commit)), r.AutomatonID)
				return out, newest
			}
		}
		verdict := r.Verdict()
		stop := func(why string) (StreakResult, *Replay) {
			out.StoppedBy = fmt.Sprintf("Automaton %s (%s): %s", r.AutomatonID, shortCommit(r.Hand.Commit), why)
			return out, newest
		}
		if verdict != VerdictMatch && verdict != VerdictAsked {
			return stop("its verdict is " + verdict)
		}
		if r.SettingsHash != settingsHash {
			return stop("the pipeline or merge settings have changed since")
		}
		key := strings.Join(r.CheckFiles, "\x00")
		cur, ok := now[key]
		if !ok {
			cur = checksHash(r.CheckFiles)
			now[key] = cur
		}
		if cur == "" || r.ChecksHash != cur {
			return stop("the check files have changed since")
		}
		if verdict == VerdictAsked {
			out.Asked++
			continue
		}
		out.Length++
		switch r.Kind {
		case "bug":
			out.Bugs++
		case "feature":
			out.Features++
		}
		if len(r.Hand.Conflicts) > 0 {
			out.WithConflict++
		}
	}
	return out, newest
}

// SortReplays orders replays newest hand merge first.
func SortReplays(rs []*Replay) {
	when := func(r *Replay) (t int64) {
		if !r.Hand.At.IsZero() {
			return r.Hand.At.UnixNano()
		}
		return r.CreatedAt.UnixNano()
	}
	sort.Slice(rs, func(i, j int) bool {
		a, b := when(rs[i]), when(rs[j])
		if a != b {
			return a > b
		}
		return rs[i].Key > rs[j].Key
	})
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
