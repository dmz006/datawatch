// Roles: who holds a role in a scope, and the proof that a session can take.
// Owner: part C.
//
// One holder per role and scope. A held role is refused unless its holder is
// gone or the caller forces it with a reason code. Who may give a role is
// recorded, not enforced: a session that gives itself one is marked SelfGiven.
//
// "Can take" is proven only by a session's own hello counting under its worker
// policy. The proof is carried to a second role of the same session while it
// holds one. Once it holds none (released, or datawatch saw it end), the next
// give sends a new hello: a session started again may have lost what let it
// take.

package delivery

import (
	"context"
	"errors"
	"strconv"
	"time"
)

type roleKeeper struct {
	sessions SessionBook
}

// NewRoleKeeper returns the keeper over datawatch's session records.
func NewRoleKeeper(sessions SessionBook) RoleKeeper { return &roleKeeper{sessions: sessions} }

// helloSource is the source type of a hello. Its ID is the binding generation
// the hello's key was made from, so a later give never makes the same key.
const helloSource = "role_hello"

func (k *roleKeeper) live(sessionID string) bool {
	if k.sessions == nil {
		return false
	}
	info, ok := k.sessions.Info(sessionID)
	return ok && info.Live
}

func boundTo(b RequestBook, role Role, scope Scope) (RoleBinding, bool) {
	for _, x := range b.Bindings() {
		if x.Role == role && x.Scope == scope {
			return x, true
		}
	}
	return RoleBinding{}, false
}

// Resolve says who a role is now. A session datawatch saw end is nobody.
func (k *roleKeeper) Resolve(b RequestBook, role Role, scope Scope) (RoleBinding, bool) {
	x, ok := boundTo(b, role, scope)
	if !ok || !k.live(x.SessionID) {
		return RoleBinding{}, false
	}
	return x, true
}

func roleInputErr(msg string) error { return errors.Join(ErrRequestInput, errors.New(msg)) }

// Give binds a role to a session. When the session has not proven it can take,
// the hello to create comes back with the binding; the caller creates it and
// saves its ID on the binding.
func (k *roleKeeper) Give(_ context.Context, b RequestBook, c Caller, in RoleInput) (RoleBinding, *CreateInput, error) {
	var none RoleBinding
	if in.Role != RoleSupervisor || in.Scope.Kind != ScopeProject {
		return none, nil, roleInputErr("only the supervisor of a project can be given")
	}
	if err := CheckScopeID(in.Scope.ID); err != nil {
		return none, nil, err
	}
	if !k.live(in.SessionID) {
		return none, nil, roleInputErr("no such live session on this computer")
	}
	old, had := boundTo(b, in.Role, in.Scope)
	same := had && old.SessionID == in.SessionID
	if had && !same && k.live(old.SessionID) {
		switch {
		case !in.Force:
			return none, nil, ErrRoleHeld
		case in.ReasonCode == "":
			return none, nil, ErrReasonNeeded
		}
		if err := CheckReasonCode(in.ReasonCode); err != nil {
			return none, nil, err
		}
	}
	if same && old.CanTakeAt != nil {
		return old, nil, nil
	}

	var openHello string
	last := old.Generation
	for _, h := range b.Requests(RequestFilter{Kind: ReqHello, ScopeKind: ScopeSession, ScopeID: in.SessionID}) {
		if !h.State.Closed() {
			openHello = h.ID
		}
		if n, err := strconv.Atoi(h.Source.ID); err == nil && h.Source.Type == helloSource && n > last {
			last = n
		}
	}
	if same && openHello != "" && old.HelloID == openHello {
		return old, nil, nil
	}
	gen := last + 1
	if same && openHello != "" {
		gen = old.Generation
	}
	n := RoleBinding{Role: in.Role, Scope: in.Scope, SessionID: in.SessionID, GivenBy: c.Observed(),
		GivenAt: bindingTime(), SelfGiven: c.Kind == CallerSession && c.SessionID == in.SessionID,
		Generation: gen}
	for _, x := range b.Bindings() {
		if x.SessionID == in.SessionID && x.CanTakeAt != nil {
			at := *x.CanTakeAt
			n.CanTakeAt, n.HelloID = &at, x.HelloID
		}
	}
	var hello *CreateInput
	switch {
	case n.CanTakeAt != nil:
	case openHello != "":
		n.HelloID = openHello
	default:
		gen := strconv.Itoa(n.Generation)
		hello = &CreateInput{
			Kind: ReqHello, Role: RoleSession, Scope: Scope{Kind: ScopeSession, ID: in.SessionID},
			Key:     "hello:" + in.SessionID + ":" + gen,
			Source:  SourceRef{Type: helloSource, ID: gen},
			Content: RequestContent{Ask: helloAsk},
		}
	}
	if err := b.PutBinding(n); err != nil {
		return none, nil, err
	}
	return n, hello, nil
}

// helloAsk is the hello's whole text. It asks for nothing but the take.
const helloAsk = "This is datawatch's hello. It checks that this session can receive requests. " +
	"There is no work in it."

// Release takes a role from its holder.
func (k *roleKeeper) Release(_ context.Context, b RequestBook, _ Caller, in RoleInput) error {
	old, had := boundTo(b, in.Role, in.Scope)
	switch {
	case !had:
		return ErrRequestNotFound
	case in.SessionID != "" && in.SessionID != old.SessionID:
		return ErrRequestState
	}
	return b.DropBinding(in.Role, in.Scope)
}

// Ended drops the roles of a session datawatch saw end.
func (k *roleKeeper) Ended(_ context.Context, b RequestBook, sessionID string) error {
	var errs []error
	for _, x := range b.Bindings() {
		if x.SessionID == sessionID {
			errs = append(errs, b.DropBinding(x.Role, x.Scope))
		}
	}
	return errors.Join(errs...)
}

// Delivered marks the roles of a session as able to take, when the delivery
// that now counts is the session's own hello.
func (k *roleKeeper) Delivered(_ context.Context, b RequestBook, r Request) error {
	if r.Kind != ReqHello || r.Holder == nil ||
		r.Scope != (Scope{Kind: ScopeSession, ID: r.Holder.SessionID}) {
		return nil
	}
	p := PolicyFor(r.Holder.Family)
	if !p.Delivered(r.Delivery) {
		return nil
	}
	at := *r.Delivery.FetchedAt
	if p.Needs == LevelInModelCall {
		at = *r.Delivery.InModelCallAt
	}
	var errs []error
	for _, x := range b.Bindings() {
		if x.SessionID == r.Holder.SessionID && x.CanTakeAt == nil {
			t := at
			x.CanTakeAt, x.HelloID = &t, r.ID
			errs = append(errs, b.PutBinding(x))
		}
	}
	return errors.Join(errs...)
}

// bindingTime stamps a binding. RoleKeeper takes no clock.
var bindingTime = time.Now
