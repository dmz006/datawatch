// Automaton merge-pending pass. A completed Automaton whose work branch still
// carries commits that are not on main is "merge pending". The pass runs from
// Coordinator.triagePass (automation_coordinator.go) and, depending on
// autonomous.automation.merge_pending_mode:
//
//   - off: nothing here runs and git is never called;
//   - shadow: the outcome is written to the bounded shadow decision list
//     (merge_shadow_record.go) and nothing else - no record, no alert;
//   - on: a pending branch gets a merge_pending TriageRecord, and one that has
//     stood for merge_pending_alert_minutes raises exactly one alert per
//     episode.
//
// Branch comparisons use CompareBranchToMain against the worktree base repo
// and are cached in memory by branch tip and base tip, so git compares again
// only when a tip moves. Nothing here deletes, merges or pushes.
package autonomous

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// mergePendingBase is the branch Automaton work branches are compared with.
const mergePendingBase = "main"

// mergePendingGitTimeout bounds the git work of one comparison.
const mergePendingGitTimeout = 30 * time.Second

// mergeCacheEntry is one cached comparison and the tips it was made at.
type mergeCacheEntry struct {
	branchTip, baseTip string
	cmp                MergeCompare
}

// mergePendingState is the Coordinator's merge-pending bookkeeping. It lives
// in memory only; nothing in it is written to the store. The zero value is
// ready to use.
type mergePendingState struct {
	mu sync.Mutex
	// git overrides the production git runner (tests).
	git MergeGitRunner
	// refTips overrides how the branch and main tips are read (tests). It
	// reports ok false when it cannot answer, and git is asked instead.
	refTips func(repo, branch, base string) (branchTip, baseTip string, ok bool)
	// cache holds the newest comparison per branch.
	cache map[string]mergeCacheEntry
	// since is when each Automaton was first seen merge pending in shadow
	// mode, so a shadow decision can say whether it would have alerted.
	since map[string]time.Time
	// logged is the last unknown-state text logged per branch, so an
	// unchanged failure is logged once, not every poll.
	logged map[string]string
	shadow *mergeShadowRecords
}

func (s *mergePendingState) runner() MergeGitRunner {
	if s.git != nil {
		return s.git
	}
	return NewMergeGitRunner()
}

// MergeShadowDecisions returns up to limit recorded shadow decisions, newest
// first (all of them when limit is not positive).
func (c *Coordinator) MergeShadowDecisions(limit int) []MergeShadowDecision {
	if c == nil || c.m == nil {
		return nil
	}
	return c.shadowRecords().list(limit)
}

// shadowRecords opens the shadow decision history on first use.
func (c *Coordinator) shadowRecords() *mergeShadowRecords {
	s := &c.mp
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shadow == nil {
		s.shadow = newMergeShadowRecords(c.m.store.dir)
	}
	return s.shadow
}

// mergeTips returns the tips of the Automaton branch and of main. They are
// read from the repository's ref files, so a poll in which nothing moved runs
// no git at all; a layout the ref files cannot answer for (symbolic or
// reftable refs, an unreadable repository) falls back to git rev-parse under
// ctx.
func (s *mergePendingState) mergeTips(ctx context.Context, repo, branch string) (branchTip, baseTip string, err error) {
	read := s.refTips
	if read == nil {
		read = readBranchTips
	}
	if bt, mt, ok := read(repo, branch, mergePendingBase); ok {
		return bt, mt, nil
	}
	out, err := s.runner().Run(ctx, repo, "rev-parse", branch, mergePendingBase)
	if err != nil {
		return "", "", fmt.Errorf("merge tips for the Automaton branch: %v: %s", err, strings.TrimSpace(out))
	}
	tips := strings.Fields(out)
	if len(tips) != 2 {
		return "", "", fmt.Errorf("merge tips for the Automaton branch: unexpected rev-parse output %q", strings.TrimSpace(out))
	}
	return tips[0], tips[1], nil
}

// readBranchTips reads the tips of two local branches from the loose ref
// files, then packed-refs, of repo. ok is false when either cannot be
// resolved that way.
func readBranchTips(repo, branch, base string) (branchTip, baseTip string, ok bool) {
	gitDir := filepath.Join(repo, ".git")
	if fi, err := os.Stat(gitDir); err != nil || !fi.IsDir() {
		gitDir = repo // a bare repository
	}
	branchTip, ok = readRefTip(gitDir, "refs/heads/"+branch)
	if !ok {
		return "", "", false
	}
	baseTip, ok = readRefTip(gitDir, "refs/heads/"+base)
	if !ok {
		return "", "", false
	}
	return branchTip, baseTip, true
}

// readRefTip resolves one full ref name under gitDir to an object id.
func readRefTip(gitDir, ref string) (string, bool) {
	if strings.Contains(ref, "..") || strings.ContainsAny(ref, "\\ \t\n") {
		return "", false
	}
	if data, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(ref))); err == nil {
		if id := strings.TrimSpace(string(data)); isObjectID(id) {
			return id, true
		}
		return "", false // a symbolic ref or something unexpected
	}
	f, err := os.Open(filepath.Join(gitDir, "packed-refs"))
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		id, name, found := strings.Cut(sc.Text(), " ")
		if found && name == ref && isObjectID(id) {
			return id, true
		}
	}
	return "", false
}

// isObjectID reports whether s is a full hexadecimal object id.
func isObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// mergeCompare returns the comparison of branch with main, running git only
// when the branch tip or the main tip moved since the cached result. ok is
// false (and the reason logged once) when the state is unknown. No lock is
// held while git runs, so a slow git cannot hold up the shadow list.
func (c *Coordinator) mergeCompare(repo, branch string) (MergeCompare, bool) {
	s := &c.mp

	unknown := func(text string) (MergeCompare, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.logged == nil {
			s.logged = map[string]string{}
		}
		if s.logged[branch] != text {
			s.logged[branch] = text
			log.Printf("[autonomous] merge pending: Automaton branch %s is unknown, leaving it unchanged: %s", branch, text)
		}
		return MergeCompare{State: MergeStateUnknown, Error: text}, false
	}
	if repo == "" {
		return unknown("no worktree base repo is configured for Automata")
	}

	ctx, cancel := context.WithTimeout(context.Background(), mergePendingGitTimeout)
	defer cancel()

	branchTip, baseTip, err := s.mergeTips(ctx, repo, branch)
	if err != nil {
		return unknown(err.Error())
	}

	s.mu.Lock()
	e, hit := s.cache[branch]
	hit = hit && e.branchTip == branchTip && e.baseTip == baseTip
	if hit {
		delete(s.logged, branch)
	}
	s.mu.Unlock()
	if hit {
		return e.cmp, true
	}

	cmp := CompareBranchToMain(ctx, s.runner(), repo, branch, mergePendingBase)
	if cmp.State == MergeStateUnknown {
		return unknown(cmp.Error)
	}
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[string]mergeCacheEntry{}
	}
	s.cache[branch] = mergeCacheEntry{branchTip: branchTip, baseTip: baseTip, cmp: cmp}
	delete(s.logged, branch)
	s.mu.Unlock()
	return cmp, true
}

// mergePendingPrune drops cached and tracked state for branches and
// Automata the pass did not see this poll, which keeps all three maps
// bounded by the number of finished Automata.
func (c *Coordinator) mergePendingPrune(branches, prdIDs map[string]struct{}) {
	s := &c.mp
	s.mu.Lock()
	defer s.mu.Unlock()
	for b := range s.cache {
		if _, ok := branches[b]; !ok {
			delete(s.cache, b)
		}
	}
	for b := range s.logged {
		if _, ok := branches[b]; !ok {
			delete(s.logged, b)
		}
	}
	for id := range s.since {
		if _, ok := prdIDs[id]; !ok {
			delete(s.since, id)
		}
	}
}

// mergePendingClear drops the Automaton's shadow episode and its stored
// merge_pending record. Any other stored record is left as it is.
func (c *Coordinator) mergePendingClear(prd *PRD) {
	c.mp.mu.Lock()
	delete(c.mp.since, prd.ID)
	c.mp.mu.Unlock()
	if prd.Triage != nil && prd.Triage.Status == TriageStatusMergePending {
		if err := c.m.ClearTriageRecord(prd.ID); err != nil {
			log.Printf("[autonomous] clear merge pending record for Automaton %s: %v", prd.ID, err)
		}
	}
}

// mergePendingCheck handles one completed Automaton with a work branch while
// merge_pending_mode is shadow or on. It returns the alert to raise, if any;
// the caller sends it after the pass. An unknown state changes nothing.
func (c *Coordinator) mergePendingCheck(prd *PRD, mode, repo string, now time.Time, threshold time.Duration) (title, body string, alert bool) {
	cmp, ok := c.mergeCompare(repo, prd.Git.Branch)
	if !ok {
		return "", "", false
	}
	name := prd.Title
	if name == "" {
		name = prd.ID
	}
	pending := cmp.State == MergeStatePending

	if mode == config.MergePendingModeShadow {
		wouldAlert := false
		if pending {
			c.mp.mu.Lock()
			if c.mp.since == nil {
				c.mp.since = map[string]time.Time{}
			}
			since, seen := c.mp.since[prd.ID]
			if !seen {
				since = now
				c.mp.since[prd.ID] = since
			}
			c.mp.mu.Unlock()
			wouldAlert = threshold > 0 && !now.Before(since.Add(threshold))
		} else {
			c.mp.mu.Lock()
			delete(c.mp.since, prd.ID)
			c.mp.mu.Unlock()
		}
		c.shadowRecords().observe(MergeShadowDecision{
			AutomatonID:  prd.ID,
			Name:         name,
			Branch:       prd.Git.Branch,
			State:        cmp.State,
			CommitsAhead: cmp.Ahead,
			WouldAlert:   wouldAlert,
		}, now)
		// shadow never writes a merge_pending record; drop any stale one
		if prd.Triage != nil {
			if err := c.m.ClearTriageRecord(prd.ID); err != nil {
				log.Printf("[autonomous] clear triage record for %s: %v", prd.ID, err)
			}
		}
		return "", "", false
	}

	if !pending {
		// merged, empty or needs_look: no record, no alert, and a record from
		// an earlier episode is cleared so a later episode alerts again
		c.mergePendingClear(prd)
		return "", "", false
	}

	// triagePass has already cleared any failed or blocked record, so the
	// stored one is a merge_pending record or none
	stored := prd.Triage
	fresh := c.mergePendingRecord(prd, cmp, now)
	alerting := threshold > 0 && stored != nil && stored.AlertedAt == nil &&
		!now.Before(stored.FirstSeenAt.Add(threshold))
	if !alerting && triageRecordUnchanged(stored, fresh) {
		return "", "", false
	}
	if alerting {
		t := now
		fresh.AlertedAt = &t
	}
	if err := c.m.SaveTriageRecord(prd.ID, fresh); err != nil {
		log.Printf("[autonomous] merge pending record for Automaton %s: %v", prd.ID, err)
		return "", "", false
	}
	if !alerting {
		return "", "", false
	}
	// SaveTriageRecord has carried the stored FirstSeenAt into fresh
	return fmt.Sprintf("Automaton %s merge pending", name), mergePendingAlertBody(fresh, now), true
}

// mergePendingRecord builds the merge_pending record for a finished
// Automaton, with the owning Automation's name and member when it has one.
func (c *Coordinator) mergePendingRecord(prd *PRD, cmp MergeCompare, now time.Time) *TriageRecord {
	rec := &TriageRecord{
		AutomationID: prd.AutomationID,
		PRDID:        prd.ID,
		PRDTitle:     prd.Title,
		Status:       TriageStatusMergePending,
		Branch:       prd.Git.Branch,
		CommitsAhead: cmp.Ahead,
		FirstSeenAt:  now,
		UpdatedAt:    now,
	}
	if automations := c.m.automations; automations != nil && prd.AutomationID != "" {
		if a, ok := automations.Get(prd.AutomationID); ok {
			rec.AutomationName = a.Name
			for i := range a.Members {
				if a.Members[i].PRDID == prd.ID {
					rec.MemberID = a.Members[i].ID
					break
				}
			}
			if rec.AutomationName == "" {
				rec.MemberID = "" // never report a member without its Automation's name
			}
		}
	}
	return rec
}

// mergePendingAlertBody renders the body of the merge-pending alert for one
// record: which Automaton, its work branch and how far ahead of main it is,
// how long it has been waiting, and what the operator can do. It is shared by
// every surface that reports a merge-pending Automaton.
func mergePendingAlertBody(rec *TriageRecord, now time.Time) string {
	var b strings.Builder
	name := rec.PRDTitle
	if name == "" {
		name = rec.PRDID
	}
	fmt.Fprintf(&b, "Automaton %q (id %s) has finished, but its work branch %s has %d commit(s) that are not on %s yet.\n",
		name, rec.PRDID, rec.Branch, rec.CommitsAhead, mergePendingBase)
	if rec.AutomationID != "" {
		member := ""
		if rec.MemberID != "" {
			member = ", member " + rec.MemberID
		}
		fmt.Fprintf(&b, "Member of Automation %q (id %s)%s.\n", rec.AutomationName, rec.AutomationID, member)
	}
	fmt.Fprintf(&b, "Merge pending since %s (%s).\n", rec.FirstSeenAt.Format(time.RFC3339), now.Sub(rec.FirstSeenAt).Round(time.Minute))
	fmt.Fprintf(&b, "Review the Automaton branch and merge it into %s; nothing has been merged, deleted or pushed.", mergePendingBase)
	return b.String()
}
