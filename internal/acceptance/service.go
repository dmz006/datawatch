package acceptance

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Errors the surfaces map to their own answers.
var (
	ErrOff         = errors.New("acceptance is off (set acceptance.mode to report)")
	ErrNotFound    = errors.New("no done-when list for that request (it reads: no criteria)")
	ErrInvalid     = errors.New("invalid request")
	ErrListChanged = errors.New("the list changed since it was read; read it again")
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// coverageItem is the item id a confirmation uses to say the list covers the
// whole ask.
const coverageItem = "list"

// Options wires a Service. Resolver, Probes and Reviewer are supplied by the
// request sources and the reviewer build; without them the items that need
// them read "cannot tell".
type Options struct {
	Path     string
	Key      []byte // encrypts the store when set
	Mode     func() string
	Settings func() Settings
	Runner   Runner
	Repo     Repo
	Resolver SubjectResolver
	Probes   map[string]Probe
	Reviewer Reviewer
	Now      func() time.Time
	// Audit receives every recorded change. It gets ids and the caller, never
	// the ask or an item's words.
	Audit func(requestID string, e Event)
}

// Service keeps the lists.
type Service struct {
	mu sync.Mutex
	// evalMu lets one run at a time work, so one verdict key never gets two
	// reviewer runs.
	evalMu sync.Mutex
	o      Options
	data   data
}

// Open reads the store. A damaged store is an error; it is not replaced.
func Open(o Options) (*Service, error) {
	if o.Mode == nil {
		o.Mode = func() string { return ModeOff }
	}
	if o.Settings == nil {
		o.Settings = func() Settings { return Settings{} }
	}
	if o.Runner == nil {
		o.Runner = Git{}
	}
	if o.Repo == nil {
		o.Repo = Git{}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	d, err := loadData(o.Path, o.Key)
	if err != nil {
		return nil, err
	}
	return &Service{o: o, data: d}, nil
}

// On reports whether report mode is on. Any other value is off.
func (s *Service) On() bool { return s != nil && s.o.Mode() == ModeReport }

func (s *Service) settings() Settings { return s.o.Settings().withDefaults() }

// lock takes the lock with report mode checked and the start mark set.
func (s *Service) lock() error {
	if !s.On() {
		return ErrOff
	}
	s.mu.Lock()
	if s.data.StartMark.IsZero() {
		s.data.StartMark = s.o.Now()
		if err := saveData(s.o.Path, s.o.Key, s.data); err != nil {
			s.data.StartMark = time.Time{}
			s.mu.Unlock()
			return err
		}
	}
	return nil
}

// mutate changes one list and saves it. Nothing is kept when fn or the save
// fails.
func (s *Service) mutate(id string, fn func(l *List) error) (*List, error) {
	if err := s.lock(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	cur, ok := s.data.Lists[id]
	if !ok {
		return nil, ErrNotFound
	}
	l, err := clone(cur)
	if err != nil {
		return nil, err
	}
	before := len(l.History)
	if err := fn(l); err != nil {
		return nil, err
	}
	return s.commit(l, before, false)
}

// commit stores l; the lock is held. ran says that a run has just read the
// current versions. Otherwise a list is closed only when it holds nothing a run
// would read.
func (s *Service) commit(l *List, historyBefore int, ran bool) (*List, error) {
	switch r := rollUp(l); {
	case !r.Done:
		l.ClosedAt = time.Time{}
	case l.ClosedAt.IsZero() && (!l.needsRun() || ran && l.boundToVersions()):
		l.ClosedAt = s.o.Now()
	}
	l.Serial++
	old, had := s.data.Lists[l.RequestID]
	s.data.Lists[l.RequestID] = l
	if err := saveData(s.o.Path, s.o.Key, s.data); err != nil {
		if had {
			s.data.Lists[l.RequestID] = old
		} else {
			delete(s.data.Lists, l.RequestID)
		}
		return nil, err
	}
	if s.o.Audit != nil {
		for _, e := range l.History[min(historyBefore, len(l.History)):] {
			s.o.Audit(l.RequestID, e)
		}
	}
	return clone(l)
}

func (s *Service) event(l *List, action, item, words string, by Caller) {
	l.History = append(l.History, Event{At: s.o.Now(), Action: action, Item: item, By: by, Words: words, AfterFreeze: l.Frozen() && action != ActionFreeze})
}

// Get returns a copy of one list.
func (s *Service) Get(id string) (*List, error) {
	if err := s.lock(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	l, ok := s.data.Lists[id]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(l)
}

// All returns copies of every list, by request id.
func (s *Service) All() ([]*List, error) {
	if err := s.lock(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.data.Lists))
	for id := range s.data.Lists {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*List, 0, len(ids))
	for _, id := range ids {
		l, err := clone(s.data.Lists[id])
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

// Disposition is what anything that closes a bug or a request, or deletes its
// work, has to read, instead of "completed". Off or no list is "no criteria".
func (s *Service) Disposition(requestID string) Rollup {
	l, err := s.Get(requestID)
	if err != nil {
		return status(StatusNoCriteria, err.Error())
	}
	return Read(l)
}

// EditInput creates or changes a list.
type EditInput struct {
	RequestID string
	// Source, Ask, Scheduled and Packs apply when the list is created. The ask
	// is kept word for word; later words go in AddAsk.
	Source    string
	Ask       string
	Scheduled bool
	Packs     []string
	// SourceKnown is set only by datawatch's own request sources, which see
	// where a request came from. No request body can set it.
	SourceKnown bool
	AddAsk      string
	Upsert      []Item
	Remove      []string
	NeededBy    *time.Time
	Words       string
	// Hash, when set, must name the list as it stands.
	Hash string
}

func validItem(it *Item) error {
	it.ID = strings.TrimSpace(it.ID)
	it.Statement = strings.TrimSpace(it.Statement)
	it.Method = strings.TrimSpace(it.Method)
	switch {
	case it.ID == coverageItem:
		return invalid("the item id %q is reserved", coverageItem)
	case it.Statement == "":
		return invalid("an item needs a statement")
	case it.Method == "":
		return invalid("an item always has a check method")
	case it.Before != BeforeNone && it.Before != BeforeFailFirst && it.Before != BeforeDiffer:
		return invalid("before must be empty, fail_first or differ")
	}
	if it.ID == "" {
		it.ID = "i-" + hashOf(it.Statement, it.Method)[:10]
	}
	if it.Source == "" || it.Source == SourceDefault || it.Source == SourceAsk {
		it.Source = SourceUser
	}
	it.Revision = 0
	return nil
}

// apply puts the upserts and removals into the list.
func (s *Service) apply(l *List, in EditInput, by Caller) (changed []string, err error) {
	for _, id := range in.Remove {
		i, it := l.item(id)
		if it == nil {
			return nil, invalid("no item %q", id)
		}
		if it.Source == SourceAsk && !l.Frozen() {
			return nil, invalid("item %q is a sentence of the ask that nothing else covers; add an item whose \"from\" quotes the whole sentence", id)
		}
		l.Items = append(l.Items[:i], l.Items[i+1:]...)
		changed = append(changed, id)
	}
	for _, it := range in.Upsert {
		if err := validItem(&it); err != nil {
			return nil, err
		}
		i, old := l.item(it.ID)
		if old == nil {
			l.Items = append(l.Items, it)
			changed = append(changed, it.ID)
			continue
		}
		same := it
		same.Source, same.Revision = old.Source, old.Revision
		if itemHash(same) == itemHash(*old) {
			continue
		}
		it.Revision = old.Revision
		if l.Frozen() {
			it.Revision++
		}
		l.Items[i] = it
		changed = append(changed, it.ID)
	}
	return changed, nil
}

// addAsk records later words. New sentences become open items and the list
// has to be revised before it can read done.
func (s *Service) addAsk(l *List, words string, by Caller) {
	before := map[string]bool{}
	for _, x := range AskSentences(askFullText(l.Ask)) {
		before[normalizeAsk(x)] = true
	}
	l.Ask.Additions = append(l.Ask.Additions, AskAddition{Text: words, By: by, At: s.o.Now()})
	l.Ask.Hash = askHash(l.Ask)
	if !l.Frozen() {
		l.RevisedAskHash = l.Ask.Hash
		syncAskItems(l, true)
	} else {
		added := 0
		for _, x := range AskSentences(words) {
			if before[normalizeAsk(x)] {
				continue
			}
			id := askItemID(x)
			if _, it := l.item(id); it != nil {
				continue
			}
			l.Items = append(l.Items, Item{ID: id, Statement: x, From: x, Source: SourceAsk, Method: MethodConfirm, Checker: "operator"})
			added++
		}
		if added == 0 {
			// Words that repeat the ask still open something to look at.
			id := fmt.Sprintf("ask-more-%d", len(l.Ask.Additions))
			l.Items = append(l.Items, Item{ID: id, Statement: words, From: words, Source: SourceAsk, Method: MethodConfirm, Checker: "operator"})
		}
	}
	s.event(l, ActionAskWords, "", words, by)
}

// Edit creates a list, changes a draft freely, or revises a fixed list. A
// revision keeps the earlier items, starts changed items again and is
// recorded with the claimed and the observed caller. The ask is sync'd with
// the items so no sentence of it goes uncovered silently.
func (s *Service) Edit(ctx context.Context, in EditInput, by Caller) (*List, error) {
	id := strings.TrimSpace(in.RequestID)
	if id == "" {
		return nil, invalid("a request id is needed")
	}
	set := s.settings()
	if err := s.lock(); err != nil {
		return nil, err
	}
	cur, exists := s.data.Lists[id]
	var l *List
	var err error
	if exists {
		l, err = clone(cur)
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	wasHash, wasSerial := "", 0
	if !exists {
		// datawatch's own delivery-only lists have an empty ask: there is a
		// message to receive and nothing in it is asked of anyone.
		if strings.TrimSpace(in.Ask) == "" && !(in.SourceKnown && KindForSource(in.Source) == KindDelivery) {
			return nil, invalid("a new list needs the ask, word for word")
		}
		if l, err = s.newList(id, in, set); err != nil {
			return nil, err
		}
		s.event(l, ActionDraft, "", in.Words, by)
	} else {
		wasHash, wasSerial = l.Hash(), l.Serial
		switch {
		case in.Hash != "" && in.Hash != wasHash:
			return nil, ErrListChanged
		case in.Ask != "" && in.Ask != l.Ask.Text:
			return nil, invalid("the ask is kept word for word; add later words with add_ask")
		case in.Source != "" && in.Source != l.Source || len(in.Packs) > 0 || in.Scheduled && !l.Scheduled:
			return nil, invalid("the source, packs and schedule are set when the list is created")
		}
	}
	histBefore := 0
	if exists {
		histBefore = len(l.History)
	}
	changed, err := s.apply(l, in, by)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.AddAsk) != "" {
		s.addAsk(l, strings.TrimSpace(in.AddAsk), by)
	}
	if in.NeededBy != nil {
		l.NeededBy = *in.NeededBy
	}
	if !l.Frozen() {
		syncAskItems(l, true)
		l.RevisedAskHash = l.Ask.Hash
		if exists && (len(changed) > 0 || in.NeededBy != nil) {
			s.event(l, ActionDraft, strings.Join(changed, ","), in.Words, by)
		}
	} else if len(changed) > 0 {
		s.revise(ctx, l, changed, set, by, in.Words)
	} else if in.NeededBy != nil {
		s.event(l, ActionRevise, "", in.Words, by)
	}

	if err := s.lock(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	now, ok := s.data.Lists[id]
	if ok != exists || ok && now.Serial != wasSerial {
		return nil, ErrListChanged
	}
	if ok {
		histBefore = min(histBefore, len(now.History))
	}
	return s.commit(l, histBefore, false)
}

func (s *Service) newList(id string, in EditInput, set Settings) (*List, error) {
	defs, err := LoadDefaults(set.DefaultsFile)
	if err != nil {
		return nil, err
	}
	source := strings.ToLower(strings.TrimSpace(in.Source))
	if source == "" {
		source = "unknown"
	}
	scheduled := in.Scheduled || source == "schedule"
	named := KindForSource(source)
	kind := named
	if !in.SourceKnown && kind != KindChange {
		// The caller's word is not what datawatch knows. KindOther carries
		// every rule; only "change" adds to it.
		kind = KindOther
	}
	l := &List{
		RequestID: id, Unit: id, Source: source, SourceKnown: in.SourceKnown, Kind: kind, Scheduled: scheduled,
		Ask: Ask{Text: in.Ask}, State: map[string]*ItemState{}, CreatedAt: s.o.Now(), DefaultsVersion: defs.Version,
	}
	l.Ask.Hash = askHash(l.Ask)
	l.RevisedAskHash = l.Ask.Hash
	if l.Items, err = defs.itemsFor(named, scheduled, in.Packs); err != nil {
		return nil, invalid("%v", err)
	}
	if named != kind {
		// The named source's defaults come on top of those of the kind the
		// list is held to.
		floor, err := defs.itemsFor(kind, false, nil)
		if err != nil {
			return nil, invalid("%v", err)
		}
		for _, it := range floor {
			if _, have := l.item(it.ID); have == nil {
				l.Items = append(l.Items, it)
			}
		}
	}
	if l.SourceKnown {
		if l.DaemonDefaults, err = clone(l.Items); err != nil {
			return nil, err
		}
	}
	if s.o.Reviewer != nil {
		l.Reviewer = s.o.Reviewer.Settings()
	}
	// The ask's own items and the caller's come before the fallback.
	probe := *l
	probe.Items = append(append([]Item(nil), l.Items...), in.Upsert...)
	syncAskItems(&probe, true)
	if len(probe.Items) == 0 {
		for _, it := range defs.Fallback {
			it.Source = SourceDefault
			l.Items = append(l.Items, it)
		}
		if l.SourceKnown {
			if l.DaemonDefaults, err = clone(l.Items); err != nil {
				return nil, err
			}
		}
	}
	return l, nil
}

func (s *Service) evaluatorFor(set Settings, verdicts map[string]Verdict) *evaluator {
	return &evaluator{set: set, now: s.o.Now, runner: s.o.Runner, repo: s.o.Repo, resolver: s.o.Resolver, probes: s.o.Probes, reviewer: s.o.Reviewer, verdicts: verdicts}
}

// revise records a change to a fixed list. A changed item and its result
// start again.
func (s *Service) revise(ctx context.Context, l *List, changed []string, set Settings, by Caller, words string) {
	e := s.evaluatorFor(set, nil)
	for _, id := range changed {
		delete(l.State, id)
		i, it := l.item(id)
		if it == nil {
			continue
		}
		saveCheck(&l.Items[i], set.ProjectChecks)
		st := l.state(id)
		// The revised check's inputs are saved as they are now: this is the
		// reviewed version.
		if l.Items[i].Check != nil && e.resolver != nil {
			if sub, err := e.subjectFor(ctx, l, l.Items[i]); err == nil {
				e.freezeInputs(ctx, l.Items[i], st, sub)
			}
		}
		if start, ok := l.StartSubjects[startEffect(l.Items[i])]; ok {
			st.Start = e.startValue(ctx, l, l.Items[i], start)
		}
	}
	l.RevisedAskHash = l.Ask.Hash
	l.ClosedAt = time.Time{}
	s.event(l, ActionRevise, strings.Join(changed, ","), words, by)
	l.Revisions = append(l.Revisions, snapshotRevision(l, len(l.Revisions)+1, s.o.Now(), by, words))
}

func startEffect(it Item) string {
	if base, _ := it.methodBase(); base != MethodReview && strings.HasPrefix(it.Effect, "ref:") {
		return it.Effect
	}
	return ""
}

func snapshotRevision(l *List, n int, at time.Time, by Caller, words string) Revision {
	items, err := clone(l.Items)
	if err != nil {
		items = append([]Item(nil), l.Items...)
	}
	return Revision{N: n, At: at, By: by, Words: words, Items: items, AskHash: l.Ask.Hash, Reviewer: l.Reviewer, Hash: l.Hash()}
}

// Freeze fixes the list before work starts: it saves a copy of each check,
// the hashes of its inputs, each start value and each start run. approved must
// name the exact list that was approved or read.
//
// The request sources call this before the first notice, spawn or typed
// input.
func (s *Service) Freeze(ctx context.Context, id, approved string, by Caller, words string) (*List, error) {
	set := s.settings()
	l, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	was, wasSerial := l.Hash(), l.Serial
	switch {
	case l.Frozen():
		return nil, invalid("the list is already fixed")
	case approved == "":
		return nil, invalid("freeze needs the hash of the list that was approved")
	case approved != was:
		return nil, ErrListChanged
	}
	e := s.evaluatorFor(set, nil)
	l.StartSubjects = map[string]Subject{}
	for i := range l.Items {
		saveCheck(&l.Items[i], set.ProjectChecks)
		it := l.Items[i]
		if !it.deterministic() || e.resolver == nil {
			continue
		}
		sub, err := e.subjectFor(ctx, l, it)
		if err != nil {
			continue
		}
		if _, ok := l.StartSubjects[""]; !ok {
			if work, err := e.resolver.Resolve(ctx, l.RequestID, ""); err == nil {
				l.StartSubjects[""] = work
			}
		}
		l.StartSubjects[startEffect(it)] = sub
		st := l.state(it.ID)
		e.freezeInputs(ctx, it, st, sub)
		st.Start = e.startValue(ctx, l, it, sub)
		// A check that exists now runs once now: passing already proves
		// nothing.
		if it.Check != nil && (it.Check.Command != "" || it.Check.Script != "") {
			st.Baseline = e.baseline(ctx, it, sub)
		}
	}
	l.FrozenAt = s.o.Now()
	s.event(l, ActionFreeze, "", words, by)
	l.Revisions = []Revision{snapshotRevision(l, 1, l.FrozenAt, by, words)}

	if err := s.lock(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	cur, ok := s.data.Lists[id]
	if !ok || cur.Serial != wasSerial {
		return nil, ErrListChanged
	}
	return s.commit(l, len(cur.History), false)
}

// Evaluate runs the deterministic checks and, when a reviewer is wired, the
// judged items. Only datawatch calls it: no route submits a result. A list
// that is done is closed with its versions and is not run again.
func (s *Service) Evaluate(ctx context.Context, id string) (*List, error) {
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	set := s.settings()
	l, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if !l.Frozen() || Read(l).Done {
		return l, nil
	}
	was, generation, serial := l.Hash(), l.Generation, l.Serial
	s.mu.Lock()
	verdicts, err := clone(s.data.Verdicts)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	e := s.evaluatorFor(set, verdicts)
	e.run(ctx, l)
	// The work may have moved while the checks ran.
	e.recheckVersions(ctx, l)

	if err := s.lock(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	// Verdicts are kept whatever happened to the list: a lost one would let
	// the same version be judged again.
	added := false
	for k, v := range verdicts {
		if _, ok := s.data.Verdicts[k]; !ok {
			s.data.Verdicts[k] = v
			added = true
		}
	}
	cur, ok := s.data.Lists[id]
	if !ok || cur.Hash() != was || cur.Generation != generation {
		if added {
			if err := saveData(s.o.Path, s.o.Key, s.data); err != nil {
				return nil, err
			}
		}
		return nil, ErrListChanged
	}
	merged, err := clone(cur)
	if err != nil {
		return nil, err
	}
	// Only what a run produces is taken over; answers and acceptances given
	// while it ran are kept.
	for _, it := range merged.Items {
		base, _ := it.methodBase()
		from := l.State[it.ID]
		if from == nil {
			continue
		}
		if base == MethodConfirm {
			// A run only says which version the work is at now.
			if from.CurrentSubject != "" {
				merged.state(it.ID).CurrentSubject = from.CurrentSubject
			}
			continue
		}
		to := merged.state(it.ID)
		to.Result, to.CurrentSubject, to.Runs, to.InfraTries = from.Result, from.CurrentSubject, from.Runs, from.InfraTries
		to.Baseline, to.WaitAnchor, to.Samples = from.Baseline, from.WaitAnchor, from.Samples
	}
	// An answer given while the run was working was not looked at by it, so
	// that run does not close the list.
	return s.commit(merged, len(merged.History), cur.Serial == serial)
}

// Confirm records a person's answer on a confirm item, or on "list" that the
// list covers the whole ask. An answer datawatch cannot show came from
// someone other than the doer is kept as a claim and does not count.
func (s *Service) Confirm(id, item string, yes bool, words, relayedFor, hash string, by Caller) (*List, error) {
	if strings.TrimSpace(words) == "" {
		return nil, invalid("a confirmation keeps the words that were said")
	}
	// An answer about the work itself belongs to the version it was given on.
	version := ""
	if s.o.Resolver != nil && item != coverageItem {
		if l, err := s.Get(id); err == nil {
			if _, it := l.item(item); it != nil && readsThePlace(it.Effect) {
				sub, err := s.evaluatorFor(s.settings(), nil).subjectFor(context.Background(), l, *it)
				if err != nil {
					return nil, invalid("the version being confirmed could not be read: %v", err)
				}
				version = sub.Commit
			}
		}
	}
	return s.mutate(id, func(l *List) error {
		if !l.Frozen() {
			return invalid("the list is not fixed yet")
		}
		if hash != "" && hash != l.Hash() {
			return ErrListChanged
		}
		c := &Confirmation{Yes: yes, Words: words, RelayedFor: relayedFor, By: by, At: s.o.Now(), Counts: l.independent(by)}
		if c.Counts {
			// A run has to read the versions again before the list can close.
			l.ClosedAt = time.Time{}
		}
		if item == coverageItem {
			if hash == "" {
				return invalid("confirming that the list covers the ask needs the list hash")
			}
			c.Subject = hash
			l.Coverage = c
			s.event(l, ActionConfirm, item, words, by)
			return nil
		}
		_, it := l.item(item)
		if it == nil {
			return invalid("no item %q", item)
		}
		if it.Method != MethodConfirm {
			return invalid("item %q is checked by %q; a person cannot set it met (it can be accepted without proof)", item, it.Method)
		}
		st := l.state(item)
		st.Confirm = c
		if c.Counts {
			value := ResultNotMet
			if yes {
				value = ResultMet
			}
			c.Subject, st.CurrentSubject = version, version
			st.Result = Result{Value: value, Reason: "answered by " + callerText(by), At: c.At, Subject: st.CurrentSubject,
				Evidence: []Evidence{{Origin: OriginPerson, What: "words of " + callerText(by), Output: words, At: c.At, Subject: st.CurrentSubject}}}
		}
		s.event(l, ActionConfirm, item, words, by)
		return nil
	})
}

// Accept records that an item is accepted without proof. It never reads met.
func (s *Service) Accept(id, item, reason, words, hash string, by Caller) (*List, error) {
	if strings.TrimSpace(reason) == "" || strings.TrimSpace(words) == "" {
		return nil, invalid("accepting without proof needs a reason and the words that were said")
	}
	return s.mutate(id, func(l *List) error {
		if !l.Frozen() {
			return invalid("the list is not fixed yet")
		}
		if hash != "" && hash != l.Hash() {
			return ErrListChanged
		}
		if _, it := l.item(item); it == nil {
			return invalid("no item %q", item)
		}
		counts := l.independent(by)
		if counts {
			l.ClosedAt = time.Time{}
		}
		l.state(item).Accept = &Acceptance{Reason: reason, Words: words, By: by, At: s.o.Now(), Counts: counts}
		s.event(l, ActionAccept, item, words, by)
		return nil
	})
}

// AddAskWords records later words linked to the ask.
func (s *Service) AddAskWords(id, words string, by Caller) (*List, error) {
	if strings.TrimSpace(words) == "" {
		return nil, invalid("no words to add")
	}
	return s.mutate(id, func(l *List) error { s.addAsk(l, strings.TrimSpace(words), by); return nil })
}

// NoteAnswer takes an answer to a question about the request. Words beyond the
// chosen option become an open item.
func (s *Service) NoteAnswer(id, option, answer string, by Caller) (*List, error) {
	extra := extraAnswerWords(option, answer)
	if extra == "" {
		return s.Get(id)
	}
	return s.AddAskWords(id, extra, by)
}

// NoteUnitReplaced records that the unit carrying the ask was reset, removed,
// cancelled or split. The list stays with the ask: open items pass to the
// named successor or to the operator, and earlier results no longer count.
func (s *Service) NoteUnitReplaced(id, how, successor string, by Caller) (*List, error) {
	return s.mutate(id, func(l *List) error {
		for _, st := range l.State {
			st.Result = Result{Value: ResultUnchecked, Reason: "the work was replaced (" + how + ")"}
			// Answers, acceptances and readings were about the replaced work.
			st.Confirm, st.Accept, st.Runs = nil, nil, nil
			st.Samples, st.WaitAnchor = nil, time.Time{}
		}
		l.Unit, l.Yours = successor, ""
		// The earlier holders no longer hold it and are still doers.
		l.FormerHolders = union(l.FormerHolders, l.Holders)
		l.Holders = nil
		l.ReplacedAt = s.o.Now()
		l.Generation++
		if successor == "" {
			l.Yours = "the unit was replaced (" + how + ") with no successor: its open items are yours"
		}
		s.event(l, ActionReplaced, "", strings.TrimSpace(how+" "+successor), by)
		return nil
	})
}

// add stores a list for a new request id. A request has one list: a second one
// could be weaker.
func (s *Service) add(l *List) (*List, error) {
	if _, ok := s.data.Lists[l.RequestID]; ok {
		return nil, invalid("request %q already has a list; change that one", l.RequestID)
	}
	return s.commit(l, 0, false)
}

// derive copies a list for another request id with every item and revision, so
// a copy can only be weakened by a change that shows against revision 1.
func (s *Service) derive(fromID, newID string, edit func(l *List) error) (*List, error) {
	if strings.TrimSpace(newID) == "" {
		return nil, invalid("a request id is needed")
	}
	if err := s.lock(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	from, ok := s.data.Lists[fromID]
	if !ok {
		return nil, ErrNotFound
	}
	l, err := clone(from)
	if err != nil {
		return nil, err
	}
	l.RequestID, l.Unit, l.Parent = newID, newID, fromID
	l.State = map[string]*ItemState{}
	for id, st := range from.State {
		l.State[id] = &ItemState{Result: Result{Value: ResultUnchecked}, InputHashes: st.InputHashes, InputsFrozen: st.InputsFrozen, Start: st.Start, Baseline: st.Baseline}
	}
	l.History, l.Serial, l.Generation = nil, 0, 0
	l.ClosedAt, l.ClaimedDoneAt, l.ReworkRounds, l.LastReworkSubject, l.Yours, l.Holders = time.Time{}, time.Time{}, 0, "", "", nil
	l.CreatedAt = s.o.Now()
	l.FormerHolders, l.ReplacedAt = nil, time.Time{}
	if err := edit(l); err != nil {
		return nil, err
	}
	return s.add(l)
}

// CloneFor gives a cloned unit the same list.
func (s *Service) CloneFor(fromID, newID string) (*List, error) {
	return s.derive(fromID, newID, func(l *List) error { l.Coverage = nil; return nil })
}

// OccurrenceID names the list of one scheduled run.
func OccurrenceID(parent, occurrence string) string { return parent + "@" + occurrence }

// NewOccurrence makes the list of one scheduled run at its due time, so a run
// that never fires still shows. It inherits the ask's items.
func (s *Service) NewOccurrence(parent, occurrence string, due time.Time) (*List, error) {
	if occurrence == "" || due.IsZero() {
		return nil, invalid("an occurrence needs an id and a due time")
	}
	return s.derive(parent, OccurrenceID(parent, occurrence), func(l *List) error {
		if !l.Frozen() {
			return invalid("the scheduled request's list is not fixed yet")
		}
		l.Occurrence, l.OccurrenceDue, l.NeededBy = occurrence, due, due
		for _, st := range l.State {
			st.Start, st.Baseline = nil, nil
		}
		return nil
	})
}

// NoteHolder records a session that does the work. Its own answers do not
// count.
func (s *Service) NoteHolder(id, sessionID string) error {
	_, err := s.mutate(id, func(l *List) error {
		if sessionID != "" && !l.isHolder(sessionID) {
			l.Holders = append(l.Holders, sessionID)
		}
		return nil
	})
	return err
}

// NoteStart records a request's first notice, spawn or typed input.
func (s *Service) NoteStart(requestID, source string) error {
	if err := s.lock(); err != nil {
		return err
	}
	defer s.mu.Unlock()
	for _, r := range s.data.Starts {
		if r.RequestID == requestID {
			return nil
		}
	}
	s.data.Starts = append(s.data.Starts, StartRecord{RequestID: requestID, Source: source, At: s.o.Now()})
	if err := saveData(s.o.Path, s.o.Key, s.data); err != nil {
		s.data.Starts = s.data.Starts[:len(s.data.Starts)-1]
		return err
	}
	return nil
}

// NoteClaimedDone records that something else reported the request done.
func (s *Service) NoteClaimedDone(id string) error {
	_, err := s.mutate(id, func(l *List) error { l.ClaimedDoneAt = s.o.Now(); return nil })
	return err
}

// RequestRework says whether another round may start. Rounds are counted on
// the list, across new holders and resets, and need a changed subject. Past
// the limit the list is the operator's.
func (s *Service) RequestRework(id, subject string, by Caller) (allowed bool, why string, err error) {
	limit := s.settings().ReworkRounds
	_, err = s.mutate(id, func(l *List) error {
		switch {
		case l.ReworkRounds >= limit:
			why = fmt.Sprintf("rework limit of %d rounds reached: yours", limit)
			l.Yours = why
		case subject == "" || subject == l.LastReworkSubject:
			why = "no changed subject since the last round"
		default:
			allowed = true
			l.ReworkRounds++
			l.LastReworkSubject = subject
			s.event(l, ActionRework, "", subject, by)
		}
		return nil
	})
	return allowed && err == nil, why, err
}

// Catalog describes the methods, default lists and project check names.
type Catalog struct {
	Methods       []string `json:"methods"`
	Defaults      Defaults `json:"defaults"`
	ProjectChecks []string `json:"project_checks"`
	Reviewer      bool     `json:"reviewer_available"`
}

// Catalog returns what a list can be built from. It names project checks and
// never shows their commands.
func (s *Service) Catalog() (Catalog, error) {
	if !s.On() {
		return Catalog{}, ErrOff
	}
	set := s.settings()
	d, err := LoadDefaults(set.DefaultsFile)
	if err != nil {
		return Catalog{}, err
	}
	names := []string{}
	for n := range set.ProjectChecks {
		names = append(names, n)
	}
	sort.Strings(names)
	return Catalog{
		Methods:  []string{MethodCommand, "project:<name>", "state:<what>", MethodReceipt, MethodConfirm, MethodReview},
		Defaults: d, ProjectChecks: names, Reviewer: s.o.Reviewer != nil,
	}, nil
}
