// Fakes of part C's seams and a harness over the request store. All tests use
// fakes and an injected clock.

package delivery

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSessions struct {
	mu   sync.Mutex
	info map[string]SessionInfo
}

func (f *fakeSessions) Info(id string) (SessionInfo, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.info[id]
	return i, ok
}

func (f *fakeSessions) add(id, family string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.info[id] = SessionInfo{ID: id, Family: family, Live: true}
}

func (f *fakeSessions) end(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.info[id]
	i.Live = false
	f.info[id] = i
}

// fakeKeeper is the least a role keeper must do.
type fakeKeeper struct {
	now   func() time.Time
	prove bool // a new holder has already proven it can take
}

func (k *fakeKeeper) Resolve(b RequestBook, role Role, scope Scope) (RoleBinding, bool) {
	for _, x := range b.Bindings() {
		if x.Role == role && x.Scope == scope {
			return x, true
		}
	}
	return RoleBinding{}, false
}

func (k *fakeKeeper) Give(_ context.Context, b RequestBook, c Caller, in RoleInput) (RoleBinding, *CreateInput, error) {
	old, held := k.Resolve(b, in.Role, in.Scope)
	if held && old.SessionID != in.SessionID && !in.Force {
		return RoleBinding{}, nil, ErrRoleHeld
	}
	n := RoleBinding{Role: in.Role, Scope: in.Scope, SessionID: in.SessionID, GivenBy: c.Observed(),
		GivenAt: k.now(), Generation: old.Generation + 1}
	var hello *CreateInput
	if k.prove {
		at := k.now()
		n.CanTakeAt = &at
	} else {
		hello = &CreateInput{Kind: ReqHello, Role: RoleSession, Scope: Scope{Kind: ScopeSession, ID: in.SessionID},
			Key: "hello:" + in.SessionID + ":" + string(in.Role) + string(rune('0'+n.Generation))}
	}
	return n, hello, b.PutBinding(n)
}

func (k *fakeKeeper) Release(_ context.Context, b RequestBook, _ Caller, in RoleInput) error {
	return b.DropBinding(in.Role, in.Scope)
}

func (k *fakeKeeper) Ended(_ context.Context, b RequestBook, id string) error {
	for _, x := range b.Bindings() {
		if x.SessionID == id {
			if err := b.DropBinding(x.Role, x.Scope); err != nil {
				return err
			}
		}
	}
	return nil
}

func (k *fakeKeeper) Delivered(_ context.Context, b RequestBook, r Request) error {
	if r.Kind != ReqHello || r.Holder == nil {
		return nil
	}
	for _, x := range b.Bindings() {
		if x.SessionID == r.Holder.SessionID && x.CanTakeAt == nil {
			at := k.now()
			x.CanTakeAt = &at
			if err := b.PutBinding(x); err != nil {
				return err
			}
		}
	}
	return nil
}

type fakeVerifier struct {
	mu       sync.Mutex
	verdicts map[string]Verdict
	rework   int // how many times failed work may go back
	waive    Verdict
	waivedBy []string
	prepared []string
	holders  []string
	started  []string
	prepErr  error
	onPrep   func(Request, RequestContent) error
}

func (v *fakeVerifier) Prepare(_ context.Context, r Request, c RequestContent) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.prepared = append(v.prepared, r.ID)
	if v.onPrep != nil {
		return v.onPrep(r, c)
	}
	return v.prepErr
}

func (v *fakeVerifier) Holder(_ context.Context, id, s string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.holders = append(v.holders, id+"="+s)
	return nil
}

func (v *fakeVerifier) Started(_ context.Context, id string, _ RequestKind) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.started = append(v.started, id)
	return nil
}

func (v *fakeVerifier) Check(_ context.Context, r Request) (Verdict, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if x, ok := v.verdicts[r.ID]; ok {
		return x, nil
	}
	return Verdict{Status: "open"}, nil
}

func (v *fakeVerifier) Rework(context.Context, Request) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.rework > 0 {
		v.rework--
		return true, nil
	}
	return false, nil
}

func (v *fakeVerifier) Waive(_ context.Context, c Caller, _ Request, _ ReasonInput) (Verdict, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.waivedBy = append(v.waivedBy, c.Observed())
	return v.waive, nil
}

func (v *fakeVerifier) set(id string, x Verdict) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.verdicts[id] = x
}

type fakeNotifier struct{ calls int }

func (n *fakeNotifier) Notify(context.Context, RequestBook, time.Time) error {
	n.calls++
	return nil
}

type fakeHolds struct {
	mu    sync.Mutex
	cause map[string]string // "" key: every session
}

func (h *fakeHolds) Held(id string, _ Scope) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c := h.cause[""]; c != "" {
		return c, true
	}
	c := h.cause[id]
	return c, c != ""
}

type reqFx struct {
	t        *testing.T
	data     string // the data folder
	key      []byte
	now      time.Time
	settings RequestSettings
	sessions *fakeSessions
	keeper   *fakeKeeper
	ver      *fakeVerifier
	notif    *fakeNotifier
	holds    *fakeHolds
	recorded []Candidate
	backlog  map[string]int
	fail     func(path string) error // a fault planted on writes
	s        *Requests
}

func testKey(b byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = b
	}
	return k
}

func newReqFx(t *testing.T, key []byte) *reqFx {
	t.Helper()
	f := &reqFx{t: t, data: t.TempDir(), key: key, now: time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC),
		settings: RequestSettings{TakeBy: time.Hour, Due: 24 * time.Hour, ReminderInterval: 15 * time.Minute,
			RemindersPerSessionHour: 2, RemindersPerHostHour: 20},
		sessions: &fakeSessions{info: map[string]SessionInfo{}},
		ver:      &fakeVerifier{verdicts: map[string]Verdict{}},
		notif:    &fakeNotifier{},
		holds:    &fakeHolds{cause: map[string]string{}},
		backlog:  map[string]int{},
	}
	f.keeper = &fakeKeeper{now: func() time.Time { return f.now }, prove: true}
	f.open()
	return f
}

func (f *reqFx) options() RequestOptions {
	return RequestOptions{
		Dir: filepath.Join(f.data, "queue.d"), Key: f.key, Host: "here",
		Now: func() time.Time { return f.now }, Interval: 5 * time.Minute,
		Settings: func() RequestSettings { return f.settings },
		Roles:    f.keeper, Notifier: f.notif, Verifier: f.ver, Sessions: f.sessions, Holds: f.holds,
		Record:  func(c Candidate) error { f.recorded = append(f.recorded, c); return nil },
		Backlog: func(k string) (int, bool) { n, ok := f.backlog[k]; return n, ok },
		writeFile: func(path string, data []byte) error {
			if f.fail != nil {
				if err := f.fail(path); err != nil {
					return err
				}
			}
			return writeRecordFile(path, data, nil)
		},
	}
}

func (f *reqFx) open() {
	f.t.Helper()
	s, err := OpenRequests(f.options())
	if err != nil {
		f.t.Fatalf("open: %v", err)
	}
	f.s = s
}

// reopen is a daemon restart.
func (f *reqFx) reopen() { f.open() }

var opCaller = Caller{Kind: CallerOperator, Surface: SurfaceREST}

func sessCaller(id string) Caller {
	return Caller{Kind: CallerSession, SessionID: id, Surface: SurfaceMCP}
}

var projScope = Scope{Kind: ScopeProject, ID: "proj"}

// supervise gives the project's supervisor role to a new live session.
func (f *reqFx) supervise(id, family string) {
	f.t.Helper()
	f.sessions.add(id, family)
	if _, err := f.s.GiveRole(context.Background(), opCaller,
		RoleInput{Role: RoleSupervisor, Scope: projScope, SessionID: id, Force: true, ReasonCode: "test"}); err != nil {
		f.t.Fatalf("give role: %v", err)
	}
}

var askSeq int

func (f *reqFx) create(kind RequestKind, mod ...func(*CreateInput)) RequestView {
	f.t.Helper()
	askSeq++
	in := CreateInput{Kind: kind, Role: RoleSupervisor, Scope: projScope,
		Key: "operator:t" + strings.Repeat("x", 1) + itoa(askSeq), Content: RequestContent{Ask: "do the thing"}}
	for _, m := range mod {
		m(&in)
	}
	v, created, err := f.s.Create(context.Background(), opCaller, in)
	if err != nil || !created {
		f.t.Fatalf("create %s: created=%v err=%v", kind, created, err)
	}
	return v
}

func itoa(n int) string {
	const d = "0123456789"
	if n < 10 {
		return d[n : n+1]
	}
	return itoa(n/10) + d[n%10:n%10+1]
}

func (f *reqFx) get(id string) Request {
	f.t.Helper()
	r, ok := f.s.Request(id)
	if !ok {
		f.t.Fatalf("no request %s", id)
	}
	return r
}

func (f *reqFx) want(id string, state RequestState) Request {
	f.t.Helper()
	r := f.get(id)
	if r.State != state {
		f.t.Fatalf("%s: state %s (reason %s), want %s", id, r.State, r.ReasonCode, state)
	}
	return r
}

func (f *reqFx) take(id, sess string) TakeReply {
	f.t.Helper()
	rep, err := f.s.Take(context.Background(), sessCaller(sess), id, TakeInput{Via: ViaTool})
	if err != nil {
		f.t.Fatalf("take %s by %s: %v", id, sess, err)
	}
	return rep
}

func (f *reqFx) accept(id, sess string) {
	f.t.Helper()
	if _, err := f.s.Accept(context.Background(), sessCaller(sess), id, AcceptInput{}); err != nil {
		f.t.Fatalf("accept %s: %v", id, err)
	}
}

func (f *reqFx) report(id, sess string) {
	f.t.Helper()
	if _, err := f.s.Report(context.Background(), sessCaller(sess), id, ReportInput{Text: "done"}); err != nil {
		f.t.Fatalf("report %s: %v", id, err)
	}
}

// tick moves the clock and runs one check.
func (f *reqFx) tick(d time.Duration) {
	f.t.Helper()
	f.now = f.now.Add(d)
	if err := f.s.Tick(context.Background()); err != nil {
		f.t.Fatalf("tick: %v", err)
	}
}

// run ticks every interval for d.
func (f *reqFx) run(d time.Duration) {
	f.t.Helper()
	for ; d > 0; d -= 5 * time.Minute {
		f.tick(5 * time.Minute)
	}
}

func (f *reqFx) end(id string) {
	f.t.Helper()
	f.sessions.end(id)
	if err := f.s.HolderEnded(context.Background(), id); err != nil {
		f.t.Fatalf("holder ended: %v", err)
	}
}

// listed are the candidates by request ID.
func (f *reqFx) listed() map[string]Candidate {
	f.t.Helper()
	cs, err := f.s.Candidates(context.Background(), f.now)
	if err != nil {
		f.t.Fatalf("candidates: %v", err)
	}
	out := map[string]Candidate{}
	for _, c := range cs {
		out[c.ID] = c
	}
	return out
}

// files is every file under the data folder with its content.
func (f *reqFx) files() map[string]string {
	f.t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(f.data, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(f.data, p)
		out[rel] = string(b)
		return err
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		f.t.Fatal(err)
	}
	return out
}
