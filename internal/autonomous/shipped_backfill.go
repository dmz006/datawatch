// Shipped-state backfill. Walks the base repository's main branch for
// merge commits whose subjects name an automaton branch
// (automaton/<8-hex prd id>), finds the first release at or after each
// merge, and records the four shipped-state fields on the matching PRDs
// (and on Automations whose non-skipped members are all shipped) with
// source backfill — without ever touching a PRD's or Automation's
// Status. A cancelled PRD named by a merge commit is recorded just like
// any other (it stays cancelled, now also shipped); a PRD no merge names
// is never marked, so a cancelled one with no merge stays unmarked.
package autonomous

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/git"
)

// shippedBackfillMergeRe pulls the 8-hex PRD id out of a merge commit
// subject that names an automaton branch — the two write forms are
// "Merge automaton/<id>" and "Merge branch automaton/<id>"; the trailing
// class guard keeps longer hex runs from matching their first 8 chars.
var shippedBackfillMergeRe = regexp.MustCompile(`automaton/([0-9a-f]{8})([^0-9a-f]|$)`)

// shippedBackfillVersionRe extracts a semver from a version-bump commit
// subject or a release tag name.
var shippedBackfillVersionRe = regexp.MustCompile(`v?([0-9]+\.[0-9]+\.[0-9]+)`)

// shippedBackfillHeadingRe matches the first version heading a merge
// helper writes into the CHANGELOG, e.g. "## v9.54.0 — release notes".
var shippedBackfillHeadingRe = regexp.MustCompile(`^[\t ]*##[\t ]+v?([0-9]+\.[0-9]+\.[0-9]+)`)

// ShippedBackfillOpts configures one BackfillShipped pass.
type ShippedBackfillOpts struct {
	// DryRun scans the history and computes the report but writes
	// nothing: no PRD or Automation receives a shipped record.
	DryRun bool
	// Repo is the base repository to read history from. Empty uses
	// Config.WorktreeBaseRepo.
	Repo string
}

// ShippedBackfillReport is the accounting of one BackfillShipped pass.
type ShippedBackfillReport struct {
	// PrdsScanned is the number of PRDs in the store the pass looked at.
	PrdsScanned int `json:"prds_scanned"`
	// PrdsMarked are PRDs that did not carry shipped state and are marked
	// by this pass — a dry run increments it for each PRD it would mark,
	// so a dry run and the real run report the same number.
	PrdsMarked int `json:"prds_marked"`
	// PrdsWouldMarkCancelled counts the PRDs marked (or that would be
	// marked) in this pass whose stored status is cancelled; the real run
	// and a dry run report the same number.
	PrdsWouldMarkCancelled int `json:"prds_would_mark_cancelled"`
	// PrdsAlreadyMarked already carried shipped state and were left alone.
	PrdsAlreadyMarked int `json:"prds_already_marked"`
	// AutomationsMarked are Automations marked shipped by this pass.
	AutomationsMarked int `json:"automations_marked"`
	// LeftCancelled is the count of cancelled PRDs still unmarked after the
	// pass: the cancelled PRDs with no merge (neither already shipped
	// before, nor newly marked in this pass).
	LeftCancelled int `json:"left_cancelled"`
	// UnmatchedIDs are 8-hex ids the history names but the store does not
	// hold (deduplicated, sorted).
	UnmatchedIDs []string `json:"unmatched_ids,omitempty"`
	// Errors are the per-record failures that were skipped without
	// stopping the pass.
	Errors []string `json:"errors,omitempty"`
}

// shippedBackfillMerge is one main-branch merge commit whose subject
// names an automaton branch PRD.
type shippedBackfillMerge struct {
	commit   string
	mergedAt time.Time
	prdID    string
}

func shippedBackfillMainBranch() string {
	if b := strings.TrimSpace(os.Getenv("MAIN_BRANCH")); b != "" {
		return b
	}
	return "main"
}

// BackfillShipped scans the base repository's main branch (MAIN_BRANCH
// when set, default "main") for merge commits naming automaton branches
// and records shipped state with source backfill on every PRD that has a
// matching merge — and on each Automation that has at least one
// non-skipped member and whose non-skipped member PRDs are all shipped —
// never changing any Status. One bad record lands in the report's Errors
// and the pass continues.
func (m *Manager) BackfillShipped(ctx context.Context, opts ShippedBackfillOpts) (ShippedBackfillReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = strings.TrimSpace(m.Config().WorktreeBaseRepo)
	}
	if repo == "" {
		return ShippedBackfillReport{}, fmt.Errorf("shipped backfill: no base repository configured (set opts.Repo or Config.WorktreeBaseRepo)")
	}
	main := shippedBackfillMainBranch()

	var rep ShippedBackfillReport
	rep.PrdsScanned = len(m.store.ListPRDs())

	merges, err := listShippedBackfillMerges(ctx, repo, main)
	if err != nil {
		return rep, fmt.Errorf("shipped backfill: %w", err)
	}

	prdBy := map[string]*PRD{}
	for _, p := range m.store.ListPRDs() {
		if p != nil {
			prdBy[p.ID] = p
		}
	}
	// newlyShipped tracks PRDs shipped during this pass (recorded, or
	// recorded-in-spirit on a dry run) so the Automation pass sees them.
	newlyShipped := map[string]bool{}
	unmatchedSeen := map[string]bool{}
	// mergeOrder gives each PRD the position of its most recent merge in the
	// newest-first list, so an Automation's "latest member merge" is chosen
	// by position — commit timestamps can tie.
	mergeOrder := map[string]int{}
	for i, mg := range merges {
		if _, exists := mergeOrder[mg.prdID]; !exists {
			mergeOrder[mg.prdID] = i
		}
	}

	for _, mg := range merges {
		p, ok := prdBy[mg.prdID]
		if !ok {
			if !unmatchedSeen[mg.prdID] {
				unmatchedSeen[mg.prdID] = true
				rep.UnmatchedIDs = append(rep.UnmatchedIDs, mg.prdID)
			}
			continue
		}
		if p.IsShipped() {
			rep.PrdsAlreadyMarked++
			continue
		}
		release := shippedBackfillReleaseForMerge(ctx, repo, main, mg.commit)
		if !opts.DryRun {
			if _, err := m.RecordPRDShipped(mg.prdID, release, mg.mergedAt, mg.commit, ShippedSourceBackfill); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("record prd %s: %v", mg.prdID, err))
				continue
			}
		}
		// A cancelled PRD named by a merge is recorded too — it stays
		// cancelled, and is counted separately so the report names it.
		rep.PrdsMarked++
		if p.Status == PRDCancelled {
			rep.PrdsWouldMarkCancelled++
		}
		newlyShipped[mg.prdID] = true
	}

	// LeftCancelled: the cancelled PRDs still unmarked after the pass —
	// the cancelled PRDs with no merge (neither already shipped, nor newly
	// marked in this pass).
	for _, p := range m.store.ListPRDs() {
		if p == nil || p.Status != PRDCancelled {
			continue
		}
		if p.IsShipped() || newlyShipped[p.ID] {
			continue
		}
		rep.LeftCancelled++
	}

	m.backfillAutomationsShipped(opts, &rep, newlyShipped, mergeOrder)
	sort.Strings(rep.UnmatchedIDs)
	return rep, nil
}

// backfillAutomationsShipped marks each non-shipped Automation whose
// non-skipped member PRDs are all shipped; the latest member merge
// supplies merged_at, merge_commit and the release name. Skipped members
// (operator skip_member) are excluded from both the count and the
// "all shipped" check. One bad automation lands in Errors, not a halt.
func (m *Manager) backfillAutomationsShipped(opts ShippedBackfillOpts, rep *ShippedBackfillReport, newlyShipped map[string]bool, mergeOrder map[string]int) {
	if m.automations == nil {
		return
	}
	for _, a := range m.automations.List() {
		if a == nil || a.IsShipped() {
			continue
		}
		nonSkipped := 0
		allShipped := true
		bestIdx := -1
		var latestMergedAt time.Time
		var latestCommit, latestRelease string
		for _, mem := range a.Members {
			if mem.Status == MemberSkipped {
				continue
			}
			nonSkipped++
			shipped := false
			var p *PRD
			if found, ok := m.store.GetPRD(mem.PRDID); ok {
				p = found
				shipped = p.IsShipped() || newlyShipped[mem.PRDID]
			}
			if !shipped {
				allShipped = false
				continue
			}
			if !p.IsShipped() || p.MergedAt == nil {
				continue // dry-run "shipped": no recorded fields to take the latest from
			}
			if idx, inOrder := mergeOrder[mem.PRDID]; inOrder && (bestIdx == -1 || idx < bestIdx) {
				bestIdx = idx
				latestMergedAt = *p.MergedAt
				latestCommit = p.MergeCommit
				latestRelease = p.MergedInRelease
			}
		}
		if nonSkipped == 0 || !allShipped {
			continue
		}
		if opts.DryRun {
			continue
		}
		if _, err := m.RecordAutomationShipped(a.ID, latestRelease, latestMergedAt, latestCommit, ShippedSourceBackfill); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("record automation %s: %v", a.ID, err))
			continue
		}
		rep.AutomationsMarked++
	}
}

// listShippedBackfillMerges lists the merge commits on main newest-first
// and keeps the most recent one per automaton PRD id whose subject names
// the branch.
func listShippedBackfillMerges(ctx context.Context, repo, main string) ([]shippedBackfillMerge, error) {
	out, err := git.Run(ctx, repo, "log", main, "--merges", "--pretty=format:%H%x00%cI%x00%s")
	if err != nil {
		return nil, fmt.Errorf("list merge commits on %s: %w", main, err)
	}
	var merges []shippedBackfillMerge
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		matches := shippedBackfillMergeRe.FindStringSubmatch(parts[2])
		if matches == nil {
			continue
		}
		id := matches[1]
		if seen[id] {
			continue // newest first: the first hit is the most recent merge
		}
		seen[id] = true
		when, werr := time.Parse(time.RFC3339, parts[1])
		if werr != nil {
			when = time.Time{}
		}
		merges = append(merges, shippedBackfillMerge{commit: parts[0], mergedAt: when, prdID: id})
	}
	return merges, nil
}

// shippedBackfillReleaseForMerge names the first release on main that
// contains the merge commit: the first version-bump commit after the
// merge (its subject carries the version, else the CHANGELOG heading the
// merge helper wrote into it), then the lowest release tag containing
// the merge, then "". A merge with no release after it is unreleased and
// records an empty merged_in_release.
func shippedBackfillReleaseForMerge(ctx context.Context, repo, main, merge string) string {
	// Every commit after the merge on main, oldest first, with the files
	// it touched (name-only) to spot a release commit whose subject does
	// not carry the version.
	out, err := git.Run(ctx, repo, "log", merge+".."+main, "--reverse", "--name-only", "--pretty=format:@@%H|%s")
	if err == nil {
		for _, block := range strings.Split(out, "@@") {
			block = strings.TrimLeft(block, "\n")
			if block == "" {
				continue
			}
			header := block
			if i := strings.IndexByte(header, '\n'); i >= 0 {
				header = header[:i]
			}
			parts := strings.SplitN(header, "|", 2)
			if len(parts) != 2 {
				continue
			}
			subject := parts[1]
			if v := shippedBackfillVersionRe.FindStringSubmatch(subject); v != nil {
				return "v" + v[1]
			}
		}
		// No version in a subject after the merge: take the first
		// release commit that touched the CHANGELOG and read the version
		// heading it wrote.
		for _, block := range strings.Split(out, "@@") {
			block = strings.TrimLeft(block, "\n")
			if block == "" || !strings.Contains(block, "CHANGELOG.md") {
				continue
			}
			header := block
			if i := strings.IndexByte(header, '\n'); i >= 0 {
				header = header[:i]
			}
			parts := strings.SplitN(header, "|", 2)
			if len(parts) != 2 {
				continue
			}
			if heading, herr := git.Run(ctx, repo, "show", parts[0]+":CHANGELOG.md"); herr == nil {
				for _, line := range strings.Split(heading, "\n") {
					if v := shippedBackfillHeadingRe.FindStringSubmatch(line); v != nil {
						return "v" + v[1]
					}
				}
			}
			break
		}
	}
	// No release commit after the merge on main: the lowest release tag
	// that contains the merge commit.
	if tags, terr := git.Run(ctx, repo, "tag", "--contains", merge); terr == nil {
		best, bestVer, haveBest := "", semverTuple{}, false
		for _, tag := range strings.Fields(tags) {
			if v := shippedBackfillVersionRe.FindStringSubmatch(tag); v != nil {
				if t, ok := semverTupleOf(v[1]); ok {
					if !haveBest || t.less(bestVer) {
						best, bestVer, haveBest = tag, t, true
					}
				}
			}
		}
		if best != "" {
			return best
		}
	}
	return ""
}

// semverTuple is (major, minor, patch) of a "x.y.z" string.
type semverTuple struct{ major, minor, patch int }

func (a semverTuple) less(b semverTuple) bool {
	return a.major < b.major || (a.major == b.major && (a.minor < b.minor || (a.minor == b.minor && a.patch < b.patch)))
}

// semverTupleOf parses "x.y.z" (or a prefix of it) into a comparable triple.
func semverTupleOf(v string) (semverTuple, bool) {
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		return semverTuple{}, false
	}
	return semverTuple{major: atoiNonNeg(parts[0]), minor: atoiNonNeg(parts[1]), patch: atoiNonNeg(parts[2])}, true
}

// atoiNonNeg reads the leading decimal digits of s ("" or none => 0) and
// stops at the first non-digit (prerelease suffixes like "-rc1").
func atoiNonNeg(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
