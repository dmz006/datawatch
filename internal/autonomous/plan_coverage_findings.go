// plan_coverage_findings.go — deterministic, no-LLM *structural* plan
// checking. Where plan_coverage.go verdicts how well a plan covers the
// spec's extractable requirements, this file checks the plan's internal
// shape: dependency resolution, acyclicity, file provenance, per-task
// verification (or an explicit B124 exemption), and naming hygiene.
// Everything here is pure string/path logic: no LLM call, no daemon, no
// state — the same PRD + stories always produce the same report.
//
// Rules (each violation is one typed StructuralFinding):
//
//  1. depends_on resolution — every story/task depends_on id must exist
//     in the plan and point to an *earlier or same-story* target;
//     unknown ids are dangling findings, later-story targets are
//     forward findings;
//  2. acyclicity — no depends_on cycle in the combined story+task
//     dependency graph (DFS over both node sets);
//  3. file provenance — every file-like path named in a story/task
//     (FilesGenerated entries and file-like tokens in titles/specs)
//     must either exist in the project tree (relative to
//     PRD.ProjectDir when set, else the current tree) or be produced by
//     a plan task at or before the referencing point (present in that
//     task's FilesGenerated); a story/task's own FilesGenerated list is
//     a *declared output*, i.e. the source of the "created by a task"
//     allowance — those entries are not treated as references, but
//     every FilesGenerated entry of a story that no task in the plan
//     generates is flagged;
//  4. verification — every task must carry a VerificationScript OR an
//     explicit VerificationScriptReason (the B124 exemption: a task may
//     legitimately have no script, but it must say why);
//  5. naming — no task title or generated file name may embed a tracker
//     id (BL + digits, case-insensitive), per AGENT.md's naming rule;
//  6. failing — any finding of any kind fails the check (Pass == false);
//     the caller decides whether that means warn or block.
package autonomous

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The typed kinds a StructuralFinding can carry. String constants so
// callers can pattern-match without duplicating the spellings.
const (
	// FindingDanglingDependsOn — a depends_on id that exists nowhere in
	// the plan.
	FindingDanglingDependsOn = "depends_on_dangling"
	// FindingForwardDependsOn — a depends_on target in a *later* story;
	// dependencies must point to an earlier or same-story target.
	FindingForwardDependsOn = "depends_on_forward"
	// FindingDependencyCycle — a cycle in the combined story+task
	// dependency graph; Detail is the cycle path (e.g. "a -> b -> a").
	FindingDependencyCycle = "dependency_cycle"
	// FindingUnresolvedFile — a named file-like path that exists neither
	// in the project tree nor in any producing task's FilesGenerated.
	FindingUnresolvedFile = "unresolved_file"
	// FindingMissingVerification — a task with neither
	// VerificationScript nor VerificationScriptReason (no B124
	// exemption stated).
	FindingMissingVerification = "missing_verification"
	// FindingTrackerIDInName — a task title or generated file name that
	// embeds a tracker id (bl + digits, case-insensitive).
	FindingTrackerIDInName = "tracker_id_in_name"
)

// StructuralFinding is one typed, deterministic finding in a decomposed
// plan. Kind is one of the Finding* constants; Detail is a stable,
// human-readable sentence (safe to diff and log, never LLM-generated).
// StoryID/TaskID identify the offending story/task when applicable
// (zero value when the finding is plan-level or story-level).
type StructuralFinding struct {
	Kind    string
	Detail  string
	StoryID string
	TaskID  string
}

// PlanStructureReport is the full result of CheckPlanStructure.
// Findings is the typed, deduplicated, plan-ordered finding list; Pass
// mirrors len(Findings) == 0 — per rule 6, a finding of *any* kind
// fails the check, and the caller decides whether that means warn or
// block.
type PlanStructureReport struct {
	Findings []StructuralFinding
	Pass     bool
}

// Count returns the number of structural findings in the report (0 for
// a nil report).
func (r *PlanStructureReport) Count() int {
	if r == nil {
		return 0
	}
	return len(r.Findings)
}

var (
	// planFileRefRe — a file-like token in free text: either a
	// slash-separated path ("reports/daily.txt",
	// "pkg/util/helper.go") or a bare file name with a 2-6 letter
	// alphabetic extension ("store.go", "README.md"). Pure-numeric
	// finals ("v5.26.64", "5.2") and single-letter "extensions"
	// ("e.g") do not match — both are filtered below as well.
	planFileRefRe = regexp.MustCompile(
		`(?:[A-Za-z0-9_+./-]+/)+[A-Za-z0-9_+.-]+|[A-Za-z0-9_][A-Za-z0-9_-]*\.[A-Za-z][A-Za-z0-9]{1,5}`)
	// trackerIDRefRe — a tracker id: BL followed by one or more digits,
	// case-insensitive ("BL124", "bl9"). Per the structural rule this
	// is a plain containment match on task titles and generated names.
	trackerIDRefRe = regexp.MustCompile(`(?i)bl[0-9]+`)
)

// CheckPlanStructure runs the six structural rules above over the
// in-memory plan stories and returns the typed finding list. It is
// deterministic: the same arguments always yield the same report.
// prd may be nil, in which case the project tree is the current
// directory. The only I/O is read-only os.Stat of candidate file paths.
func CheckPlanStructure(prd *PRD, stories []Story) *PlanStructureReport {
	report := &PlanStructureReport{Findings: []StructuralFinding{}}
	add := func(f StructuralFinding) {
		for _, x := range report.Findings {
			if x.Kind == f.Kind && x.StoryID == f.StoryID &&
				x.TaskID == f.TaskID && x.Detail == f.Detail {
				return // already reported — keep findings deduplicated
			}
		}
		report.Findings = append(report.Findings, f)
	}
	defer func() { report.Pass = len(report.Findings) == 0 }()

	// Plan-order indexes used by rules 1, 2 and 3.
	storyIdx := make(map[string]int, len(stories))
	taskSlot := make(map[string]taskSlotT) // taskID -> (story, task) slot
	for si, s := range stories {
		storyIdx[s.ID] = si
		for ti := range s.Tasks {
			taskSlot[s.Tasks[ti].ID] = taskSlotT{si: si, ti: ti}
		}
	}

	// --- Rule 1: depends_on resolution + ordering. ---
	for si, s := range stories {
		for _, d := range s.DependsOn {
			tj, ok := storyIdx[d]
			if !ok {
				add(StructuralFinding{
					Kind:    FindingDanglingDependsOn,
					StoryID: s.ID,
					Detail:  "story " + s.ID + " depends_on references unknown story id " + strconv.Quote(d),
				})
				continue
			}
			if tj > si {
				add(StructuralFinding{
					Kind:    FindingForwardDependsOn,
					StoryID: s.ID,
					Detail: "story " + s.ID + " depends_on points to later story " +
						stories[tj].ID + " (" + strconv.Quote(d) +
						"); dependencies must point to an earlier or same-story target",
				})
			}
		}
		for _, t := range s.Tasks {
			for _, d := range t.DependsOn {
				pos, ok := taskSlot[d]
				if !ok {
					add(StructuralFinding{
						Kind:    FindingDanglingDependsOn,
						StoryID: s.ID,
						TaskID:  t.ID,
						Detail: "task " + t.ID + " depends_on references unknown task id " + strconv.Quote(d),
					})
					continue
				}
				if pos.si > si {
					add(StructuralFinding{
						Kind:    FindingForwardDependsOn,
						StoryID: s.ID,
						TaskID:  t.ID,
						Detail: "task " + t.ID + " depends_on points to task " + d +
							" in later story " + stories[pos.si].ID +
							"; a task may only depend on an earlier or same-story task",
					})
				}
			}
		}
	}

	// --- Rule 2: cycles, over the combined story+task graph. ---
	edges := make(map[string][]string) // node -> in-plan successors
	nodeOrder := make([]string, 0, len(stories))
	for _, s := range stories {
		nodeOrder = append(nodeOrder, s.ID)
		for _, d := range s.DependsOn {
			if _, ok := storyIdx[d]; ok {
				edges[s.ID] = append(edges[s.ID], d)
			}
		}
	}
	for _, s := range stories {
		for _, t := range s.Tasks {
			nodeOrder = append(nodeOrder, t.ID)
			for _, d := range t.DependsOn {
				if _, ok := taskSlot[d]; ok {
					edges[t.ID] = append(edges[t.ID], d)
				}
			}
		}
	}
	// Classic three-color DFS; a back edge to a gray node closes a
	// cycle, recovered from the recursion stack.
	state := make(map[string]int, len(nodeOrder)) // 0 white, 1 gray, 2 black
	var stack []string
	cycleSeen := make(map[string]bool)
	var dfs func(node string)
	dfs = func(node string) {
		state[node] = 1
		stack = append(stack, node)
		for _, nx := range edges[node] {
			switch state[nx] {
			case 0:
				dfs(nx)
			case 1:
				i := 0
				for k, v := range stack {
					if v == nx {
						i = k
						break
					}
				}
				cycle := append(append([]string{}, stack[i:]...), nx)
				key := canonicalCycleKey(cycle)
				if !cycleSeen[key] {
					cycleSeen[key] = true
					add(StructuralFinding{
						Kind:   FindingDependencyCycle,
						Detail: "dependency cycle: " + strings.Join(cycle, " -> "),
					})
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[node] = 2
	}
	for _, node := range nodeOrder {
		if state[node] == 0 {
			dfs(node)
		}
	}

	// --- Rule 3: file provenance. ---
	baseDir := planStructureBaseDir(prd)
	existsInTree := func(p string) bool {
		if filepath.IsAbs(p) {
			_, err := os.Stat(p)
			return err == nil
		}
		_, err := os.Stat(filepath.Join(baseDir, p))
		return err == nil
	}
	// generatesAtOrBefore reports whether p is listed in the
	// FilesGenerated of a task at or before the (si, maxTI) point in
	// plan order; maxTI == -1 covers the whole story (story-level
	// references).
	generatesAtOrBefore := func(si, maxTI int, p string) bool {
		for i, s := range stories {
			if i > si {
				break
			}
			last := len(s.Tasks)
			if i == si && maxTI >= 0 && maxTI < len(s.Tasks) {
				last = maxTI + 1
			}
			for j := 0; j < last; j++ {
				for _, g := range s.Tasks[j].FilesGenerated {
					if g == p {
						return true
					}
				}
			}
		}
		return false
	}
	// checkFileTokens flags any file-like path named in title/spec that
	// is neither in the tree nor produced by a task at or before the
	// point. ti == -1 marks a story-level reference.
	checkFileTokens := func(where string, s *Story, si, ti int, texts ...string) {
		for _, p := range fileLikeTokens(strings.Join(texts, "\n")) {
			if existsInTree(p) || generatesAtOrBefore(si, ti, p) {
				continue
			}
			add(StructuralFinding{
				Kind:    FindingUnresolvedFile,
				StoryID: s.ID,
				TaskID:  taskIDAt(s, ti),
				Detail:  where + " names file " + strconv.Quote(p) +
					" which exists neither in the project tree (" + baseDir +
					") nor in an earlier task's FilesGenerated",
			})
		}
	}
	// Story FilesGenerated entries: they are declared outputs, so they
	// pass when produced by any task in the plan; if no task generates
	// the file (and it is not in the tree), flag it.
	for _, s := range stories {
		for _, g := range s.FilesGenerated {
			if existsInTree(g) || generatedByAnyTask(stories, g) {
				continue
			}
			add(StructuralFinding{
				Kind:    FindingUnresolvedFile,
				StoryID: s.ID,
				Detail:  "story " + s.ID + " FilesGenerated entry " + strconv.Quote(g) +
					" is produced by no task in the plan and not in tree " + baseDir,
			})
		}
	}
	for si, s := range stories {
		checkFileTokens("story "+s.ID, &s, si, -1, s.Title, s.Description)
		for ti, t := range s.Tasks {
			checkFileTokens("task "+t.ID, &s, si, ti, t.Title, t.Spec)
			// Task FilesGenerated entries are declared outputs by
			// construction (that list *is* the producer record);
			// references to them elsewhere satisfy rule 3 via
			// generatesAtOrBefore.
		}
	}

	// --- Rule 4: verification script OR explicit B124 reason. ---
	for _, s := range stories {
		for _, t := range s.Tasks {
			if t.VerificationScript == "" && t.VerificationScriptReason == "" {
				add(StructuralFinding{
					Kind:    FindingMissingVerification,
					StoryID: s.ID,
					TaskID:  t.ID,
					Detail: "task " + t.ID + " has neither verification_script nor " +
						"verification_script_reason; per B124 a task without a script must state an explicit exemption reason",
				})
			}
		}
	}

	// --- Rule 5: no tracker ids in task titles / generated names. ---
	for _, s := range stories {
		if m := trackerIDRefRe.FindString(s.Title); m != "" {
			add(StructuralFinding{
				Kind:    FindingTrackerIDInName,
				StoryID: s.ID,
				Detail:  "story " + s.ID + " title " + strconv.Quote(s.Title) + " contains tracker id " + m + "; name by what it does, not by its tracker id (AGENT.md naming rule)",
			})
		}
		for _, t := range s.Tasks {
			names := append([]string{t.Title}, t.FilesGenerated...)
			names = append(names, s.FilesGenerated...) // story-level generated names
			for _, name := range names {
				if m := trackerIDRefRe.FindString(name); m != "" {
					add(StructuralFinding{
						Kind:    FindingTrackerIDInName,
						StoryID: s.ID,
						TaskID:  t.ID,
						Detail:  "task " + t.ID + " name " + strconv.Quote(name) + " contains tracker id " + m + "; name by what it does, not by its tracker id (AGENT.md naming rule)",
					})
				}
			}
		}
	}

	return report
}

// taskSlotT is a (story, task) slot in plan order, indexed by taskID in
// the CheckPlanStructure scope.
type taskSlotT struct{ si, ti int }

// taskIDAt resolves the referencing task's ID for a (story, ti) slot;
// ti == -1 means a story-level finding (no task).
func taskIDAt(s *Story, ti int) string {
	if s == nil || ti < 0 || ti >= len(s.Tasks) {
		return ""
	}
	return s.Tasks[ti].ID
}

// generatedByAnyTask reports whether some task in the plan lists p in
// its FilesGenerated.
func generatedByAnyTask(stories []Story, p string) bool {
	for _, s := range stories {
		for _, t := range s.Tasks {
			for _, g := range t.FilesGenerated {
				if g == p {
					return true
				}
			}
		}
	}
	return false
}

// planStructureBaseDir resolves the project tree root the file
// existence checks run against: PRD.ProjectDir when set (relative names
// resolved against the current directory), else the current tree.
func planStructureBaseDir(prd *PRD) string {
	if prd != nil && prd.ProjectDir != "" {
		if filepath.IsAbs(prd.ProjectDir) {
			return prd.ProjectDir
		}
		if abs, err := filepath.Abs(prd.ProjectDir); err == nil {
			return abs
		}
		return prd.ProjectDir
	}
	return "."
}

// fileLikeTokens returns the deduplicated, appearance-ordered file-like
// tokens in text (see planFileRefRe): slash-separated paths and bare
// names with a plausible alphanumeric file extension. Pure-numeric
// tokens ("10.20.30") and traversal-looking paths are excluded.
func fileLikeTokens(text string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, tok := range planFileRefRe.FindAllString(text, -1) {
		if len(tok) < 4 || isPureNumeric(tok) || strings.Contains(tok, "..") {
			continue
		}
		if seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// canonicalCycleKey is the dedup key for one discovered cycle: the
// least-lexicographic rotation of its node path (the repeated closing
// node stripped), so "a -> b -> a" and "b -> a -> b" share a key.
func canonicalCycleKey(cycle []string) string {
	nodes := cycle[:len(cycle)-1]
	if len(nodes) == 0 {
		nodes = []string{cycle[len(cycle)-1]}
	}
	best := 0
	for i := range nodes {
		if lessRotation(nodes, i, best) {
			best = i
		}
	}
	rot := make([]string, 0, len(nodes))
	rot = append(rot, nodes[best:]...)
	rot = append(rot, nodes[:best]...)
	return strings.Join(rot, "\x00")
}

// lessRotation reports whether the rotation of nodes starting at i is
// lexicographically smaller than the rotation starting at j.
func lessRotation(nodes []string, i, j int) bool {
	n := len(nodes)
	if i == j {
		return false
	}
	for k := 1; k <= n; k++ {
		a := nodes[(i+k-1)%n]
		b := nodes[(j+k-1)%n]
		if a != b {
			return a < b
		}
	}
	return false
}

