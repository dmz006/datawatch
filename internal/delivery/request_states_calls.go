// The calls of the request service: create, the holder's calls, the operator's
// actions and the roles. Owner: part A.

package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Codes of a refused call.
const (
	refusedNotHolder   = "not_holder"
	refusedState       = "state"
	refusedWrongDigest = "wrong_digest"
	refusedWrongCode   = "wrong_code"
	refusedInput       = "input"
)

// Verbs of calls that have no route of their own.
const (
	verbCreate  = "create"
	maxClaimed  = 20
	maxClaimLen = 128
)

var versionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:+-]{0,127}$`)

// codeOr returns s when it is a code, else "". Records hold codes only.
func codeOr(s string) string {
	if reasonCodeRe.MatchString(s) {
		return s
	}
	return ""
}

func versionOr(s string) string {
	if versionRe.MatchString(s) {
		return s
	}
	return ""
}

func inputErr(format string, a ...any) error {
	return errors.Join(ErrRequestInput, fmt.Errorf(format, a...))
}

func needReason(code string) error {
	if code == "" {
		return ErrReasonNeeded
	}
	return CheckReasonCode(code)
}

// refusePeer: tracked requests to or from another computer are not supported.
func refusePeer(c Caller) error {
	if c.Kind == CallerPeer {
		return ErrCrossHost
	}
	return nil
}

// nonOperator reports a caller that is neither the operator's token nor
// datawatch itself.
func nonOperator(c Caller) bool { return c.Kind != CallerOperator && c.Kind != CallerDatawatch }

func (s *Requests) settings() RequestSettings {
	var set RequestSettings
	if s.opts.Settings != nil {
		set = s.opts.Settings()
	}
	// No request is open-ended.
	if set.TakeBy <= 0 {
		set.TakeBy = time.Hour
	}
	if set.Due <= 0 {
		set.Due = 24 * time.Hour
	}
	return set
}

func seconds(d time.Duration) int64 { return int64((d + time.Second - 1) / time.Second) }

func holds(r *Request, id string) bool { return r.Holder != nil && r.Holder.SessionID == id }

// work is one change over the records. It is used with s.mu held.
type work struct {
	s   *Requests
	t   *txn
	now time.Time
}

// update makes one change under s.mu and commits it. An error from fn changes
// nothing.
func (s *Requests) update(fn func(w *work) error) error {
	s.mu.Lock()
	w := &work{s: s, t: newTxn(), now: s.opts.Now()}
	err := fn(w)
	if err == nil && !w.t.empty() {
		err = s.commitLocked(w.t)
	}
	s.mu.Unlock()
	s.alarm()
	return err
}

// peek reads a request as the change has it. The result must not be changed.
func (w *work) peek(id string) *storedRequest {
	if st, ok := w.t.reqs[id]; ok {
		return st
	}
	return w.s.reqs[id]
}

// edit returns the copy of a request that the change will save.
func (w *work) edit(id string) *storedRequest {
	if st, ok := w.t.reqs[id]; ok {
		return st
	}
	st, ok := w.s.reqs[id]
	if !ok {
		return nil
	}
	c := st.clone()
	w.t.reqs[id] = c
	return c
}

// ids are all requests, oldest first.
func (w *work) ids() []string {
	seen := map[string]bool{}
	var out []string
	for id := range w.s.reqs {
		seen[id] = true
		out = append(out, id)
	}
	for id := range w.t.reqs {
		if !seen[id] {
			out = append(out, id)
		}
	}
	live := out[:0]
	for _, id := range out {
		if w.peek(id) != nil {
			live = append(live, id)
		}
	}
	sort.Slice(live, func(i, j int) bool {
		a, b := &w.peek(live[i]).index, &w.peek(live[j]).index
		if !a.Created.Equal(b.Created) {
			return a.Created.Before(b.Created)
		}
		return a.ID < b.ID
	})
	return live
}

func (w *work) setState(r *Request, state RequestState, why string) {
	r.State, r.ReasonCode, r.StateAt = state, why, w.now
}

// close ends a request. What waits for it ("after") fails with it.
func (w *work) close(r *Request, state RequestState, why, by string) {
	w.setState(r, state, why)
	at := w.now
	r.ClosedAt, r.ClosedBy, r.HeldSince, r.HeldCause = &at, by, nil, ""
	if state == StateDone {
		return
	}
	for _, id := range w.ids() {
		if c := w.peek(id); c.index.After == r.ID && !c.index.State.Closed() {
			w.close(&w.edit(id).index, StateFailed, WhyParentClosed, "datawatch")
		}
	}
}

// dropHolder takes a request off its holder. The next one starts a delivery
// record of its own.
func (w *work) dropHolder(r *Request) {
	if r.Holder != nil {
		r.FormerHolders = append(r.FormerHolders, r.Holder.SessionID)
	}
	r.Holder, r.Delivery = nil, DeliveryRecord{}
}

// loseHolder applies what happens to one request when its holder ends, or
// loses the role while it lives.
func (w *work) loseHolder(id string, ended bool) {
	cur := w.peek(id)
	if cur == nil || cur.index.State.Closed() || cur.index.State == StateReported {
		return // a report stays a report, whoever made it
	}
	rule, _ := RuleFor(cur.index.Kind)
	handed := cur.index.Delivery.MaybeHandedOver()
	if cur.index.Role == RoleSession {
		if ended {
			w.close(&w.edit(id).index, StateFailed, WhyHolderGone, "datawatch")
		}
		return
	}
	state := cur.index.State
	again := (state == StateWaiting && !handed) || (state != StateAccepted && rule.ReadOnly)
	switch {
	case again:
		r := &w.edit(id).index
		w.dropHolder(r)
		w.setState(r, StateWaiting, WhyNoHolder)
	case ended:
		// Handed over and not safe to send again: it goes to the operator.
		w.close(&w.edit(id).index, StateFailed, WhyHolderGone, "datawatch")
	}
	// A live holder keeps what it may already be working on.
}

// ---- assignment -----------------------------------------------------------

// parentOpen reports that a request still waits for the one it comes after.
func (s *Requests) parentOpen(r *Request) bool {
	if r.After == "" {
		return false
	}
	p, ok := s.Request(r.After)
	return ok && p.State != StateDone
}

// holderFor says who a waiting request goes to now, or why nobody.
func (s *Requests) holderFor(r *Request) (*Holder, string) {
	var id string
	if r.Role == RoleSession {
		id = r.Scope.ID
	} else {
		if s.opts.Roles == nil {
			return nil, WhyNoHolder
		}
		b, ok := s.opts.Roles.Resolve(s, r.Role, r.Scope)
		if !ok {
			return nil, WhyNoHolder
		}
		if rule, _ := RuleFor(r.Kind); rule.MustDeliver && b.CanTakeAt == nil {
			return nil, WhyTakeNotProven
		}
		id = b.SessionID
	}
	info, ok := s.session(id)
	if !ok || !info.Live {
		return nil, WhyNoHolder
	}
	return &Holder{SessionID: id, Family: info.Family, Since: s.opts.Now()}, ""
}

func (s *Requests) session(id string) (SessionInfo, bool) {
	if s.opts.Sessions == nil {
		return SessionInfo{}, false
	}
	return s.opts.Sessions.Info(id)
}

// assign gives every waiting request without a holder to whoever has its role
// now. Callers hold s.opMu.
func (s *Requests) assign(ctx context.Context) error {
	holders, why := map[string]*Holder{}, map[string]string{}
	for _, r := range s.Requests(RequestFilter{State: StateWaiting}) {
		r := r
		if r.Holder != nil || s.parentOpen(&r) {
			continue
		}
		h, reason := s.holderFor(&r)
		if h == nil {
			if r.ReasonCode != reason && r.ReasonCode != WhyForced {
				why[r.ID] = reason
			}
			continue
		}
		// The list learns who does the work before any receipt is read.
		if s.opts.Verifier != nil {
			if err := s.opts.Verifier.Holder(ctx, r.ID, h.SessionID); err != nil {
				continue // tried again at the next check
			}
		}
		holders[r.ID] = h
	}
	if len(holders)+len(why) == 0 {
		return nil
	}
	return s.update(func(w *work) error {
		for id, h := range holders {
			if st := w.edit(id); st != nil && st.index.State == StateWaiting && st.index.Holder == nil {
				st.index.Holder, st.index.ReasonCode = h, ""
			}
		}
		for id, reason := range why {
			if st := w.edit(id); st != nil && st.index.State == StateWaiting && st.index.Holder == nil {
				st.index.ReasonCode = reason
			}
		}
		return nil
	})
}

// ---- create ---------------------------------------------------------------

var scopeKinds = map[ScopeKind]bool{ScopeTask: true, ScopeAutomaton: true, ScopeAutomation: true,
	ScopeProject: true, ScopeHost: true, ScopeSession: true}

var roleNames = map[Role]bool{RoleSupervisor: true, RoleSession: true, RoleDoer: true, RoleWatcher: true,
	RoleWorker: true, RoleVerifier: true}

var itemMethods = map[string]bool{"receipt": true, "state:file": true, "confirm": true, "review": true}

func (s *Requests) checkCreate(in *CreateInput) (KindRule, error) {
	rule, ok := RuleFor(in.Kind)
	if !ok {
		return rule, inputErr("unknown request kind")
	}
	if in.Host != "" && in.Host != s.opts.Host {
		return rule, ErrCrossHost
	}
	if !roleNames[in.Role] || !scopeKinds[in.Scope.Kind] {
		return rule, inputErr("unknown role or scope")
	}
	if (in.Role == RoleSession) != (in.Scope.Kind == ScopeSession) {
		return rule, inputErr("the session role and the session scope go together")
	}
	if err := CheckScopeID(in.Scope.ID); err != nil {
		return rule, err
	}
	// The source is readable on disk: identifiers only.
	for _, id := range []string{in.Source.Type, in.Source.ID} {
		if id != "" && !scopeIDRe.MatchString(id) {
			return rule, inputErr("a source type and ID are identifiers")
		}
	}
	if in.TakeBy < 0 || in.Due < 0 {
		return rule, inputErr("take-by and due cannot be negative")
	}
	for _, it := range in.Content.Items {
		if !itemMethods[it.Method] {
			return rule, ErrCheckMethod
		}
	}
	return rule, nil
}

// build makes the record of a new request. The parts of the content that only
// datawatch writes are replaced.
func (s *Requests) build(c Caller, in CreateInput, rule KindRule) (*storedRequest, error) {
	set := s.settings()
	takeBy, due := in.TakeBy, in.Due
	if takeBy == 0 {
		takeBy = set.TakeBy
	}
	if due == 0 {
		due = set.DueByKind[in.Kind]
	}
	if due <= 0 {
		due = rule.Due
	}
	if due <= 0 {
		due = set.Due
	}
	id, err := newRandom(10)
	if err != nil {
		return nil, err
	}
	code, err := newRandom(5)
	if err != nil {
		return nil, err
	}
	content := *cloneJSON(&in.Content)
	content.Key, content.ReceiptCode = in.Key, "rc-"+code
	content.ReportText, content.ResultID, content.DeclineText, content.Words, content.ClaimedNames = "", "", "", "", nil
	now := s.opts.Now()
	st := &storedRequest{content: &content, index: Request{
		ID: RequestIDPrefix + id, Kind: in.Kind, Class: rule.Class, Role: in.Role, Scope: in.Scope,
		State: StateWaiting, KeyHash: KeyHash(in.Key), Source: in.Source, After: in.After,
		Creator: c.Observed(), Created: now, StateAt: now,
		TakeBySeconds: seconds(takeBy), DueSeconds: seconds(due),
	}}
	addCall(st, c, verbCreate, "", now)
	return st, nil
}

// insert saves a new request, after its "done when" list is made, together
// with whatever also goes into that write. Callers hold s.opMu.
func (s *Requests) insert(ctx context.Context, st *storedRequest, also func(w *work)) error {
	s.mu.Lock()
	faulty := len(s.faults) > 0
	s.mu.Unlock()
	if faulty {
		return ErrRecords
	}
	if s.opts.Verifier != nil {
		if err := s.opts.Verifier.Prepare(ctx, *cloneJSON(&st.index), *cloneJSON(st.content)); err != nil {
			return err
		}
	}
	return s.update(func(w *work) error {
		if st.index.After != "" {
			p := w.peek(st.index.After)
			if p == nil {
				return inputErr("the request it comes after does not exist")
			}
			if p.index.State.Closed() && p.index.State != StateDone {
				w.t.reqs[st.index.ID] = st
				w.close(&st.index, StateFailed, WhyParentClosed, "datawatch")
			}
		}
		w.t.reqs[st.index.ID] = st
		if also != nil {
			also(w)
		}
		return nil
	})
}

// byKey returns the request of an occurrence key. While a file is red an
// unknown key reads "cannot be read", never "no request".
func (s *Requests) byKey(key string) (Request, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.keys[KeyHash(key)]; ok {
		return *cloneJSON(&s.reqs[id].index), true, nil
	}
	if len(s.faults) > 0 {
		return Request{}, false, ErrRecords
	}
	return Request{}, false, nil
}

// createLocked is Create with s.opMu held.
func (s *Requests) createLocked(ctx context.Context, c Caller, in CreateInput) (RequestView, bool, error) {
	if err := refusePeer(c); err != nil {
		return RequestView{}, false, err
	}
	rule, err := s.checkCreate(&in)
	if err != nil {
		return RequestView{}, false, err
	}
	if in.Key == "" {
		return RequestView{}, false, inputErr("a request needs an occurrence key")
	}
	old, found, err := s.byKey(in.Key)
	if err != nil {
		return RequestView{}, false, err
	}
	if found {
		return view(old), false, nil
	}
	st, err := s.build(c, in, rule)
	if err != nil {
		return RequestView{}, false, err
	}
	if nonOperator(c) {
		st.index.NonOperatorActions++
	}
	id := st.index.ID
	if err := s.insert(ctx, st, nil); err != nil {
		return RequestView{}, false, err
	}
	// The request is saved. A failure from here is tried again by the check.
	_ = s.assign(ctx)
	return s.viewOf(id), true, nil
}

func (s *Requests) viewOf(id string) RequestView {
	r, _ := s.Request(id)
	return view(r)
}

// Create implements RequestService.
func (s *Requests) Create(ctx context.Context, c Caller, in CreateInput) (RequestView, bool, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.createLocked(ctx, c, in)
}

// FromBacklog implements RequestService: an entry becomes a request once.
func (s *Requests) FromBacklog(ctx context.Context, c Caller, in BacklogInput) (RequestView, bool, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := refusePeer(c); err != nil {
		return RequestView{}, false, err
	}
	if in.EntryKey == "" {
		return RequestView{}, false, inputErr("name the backlog entry")
	}
	episode := 1
	source := in.EntryKey
	if !scopeIDRe.MatchString(source) {
		source = "k-" + KeyHash(in.EntryKey)[:32]
	}
	if s.opts.Backlog != nil {
		n, ok := s.opts.Backlog(in.EntryKey)
		if !ok {
			// An entry already turned into a request may have left the list.
			for _, r := range s.Requests(RequestFilter{}) {
				if r.Source == (SourceRef{Type: "backlog", ID: source}) {
					return view(r), false, nil
				}
			}
			return RequestView{}, false, inputErr("no such backlog entry")
		}
		episode = n
	}
	return s.createLocked(ctx, c, CreateInput{Kind: ReqAsk, Role: in.Role, Scope: in.Scope,
		Key:    "backlog:" + in.EntryKey + ":" + strconv.Itoa(episode),
		Source: SourceRef{Type: "backlog", ID: source}, Content: in.Content})
}

// ---- the calls that are recorded --------------------------------------------

// addCall records a call under the observed caller. The first and the newest
// are kept. A claimed name goes into the sealed content.
func addCall(st *storedRequest, c Caller, verb, refused string, now time.Time) {
	r := &st.index
	r.Calls = append(r.Calls, CallRecord{At: now, Verb: verb, Observed: c.Observed(), Surface: c.Surface, Refused: refused})
	if n := len(r.Calls); n > MaxCallRecords {
		r.Calls = append(r.Calls[:1], r.Calls[n-MaxCallRecords+1:]...)
	}
	name := c.ClaimedName
	if name == "" || st.content == nil || len(st.content.ClaimedNames) >= maxClaimed {
		return
	}
	if len(name) > maxClaimLen {
		name = name[:maxClaimLen]
	}
	for _, have := range st.content.ClaimedNames {
		if have == name {
			return
		}
	}
	st.content.ClaimedNames = append(st.content.ClaimedNames, name)
}

// refusal is a call that changes nothing but the call history.
type refusal struct {
	code string
	err  error
}

func badState() *refusal { return &refusal{refusedState, ErrRequestState} }

// recorded runs one call on one request. fn changes the copy it is given, or
// refuses; a refused call is saved under the real caller and changes nothing
// else. operator marks an operator action, counted when somebody else makes it.
func (s *Requests) recorded(c Caller, id, verb string, operator bool, fn func(w *work, st *storedRequest) *refusal) (Request, error) {
	var out Request
	var refused error
	err := s.update(func(w *work) error {
		cur, err := s.lookupLocked(id)
		if err != nil {
			return err
		}
		st := cur.clone()
		w.t.reqs[id] = st
		var no *refusal
		if !operator && (st.index.Holder == nil || !c.Holds(st.index.Holder.SessionID)) {
			no = &refusal{refusedNotHolder, ErrNotHolder}
		} else {
			no = fn(w, st)
		}
		code := ""
		if no != nil {
			refused, code = no.err, no.code
			for k := range w.t.reqs {
				delete(w.t.reqs, k)
			}
			st = cur.clone()
			w.t.reqs[id] = st
		}
		if operator && nonOperator(c) {
			st.index.NonOperatorActions++
		}
		addCall(st, c, verb, code, w.now)
		out = *cloneJSON(&st.index)
		return nil
	})
	if err != nil {
		return Request{}, err
	}
	// The commit set the record's write number on the saved copy.
	if saved, ok := s.Request(id); ok {
		out = saved
	}
	return out, refused
}

// advance moves a waiting request on once its delivery counts.
func (w *work) advance(r *Request) {
	if r.State != StateWaiting || r.Holder == nil || !PolicyFor(r.Holder.Family).Delivered(r.Delivery) {
		return
	}
	w.setState(r, StateDelivered, "")
	if r.Delivery.AcceptedAt != nil {
		w.setState(r, StateAccepted, "")
	}
}

// delivered tells the role keeper that a delivery counts (a hello proves the
// take), and gives waiting work to whoever can now take it. Callers hold
// s.opMu.
func (s *Requests) delivered(ctx context.Context, r Request) {
	if s.opts.Roles == nil {
		return
	}
	// A failure here is tried again by the check.
	if err := s.opts.Roles.Delivered(ctx, s, r); err == nil {
		_ = s.assign(ctx)
	}
}

func countsNow(before, after RequestState) bool {
	return before == StateWaiting && (after == StateDelivered || after == StateAccepted)
}

// composeContent is the exact text a take hands out. It depends only on what
// was saved at create, so the same take again gives the same bytes.
func composeContent(r *Request, c *RequestContent, withCode bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "datawatch request %s (%s)\n", r.ID, r.Kind)
	if c.Ask != "" {
		fmt.Fprintf(&b, "\n%s\n", c.Ask)
	}
	if c.Detail != "" {
		fmt.Fprintf(&b, "\n%s\n", c.Detail)
	}
	if len(c.Params) > 0 {
		keys := make([]string, 0, len(c.Params))
		for k := range c.Params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("\n")
		for _, k := range keys {
			fmt.Fprintf(&b, "%s: %s\n", k, c.Params[k])
		}
	}
	if len(c.Items) > 0 {
		b.WriteString("\nDone when:\n")
		for _, it := range c.Items {
			fmt.Fprintf(&b, "- %s (%s", it.Statement, it.Method)
			if it.Path != "" {
				fmt.Fprintf(&b, " %s", it.Path)
			}
			b.WriteString(")\n")
		}
	}
	if withCode {
		fmt.Fprintf(&b, "\nReceipt code: %s\n", c.ReceiptCode)
	}
	return b.String()
}

func digestOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Take implements RequestService.
func (s *Requests) Take(ctx context.Context, c Caller, id string, in TakeInput) (TakeReply, error) {
	if err := refusePeer(c); err != nil {
		return TakeReply{}, err
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	var reply TakeReply
	// Asked before the store is locked: it may read the store.
	rules := ""
	if cur, ok := s.Request(id); ok && cur.RulesVersionGiven == "" && s.opts.RulesVersion != nil {
		rules = versionOr(s.opts.RulesVersion(cur.Scope))
	}
	var before RequestState
	first := false
	r, err := s.recorded(c, id, VerbTake, false, func(w *work, st *storedRequest) *refusal {
		r := &st.index
		if in.Via != "" && in.Via != ViaTool && in.Via != ViaProgram {
			return &refusal{refusedInput, inputErr("via is tool or program")}
		}
		if r.State.Closed() || st.content == nil {
			return badState()
		}
		before = r.State
		text := composeContent(r, st.content, PolicyFor(r.Holder.Family).ReceiptCode)
		digest := digestOf(text)
		reply.Repeat = r.Delivery.FetchedAt != nil
		if !reply.Repeat {
			at := w.now
			r.Delivery.FetchedAt, r.Delivery.FetchedVia, r.Delivery.Digest = &at, in.Via, digest
			first = true
			if r.RulesVersionGiven == "" {
				r.RulesVersionGiven = rules
			}
		}
		reply.Content, reply.Digest, reply.RulesVersion = text, digest, r.RulesVersionGiven
		w.advance(r)
		return nil
	})
	if err != nil {
		return TakeReply{}, err
	}
	if first && s.opts.Verifier != nil {
		_ = s.opts.Verifier.Started(ctx, id, r.Kind)
	}
	if countsNow(before, r.State) {
		s.delivered(ctx, r)
	}
	rule, _ := RuleFor(r.Kind)
	reply.ResendOK = rule.ReadOnly
	reply.Request = s.viewOf(id)
	reply.Open = []string{}
	for _, o := range s.Requests(RequestFilter{Holder: c.SessionID}) {
		if !o.State.Closed() {
			reply.Open = append(reply.Open, o.ID)
		}
	}
	return reply, nil
}

// holderCall is the shared shape of seen, accept, report and decline.
func (s *Requests) holderCall(ctx context.Context, c Caller, id, verb string, fn func(w *work, st *storedRequest) *refusal) (RequestView, error) {
	if err := refusePeer(c); err != nil {
		return RequestView{}, err
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	var before RequestState
	r, err := s.recorded(c, id, verb, false, func(w *work, st *storedRequest) *refusal {
		before = st.index.State
		return fn(w, st)
	})
	if err != nil {
		return RequestView{}, err
	}
	if countsNow(before, r.State) {
		s.delivered(ctx, r)
	}
	if r.State == StateFailed {
		_ = s.assign(ctx)
	}
	return s.viewOf(id), nil
}

// Seen implements RequestService.
func (s *Requests) Seen(ctx context.Context, c Caller, id string, in SeenInput) (RequestView, error) {
	return s.holderCall(ctx, c, id, VerbSeen, func(w *work, st *storedRequest) *refusal {
		r := &st.index
		if (in.Via != "" && in.Via != ViaTool && in.Via != ViaProgram) ||
			(in.ReporterVersion != "" && versionOr(in.ReporterVersion) == "") {
			return &refusal{refusedInput, inputErr("via is tool or program, and a version is an identifier")}
		}
		if r.Delivery.FetchedAt == nil {
			return badState()
		}
		if in.Digest != r.Delivery.Digest {
			return &refusal{refusedWrongDigest, ErrRequestState}
		}
		if r.Delivery.InModelCallAt != nil {
			return nil // the same report again
		}
		if r.State.Closed() {
			return badState()
		}
		at := w.now
		r.Delivery.InModelCallAt, r.Delivery.ReporterVersion = &at, in.ReporterVersion
		w.advance(r)
		return nil
	})
}

// Accept implements RequestService.
func (s *Requests) Accept(ctx context.Context, c Caller, id string, in AcceptInput) (RequestView, error) {
	return s.holderCall(ctx, c, id, VerbAccept, func(w *work, st *storedRequest) *refusal {
		r := &st.index
		codeOK := st.content != nil && in.Code != "" && in.Code == st.content.ReceiptCode
		if in.Code != "" && !codeOK {
			return &refusal{refusedWrongCode, inputErr("that is not the receipt code of this request")}
		}
		if (r.State == StateReported || r.State == StateDone) && r.Delivery.AcceptedAt != nil {
			return nil // the same accept again
		}
		if r.State.Closed() || r.State == StateReported {
			return badState()
		}
		if r.Delivery.AcceptedAt == nil {
			at := w.now
			r.Delivery.AcceptedAt = &at
		}
		r.Delivery.CodeOK = r.Delivery.CodeOK || codeOK
		if r.State == StateDelivered {
			w.setState(r, StateAccepted, "")
		}
		return nil
	})
}

// Report implements RequestService. It never makes a request done.
func (s *Requests) Report(ctx context.Context, c Caller, id string, in ReportInput) (RequestView, error) {
	return s.holderCall(ctx, c, id, VerbReport, func(w *work, st *storedRequest) *refusal {
		r := &st.index
		if r.State == StateReported {
			return nil // the same report again
		}
		rule, _ := RuleFor(r.Kind)
		if st.content == nil || !CanMove(rule, r.State, StateReported, true) {
			return badState()
		}
		w.setState(r, StateReported, "")
		r.Report = &ReportMark{At: w.now, By: c.Observed(), RulesVersion: versionOr(in.RulesVersion)}
		r.Verdict, r.Overdue, r.HeldSince, r.HeldCause = nil, false, nil, ""
		review := w.now.Add(s.settings().Due)
		r.ReviewBy = &review
		st.content.ReportText, st.content.ResultID = in.Text, in.ResultID
		return nil
	})
}

// Decline implements RequestService.
func (s *Requests) Decline(ctx context.Context, c Caller, id string, in DeclineInput) (RequestView, error) {
	return s.holderCall(ctx, c, id, VerbDecline, func(w *work, st *storedRequest) *refusal {
		r := &st.index
		if err := needReason(in.ReasonCode); err != nil {
			return &refusal{refusedInput, err}
		}
		if r.State == StateFailed && r.ReasonCode == WhyDeclined {
			return nil
		}
		if r.State.Closed() || r.State == StateReported || st.content == nil {
			return badState()
		}
		st.content.DeclineText = in.Text
		w.close(r, StateFailed, WhyDeclined, c.Observed())
		return nil
	})
}

// ---- operator actions -----------------------------------------------------

func addWords(st *storedRequest, words string) {
	if words != "" && st.content != nil {
		st.content.Words = words
	}
}

// Drop implements RequestService. A session's drop cancels and stays listed.
func (s *Requests) Drop(ctx context.Context, c Caller, id string, in ReasonInput) (RequestView, error) {
	if err := refusePeer(c); err != nil {
		return RequestView{}, err
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := needReason(in.ReasonCode); err != nil {
		return RequestView{}, err
	}
	_, err := s.recorded(c, id, VerbDrop, true, func(w *work, st *storedRequest) *refusal {
		r := &st.index
		if r.State == StateDone {
			return badState()
		}
		addWords(st, in.Words)
		if !r.State.Closed() {
			w.close(r, StateCancelled, in.ReasonCode, c.Observed())
		}
		if !nonOperator(c) && r.SettledAt == nil {
			at := w.now
			r.SettledAt, r.SettledBy = &at, c.Observed()
		}
		return nil
	})
	if err != nil {
		return RequestView{}, err
	}
	_ = s.assign(ctx)
	return s.viewOf(id), nil
}

// mayRunTwice reports that work of a closed request may already have been done.
func mayRunTwice(r *Request, rule KindRule) bool {
	return r.MayRunTwice || r.Report != nil || r.Delivery.AcceptedAt != nil ||
		(r.Delivery.MaybeHandedOver() && !rule.ReadOnly)
}

// Reissue implements RequestService: a new request linked to a closed one.
func (s *Requests) Reissue(ctx context.Context, c Caller, id string, in ReasonInput) (RequestView, error) {
	if err := refusePeer(c); err != nil {
		return RequestView{}, err
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := needReason(in.ReasonCode); err != nil {
		return RequestView{}, err
	}
	old, ok := s.Request(id)
	if !ok {
		_, err := s.Content(id)
		return RequestView{}, err
	}
	rule, _ := RuleFor(old.Kind)
	refuse := func(no refusal) (RequestView, error) {
		_, err := s.recorded(c, id, VerbReissue, true, func(*work, *storedRequest) *refusal { return &no })
		return RequestView{}, err
	}
	if old.ReissuedAs != "" {
		return s.viewOf(old.ReissuedAs), nil
	}
	if !old.State.Closed() || old.State == StateDone || old.Pruned {
		return refuse(*badState())
	}
	twice := mayRunTwice(&old, rule)
	if twice && !in.RepeatOK {
		return refuse(refusal{refusedState, ErrMayRunTwice})
	}
	content, err := s.Content(id)
	if err != nil {
		return RequestView{}, err
	}
	after := old.After
	if p, ok := s.Request(after); !ok || (p.State.Closed() && p.State != StateDone) {
		after = ""
	}
	input := CreateInput{Kind: old.Kind, Role: old.Role, Scope: old.Scope, Key: "reissue:" + id, Source: old.Source,
		After: after, TakeBy: time.Duration(old.TakeBySeconds) * time.Second,
		Due: time.Duration(old.DueSeconds) * time.Second, Content: content}
	st, err := s.build(c, input, rule)
	if err != nil {
		return RequestView{}, err
	}
	st.index.ReissueOf, st.index.MayRunTwice = id, twice
	nid := st.index.ID
	err = s.insert(ctx, st, func(w *work) {
		prev := w.edit(id)
		prev.index.ReissuedAs = nid
		if prev.index.SettledAt == nil {
			at := w.now
			prev.index.SettledAt, prev.index.SettledBy = &at, c.Observed()
		}
		if nonOperator(c) {
			prev.index.NonOperatorActions++
		}
		addWords(prev, in.Words)
		addCall(prev, c, VerbReissue, "", w.now)
	})
	if err != nil {
		return RequestView{}, err
	}
	_ = s.assign(ctx)
	return s.viewOf(nid), nil
}

// Force implements RequestService: the one move off a live holder.
func (s *Requests) Force(ctx context.Context, c Caller, id string, in ForceInput) (RequestView, error) {
	if err := refusePeer(c); err != nil {
		return RequestView{}, err
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := needReason(in.ReasonCode); err != nil {
		return RequestView{}, err
	}
	var to *Holder
	if in.ToSession != "" {
		info, ok := s.session(in.ToSession)
		if !ok || !info.Live {
			return RequestView{}, inputErr("no such live session")
		}
		to = &Holder{SessionID: in.ToSession, Family: info.Family, Since: s.opts.Now()}
		if _, ok := s.Request(id); ok && s.opts.Verifier != nil {
			if err := s.opts.Verifier.Holder(ctx, id, in.ToSession); err != nil {
				return RequestView{}, err
			}
		}
	}
	_, err := s.recorded(c, id, VerbForce, true, func(w *work, st *storedRequest) *refusal {
		r := &st.index
		if r.State.Closed() || r.State == StateReported || r.Role == RoleSession {
			return badState()
		}
		w.dropHolder(r)
		why := WhyForced
		if to != nil {
			r.Holder, why = to, ""
		}
		w.setState(r, StateWaiting, why)
		r.MayRunTwice, r.Overdue = true, false
		addWords(st, in.Words)
		return nil
	})
	if err != nil {
		return RequestView{}, err
	}
	_ = s.assign(ctx)
	return s.viewOf(id), nil
}

// checkable reports a request datawatch's check reads.
func checkable(r *Request) bool {
	if r.State == StateReported {
		return true
	}
	rule, _ := RuleFor(r.Kind)
	return rule.DeliveryOnly && (r.State == StateDelivered || r.State == StateAccepted)
}

func cleanVerdict(v Verdict, now time.Time) *Verdict {
	v.At, v.Status, v.Reason = now, codeOr(v.Status), codeOr(v.Reason)
	return &v
}

// AcceptUnproven implements RequestService. It goes through the "done when"
// list; the next check reads the list and closes the request if the list then
// says so.
func (s *Requests) AcceptUnproven(ctx context.Context, c Caller, id string, in ReasonInput) (RequestView, error) {
	if err := refusePeer(c); err != nil {
		return RequestView{}, err
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := needReason(in.ReasonCode); err != nil {
		return RequestView{}, err
	}
	var verdict Verdict
	if r, ok := s.Request(id); ok && checkable(&r) {
		if s.opts.Verifier == nil {
			return RequestView{}, ErrRequestState
		}
		var err error
		if verdict, err = s.opts.Verifier.Waive(ctx, c, r, in); err != nil {
			return RequestView{}, err
		}
	}
	_, err := s.recorded(c, id, VerbAcceptUnproven, true, func(w *work, st *storedRequest) *refusal {
		if !checkable(&st.index) {
			return badState()
		}
		st.index.Verdict = cleanVerdict(verdict, w.now)
		addWords(st, in.Words)
		return nil
	})
	if err != nil {
		return RequestView{}, err
	}
	return s.viewOf(id), nil
}

// ---- roles ------------------------------------------------------------------

// moveRole applies the holder rules to the requests of a role whose holder is
// no longer keep.
func (s *Requests) moveRole(role Role, scope Scope, keep string) error {
	return s.update(func(w *work) error {
		for _, id := range w.ids() {
			r := &w.peek(id).index
			if r.Role == role && r.Scope == scope && r.Holder != nil && r.Holder.SessionID != keep {
				w.loseHolder(id, false)
			}
		}
		return nil
	})
}

// GiveRole implements RequestService.
func (s *Requests) GiveRole(ctx context.Context, c Caller, in RoleInput) (RoleBinding, error) {
	if err := refusePeer(c); err != nil {
		return RoleBinding{}, err
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.opts.Roles == nil {
		return RoleBinding{}, ErrRequestsNotBuilt
	}
	b, hello, err := s.opts.Roles.Give(ctx, s, c, in)
	if err != nil {
		return RoleBinding{}, err
	}
	if err := s.moveRole(in.Role, in.Scope, b.SessionID); err != nil {
		return b, err
	}
	if hello != nil {
		v, _, err := s.createLocked(ctx, Caller{Kind: CallerDatawatch, Surface: SurfaceDaemon}, *hello)
		if err != nil {
			return b, err
		}
		b.HelloID = v.ID
		if err := s.PutBinding(b); err != nil {
			return b, err
		}
	}
	if err := s.assign(ctx); err != nil {
		return b, err
	}
	return b, nil
}

// ReleaseRole implements RequestService.
func (s *Requests) ReleaseRole(ctx context.Context, c Caller, in RoleInput) error {
	if err := refusePeer(c); err != nil {
		return err
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.opts.Roles == nil {
		return ErrRequestsNotBuilt
	}
	if err := s.opts.Roles.Release(ctx, s, c, in); err != nil {
		return err
	}
	keep := ""
	if b, ok := s.opts.Roles.Resolve(s, in.Role, in.Scope); ok {
		keep = b.SessionID
	}
	if err := s.moveRole(in.Role, in.Scope, keep); err != nil {
		return err
	}
	return s.assign(ctx)
}
