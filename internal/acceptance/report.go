package acceptance

import (
	"fmt"
	"sort"
	"time"
)

// Invariant ids of the standing check. It only reports; it blocks nothing.
const (
	CheckFrozenBeforeStart = "I18"
	CheckDoneIsProven      = "I19"
	CheckChangesRecorded   = "I20"
)

// Finding is one thing the standing check saw.
type Finding struct {
	Check     string `json:"check"`
	RequestID string `json:"request_id"`
	Item      string `json:"item,omitempty"`
	Text      string `json:"text"`
}

// Group counts open items that share a source and a reason, so one cause is
// one line.
type Group struct {
	Source string   `json:"source"`
	Reason string   `json:"reason"`
	Count  int      `json:"count"`
	Lists  []string `json:"lists"`
}

// Summary is one list in short.
type Summary struct {
	RequestID string    `json:"request_id"`
	Kind      Kind      `json:"kind"`
	Source    string    `json:"source"`
	Status    Status    `json:"status"`
	Label     string    `json:"label"`
	Done      bool      `json:"done"`
	Open      int       `json:"open_items"`
	Items     int       `json:"items"`
	Due       string    `json:"due,omitempty"`
	NeededBy  time.Time `json:"needed_by,omitempty"`
	Yours     string    `json:"yours,omitempty"`
}

// Overview is every list in short, with the grouped open items and counters.
type Overview struct {
	Lists    []Summary      `json:"lists"`
	Groups   []Group        `json:"groups"`
	Statuses map[Status]int `json:"statuses"`
	Results  map[string]int `json:"results"`
}

// Report is the standing check's findings.
type Report struct {
	At        time.Time `json:"at"`
	StartMark time.Time `json:"start_mark"`
	// Starts is how many request starts were recorded after the mark. Zero
	// means no request source reports starts, so I18 saw nothing.
	Starts   int       `json:"starts_seen"`
	Findings []Finding `json:"findings"`
	Overview Overview  `json:"overview"`
}

func overview(lists []*List, now time.Time, warn time.Duration) Overview {
	o := Overview{Lists: []Summary{}, Groups: []Group{}, Statuses: map[Status]int{}, Results: map[string]int{}}
	groups := map[[2]string]*Group{}
	for _, l := range lists {
		r := Read(l)
		sum := Summary{RequestID: l.RequestID, Kind: l.Kind, Source: l.Source, Status: r.Status, Label: r.Label, Done: r.Done,
			Items: len(l.Items), Due: Due(l, now, warn), NeededBy: l.NeededBy, Yours: l.Yours}
		o.Statuses[r.Status]++
		for _, it := range r.Items {
			switch {
			case it.Accepted:
				o.Results["accepted_without_proof"]++
				continue
			case it.Result.Waiting != nil:
				o.Results["waiting"]++
				// Waiting is nobody's action until it is overdue.
				if it.Result.Waiting.Due.IsZero() || !now.After(it.Result.Waiting.Due.Add(warn)) {
					sum.Open++
					continue
				}
			default:
				o.Results[string(it.Result.Value)]++
			}
			if it.Result.Value == ResultMet {
				continue
			}
			sum.Open++
			reason := it.Result.Reason
			if reason == "" {
				reason = "not checked yet"
			}
			k := [2]string{l.Source, reason}
			g := groups[k]
			if g == nil {
				g = &Group{Source: k[0], Reason: k[1]}
				groups[k] = g
			}
			g.Count++
			if n := len(g.Lists); n == 0 || g.Lists[n-1] != l.RequestID {
				g.Lists = append(g.Lists, l.RequestID)
			}
		}
		o.Lists = append(o.Lists, sum)
	}
	for _, g := range groups {
		o.Groups = append(o.Groups, *g)
	}
	sort.Slice(o.Groups, func(i, j int) bool {
		a, b := o.Groups[i], o.Groups[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Source+"\x00"+a.Reason < b.Source+"\x00"+b.Reason
	})
	return o
}

func findings(d data) []Finding {
	out := []Finding{}
	add := func(check, id, item, format string, a ...any) {
		out = append(out, Finding{Check: check, RequestID: id, Item: item, Text: fmt.Sprintf(format, a...)})
	}
	for _, s := range d.Starts {
		if s.At.Before(d.StartMark) {
			continue
		}
		switch l := d.Lists[s.RequestID]; {
		case l == nil:
			add(CheckFrozenBeforeStart, s.RequestID, "", "started with no list")
		case !l.Frozen():
			add(CheckFrozenBeforeStart, s.RequestID, "", "started and its list is not fixed")
		case l.FrozenAt.After(s.At):
			add(CheckFrozenBeforeStart, s.RequestID, "", "its list was fixed after its first notice or spawn")
		}
	}
	ids := make([]string, 0, len(d.Lists))
	for id := range d.Lists {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		l := d.Lists[id]
		r := Read(l)
		claimed := !l.ClaimedDoneAt.IsZero()
		if claimed && !r.Done {
			add(CheckDoneIsProven, id, "", "reported done, but the list reads %q", r.Label)
		}
		if claimed || r.Done || !l.ClosedAt.IsZero() {
			for _, it := range l.Items {
				if e := effective(l, it); !e.Accepted && e.Result.Value != ResultMet {
					add(CheckDoneIsProven, id, it.ID, "no current proof and no acceptance")
				}
			}
			// Also against revision 1, whatever the status says.
			if c := l.ChangesSinceFirst(); c.Any() {
				for _, t := range describeChanges(c, nil) {
					add(CheckDoneIsProven, id, "", "done on a list changed since revision 1: %s", t)
				}
			}
		}
		if askHash(l.Ask) != l.Ask.Hash {
			add(CheckChangesRecorded, id, "", "the ask's words differ from its recorded hash")
		}
		if n := len(l.Revisions); l.Frozen() {
			switch {
			case n == 0:
				add(CheckChangesRecorded, id, "", "a fixed list has no revision")
			case l.Revisions[n-1].Hash != l.Hash() && !unrecordedAskOnly(l):
				add(CheckChangesRecorded, id, "", "changed without a record: the list differs from its last revision")
			}
		}
		for _, e := range l.History {
			if e.AfterFreeze && e.Action != ActionRework && e.Words == "" {
				add(CheckChangesRecorded, id, e.Item, "%s by %s: no words", e.Action, callerText(e.By))
			}
		}
	}
	return out
}

// unrecordedAskOnly reports whether the list differs from its last revision
// only because later ask words, which are recorded as an event, added open
// items.
func unrecordedAskOnly(l *List) bool {
	last := l.Revisions[len(l.Revisions)-1]
	if l.Ask.Hash == last.AskHash {
		return false
	}
	var kept []Item
	was := map[string]bool{}
	for _, it := range last.Items {
		was[it.ID] = true
	}
	for _, it := range l.Items {
		if was[it.ID] || it.Source != SourceAsk {
			kept = append(kept, it)
		}
	}
	return listHash(last.AskHash, l.Kind, kept, l.Reviewer) == last.Hash
}

// Overview returns every list in short.
func (s *Service) Overview() (Overview, error) {
	lists, err := s.All()
	if err != nil {
		return Overview{}, err
	}
	return overview(lists, s.o.Now(), s.settings().WarnBefore), nil
}

// Report runs the standing check. It changes and blocks nothing.
func (s *Service) Report() (Report, error) {
	ov, err := s.Overview()
	if err != nil {
		return Report{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := 0
	for _, r := range s.data.Starts {
		if !r.At.Before(s.data.StartMark) {
			seen++
		}
	}
	return Report{At: s.o.Now(), StartMark: s.data.StartMark, Starts: seen, Findings: findings(s.data), Overview: ov}, nil
}
