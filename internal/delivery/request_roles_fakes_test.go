// Fakes for part C's tests. Every name starts with cx so that it cannot clash
// with part A's test fakes in this package.

package delivery

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"
)

var cxStart = time.Date(2026, 11, 27, 9, 0, 0, 123456789, time.UTC)

type cxClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *cxClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *cxClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// cxBook is an in-memory RequestBook. failSave makes every save fail.
type cxBook struct {
	reqs     map[string]*Request
	content  map[string]RequestContent
	bindings []RoleBinding
	notices  []RequestNotice
	failSave bool
	// trace records saves, in order, beside what a cxTyper typed.
	trace *[]string
}

func cxNewBook() *cxBook {
	return &cxBook{reqs: map[string]*Request{}, content: map[string]RequestContent{}, trace: &[]string{}}
}

func (b *cxBook) put(r Request, c RequestContent) {
	b.reqs[r.ID] = &r
	b.content[r.ID] = c
}

func (b *cxBook) Requests(f RequestFilter) []Request {
	var out []Request
	for _, r := range b.reqs {
		holder := ""
		if r.Holder != nil {
			holder = r.Holder.SessionID
		}
		if (f.State != "" && r.State != f.State) || (f.Kind != "" && r.Kind != f.Kind) ||
			(f.Role != "" && r.Role != f.Role) || (f.ScopeKind != "" && r.Scope.Kind != f.ScopeKind) ||
			(f.ScopeID != "" && r.Scope.ID != f.ScopeID) || (f.Holder != "" && holder != f.Holder) {
			continue
		}
		out = append(out, cxCopy(*r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func cxCopy(r Request) Request {
	if r.Holder != nil {
		h := *r.Holder
		r.Holder = &h
	}
	r.Calls = append([]CallRecord(nil), r.Calls...)
	r.FormerHolders = append([]string(nil), r.FormerHolders...)
	return r
}

func (b *cxBook) Request(id string) (Request, bool) {
	r, ok := b.reqs[id]
	if !ok {
		return Request{}, false
	}
	return cxCopy(*r), true
}

func (b *cxBook) Content(id string) (RequestContent, error) {
	c, ok := b.content[id]
	if !ok {
		return RequestContent{}, ErrRequestNotFound
	}
	return c, nil
}

func (b *cxBook) Change(id string, fn func(*Request) error) (Request, error) {
	cur, ok := b.reqs[id]
	if !ok {
		return Request{}, ErrRequestNotFound
	}
	next := cxCopy(*cur)
	if err := fn(&next); err != nil {
		return Request{}, err
	}
	if b.failSave {
		return Request{}, ErrCannotRecord
	}
	b.reqs[id] = &next
	*b.trace = append(*b.trace, "save "+id)
	return cxCopy(next), nil
}

func (b *cxBook) Bindings() []RoleBinding { return append([]RoleBinding(nil), b.bindings...) }

func (b *cxBook) without(role Role, scope Scope) []RoleBinding {
	var out []RoleBinding
	for _, x := range b.bindings {
		if x.Role != role || x.Scope != scope {
			out = append(out, x)
		}
	}
	return out
}

func (b *cxBook) PutBinding(n RoleBinding) error {
	if b.failSave {
		return ErrCannotRecord
	}
	b.bindings = append(b.without(n.Role, n.Scope), n)
	return nil
}

func (b *cxBook) SetToldOlder(given RoleBinding, at *time.Time) error {
	if b.failSave {
		return ErrCannotRecord
	}
	for i := range b.bindings {
		if x := &b.bindings[i]; x.Role == given.Role && x.Scope == given.Scope && x.SessionID == given.SessionID &&
			x.GivenAt.Equal(given.GivenAt) {
			x.ToldOlderAt = at
		}
	}
	return nil
}

func (b *cxBook) DropBinding(role Role, scope Scope) error {
	if b.failSave {
		return ErrCannotRecord
	}
	b.bindings = b.without(role, scope)
	return nil
}

func (b *cxBook) Notices(since time.Time) []RequestNotice {
	var out []RequestNotice
	for _, n := range b.notices {
		if !n.At.Before(since) {
			out = append(out, n)
		}
	}
	return out
}

func (b *cxBook) AddNotice(n RequestNotice) error {
	if b.failSave {
		return ErrCannotRecord
	}
	b.notices = append(b.notices, n)
	*b.trace = append(*b.trace, "notice "+n.SessionID)
	return nil
}

type cxSessions map[string]SessionInfo

func (s cxSessions) Info(id string) (SessionInfo, bool) {
	i, ok := s[id]
	return i, ok
}

func (s cxSessions) add(id, family string) {
	s[id] = SessionInfo{ID: id, Family: family, Live: true}
}

type cxHolds map[string]string // session ID, or "" for every session

func (h cxHolds) Held(id string, _ Scope) (string, bool) {
	if c := h[""]; c != "" {
		return c, true
	}
	c := h[id]
	return c, c != ""
}

type cxTyped struct{ session, id, text string }

type cxTyper struct {
	notices  []cxTyped
	requests []cxTyped
	err      error
	trace    *[]string
}

func (t *cxTyper) Notice(_ context.Context, session, line string) error {
	if t.err != nil {
		return t.err
	}
	t.notices = append(t.notices, cxTyped{session: session, text: line})
	if t.trace != nil {
		*t.trace = append(*t.trace, "type "+session)
	}
	return nil
}

func (t *cxTyper) Request(_ context.Context, session, id, text string) error {
	if t.trace != nil {
		*t.trace = append(*t.trace, "type "+session)
	}
	if t.err != nil {
		return t.err
	}
	t.requests = append(t.requests, cxTyped{session: session, id: id, text: text})
	return nil
}

func cxProject(id string) Scope { return Scope{Kind: ScopeProject, ID: id} }

// cxHeld is a waiting request held by a session of family.
func cxHeld(id, session, family string) Request {
	return Request{ID: id, Kind: ReqAsk, Class: ClassAsk, Role: RoleSupervisor, Scope: cxProject("p1"),
		State: StateWaiting, Created: cxStart, StateAt: cxStart,
		Holder: &Holder{SessionID: session, Family: family, Since: cxStart}}
}

func cxNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
