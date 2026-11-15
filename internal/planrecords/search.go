package planrecords

import (
	"fmt"
	"sort"
	"strings"
)

// Paging defaults for List and Search.
const (
	DefaultLimit = 50
	MaxLimit     = 500
)

// Filter narrows List and Search. Zero fields match everything.
type Filter struct {
	Kind         Kind
	Project      string
	State        State
	LegacyNumber int
	// Automaton matches shipped_by, raised_by or a link of kind "automaton".
	Automaton string
	Limit     int
	Offset    int
	// IncludeArchived adds archived plans to the result; ArchivedOnly returns
	// only archived plans. With neither set, archived plans are left out.
	IncludeArchived bool
	ArchivedOnly    bool
}

func (f Filter) matches(r *Record) bool {
	if !(ArchiveScope{IncludeArchived: f.IncludeArchived, ArchivedOnly: f.ArchivedOnly}).allows(r.Archived) {
		return false
	}
	if f.Kind != "" && r.Kind != f.Kind {
		return false
	}
	if f.Project != "" && r.Project != f.Project {
		return false
	}
	if f.State != "" && r.State != f.State {
		return false
	}
	if f.LegacyNumber > 0 && r.LegacyNumber != f.LegacyNumber {
		return false
	}
	if f.Automaton != "" {
		a := strings.TrimSpace(f.Automaton)
		ok := r.ShippedBy == a || r.RaisedBy == a
		for _, l := range r.Links {
			if l.Kind == "automaton" && l.Ref == a {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func (f Filter) page(total int) (lo, hi int) {
	lim := f.Limit
	if lim <= 0 {
		lim = DefaultLimit
	}
	if lim > MaxLimit {
		lim = MaxLimit
	}
	lo = f.Offset
	if lo < 0 {
		lo = 0
	}
	if lo > total {
		lo = total
	}
	hi = lo + lim
	if hi > total {
		hi = total
	}
	return lo, hi
}

func newestFirst(rs []Record) {
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].UpdatedAt != rs[j].UpdatedAt {
			return rs[i].UpdatedAt > rs[j].UpdatedAt
		}
		return rs[i].ID < rs[j].ID
	})
}

// List returns one page of matching records, newest-updated first, and the
// total number of matches before paging.
func (s *Store) List(f Filter) ([]Record, int) {
	s.mu.RLock()
	var all []Record
	for _, r := range s.byUID {
		if f.matches(r) {
			all = append(all, *r)
		}
	}
	s.mu.RUnlock()
	newestFirst(all)
	lo, hi := f.page(len(all))
	out := make([]Record, 0, hi-lo)
	for _, r := range all[lo:hi] {
		out = append(out, r.clone())
	}
	return out, len(all)
}

// Where a search hit matched, best first.
const (
	MatchTitle = "title"
	MatchAlias = "alias"
	MatchBody  = "body"
	MatchAll   = "all"
)

// Field weights; title hits always outrank alias hits, alias hits outrank body hits.
const (
	scoreTitleExact  = 100
	scoreTitlePrefix = 80
	scoreTitleHas    = 60
	scoreAlias       = 40
	scoreBody        = 20
)

// Hit is one search result.
type Hit struct {
	Record  Record `json:"record"`
	Score   int    `json:"score"`
	Matched string `json:"matched"`
}

func scoreToken(r *Record, tok string) (int, string) {
	title := strings.ToLower(r.Title)
	switch {
	case NormTitle(r.Title) == tok:
		return scoreTitleExact, MatchTitle
	case strings.HasPrefix(title, tok):
		return scoreTitlePrefix, MatchTitle
	case strings.Contains(title, tok):
		return scoreTitleHas, MatchTitle
	}
	for _, a := range r.Aliases {
		if strings.Contains(strings.ToLower(a), tok) {
			return scoreAlias, MatchAlias
		}
	}
	if strings.Contains(strings.ToLower(r.Body), tok) {
		return scoreBody, MatchBody
	}
	return 0, ""
}

// Search matches the query case-insensitively over title, aliases and body.
// Every whitespace-separated word must appear somewhere; title hits rank
// before alias hits, which rank before body hits; ties go newest-updated
// first. An empty query returns every record the filter allows. The second
// result is the total number of matches before paging.
func (s *Store) Search(query string, f Filter) ([]Hit, int) {
	toks := strings.Fields(strings.ToLower(query))
	phrase := strings.ToLower(strings.Join(toks, " "))
	s.mu.RLock()
	var hits []Hit
	for _, r := range s.byUID {
		if !f.matches(r) {
			continue
		}
		h := Hit{Record: *r, Matched: MatchAll}
		if len(toks) > 0 {
			sum, worst := 0, scoreTitleExact+1
			h.Matched = MatchTitle
			ok := true
			for _, t := range toks {
				sc, where := scoreToken(r, t)
				if sc == 0 {
					ok = false
					break
				}
				sum += sc
				if sc < worst {
					worst, h.Matched = sc, where
				}
			}
			if !ok {
				continue
			}
			h.Score = sum / len(toks)
			// The whole phrase inside the title counts as a title hit for the phrase.
			if len(toks) > 1 {
				if ps, _ := scoreToken(r, phrase); ps >= scoreTitleHas && ps > h.Score {
					h.Score = ps
				}
			}
		}
		hits = append(hits, h)
	}
	s.mu.RUnlock()
	sort.Slice(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Record.UpdatedAt != b.Record.UpdatedAt {
			return a.Record.UpdatedAt > b.Record.UpdatedAt
		}
		return a.Record.ID < b.Record.ID
	})
	lo, hi := f.page(len(hits))
	out := make([]Hit, 0, hi-lo)
	for _, h := range hits[lo:hi] {
		h.Record = h.Record.clone()
		out = append(out, h)
	}
	return out, len(hits)
}

// Item types a title search can return.
const (
	TypePlan        = "plan"
	TypeAutomaton   = "automaton"
	TypeAutomation  = "automation"
	TypeRegistryRow = "registry_row"
	TypeFinding     = "finding"
)

// TitleFilter narrows SearchTitles. Zero fields match everything; Types limits
// the item types searched. State applies to every type: a plan, row or finding
// state, or the state last reported for a linked Automaton or Automation.
type TitleFilter struct {
	Types   []string
	Project string
	State   string
	Limit   int
	Offset  int
	// IncludeArchived adds archived plans to the result; ArchivedOnly returns
	// only archived plans (and the Automata and Automations linked to them).
	// With neither set, archived plans are left out. An Automaton or Automation
	// is never hidden because its plan is archived.
	IncludeArchived bool
	ArchivedOnly    bool
}

// TitleHit is one title search result.
type TitleHit struct {
	Type    string `json:"type"`
	Ref     string `json:"ref"`
	Title   string `json:"title"`
	State   string `json:"state,omitempty"`
	Project string `json:"project,omitempty"`
	// PlanRef is the plan an Automaton, Automation or plan-attached finding
	// belongs to.
	PlanRef string `json:"plan_ref,omitempty"`
	Score   int    `json:"score"`
}

func titleScore(title string, toks []string, phrase string) int {
	low := strings.ToLower(title)
	if len(toks) == 0 {
		return 1
	}
	for _, t := range toks {
		if !strings.Contains(low, t) {
			return 0
		}
	}
	switch {
	case NormTitle(title) == phrase:
		return scoreTitleExact
	case strings.HasPrefix(low, phrase):
		return scoreTitlePrefix
	}
	return scoreTitleHas
}

// SearchTitles finds plans, Automata, Automations, registry rows and findings
// by title (every word must appear, case-insensitive) with an optional state
// filter. It reads only the summary index, never a full record. Better title
// matches come first; ties go to plans in priority order, then by title.
func (s *Store) SearchTitles(query string, f TitleFilter) ([]TitleHit, int) {
	toks := strings.Fields(strings.ToLower(query))
	phrase := strings.Join(toks, " ")
	want := map[string]bool{}
	for _, t := range f.Types {
		want[t] = true
	}
	wants := func(t string) bool { return len(want) == 0 || want[t] }
	scope := ArchiveScope{IncludeArchived: f.IncludeArchived, ArchivedOnly: f.ArchivedOnly}
	s.mu.RLock()
	var hits []TitleHit
	order := map[string]int{}
	for _, sm := range s.li.sums {
		if !wants(string(sm.Kind)) || (f.Project != "" && sm.Project != f.Project) ||
			(f.State != "" && string(sm.State) != f.State) || !scope.allows(sm.Archived) {
			continue
		}
		sc := titleScore(sm.Title, toks, phrase)
		if sc == 0 {
			continue
		}
		h := TitleHit{Type: string(sm.Kind), Ref: sm.ID, Title: sm.Title, State: string(sm.State), Project: sm.Project, Score: sc}
		if sm.Kind == KindPlan {
			order[sm.ID] = sm.Order
		}
		if sm.Kind == KindFinding && sm.AttachKind == AttachPlan {
			h.PlanRef = sm.AttachRef
		}
		hits = append(hits, h)
	}
	for k, e := range s.li.linked {
		typ, id, _ := strings.Cut(k, ":")
		plan, ok := s.li.sums[e.planUID]
		if !ok || !wants(typ) || (f.Project != "" && plan.Project != f.Project) ||
			(f.State != "" && e.info.State != f.State) || (scope.ArchivedOnly && !plan.Archived) {
			continue
		}
		sc := titleScore(e.info.Title, toks, phrase)
		if sc == 0 || (len(toks) > 0 && e.info.Title == "") {
			continue
		}
		hits = append(hits, TitleHit{Type: typ, Ref: id, Title: e.info.Title, State: e.info.State,
			Project: plan.Project, PlanRef: plan.ID, Score: sc})
	}
	s.mu.RUnlock()
	sort.Slice(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Type == TypePlan && b.Type == TypePlan && order[a.Ref] != order[b.Ref] {
			return order[a.Ref] < order[b.Ref]
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return a.Type+a.Ref < b.Type+b.Ref
	})
	lo, hi := Filter{Limit: f.Limit, Offset: f.Offset}.page(len(hits))
	return append([]TitleHit(nil), hits[lo:hi]...), len(hits)
}

// ConflictPolicy says how an import resolves a title conflict.
type ConflictPolicy string

// The conflict policies.
const (
	// PolicySuffix adds a short suffix to the new title and flags the record
	// needs_review. This is the default.
	PolicySuffix ConflictPolicy = "suffix"
	// PolicyRename asks the operator for a new title; the preview carries no
	// title for it (RequiresInput).
	PolicyRename ConflictPolicy = "rename"
	// PolicySkip leaves the conflicting proposed record out.
	PolicySkip ConflictPolicy = "skip"
)

// ParsePolicy parses a policy name; empty means the default (suffix).
func ParsePolicy(s string) (ConflictPolicy, error) {
	switch p := ConflictPolicy(strings.ToLower(strings.TrimSpace(s))); p {
	case "":
		return PolicySuffix, nil
	case PolicySuffix, PolicyRename, PolicySkip:
		return p, nil
	}
	return "", fmt.Errorf("planrecords: conflict policy must be suffix, rename or skip, got %q", s)
}

// Conflict reasons.
const (
	ConflictTitle        = "title"
	ConflictLegacyNumber = "legacy_number"
)

// Resolution is what one policy would do to one conflicting proposed record.
type Resolution struct {
	Policy ConflictPolicy `json:"policy"`
	// NewTitle is the title the record would get (suffix); empty otherwise.
	NewTitle string `json:"new_title,omitempty"`
	Skip     bool   `json:"skip,omitempty"`
	// RequiresInput is true for rename: the operator must supply the title.
	RequiresInput bool `json:"requires_input,omitempty"`
	NeedsReview   bool `json:"needs_review,omitempty"`
}

// Conflict is one proposed record that collides with an existing record or
// with an earlier proposed record in the same batch.
type Conflict struct {
	Index    int    `json:"index"`
	Reason   string `json:"reason"`
	Proposed Record `json:"proposed"`
	// Existing is the stored record in the way; nil for a batch-internal clash.
	Existing *Record `json:"existing,omitempty"`
	// EarlierIndex is the earlier proposed record in the way (-1 when Existing is set).
	EarlierIndex int `json:"earlier_index"`
	// Resolutions holds what each policy would do, keyed by policy name.
	Resolutions map[ConflictPolicy]Resolution `json:"resolutions"`
	// Applied is the resolution of the policy that was asked for.
	Applied Resolution `json:"applied"`
}

// Preview is the dry-run result of PreviewConflicts.
type Preview struct {
	Policy    ConflictPolicy `json:"policy"`
	Total     int            `json:"total"`
	Conflicts []Conflict     `json:"conflicts"`
	// Clean is the number of proposed records with no conflict.
	Clean int `json:"clean"`
}

// PreviewConflicts lists every title and legacy-number conflict among the
// proposed records, against the store and against each other, and the
// resolution each policy would apply, without writing anything. An empty
// policy uses the store's configured default. Proposed records that fail
// validation are not conflicts; the import reports them separately.
func (s *Store) PreviewConflicts(proposed []Record, policy ConflictPolicy) Preview {
	if policy == "" {
		policy = s.opts.ImportConflictPolicy
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	pv := Preview{Policy: policy, Total: len(proposed), Conflicts: []Conflict{}}
	claimedTitle := map[string]int{}  // project|title -> proposed index
	claimedNumber := map[string]int{} // project|number -> proposed index
	for i, p := range proposed {
		tk := titleKey(p.Project, p.Title)
		nk := fmt.Sprintf("%s\x00%d", p.Project, p.LegacyNumber)
		c := Conflict{Index: i, Proposed: p.clone(), EarlierIndex: -1}
		switch {
		case p.LegacyNumber > 0 && s.legacyHolder(p.Project, p.LegacyNumber) != nil:
			ex := s.legacyHolder(p.Project, p.LegacyNumber).clone()
			c.Reason, c.Existing = ConflictLegacyNumber, &ex
		case p.LegacyNumber > 0 && claimedNumber[nk] > 0:
			c.Reason, c.EarlierIndex = ConflictLegacyNumber, claimedNumber[nk]-1
		case s.byTitle[tk] != "":
			ex := s.byUID[s.byTitle[tk]].clone()
			c.Reason, c.Existing = ConflictTitle, &ex
		case claimedTitle[tk] > 0:
			c.Reason, c.EarlierIndex = ConflictTitle, claimedTitle[tk]-1
		}
		if c.Reason == "" {
			claimedTitle[tk] = i + 1
			if p.LegacyNumber > 0 {
				claimedNumber[nk] = i + 1
			}
			pv.Clean++
			continue
		}
		c.Resolutions = map[ConflictPolicy]Resolution{}
		for _, pol := range []ConflictPolicy{PolicySuffix, PolicyRename, PolicySkip} {
			c.Resolutions[pol] = s.resolution(pol, c, p, claimedTitle)
		}
		c.Applied = c.Resolutions[policy]
		if r := c.Applied; r.NewTitle != "" {
			claimedTitle[titleKey(p.Project, r.NewTitle)] = i + 1
		}
		pv.Conflicts = append(pv.Conflicts, c)
	}
	return pv
}

// resolution decides what a policy does to one conflict. A clash on the row
// number means the same row was already imported, so every policy skips it.
func (s *Store) resolution(pol ConflictPolicy, c Conflict, p Record, claimed map[string]int) Resolution {
	if c.Reason == ConflictLegacyNumber || pol == PolicySkip {
		return Resolution{Policy: pol, Skip: true}
	}
	if pol == PolicyRename {
		return Resolution{Policy: pol, RequiresInput: true}
	}
	taken := func(t string) bool {
		k := titleKey(p.Project, t)
		return s.byTitle[k] != "" || claimed[k] > 0
	}
	cand := strings.TrimSpace(p.Title)
	if p.LegacyNumber > 0 {
		cand = fmt.Sprintf("%s (row %d)", strings.TrimSpace(p.Title), p.LegacyNumber)
	}
	for n := 2; taken(cand); n++ {
		cand = fmt.Sprintf("%s (%d)", strings.TrimSpace(p.Title), n)
	}
	return Resolution{Policy: pol, NewTitle: cand, NeedsReview: true}
}
