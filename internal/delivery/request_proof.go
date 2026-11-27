// "Done" for a request is its "done when" list reading done (internal/
// acceptance). There is no second proof engine here, and no request runs a
// command. Owner: part C.
//
// What the list needs from this side (audit/review-acceptance-a-code-report.md,
// "Job 4 must"): the observed caller on every change; the holder recorded
// before any receipt is read; SourceKnown for datawatch's own sources; the
// recipient named on work lists; the receipt's time with sub-second
// precision, with the list made before delivery; no occurrence except for a
// scheduled run; and something must call Evaluate.
//
// A list holds the request's words, so on a secure install its store is part of
// the request store: wired with the delivery store key, not the key tied to the
// config file's salt, and covered by the marker scan.

package delivery

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/acceptance"
)

type listsVerifier struct{ lists *acceptance.Service }

// NewListsVerifier is the Verifier over the "done when" lists. With lists off
// or missing it never closes anything.
func NewListsVerifier(lists *acceptance.Service) Verifier { return &listsVerifier{lists: lists} }

// datawatchActor is the caller of every change datawatch makes to a list. No
// surface records this name: a surface's caller is the daemon token, a session
// or a peer.
var datawatchActor = acceptance.Caller{Observed: "datawatch"}

// verdictListsOff and verdictNoList are the verdicts of a request that cannot
// close.
var (
	verdictListsOff = Verdict{Status: WhyListsOff, Reason: WhyListsOff}
	verdictNoList   = Verdict{Status: string(acceptance.StatusNoCriteria), Reason: WhyNoList}
)

var errNotOurList = fmt.Errorf("%w: a done-when list that datawatch did not make is already there under this request's ID", ErrRequestState)

// ownList reports a list datawatch itself made for one of its requests.
func ownList(l *acceptance.List) bool {
	return l.SourceKnown && len(l.History) > 0 && l.History[0].By.Observed == datawatchActor.Observed
}

// listErr maps a list store error to the request errors. A list error names
// IDs and rules, never the ask or an item's words.
func listErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, acceptance.ErrInvalid):
		return fmt.Errorf("%w: %v", ErrRequestInput, err)
	case errors.Is(err, acceptance.ErrListChanged):
		return err
	}
	return fmt.Errorf("%w: the done-when lists could not be saved or read: %v", ErrCannotRecord, err)
}

// listItems turns a creator's items into list items. A request list runs no
// command, so no method that could is accepted. A file item must show a change
// from the value saved before delivery: a file that was already right proves no
// work (design v2, break 6).
func listItems(items []DoneItem) ([]acceptance.Item, error) {
	out := make([]acceptance.Item, 0, len(items))
	for _, in := range items {
		it := acceptance.Item{ID: in.ID, Statement: in.Statement, From: in.From, Method: in.Method,
			Effect: in.Effect, Change: in.Change, Before: in.Before}
		switch in.Method {
		case acceptance.MethodReceipt:
			if in.Target != "" {
				it.State = &acceptance.StateSpec{Target: in.Target}
			}
		case "state:file":
			it.State = &acceptance.StateSpec{Path: in.Path, Contains: in.Contains}
			it.Change = true
			if it.Before == acceptance.BeforeNone {
				it.Before = acceptance.BeforeDiffer
			}
		case acceptance.MethodConfirm, acceptance.MethodReview:
		default:
			return nil, ErrCheckMethod
		}
		out = append(out, it)
	}
	return out, nil
}

// projectAt carries a request's project folder to the resolver while its list
// is made: the request is not saved yet, so the store cannot be asked.
type projectAt struct{ requestID, dir string }

type projectKey struct{}

// Prepare makes and fixes the list before the first delivery.
func (v *listsVerifier) Prepare(ctx context.Context, r Request, content RequestContent) error {
	rule, ok := RuleFor(r.Kind)
	if !ok {
		return ErrRequestInput
	}
	items, err := listItems(content.Items)
	if err != nil {
		return err
	}
	if !v.lists.On() {
		return nil // saved, and listed as one that cannot close
	}
	ctx = context.WithValue(ctx, projectKey{}, projectAt{r.ID, content.ProjectDir})
	l, err := v.lists.Get(r.ID)
	switch {
	case err == nil && !ownList(l):
		return errNotOurList
	case err == nil && l.Frozen():
		return nil
	case errors.Is(err, acceptance.ErrNotFound):
		in := acceptance.EditInput{RequestID: r.ID, Source: rule.ListSource, SourceKnown: true, Upsert: items}
		if !rule.DeliveryOnly {
			// Detail is part of what is asked, so a sentence in it that no
			// item covers stays open too.
			in.Ask = strings.TrimSpace(content.Ask + "\n" + content.Detail)
			if in.Ask == "" {
				in.Ask = "datawatch request " + r.ID
			}
		}
		if l, err = v.lists.Edit(ctx, in, datawatchActor); err != nil {
			return listErr(err)
		}
	case err != nil:
		return listErr(err)
	}
	_, err = v.lists.Freeze(ctx, r.ID, l.Hash(), datawatchActor, "")
	return listErr(err)
}

// own returns datawatch's own list of a request, or nil when it has none.
func (v *listsVerifier) own(id string) (*acceptance.List, error) {
	l, err := v.lists.Get(id)
	switch {
	case errors.Is(err, acceptance.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, listErr(err)
	case !ownList(l):
		return nil, nil
	}
	return l, nil
}

// Holder records who does the work, before any receipt is read.
func (v *listsVerifier) Holder(_ context.Context, requestID, sessionID string) error {
	if !v.lists.On() || sessionID == "" {
		return nil
	}
	l, err := v.own(requestID)
	if err != nil || l == nil {
		return err
	}
	return v.noteHolder(l, sessionID)
}

func (v *listsVerifier) noteHolder(l *acceptance.List, sessionID string) error {
	if len(l.Holders) == 1 && l.Holders[0] == sessionID {
		return nil
	}
	if len(l.Holders) > 0 {
		// The earlier holder stays a doer; its receipt and answers stop
		// counting. The unit that carries the ask is still this request.
		if _, err := v.lists.NoteUnitReplaced(l.RequestID, "new_holder", l.RequestID, datawatchActor); err != nil {
			return listErr(err)
		}
	}
	return listErr(v.lists.NoteHolder(l.RequestID, sessionID))
}

// syncHolders makes the list know every doer the request knows, also those it
// missed while the lists were off.
func (v *listsVerifier) syncHolders(l *acceptance.List, r Request) error {
	doers := append([]string(nil), r.FormerHolders...)
	if r.Holder != nil {
		doers = append(doers, r.Holder.SessionID)
	}
	for _, id := range doers {
		current := r.Holder != nil && r.Holder.SessionID == id
		if slices.Contains(l.Holders, id) || !current && slices.Contains(l.FormerHolders, id) {
			continue
		}
		if err := v.noteHolder(l, id); err != nil {
			return err
		}
		var err error
		if l, err = v.lists.Get(r.ID); err != nil {
			return listErr(err)
		}
	}
	return nil
}

// Started records the first notice, take or typed input.
func (v *listsVerifier) Started(_ context.Context, requestID string, kind RequestKind) error {
	if !v.lists.On() {
		return nil
	}
	rule, _ := RuleFor(kind)
	return listErr(v.lists.NoteStart(requestID, rule.ListSource))
}

// read runs a request's list. A nil list: the request has none of its own.
func (v *listsVerifier) read(ctx context.Context, r Request) (*acceptance.List, error) {
	l, err := v.own(r.ID)
	if err != nil || l == nil {
		return nil, err
	}
	if err = v.syncHolders(l, r); err != nil {
		return nil, err
	}
	l, err = v.lists.Evaluate(ctx, r.ID)
	if err != nil {
		return nil, listErr(err)
	}
	return l, nil
}

// listVerdict reads a list for a request of this kind.
func listVerdict(kind RequestKind, l *acceptance.List) Verdict {
	rule, _ := RuleFor(kind)
	read := acceptance.Read(l)
	out := Verdict{
		Status: string(read.Status),
		Closes: read.Done || rule.DeliveryOnly && read.Status == acceptance.StatusDelivered,
		NotMet: read.Status == acceptance.StatusNotMet,
	}
	switch read.Status {
	case acceptance.StatusNotMet:
		out.Reason = WhyProofFailed
	case acceptance.StatusNotReviewed, acceptance.StatusThinList, acceptance.StatusChangedList,
		acceptance.StatusAskChanged, acceptance.StatusReportOnly:
		out.Reason = WhyWaitingOnYou
	}
	return out
}

// Check runs the list and reads it.
func (v *listsVerifier) Check(ctx context.Context, r Request) (Verdict, error) {
	if !v.lists.On() {
		return verdictListsOff, nil
	}
	l, err := v.read(ctx, r)
	switch {
	case errors.Is(err, acceptance.ErrOff):
		return verdictListsOff, nil
	case err != nil:
		return Verdict{}, err
	case l == nil:
		return verdictNoList, nil
	}
	return listVerdict(r.Kind, l), nil
}

// Rework asks the list whether failed work may go back once more: it counts
// the rounds, up to its own limit, and each needs a changed version of what
// failed. An item with no version to name (a person's "no" that is not about
// the work's files) has nothing that can change, so it does not go back.
func (v *listsVerifier) Rework(_ context.Context, r Request) (bool, error) {
	if !v.lists.On() {
		return false, nil
	}
	l, err := v.own(r.ID)
	if err != nil || l == nil {
		return false, err
	}
	var versions []string
	for _, item := range acceptance.Read(l).Items {
		if item.Result.Value == acceptance.ResultNotMet && item.Result.Subject != "" &&
			!slices.Contains(versions, item.Result.Subject) {
			versions = append(versions, item.Result.Subject)
		}
	}
	sort.Strings(versions)
	allowed, _, err := v.lists.RequestRework(r.ID, strings.Join(versions, ","), datawatchActor)
	return allowed, listErr(err)
}

// listCaller is a request caller as the list records it. The claimed session
// may only count against a caller: it is used when it names a doer, and never
// to make a caller somebody else.
func listCaller(c Caller, r Request) acceptance.Caller {
	out := acceptance.Caller{Claimed: c.ClaimedName, Observed: c.Observed()}
	switch c.Kind {
	case CallerSession:
		out.SessionID, out.ViaTool = c.SessionID, c.Surface == SurfaceMCP
	case CallerOperator:
		out.ViaTool = c.Surface == SurfaceMCP
		doer := c.ClaimedSession != "" && (slices.Contains(r.FormerHolders, c.ClaimedSession) ||
			r.Holder != nil && r.Holder.SessionID == c.ClaimedSession)
		if doer {
			out.SessionID = c.ClaimedSession
			out.Observed += " from session:" + c.ClaimedSession
		}
	default:
		out.ViaTool = true // never counts
	}
	return out
}

// Waive records "accepted without proof" on the open items as this caller,
// then reads the list. A doer's waiver is a claim and closes nothing.
func (v *listsVerifier) Waive(ctx context.Context, c Caller, r Request, in ReasonInput) (Verdict, error) {
	if !v.lists.On() {
		return Verdict{}, fmt.Errorf("%w: the done-when lists are off, so nothing can be accepted", ErrRequestState)
	}
	words := strings.TrimSpace(in.Words)
	if words == "" {
		return Verdict{}, fmt.Errorf("%w: accepting without proof keeps the words that were said", ErrRequestInput)
	}
	// Run first, so that what is met is not accepted without proof.
	l, err := v.read(ctx, r)
	if err != nil {
		return Verdict{}, err
	}
	if l == nil {
		return Verdict{}, fmt.Errorf("%w: this request has no done-when list", ErrRequestState)
	}
	by := listCaller(c, r)
	for _, item := range acceptance.Read(l).Items {
		met := item.Result.Value == acceptance.ResultMet && !item.Result.AlreadyTrue
		// An acceptance that counts is left alone: a later claim must not
		// replace it.
		if met || item.Accepted {
			continue
		}
		if _, err := v.lists.Accept(r.ID, item.ID, in.ReasonCode, words, "", by); err != nil {
			return Verdict{}, listErr(err)
		}
	}
	if l, err = v.lists.Evaluate(ctx, r.ID); err != nil {
		return Verdict{}, listErr(err)
	}
	return listVerdict(r.Kind, l), nil
}

type receiptProbe struct{ book func() RequestBook }

// NewReceiptProbe answers the list's receipt items from the delivery records.
// It answers "fetched" only for a delivery that counts under the holder's
// worker policy. book may return nil until the store is open.
func NewReceiptProbe(book func() RequestBook) acceptance.Probe { return &receiptProbe{book: book} }

// observedCall reports that datawatch recorded this call from the session's
// own credential and did not refuse it.
func observedCall(r Request, verb, sessionID string) bool {
	for _, c := range r.Calls {
		if c.Verb == verb && c.Refused == "" && c.Observed == "session:"+sessionID {
			return true
		}
	}
	return false
}

// Read answers from the request's delivery record. Anything the list takes as
// a receipt ("fetched" or "confirmed") is said only for a delivery that
// counts; a lesser level is named so that it reads "cannot tell".
func (p *receiptProbe) Read(_ context.Context, ask acceptance.ProbeRequest) (acceptance.Reading, error) {
	var none acceptance.Reading // not answered: "no receipt yet"
	book := p.book()
	if book == nil {
		return none, nil
	}
	r, ok := book.Request(ask.RequestID)
	if !ok || r.Holder == nil {
		return none, nil
	}
	policy := PolicyFor(r.Holder.Family)
	if !policy.Delivered(r.Delivery) {
		level := policy.Level(r.Delivery)
		if level == LevelNone {
			return none, nil
		}
		return acceptance.Reading{Answered: true, Value: "unconfirmed_" + string(level)}, nil
	}
	value, verb, at := "fetched", VerbTake, r.Delivery.FetchedAt
	if policy.Needs == LevelInModelCall {
		value, verb, at = "confirmed", VerbSeen, r.Delivery.InModelCallAt
	}
	out := acceptance.Reading{Answered: true, Value: value, SourceAt: *at}
	if observedCall(r, verb, r.Holder.SessionID) {
		out.By = r.Holder.SessionID
	}
	return out, nil
}

type requestResolver struct{ book func() RequestBook }

// NewRequestResolver says which version of the work a request's file items are
// read at: the commit its project folder is on, or the commit of the branch a
// "ref:<name>" effect names. For any other list it errors, so that item reads
// "cannot tell".
func NewRequestResolver(book func() RequestBook) acceptance.SubjectResolver {
	return &requestResolver{book: book}
}

func (r *requestResolver) projectDir(ctx context.Context, requestID string) (string, error) {
	if p, ok := ctx.Value(projectKey{}).(projectAt); ok && p.requestID == requestID {
		return p.dir, nil
	}
	book := r.book()
	if book == nil {
		return "", errors.New("the request store is not open")
	}
	if _, ok := book.Request(requestID); !ok {
		return "", errors.New("not the list of a request")
	}
	c, err := book.Content(requestID)
	return c.ProjectDir, err
}

func (r *requestResolver) Resolve(ctx context.Context, requestID, effect string) (acceptance.Subject, error) {
	var none acceptance.Subject
	dir, err := r.projectDir(ctx, requestID)
	if err != nil {
		return none, err
	}
	if !filepath.IsAbs(dir) {
		return none, errors.New("the request names no project folder")
	}
	ref := "HEAD"
	if effect != "" && effect != "work" {
		name, ok := strings.CutPrefix(effect, "ref:")
		if !ok || name == "" {
			return none, errors.New("not a version of the project's files")
		}
		ref = name
	}
	sub, err := acceptance.Git{}.Version(ctx, dir, ref)
	if err != nil {
		// git's own words would carry the folder path into the list.
		return none, errors.New("the project folder's version could not be read")
	}
	return sub, nil
}

// RefusingRunner runs no check. Until check execution is isolated (job 5),
// datawatch is wired with it, and command items read "cannot tell".
type RefusingRunner struct{}

// Run implements acceptance.Runner.
func (RefusingRunner) Run(context.Context, acceptance.CheckSpec, acceptance.Subject, time.Duration) acceptance.RunOutcome {
	return acceptance.RunOutcome{ExitCode: -1, Reason: "datawatch runs no command checks yet: running them is not isolated"}
}
