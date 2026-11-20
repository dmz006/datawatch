package autonomous

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// JudgeFileLines is one changed file of a diff and its changed lines (added
// plus deleted).
type JudgeFileLines struct {
	Path  string
	Lines int
}

// JudgeGroup is one part of a split judge: the files it covers, its changed
// lines in total and a short name the merged verdict uses.
type JudgeGroup struct {
	Name  string
	Files []string
	Lines int
}

// JudgeDiffFiles lists the changed files of the Automaton's branch against its
// merge base with their changed lines, ignoring generated lock files (the same
// diff JudgeDiffLines counts). Binary files count 0 lines.
func JudgeDiffFiles(ctx context.Context, prd *PRD, projectDir string) ([]JudgeFileLines, error) {
	out, err := judgeNumstat(ctx, prd, projectDir)
	if err != nil {
		return nil, err
	}
	return ParseJudgeNumstatFiles(out)
}

// ParseJudgeNumstatFiles reads `git diff --numstat` output into one entry per
// file. A binary row counts 0 lines; a rename row keeps its new path.
func ParseJudgeNumstatFiles(out string) ([]JudgeFileLines, error) {
	var files []JudgeFileLines
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) < 3 {
			return nil, fmt.Errorf("judge diff files: unexpected numstat row %q", line)
		}
		n := 0
		for _, c := range cols[:2] {
			if c == "-" {
				continue
			}
			v, err := strconv.Atoi(c)
			if err != nil {
				return nil, fmt.Errorf("judge diff files: numstat row %q: %w", line, err)
			}
			n += v
		}
		files = append(files, JudgeFileLines{Path: renameTarget(cols[2]), Lines: n})
	}
	return files, nil
}

// renameTarget turns a numstat rename path ("a/{b => c}/d" or "a => b") into
// the new path.
func renameTarget(p string) string {
	if i := strings.Index(p, "{"); i >= 0 {
		if j := strings.Index(p[i:], "}"); j > 0 {
			inner := p[i+1 : i+j]
			if k := strings.Index(inner, " => "); k >= 0 {
				return path.Clean(p[:i] + inner[k+4:] + p[i+j+1:])
			}
		}
	}
	if k := strings.Index(p, " => "); k >= 0 {
		return p[k+4:]
	}
	return p
}

func isGeneratedLockFile(p string) bool {
	base := path.Base(p)
	for _, n := range generatedLockFiles {
		if base == n {
			return true
		}
	}
	return false
}

// SplitJudgeFiles groups the changed files by directory into groups of at most
// about threshold changed lines. Lock files are left out, as in
// JudgeDiffLines. Files of one directory stay together while they fit;
// a directory larger than the threshold is split file by file, and a single
// file larger than the threshold is a group of its own. Small directories are
// packed together in path order. A diff within the threshold (or with
// threshold 0) is one group. Every file lands in exactly one group, and the
// result is deterministic.
func SplitJudgeFiles(files []JudgeFileLines, threshold int) []JudgeGroup {
	kept := make([]JudgeFileLines, 0, len(files))
	for _, f := range files {
		if !isGeneratedLockFile(f.Path) {
			kept = append(kept, f)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Path < kept[j].Path })
	total := 0
	for _, f := range kept {
		total += f.Lines
	}
	if len(kept) == 0 {
		return nil
	}
	if threshold <= 0 || total <= threshold {
		return []JudgeGroup{newJudgeGroup(kept)}
	}
	// Bucket by directory, in path order.
	var dirs []string
	byDir := map[string][]JudgeFileLines{}
	for _, f := range kept {
		d := path.Dir(f.Path)
		if _, ok := byDir[d]; !ok {
			dirs = append(dirs, d)
		}
		byDir[d] = append(byDir[d], f)
	}
	// Atoms are runs of files that must not be separated.
	var atoms [][]JudgeFileLines
	for _, d := range dirs {
		fs := byDir[d]
		sum := 0
		for _, f := range fs {
			sum += f.Lines
		}
		if sum <= threshold {
			atoms = append(atoms, fs)
			continue
		}
		var cur []JudgeFileLines
		curLines := 0
		for _, f := range fs {
			if f.Lines > threshold {
				if len(cur) > 0 {
					atoms = append(atoms, cur)
					cur, curLines = nil, 0
				}
				atoms = append(atoms, []JudgeFileLines{f})
				continue
			}
			if curLines+f.Lines > threshold && len(cur) > 0 {
				atoms = append(atoms, cur)
				cur, curLines = nil, 0
			}
			cur = append(cur, f)
			curLines += f.Lines
		}
		if len(cur) > 0 {
			atoms = append(atoms, cur)
		}
	}
	// Pack neighbouring atoms while they fit.
	var groups []JudgeGroup
	var cur []JudgeFileLines
	curLines := 0
	flush := func() {
		if len(cur) > 0 {
			groups = append(groups, newJudgeGroup(cur))
			cur, curLines = nil, 0
		}
	}
	for _, a := range atoms {
		n := 0
		for _, f := range a {
			n += f.Lines
		}
		if curLines+n > threshold {
			flush()
		}
		cur = append(cur, a...)
		curLines += n
	}
	flush()
	// Names must be unique: the merged verdict names a group.
	seen := map[string]int{}
	for i := range groups {
		seen[groups[i].Name]++
		if c := seen[groups[i].Name]; c > 1 {
			groups[i].Name = fmt.Sprintf("%s (part %d)", groups[i].Name, c)
		}
	}
	return groups
}

func newJudgeGroup(fs []JudgeFileLines) JudgeGroup {
	g := JudgeGroup{}
	var dirs []string
	seen := map[string]bool{}
	for _, f := range fs {
		g.Files = append(g.Files, f.Path)
		g.Lines += f.Lines
		d := path.Dir(f.Path)
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	switch {
	case len(fs) == 1:
		g.Name = fs[0].Path
	case len(dirs) <= 3:
		g.Name = strings.Join(dirs, ", ")
	default:
		g.Name = fmt.Sprintf("%s and %d more directories", strings.Join(dirs[:2], ", "), len(dirs)-2)
	}
	return g
}

// JudgeGroupResult is the outcome of the judge on one group. TimedOut is true
// when the judge did not finish on any backend within its limit.
type JudgeGroupResult struct {
	Group    string
	Verdict  GuardrailVerdict
	TimedOut bool
}

// MergeJudgeVerdicts merges the per-group verdicts of a split judge into one.
// A group that was judged and found a problem always wins: the merged verdict
// is a block whose issues are every group's issues, each prefixed with the
// group name (a group that timed out is listed too, so it is not forgotten).
// Otherwise a group that timed out makes the merged verdict a judge-timeout
// hold naming the group, so nothing is passed unjudged. Otherwise a group whose
// judge failed without judging (infra, invalid response) returns that verdict
// with the group named. Otherwise warn, otherwise pass. at stamps the merged
// verdict.
func MergeJudgeVerdicts(guardrail string, results []JudgeGroupResult, at time.Time) GuardrailVerdict {
	merged := GuardrailVerdict{Guardrail: guardrail, VerdictAt: at}
	if len(results) == 0 {
		merged.Outcome, merged.Summary = "pass", "no changed files to judge"
		return merged
	}
	var content, timedOut, failed, warned []JudgeGroupResult
	for _, r := range results {
		v := r.Verdict
		switch {
		case r.TimedOut || IsJudgeTimeoutHold(v):
			r.TimedOut = true
			timedOut = append(timedOut, r)
		case v.Outcome == "block" && (v.Category == "infra" || v.Category == "invalid-response"):
			failed = append(failed, r)
		case v.Outcome == "block":
			content = append(content, r)
		case v.Outcome == "warn":
			warned = append(warned, r)
		}
	}
	prefixed := func(rs []JudgeGroupResult) []string {
		var out []string
		for _, r := range rs {
			if len(r.Verdict.Issues) == 0 && r.Verdict.Summary != "" {
				out = append(out, fmt.Sprintf("[%s] %s", r.Group, r.Verdict.Summary))
			}
			for _, is := range r.Verdict.Issues {
				out = append(out, fmt.Sprintf("[%s] %s", r.Group, is))
			}
		}
		return out
	}
	names := func(rs []JudgeGroupResult) string {
		var n []string
		for _, r := range rs {
			n = append(n, r.Group)
		}
		return strings.Join(n, ", ")
	}
	switch {
	case len(content) > 0:
		merged.Outcome = "block"
		merged.Severity = maxJudgeSeverity(content)
		for _, r := range content {
			if r.Verdict.Category == "policy" {
				merged.Category = "policy"
			}
		}
		merged.Issues = prefixed(content)
		for _, r := range timedOut {
			merged.Issues = append(merged.Issues, fmt.Sprintf("[%s] the judge did not finish on this group within its time limit; it was not judged", r.Group))
		}
		merged.Summary = fmt.Sprintf("rules review found problems in %s", names(content))
		if len(timedOut) > 0 {
			merged.Summary += fmt.Sprintf("; the judge did not finish on %s", names(timedOut))
		}
		merged.JudgeSessionID = content[0].Verdict.JudgeSessionID
		merged.RawJudge = content[0].Verdict.RawJudge
		merged.JudgeOutputPath = content[0].Verdict.JudgeOutputPath
	case len(timedOut) > 0:
		merged.Outcome, merged.Severity, merged.Category = "block", "high", JudgeTimeoutCategory
		merged.Summary = fmt.Sprintf("judge did not finish on %s within its time limit; the Automaton needs review", names(timedOut))
		for _, r := range timedOut {
			if len(r.Verdict.Issues) == 0 {
				merged.Issues = append(merged.Issues, fmt.Sprintf("[%s] %s", r.Group, r.Verdict.Summary))
			}
			for _, is := range r.Verdict.Issues {
				merged.Issues = append(merged.Issues, fmt.Sprintf("[%s] %s", r.Group, is))
			}
			merged.JudgeAttempts = append(merged.JudgeAttempts, r.Verdict.JudgeAttempts...)
		}
	case len(failed) > 0:
		merged = failed[0].Verdict
		merged.Guardrail, merged.VerdictAt = guardrail, at
		merged.Summary = fmt.Sprintf("[%s] %s", failed[0].Group, merged.Summary)
	case len(warned) > 0:
		merged.Outcome, merged.Severity = "warn", maxJudgeSeverity(warned)
		merged.Issues = prefixed(warned)
		merged.Summary = fmt.Sprintf("rules review has warnings in %s", names(warned))
	default:
		merged.Outcome = "pass"
		merged.Summary = fmt.Sprintf("rules review passed on all %d groups", len(results))
		merged.JudgeSessionID = results[len(results)-1].Verdict.JudgeSessionID
	}
	return merged
}

func maxJudgeSeverity(rs []JudgeGroupResult) string {
	rank := map[string]int{"": 0, "info": 1, "low": 2, "medium": 3, "high": 4, "critical": 5}
	best := ""
	for _, r := range rs {
		if rank[r.Verdict.Severity] > rank[best] {
			best = r.Verdict.Severity
		}
	}
	return best
}
