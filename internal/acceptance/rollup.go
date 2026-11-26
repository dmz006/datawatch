package acceptance

import (
	"fmt"
	"regexp"
	"time"
)

// Status is how a whole list reads.
type Status string

// The statuses. Only StatusDone and StatusDoneAccepted are done.
const (
	StatusNoCriteria  Status = "no_criteria"
	StatusDraft       Status = "draft"
	StatusOpen        Status = "open"
	StatusWaiting     Status = "waiting"
	StatusNotMet      Status = "not_met"
	StatusAskChanged  Status = "ask_changed"
	StatusReportOnly  Status = "failure_reported_still_broken"
	StatusThinList    Status = "thin_list"
	StatusNotReviewed Status = "met_not_reviewed"
	// StatusChangedList is met on a list changed after work began. How it
	// should read is an open operator choice (choice 5); until then it is not
	// done.
	StatusChangedList  Status = "met_on_changed_list"
	StatusDoneAccepted Status = "done_accepted"
	StatusDone         Status = "done"
	StatusDelivered    Status = "delivered"
	StatusFiled        Status = "filed"
)

var statusLabels = map[Status]string{
	StatusNoCriteria:   "no criteria",
	StatusDraft:        "draft: not fixed yet",
	StatusOpen:         "open",
	StatusWaiting:      "waiting",
	StatusNotMet:       "not met",
	StatusAskChanged:   "the ask changed; the list has not been revised",
	StatusReportOnly:   "failure now reported, still broken",
	StatusThinList:     "the list is too thin for the ask",
	StatusNotReviewed:  "met, but nobody has checked that the list covers the ask",
	StatusChangedList:  "met on a changed list (not plain done; how this reads is an open operator choice)",
	StatusDoneAccepted: "done, accepted without proof",
	StatusDone:         "done",
	StatusDelivered:    "delivered (not the same as done)",
	StatusFiled:        "filed (not the same as done)",
}

// ItemRead is how one item reads now.
type ItemRead struct {
	ID       string `json:"id"`
	Result   Result `json:"result"`
	Accepted bool   `json:"accepted_without_proof,omitempty"`
	// Claim is an answer or acceptance that does not count because datawatch
	// cannot show it came from someone other than the doer.
	Claim   string `json:"claim,omitempty"`
	Changed string `json:"changed,omitempty"` // added or altered since revision 1
}

// Rollup is the reading of a whole list.
type Rollup struct {
	Status Status `json:"status"`
	Label  string `json:"label"`
	// Done is true only when every item is met or accepted on the current
	// version; PlainDone only when nothing was accepted without proof.
	Done      bool       `json:"done"`
	PlainDone bool       `json:"plain_done"`
	Reasons   []string   `json:"reasons,omitempty"`
	Items     []ItemRead `json:"items,omitempty"`
}

func status(s Status, reasons ...string) Rollup {
	return Rollup{Status: s, Label: statusLabels[s], Reasons: reasons, Done: s == StatusDone || s == StatusDoneAccepted, PlainDone: s == StatusDone}
}

// effective is how an item reads now. Only an explicit, current result with
// evidence counts as met; everything else is not met.
func effective(l *List, it Item) ItemRead {
	r := ItemRead{ID: it.ID}
	st := l.State[it.ID]
	if st == nil {
		r.Result = Result{Value: ResultUnchecked}
		return r
	}
	res := st.Result
	switch res.Value {
	case ResultUnchecked, ResultNotMet, ResultCannotTell:
	case ResultMet:
		if len(res.Evidence) == 0 {
			res = cannot("a met result without evidence does not count")
		}
	default:
		res = Result{Value: ResultUnchecked, Reason: "no explicit result"}
	}
	if res.Value != ResultUnchecked && res.Subject != "" && st.CurrentSubject != "" && res.Subject != st.CurrentSubject {
		res = Result{Value: ResultUnchecked, Reason: "stale: the subject changed since this result"}
	}
	r.Result = res
	if c := st.Confirm; c != nil && !c.Counts {
		answer := "not met"
		if c.Yes {
			answer = "met"
		}
		r.Claim = fmt.Sprintf("claimed %s by %s; datawatch cannot show this is not the doer", answer, callerText(c.By))
	}
	if a := st.Accept; a != nil {
		if a.Counts {
			r.Accepted = res.Value != ResultMet
		} else {
			r.Claim = fmt.Sprintf("acceptance claimed by %s; datawatch cannot show this is not the doer", callerText(a.By))
		}
	}
	return r
}

func callerText(c Caller) string {
	observed, claimed := c.Observed, "no name given"
	if observed == "" {
		observed = "unknown caller"
	}
	if c.Claimed != "" {
		claimed = fmt.Sprintf("%q", c.Claimed)
	}
	return fmt.Sprintf("%s (observed: %s)", claimed, observed)
}

var reportOnlyWords = regexp.MustCompile(`(?i)\b(or|otherwise|else|failing that)\b[^.;]*\b(alerts?|reports?|reported|logs?|logged|notif\w*|warns?|warning|says?|explains?|records?|surfac\w*|raises?|raised|flags?|flagged|tells?|why)\b`)

// reportOnly reports whether an item can be satisfied by only saying that
// something failed.
func reportOnly(it Item) bool {
	return it.ReportOnly || it.Source != SourceDefault && reportOnlyWords.MatchString(it.Statement)
}

func allReceipt(items []Item) bool {
	for _, it := range items {
		if it.Method != MethodReceipt {
			return false
		}
	}
	return true
}

func thinList(l *List) bool {
	if l.Kind == KindDelivery {
		return false
	}
	for _, it := range l.Items {
		if it.Source != SourceDefault && it.Method != MethodReceipt {
			return false
		}
	}
	return true
}

// covered reports whether someone other than the doer found that the current
// list covers the whole ask.
func covered(l *List) bool {
	h := l.Hash()
	if l.ReviewedHash == h {
		return true
	}
	c := l.Coverage
	return c != nil && c.Counts && c.Yes && c.Subject == h
}

// Read rolls a list up. A nil list reads "no criteria".
func Read(l *List) Rollup {
	if l == nil || len(l.Items) == 0 {
		return status(StatusNoCriteria)
	}
	var items []ItemRead
	changes := l.ChangesSinceFirst()
	added, altered := map[string]bool{}, map[string]bool{}
	for _, id := range changes.Added {
		added[id] = true
	}
	for _, id := range changes.Altered {
		altered[id] = true
	}
	var notMet, open, waits, accepted, reportItems []string
	for _, it := range l.Items {
		r := effective(l, it)
		switch {
		case added[it.ID]:
			r.Changed = "added"
		case altered[it.ID]:
			r.Changed = "altered"
		}
		items = append(items, r)
		if reportOnly(it) {
			reportItems = append(reportItems, it.ID)
		}
		switch {
		case r.Accepted:
			accepted = append(accepted, it.ID)
		case r.Result.Value == ResultMet:
		case r.Result.Value == ResultNotMet:
			notMet = append(notMet, it.ID)
		case r.Result.Waiting != nil:
			waits = append(waits, it.ID)
		default:
			open = append(open, it.ID)
		}
	}
	roll := func(s Status, reasons ...string) Rollup {
		out := status(s, reasons...)
		out.Items = items
		return out
	}
	onlyAlreadyTrue := len(accepted) == 0
	for _, r := range items {
		if r.Result.Value == ResultMet && !r.Result.AlreadyTrue {
			onlyAlreadyTrue = false
		}
	}
	satisfied := len(notMet)+len(open)+len(waits) == 0
	switch {
	case !l.Frozen():
		return roll(StatusDraft)
	case len(notMet) > 0:
		return roll(StatusNotMet, notMet...)
	case l.Ask.Hash != l.RevisedAskHash:
		return roll(StatusAskChanged)
	}
	if allReceipt(l.Items) && satisfied && len(accepted) == 0 {
		if l.Source == "intake" {
			return roll(StatusFiled)
		}
		return roll(StatusDelivered)
	}
	switch {
	case len(open) > 0:
		return roll(StatusOpen, append(open, waits...)...)
	case len(waits) > 0:
		return roll(StatusWaiting, waits...)
	case (l.Kind == KindChange || l.Kind == KindOther) && len(reportItems) > 0:
		// An unknown source may well be a bug, so it is held to the same rule.
		return roll(StatusReportOnly, reportItems...)
	case l.Kind == KindChange && onlyAlreadyTrue:
		return roll(StatusOpen, "everything was already true at the start, so nothing shows the work")
	case allReceipt(l.Items):
		return roll(StatusThinList, "only receipt items")
	case thinList(l):
		return roll(StatusThinList, "only default or receipt items")
	case !covered(l):
		return roll(StatusNotReviewed)
	case changes.Any():
		return roll(StatusChangedList, describeChanges(changes, accepted)...)
	case len(accepted) > 0:
		return roll(StatusDoneAccepted, accepted...)
	}
	return roll(StatusDone)
}

func describeChanges(c Changes, accepted []string) []string {
	var out []string
	for _, id := range c.Added {
		out = append(out, "added "+id)
	}
	for _, id := range c.Altered {
		out = append(out, "altered "+id)
	}
	for _, it := range c.Removed {
		out = append(out, "removed "+it.ID)
	}
	if c.AskText {
		out = append(out, "ask changed")
	}
	if c.Reviewer {
		out = append(out, "reviewer settings changed")
	}
	for _, id := range accepted {
		out = append(out, "accepted without proof "+id)
	}
	return out
}

// Due is the early warning for a list with a needed-by time.
func Due(l *List, now time.Time, warn time.Duration) string {
	switch {
	case l == nil || l.NeededBy.IsZero() || Read(l).Done:
		return ""
	case now.After(l.NeededBy):
		return "overdue"
	case l.NeededBy.Sub(now) <= warn:
		return "due_soon"
	}
	return ""
}
