package autonomous

// overlap_finder.go composes the pieces of the "search before creating
// a PRD" feature into one finder: an Automata lister that returns
// Automata of every status, the registry-row and unmerged-branch
// sources, the match threshold, and a project-root resolver.
//
// Find never reports an error to its caller. Each source runs inside
// its own panic-recovering call: an error or a panic from a source
// becomes one warning of kind "unavailable" that names that source,
// and a source with no project root to scan is skipped and named
// instead of warned about. Operator copy always says Automaton.

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Warning kinds for overlap warnings.
const (
	warningKindAutomaton   = "automaton"
	warningKindRegistry    = "registry"
	warningKindBranch      = "branch"
	warningKindUnavailable = "unavailable"
)

// Source names exposed in the skipped-sources list and in
// unavailable warning copy.
const (
	sourceNameAutomaton = "automaton"
	sourceNameRegistry  = "registry"
	sourceNameBranch    = "branch"
)

// OverlapWarning is one finding for an operator: a thing that already
// covers the plan they are about to create, or a source that could not
// be checked this time.
type OverlapWarning struct {
	// Kind is one of automaton, registry, branch, unavailable.
	Kind string
	// ID is the Automaton id (automaton kind only).
	ID string
	// Status is the Automaton's status (automaton kind only).
	Status string
	// Row is the registry row number (registry kind only).
	Row int
	// Branch is the branch name (branch kind only).
	Branch string
	// Score is the overlap score, rounded to two decimals.
	Score float64
	// SharedTerms are the shared terms, at most five.
	SharedTerms []string
	// Reason is one sentence of operator copy that says Automaton,
	// never PRD.
	Reason string
	// ReadHint is how the operator can read the underlying thing.
	ReadHint string
}

// OverlapAutomaton is the state the finder needs from one stored
// Automaton, whatever status it is in.
type OverlapAutomaton struct {
	ID     string
	Title  string
	Spec   string
	Status string
}

// AutomatonLister returns every stored Automaton of every status.
type AutomatonLister interface {
	ListAutomata() []OverlapAutomaton
}

// storeAutomatonLister wraps the existing store list so callers that
// hold a *Store can hand it to the finder.
type storeAutomatonLister struct {
	store *Store
}

// NewStoreAutomatonLister returns an AutomatonLister backed by the
// store's existing Automata list (all statuses).
func NewStoreAutomatonLister(store *Store) AutomatonLister {
	return storeAutomatonLister{store: store}
}

// ListAutomata returns every Automaton in the store.
func (l storeAutomatonLister) ListAutomata() []OverlapAutomaton {
	prds := l.store.ListPRDs()
	out := make([]OverlapAutomaton, 0, len(prds))
	for _, p := range prds {
		if p == nil {
			continue
		}
		out = append(out, OverlapAutomaton{
			ID:     p.ID,
			Title:  p.Title,
			Spec:   p.Spec,
			Status: string(p.Status),
		})
	}
	return out
}

// overlapRegistrySource reads registry-row candidates behind an
// interface so tests can fake it.
type overlapRegistrySource interface {
	registryCandidates(projectRoot string) ([]overlapCandidate, error)
}

// overlapBranchSource reads unmerged-branch candidates behind an
// interface so tests can fake it.
type overlapBranchSource interface {
	branchCandidates() ([]overlapCandidate, error)
}

// defaultRegistrySource is the registry source from the sources task.
type defaultRegistrySource struct{}

func (defaultRegistrySource) registryCandidates(projectRoot string) ([]overlapCandidate, error) {
	return overlapRowCandidates(projectRoot)
}

// defaultBranchSource is the branch source from the sources task.
type defaultBranchSource struct {
	runner gitRunner
}

func (s defaultBranchSource) branchCandidates() ([]overlapCandidate, error) {
	return overlapBranchCandidates(s.runner)
}

// projectRootResolver returns the project root the registry and
// branch sources should scan, and whether any exists at all.
type projectRootResolver func(projectDir string) (string, bool)

// worktreeBaseRootResolver prefers the new Automaton's own project
// directory; when that is empty it falls back to the configured
// worktree base repository; when neither exists it reports none.
func worktreeBaseRootResolver(worktreeBaseRepo string) projectRootResolver {
	return func(projectDir string) (string, bool) {
		if strings.TrimSpace(projectDir) != "" {
			return projectDir, true
		}
		if strings.TrimSpace(worktreeBaseRepo) != "" {
			return worktreeBaseRepo, true
		}
		return "", false
	}
}

// OverlapFinder composes the automata lister, the registry and branch
// sources, the threshold, and the project-root resolver.
type OverlapFinder struct {
	lister      AutomatonLister
	registry    overlapRegistrySource
	branches    overlapBranchSource
	threshold   float64
	resolveRoot projectRootResolver
}

// NewOverlapFinder builds an OverlapFinder. Nil sources get the
// default implementations from the sources task; a nil resolver
// becomes worktreeBaseRootResolver("").
func NewOverlapFinder(lister AutomatonLister, registry overlapRegistrySource, branches overlapBranchSource, threshold float64, resolveRoot projectRootResolver) *OverlapFinder {
	if registry == nil {
		registry = defaultRegistrySource{}
	}
	if branches == nil {
		branches = defaultBranchSource{}
	}
	if resolveRoot == nil {
		resolveRoot = worktreeBaseRootResolver("")
	}
	return &OverlapFinder{
		lister:      lister,
		registry:    registry,
		branches:    branches,
		threshold:   threshold,
		resolveRoot: resolveRoot,
	}
}

// Find reports the Automata, registry rows, and unmerged branches
// that already cover the given title and spec, excluding the Automaton
// named by excludeID. It returns the warnings sorted by score
// descending, then kind, then id/row/branch, and the names of the
// sources skipped because no project root existed. It returns no
// error: each source runs in its own panic-recovering call, and an
// error or panic becomes one unavailable warning that names the
// source.
func (f *OverlapFinder) Find(title, spec, excludeID, projectDir string) (warnings []OverlapWarning, skipped []string) {
	newText := strings.TrimSpace(title + "\n" + spec)

	warnings = make([]OverlapWarning, 0, 4)

	// Automaton source.
	if f.lister != nil {
		recs, err, panicked := safeListAutomata(f.lister.ListAutomata)
		if err != nil {
			warnings = append(warnings, unavailableWarning(sourceNameAutomaton, panicked))
		} else {
			for _, rec := range recs {
				if rec.ID == excludeID {
					continue
				}
				res := MatchTermOverlap(f.threshold, newText, strings.TrimSpace(rec.Title+"\n"+rec.Spec))
				if !res.Matched {
					continue
				}
				warnings = append(warnings, OverlapWarning{
					Kind:        warningKindAutomaton,
					ID:          rec.ID,
					Status:      rec.Status,
					Score:       roundOverlapScore(res.Score),
					SharedTerms: res.Shared,
					Reason:      fmt.Sprintf("Automaton %s already describes this plan.", rec.ID),
					ReadHint:    "datawatch autonomous get " + rec.ID,
				})
			}
		}
	}

	// Project root for the file-based sources.
	res := f.resolveRoot
	if res == nil {
		res = worktreeBaseRootResolver("")
	}
	root, hasRoot := res(projectDir)
	if !hasRoot {
		skipped = append(skipped, sourceNameRegistry, sourceNameBranch)
		return sortOverlapWarnings(warnings), skipped
	}

	// Registry-row source.
	cands, err, panicked := safeRunCandidates(func() ([]overlapCandidate, error) {
		return f.registry.registryCandidates(root)
	})
	if err != nil {
		warnings = append(warnings, unavailableWarning(sourceNameRegistry, panicked))
	} else {
		warnings = append(warnings, f.candidateWarnings(newText, cands, func(c overlapCandidate) OverlapWarning {
			return OverlapWarning{
				Kind:     warningKindRegistry,
				Row:      c.Row,
				Reason:   fmt.Sprintf("An existing Automaton plan is already on the roadmap (row %d).", c.Row),
				ReadHint: fmt.Sprintf("docs/plans/README.md row %d", c.Row),
			}
		})...)
	}

	// Unmerged-branch source.
	cands, err, panicked = safeRunCandidates(func() ([]overlapCandidate, error) {
		return f.branches.branchCandidates()
	})
	if err != nil {
		warnings = append(warnings, unavailableWarning(sourceNameBranch, panicked))
	} else {
		warnings = append(warnings, f.candidateWarnings(newText, cands, func(c overlapCandidate) OverlapWarning {
			return OverlapWarning{
				Kind:     warningKindBranch,
				Branch:   c.Ref,
				Reason:   fmt.Sprintf("An unmerged Automaton branch already carries this work (%s).", c.Ref),
				ReadHint: "git log main.." + c.Ref,
			}
		})...)
	}

	return sortOverlapWarnings(warnings), skipped
}

// candidateWarnings turns matched candidates into warnings, scoring
// each against the new title plus spec.
func (f *OverlapFinder) candidateWarnings(newText string, cands []overlapCandidate, shape func(overlapCandidate) OverlapWarning) []OverlapWarning {
	var out []OverlapWarning
	for _, c := range cands {
		res := MatchTermOverlap(f.threshold, newText, c.Text)
		if !res.Matched {
			continue
		}
		w := shape(c)
		w.Score = roundOverlapScore(res.Score)
		w.SharedTerms = res.Shared
		out = append(out, w)
	}
	return out
}

// unavailableWarning is the one warning a failed source produces: its
// kind always names the problem, never the source; the reason names
// the source in one sentence of operator copy.
func unavailableWarning(sourceName string, panicked bool) OverlapWarning {
	reason := "The " + sourceName + " source could not be read, so it was not checked this time."
	if panicked {
		reason = "The " + sourceName + " source stopped working, so it was skipped this time."
	}
	return OverlapWarning{Kind: warningKindUnavailable, Reason: reason}
}

// safeListAutomata calls a list and recovers from a panic instead of
// letting one escape the finder.
func safeListAutomata(fn func() []OverlapAutomaton) (recs []OverlapAutomaton, err error, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			recs = nil
			err = fmt.Errorf("%v", r)
			panicked = true
		}
	}()
	recs = fn()
	return recs, nil, false
}

// safeRunCandidates calls a candidate source and recovers from a
// panic instead of letting one escape the finder.
func safeRunCandidates(fn func() ([]overlapCandidate, error)) (cands []overlapCandidate, err error, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			cands = nil
			err = fmt.Errorf("%v", r)
			panicked = true
		}
	}()
	cands, err = fn()
	return cands, err, false
}

// roundOverlapScore rounds a score to two decimals.
func roundOverlapScore(score float64) float64 {
	return math.Round(score*100) / 100
}

// sortOverlapWarnings orders warnings by score descending, then kind
// ascending, then id / row / branch.
func sortOverlapWarnings(warnings []OverlapWarning) []OverlapWarning {
	sort.SliceStable(warnings, func(i, j int) bool {
		a, b := warnings[i], warnings[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		switch a.Kind {
		case warningKindRegistry:
			return a.Row < b.Row
		case warningKindBranch:
			return a.Branch < b.Branch
		default: // automaton and unavailable
			return a.ID < b.ID
		}
	})
	return warnings
}
