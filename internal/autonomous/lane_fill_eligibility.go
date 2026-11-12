package autonomous

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// This is the single home of the lane fill refusal codes. Every reason lane
// fill has for refusing a queued Automaton is a constant in this one list; a
// new rule adds its code here and nowhere else, so the status views, the
// decision records and the audit trail all speak the same names.
const (
	// LaneFillReasonStale: a queued Automaton that has not been touched for
	// longer than the stale limit (probes and old drafts).
	LaneFillReasonStale = "stale"
	// LaneFillReasonHeld: a HOLD line, the words "do not run" or "sequenced
	// last", or a judge that timed out while the Automaton needs review.
	LaneFillReasonHeld = "held"
	// LaneFillReasonSequencedAfterUnshipped: the spec says it is sequenced
	// after another Automaton that is not completed, or that resolves to no
	// Automaton at all.
	LaneFillReasonSequencedAfterUnshipped = "sequenced_after_unshipped"
	// LaneFillReasonProposalNotConfirmed: an implementation Automaton whose
	// named proposal file is readable but carries no Confirmed: yes line.
	LaneFillReasonProposalNotConfirmed = "proposal_not_confirmed"
	// LaneFillReasonProposalMissing: an implementation Automaton whose spec
	// names no proposal file, or none of the named files can be read.
	LaneFillReasonProposalMissing = "proposal_missing"
	// LaneFillReasonMainCarriesWork: the base branch already carries what the
	// Automaton asks for (a merge or task commit for it, or its branch is
	// entirely on the base branch).
	LaneFillReasonMainCarriesWork = "main_carries_work"
	// LaneFillReasonWorktreeInvalid: the Automaton has a recorded worktree
	// that is gone, has no .git entry, or is not in the registered worktree
	// list of its repository.
	LaneFillReasonWorktreeInvalid = "worktree_invalid"
)

// LaneFillRefusal is why lane fill will not take a queued Automaton: a stable
// code from the list above and a readable text. The zero value means not
// refused.
type LaneFillRefusal struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// Refused reports whether the refusal is set.
func (r LaneFillRefusal) Refused() bool { return r.Code != "" }

// LaneFillSource says which fill source a queued Automaton belongs to. The
// sources are tried in this order for every idle lane.
type LaneFillSource int

const (
	// LaneFillSourceNone: not queued work, or excluded (held, chained).
	LaneFillSourceNone LaneFillSource = iota
	// LaneFillSourceReady: decomposed and approved (or awaiting review), can
	// start on an idle lane.
	LaneFillSourceReady
	// LaneFillSourceMerge: every task is done and the work needs a merge.
	// It is reported only; lane fill never starts or merges it.
	LaneFillSourceMerge
	// LaneFillSourceDraft: a draft that can be decomposed.
	LaneFillSourceDraft
)

// PRDRefLookup resolves a reference found in a spec, either an Automaton id
// (8 hex characters) or a title fragment, to the status of the Automaton it
// names. found is false when the reference resolves to nothing. It must not
// do I/O that can block the planner for long; the caller builds it from a
// snapshot.
type PRDRefLookup func(ref string) (status PRDStatus, found bool)

var (
	// holdLinePattern matches a line that starts with HOLD, ignoring case and
	// leading markdown decoration.
	holdLinePattern = regexp.MustCompile(`(?im)^[\s>*_#-]*hold\b`)
	// doNotRunPattern and sequencedLastPattern match the two phrases that
	// keep an Automaton out of every lane.
	doNotRunPattern       = regexp.MustCompile(`(?i)\bdo\s+not\s+run\b`)
	sequencedLastPattern  = regexp.MustCompile(`(?i)\bsequenced\s+last\b`)
	sequencedAfterPattern = regexp.MustCompile(`(?i)\bsequenced\s+after\s+(?:"([^"\n]+)"|'([^'\n]+)'|([^\n.;,]+))`)
	refIDPattern          = regexp.MustCompile(`^[0-9a-fA-F]{8}$`)
	refLeadWordsPattern   = regexp.MustCompile(`(?i)^(?:the|prd|automaton|automata)\s+`)
)

// maxHoldRefChars caps a "sequenced after" reference so a long sentence is
// never used as a title fragment.
const maxHoldRefChars = 80

// PRDHoldReason returns why the Automaton must not be started, or the empty
// string when nothing holds it. It is the text half of PRDHoldCode; see there
// for the rules.
func PRDHoldReason(prd *PRD, lookup PRDRefLookup) string {
	_, text := PRDHoldCode(prd, lookup)
	return text
}

// PRDHoldCode returns the refusal code and the readable text for an
// Automaton that must not be started, or two empty strings when nothing holds
// it. The title and the spec are searched, as data, with regular expressions
// only: nothing in them is ever run. A hold is any of:
//
//   - a judge that timed out on every backend, while the PRD needs review
//     (code held);
//   - a completion rules review block that no corrective task is fixing,
//     while the Automaton needs review (code held);
//   - a line starting with HOLD (any case) (code held);
//   - the words "do not run" (code held);
//   - the words "sequenced last" (code held);
//   - "sequenced after <ref>", where <ref> is an Automaton id or a title
//     fragment that does not resolve, through lookup, to a completed
//     Automaton (code sequenced_after_unshipped). A ref that resolves to
//     nothing counts as unshipped.
//
// A nil lookup resolves nothing.
func PRDHoldCode(prd *PRD, lookup PRDRefLookup) (code, text string) {
	if prd == nil {
		return "", ""
	}
	if judgeHoldHeld(prd) {
		return LaneFillReasonHeld, "its judge timed out on every backend and it needs review"
	}
	if text := reviewBlockHoldText(prd); text != "" {
		return LaneFillReasonHeld, text
	}
	body := prd.Title + "\n" + prd.Spec
	if holdLinePattern.MatchString(body) {
		return LaneFillReasonHeld, "a line starts with HOLD"
	}
	if doNotRunPattern.MatchString(body) {
		return LaneFillReasonHeld, "the text says do not run"
	}
	if sequencedLastPattern.MatchString(body) {
		return LaneFillReasonHeld, "the text says sequenced last"
	}
	for _, m := range sequencedAfterPattern.FindAllStringSubmatch(body, -1) {
		ref := firstNonEmpty(m[1], m[2], m[3])
		ref = cleanHoldRef(ref)
		if ref == "" {
			continue
		}
		if refIDPattern.MatchString(ref) {
			ref = strings.ToLower(ref)
		}
		if reason := unshippedRefReason(ref, lookup); reason != "" {
			return LaneFillReasonSequencedAfterUnshipped, reason
		}
	}
	return "", ""
}

// cleanHoldRef trims a captured reference: surrounding space, a leading
// article or noun such as "the" or "automaton", and any length beyond the cap.
func cleanHoldRef(ref string) string {
	ref = strings.TrimSpace(ref)
	for {
		trimmed := refLeadWordsPattern.ReplaceAllString(ref, "")
		if trimmed == ref {
			break
		}
		ref = trimmed
	}
	if len(ref) > maxHoldRefChars {
		ref = strings.TrimSpace(ref[:maxHoldRefChars])
	}
	return ref
}

// unshippedRefReason returns a hold reason naming ref unless lookup resolves
// it to a completed Automaton.
func unshippedRefReason(ref string, lookup PRDRefLookup) string {
	if lookup == nil {
		return fmt.Sprintf("sequenced after %s, which resolves to no Automaton (counted as unshipped)", ref)
	}
	status, found := lookup(ref)
	switch {
	case !found:
		return fmt.Sprintf("sequenced after %s, which resolves to no Automaton (counted as unshipped)", ref)
	case NormalizePRDStatus(status) != PRDCompleted:
		return fmt.Sprintf("sequenced after %s, which is %s and not completed", ref, status)
	}
	return ""
}

// laneFillQueuedStatus says the status is queued work: a draft, approved or
// needs_review Automaton, the ones lane fill may refuse.
func laneFillQueuedStatus(s PRDStatus) bool {
	switch NormalizePRDStatus(s) {
	case PRDDraft, PRDApproved, PRDNeedsReview:
		return true
	}
	return false
}

// laneFillCandidateRefusal is the refusal in effect for a candidate: the one
// the snapshot filled in, else a held refusal built from HoldReason (the
// field a candidate carries when it was built without the evaluator), else
// none. Only queued work is ever refused.
func laneFillCandidateRefusal(c LaneFillCandidate) LaneFillRefusal {
	if !laneFillQueuedStatus(c.Status) {
		return LaneFillRefusal{}
	}
	if c.Refusal.Refused() {
		return c.Refusal
	}
	if c.HoldReason != "" {
		code := c.HoldCode
		if code == "" {
			code = LaneFillReasonHeld
		}
		return LaneFillRefusal{Code: code, Text: c.HoldReason}
	}
	return LaneFillRefusal{}
}

// laneFillExcludedText words the exclusion of a refused candidate for the
// idle lane blocker.
func laneFillExcludedText(id string, r LaneFillRefusal) string {
	if r.Code == LaneFillReasonProposalNotConfirmed || r.Code == LaneFillReasonProposalMissing {
		return r.Text // these texts already name the Automaton
	}
	if r.Code == LaneFillReasonHeld || r.Code == LaneFillReasonSequencedAfterUnshipped {
		return fmt.Sprintf("Automaton %s: held: %s", id, r.Text)
	}
	return fmt.Sprintf("Automaton %s: %s: %s", id, r.Code, r.Text)
}

// EvaluateLaneFillEligibility applies the refusal rules to one candidate, in a
// fixed order, and returns the code and text of the first that refuses it, or
// two empty strings when the candidate is eligible. It is pure: the clock is
// the now argument. The rules, in order:
//
//  1. held: the candidate carries a hold (HOLD line, do not run, sequenced
//     last, a judge time-out, or sequenced after an unshipped Automaton);
//  2. stale: a queued Automaton (draft, approved or needs_review) whose last
//     update is older than the stale limit;
//  3. proposal_not_confirmed / proposal_missing: an implementation Automaton
//     whose proposal file does not carry a Confirmed: yes line, or which has
//     no readable proposal file (only when require_confirmed_proposal is on);
//  4. main_carries_work: the base branch already carries the work (only when
//     check_main_carries is on);
//  5. worktree_invalid: a recorded worktree that is missing, has no .git
//     entry or is not registered (only when require_worktree is on).
//
// Work that is not queued (running, finished, cancelled) is never refused.
func EvaluateLaneFillEligibility(cfg LaneFillConfig, c LaneFillCandidate, now time.Time) (code, text string) {
	if !laneFillQueuedStatus(c.Status) {
		return "", ""
	}
	if c.HoldReason != "" {
		code = c.HoldCode
		if code == "" {
			code = LaneFillReasonHeld
		}
		return code, c.HoldReason
	}
	if text := laneFillStaleText(cfg, c, now); text != "" {
		return LaneFillReasonStale, text
	}
	if cfg.EffectiveRequireConfirmedProposal() {
		if code, text := laneFillProposalRefusal(c); code != "" {
			return code, text
		}
	}
	if cfg.EffectiveCheckMainCarries() {
		if code, text := laneFillMainRefusal(c); code != "" {
			return code, text
		}
	}
	if cfg.EffectiveRequireWorktree() {
		if code, text := laneFillWorktreeRefusal(c); code != "" {
			return code, text
		}
	}
	return "", ""
}

// laneFillStaleText returns the stale refusal text, or "" when the candidate
// is fresh, has no timestamp, or the rule is off (0 hours).
func laneFillStaleText(cfg LaneFillConfig, c LaneFillCandidate, now time.Time) string {
	limit := cfg.EffectiveStaleAfterHours()
	if limit <= 0 {
		return ""
	}
	last := c.UpdatedAt
	if last.IsZero() {
		last = c.CreatedAt
	}
	if last.IsZero() {
		return ""
	}
	age := now.Sub(last)
	if age <= time.Duration(limit)*time.Hour {
		return ""
	}
	return fmt.Sprintf("Automaton last updated %d hours ago, older than the %d hour stale limit",
		int(math.Ceil(age.Hours())), limit)
}

// ClassifyLaneFillCandidate says which fill source the candidate belongs to.
// When the candidate is queued work that is kept out, the source is
// LaneFillSourceNone and excluded gives the reason; for work that is simply
// not queued (running, completed, cancelled) both are empty.
//
//   - Source 1 (ready): status approved or needs_review, not refused (held,
//     stale), not a member of an Automation (the Automation starts its
//     members), not already running.
//   - Source 2 (merge): every task done and the merge flag set. Reported
//     only, whatever the status.
//   - Source 3 (draft): status draft, not refused, not a member of an
//     Automation.
func ClassifyLaneFillCandidate(c LaneFillCandidate) (source LaneFillSource, excluded string) {
	if c.AllTasksDone && c.NeedsMergeFlag {
		return LaneFillSourceMerge, ""
	}
	var want LaneFillSource
	switch NormalizePRDStatus(c.Status) {
	case PRDApproved, PRDNeedsReview:
		want = LaneFillSourceReady
	case PRDDraft:
		want = LaneFillSourceDraft
	default:
		return LaneFillSourceNone, ""
	}
	if r := laneFillCandidateRefusal(c); r.Refused() {
		return LaneFillSourceNone, laneFillExcludedText(c.ID, r)
	}
	if c.InAutomation {
		return LaneFillSourceNone, fmt.Sprintf("Automaton %s: member of an Automation, which starts it", c.ID)
	}
	return want, ""
}

// preferenceFitsLane reports whether an Automaton with the given lane
// preference may use a lane of type t. An empty preference means any.
func preferenceFitsLane(pref string, t LaneType) bool {
	p, _ := NormalizeLanePreference(pref)
	switch p {
	case "":
		return true
	case LanePreferenceGPU:
		return t == LaneTypeGPU
	case LanePreferenceClaude:
		return t == LaneTypeClaude
	}
	return false
}

// QueuedEligible counts the source 1 items (ready to start) per lane type. A
// gpu lane counts the gpu preference plus any; a claude lane counts the claude
// preference plus any, so an any item is counted in both.
func QueuedEligible(candidates []LaneFillCandidate) map[LaneType]int {
	counts := map[LaneType]int{LaneTypeGPU: 0, LaneTypeClaude: 0}
	for _, c := range candidates {
		if src, _ := ClassifyLaneFillCandidate(c); src != LaneFillSourceReady {
			continue
		}
		for _, t := range []LaneType{LaneTypeGPU, LaneTypeClaude} {
			if preferenceFitsLane(c.LanePreference, t) {
				counts[t]++
			}
		}
	}
	return counts
}
