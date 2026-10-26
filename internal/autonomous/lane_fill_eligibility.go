package autonomous

import (
	"fmt"
	"regexp"
	"strings"
)

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
// string when nothing holds it. The title and the spec are searched, as data,
// with regular expressions only: nothing in them is ever run. A hold is any of:
//
//   - a judge that timed out on every backend, while the PRD needs review;
//   - a line starting with HOLD (any case);
//   - the words "do not run";
//   - the words "sequenced last";
//   - "sequenced after <ref>", where <ref> is an Automaton id or a title
//     fragment that does not resolve, through lookup, to a completed
//     Automaton. A ref that resolves to nothing counts as unshipped.
//
// A nil lookup resolves nothing.
func PRDHoldReason(prd *PRD, lookup PRDRefLookup) string {
	if prd == nil {
		return ""
	}
	if judgeHoldHeld(prd) {
		return "its judge timed out on every backend and it needs review"
	}
	text := prd.Title + "\n" + prd.Spec
	if holdLinePattern.MatchString(text) {
		return "a line starts with HOLD"
	}
	if doNotRunPattern.MatchString(text) {
		return "the text says do not run"
	}
	if sequencedLastPattern.MatchString(text) {
		return "the text says sequenced last"
	}
	for _, m := range sequencedAfterPattern.FindAllStringSubmatch(text, -1) {
		ref := firstNonEmpty(m[1], m[2], m[3])
		ref = cleanHoldRef(ref)
		if ref == "" {
			continue
		}
		if refIDPattern.MatchString(ref) {
			ref = strings.ToLower(ref)
		}
		if reason := unshippedRefReason(ref, lookup); reason != "" {
			return reason
		}
	}
	return ""
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

// ClassifyLaneFillCandidate says which fill source the candidate belongs to.
// When the candidate is queued work that is kept out, the source is
// LaneFillSourceNone and excluded gives the reason; for work that is simply
// not queued (running, completed, cancelled) both are empty.
//
//   - Source 1 (ready): status approved or needs_review, not held, not a
//     member of an Automation (the Automation starts its members), not
//     already running.
//   - Source 2 (merge): every task done and the merge flag set. Reported
//     only, whatever the status.
//   - Source 3 (draft): status draft, not held, not a member of an Automation.
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
	if c.HoldReason != "" {
		return LaneFillSourceNone, fmt.Sprintf("PRD %s: held: %s", c.ID, c.HoldReason)
	}
	if c.InAutomation {
		return LaneFillSourceNone, fmt.Sprintf("PRD %s: member of an Automation, which starts it", c.ID)
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
