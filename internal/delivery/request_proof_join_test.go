// Part C's roles, reminders, list check and receipt probe over part A's real
// store. These tests skip while the store is not built, and run once part A is
// merged.

package delivery

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/acceptance"
	"github.com/dmz006/datawatch/internal/llm/backends/pi"
)

type cxJoinFx struct {
	*cxProofFx
	s     cxSessions
	holds cxHolds
	ty    *cxTyper
	store *Requests
	data  string
	key   []byte
}

func cxJoin(t *testing.T, key []byte) *cxJoinFx {
	t.Helper()
	probe, err := OpenRequests(RequestOptions{Dir: filepath.Join(t.TempDir(), "queue.d")})
	if errors.Is(err, ErrRequestsNotBuilt) {
		t.Skip("part A's request store is not in this tree yet")
	}
	_ = probe
	f := &cxJoinFx{cxProofFx: cxProof(t), s: cxSessions{}, holds: cxHolds{}, ty: &cxTyper{}, data: t.TempDir(), key: key}
	f.open()
	return f
}

// open wires the parts as the daemon does. A second call is a restart.
func (f *cxJoinFx) open() {
	f.t.Helper()
	book := func() RequestBook {
		if f.store == nil {
			return nil
		}
		return f.store
	}
	lists, err := acceptance.Open(acceptance.Options{
		Path: filepath.Join(f.data, "acceptance.json"), Key: f.key, Mode: func() string { return f.mode },
		Runner: RefusingRunner{}, Resolver: NewRequestResolver(book),
		Probes: map[string]acceptance.Probe{acceptance.MethodReceipt: NewReceiptProbe(book)}, Now: f.clock.now,
	})
	cxNoErr(f.t, err)
	f.lists, f.v = lists, NewListsVerifier(lists)
	settings := func() RequestSettings {
		return RequestSettings{TakeBy: time.Hour, Due: 24 * time.Hour, ReminderInterval: 15 * time.Minute,
			RemindersPerSessionHour: 2, RemindersPerHostHour: 20}
	}
	keeper := NewRoleKeeper(f.s)
	store, err := OpenRequests(RequestOptions{
		Dir: filepath.Join(f.data, "queue.d"), Key: f.key, Now: f.clock.now, Interval: 10 * time.Minute,
		Settings: settings, Roles: keeper, Verifier: f.v, Sessions: f.s, Holds: f.holds,
		Notifier: NewNotifier(NotifierOptions{Typer: f.ty, Holds: f.holds, Sessions: f.s, Roles: keeper,
			Verifier: f.v, Settings: settings}),
		Record: func(Candidate) error { return nil },
	})
	cxNoErr(f.t, err)
	f.store = store
}

func (f *cxJoinFx) tick() {
	f.t.Helper()
	f.clock.add(time.Minute)
	cxNoErr(f.t, f.store.Tick(context.Background()))
}

func (f *cxJoinFx) view(id string) RequestView {
	f.t.Helper()
	v, err := f.store.Get(context.Background(), cxOp, id)
	cxNoErr(f.t, err)
	return v
}

func cxAs(id string) Caller { return Caller{Kind: CallerSession, SessionID: id, Surface: SurfaceREST} }

// supervisor gives session the project's supervisor role.
func (f *cxJoinFx) supervisor(id, family string) RoleBinding {
	f.t.Helper()
	f.s.add(id, family)
	b, err := f.store.GiveRole(context.Background(), cxOp, cxGive(id))
	cxNoErr(f.t, err)
	if b.HelloID == "" || b.CanTakeAt != nil {
		f.t.Fatalf("a new holder gets a hello: %+v", b)
	}
	return b
}

func (f *cxJoinFx) canTake() bool {
	b, ok := NewRoleKeeper(f.s).Resolve(f.store, RoleSupervisor, cxProject("p1"))
	return ok && b.CanTakeAt != nil
}

func (f *cxJoinFx) ask(key string, items ...DoneItem) string {
	f.t.Helper()
	k, err := OccurrenceKey("operator", key)
	cxNoErr(f.t, err)
	v, created, err := f.store.Create(context.Background(), cxOp, CreateInput{Kind: ReqAsk, Role: RoleSupervisor,
		Scope: cxProject("p1"), Key: k,
		Content: RequestContent{Ask: "Write the file.", Detail: "SECRET-CXJ-7.", ProjectDir: f.repo, Items: items}})
	cxNoErr(f.t, err)
	if !created {
		f.t.Fatal("not created")
	}
	return v.ID
}

func (f *cxJoinFx) take(session, id string) TakeReply {
	f.t.Helper()
	f.clock.add(time.Second)
	r, err := f.store.Take(context.Background(), cxAs(session), id, TakeInput{Via: ViaTool})
	cxNoErr(f.t, err)
	return r
}

func cxJoinItems() []DoneItem {
	detail := cxFileItem
	detail.ID, detail.From = "f2", "SECRET-CXJ-7."
	return []DoneItem{cxFileItem, detail}
}

func TestReqProofJoinClaudeFromHelloToDone(t *testing.T) {
	f := cxJoin(t, nil)
	b := f.supervisor("s1", "claude-code")
	ask := f.ask("j1", cxJoinItems()...)
	if v := f.view(ask); v.Holder != nil || v.ReasonCode != WhyTakeNotProven {
		t.Fatalf("no counted hello, no must-deliver work: %+v", v.Request)
	}
	f.tick()
	if len(f.ty.notices) != 1 || strings.Contains(f.ty.notices[0].text, "SECRET") || f.canTake() {
		t.Fatalf("%+v", f.ty.notices)
	}
	f.take("s1", b.HelloID)
	if !f.canTake() {
		t.Fatal("claude's take proves the take")
	}
	f.tick()
	if v := f.view(b.HelloID); v.State != StateDone || v.ProofAs != ProofDelivered {
		t.Fatalf("hello: %s %s %+v", v.State, v.ProofAs, v.Verdict)
	}
	if v := f.view(ask); v.Holder == nil || v.Holder.SessionID != "s1" {
		t.Fatalf("%+v", v.Request)
	}
	f.take("s1", ask)
	ctx := context.Background()
	_, err := f.store.Accept(ctx, cxAs("s1"), ask, AcceptInput{})
	cxNoErr(t, err)
	f.commit("out.txt", "ready")
	_, err = f.store.Report(ctx, cxAs("s1"), ask, ReportInput{Text: "done"})
	cxNoErr(t, err)
	f.tick()
	if v := f.view(ask); v.State != StateReported || v.Verdict == nil || v.Verdict.Status != string(acceptance.StatusNotReviewed) {
		t.Fatalf("without coverage it is not done: %s %+v", v.State, v.Verdict)
	}
	_, err = f.lists.Confirm(ask, "list", true, "yes", "", f.list(ask).Hash(), acceptance.Caller{Observed: "session:s1", SessionID: "s1"})
	cxNoErr(t, err)
	f.tick()
	if v := f.view(ask); v.State != StateReported {
		t.Fatalf("the holder's own coverage counted: %s", v.State)
	}
	f.coverage(ask)
	f.tick()
	if v := f.view(ask); v.State != StateDone || v.ProofAs != ProofDone {
		t.Fatalf("%s %s %+v", v.State, v.ProofAs, v.Verdict)
	}
}

func TestReqProofJoinPiCountsOnlyAtSeen(t *testing.T) {
	f := cxJoin(t, nil)
	b := f.supervisor("s2", "pi")
	ask := f.ask("j1", cxJoinItems()...)
	reply := f.take("s2", b.HelloID)
	f.tick()
	if v := f.view(b.HelloID); f.canTake() || v.State != StateWaiting {
		t.Fatalf("a pi fetch counted: %s", v.State)
	}
	if len(f.ty.notices)+len(f.ty.requests) != 0 {
		t.Fatalf("pi was typed into: %+v %+v", f.ty.notices, f.ty.requests)
	}
	// What the built extension sends.
	_, err := f.store.Seen(context.Background(), cxAs("s2"), b.HelloID,
		SeenInput{Digest: reply.Digest, Via: ViaProgram, ReporterVersion: pi.DeliveryExtensionVersion})
	cxNoErr(t, err)
	if !f.canTake() {
		t.Fatal("pi's hello counts at seen")
	}
	f.tick()
	if v := f.view(b.HelloID); v.State != StateDone || v.ProofAs != ProofDelivered {
		t.Fatalf("hello: %s %+v", v.State, v.Verdict)
	}
	if v := f.view(ask); v.Holder == nil || v.Holder.SessionID != "s2" {
		t.Fatalf("%+v", v.Request)
	}
}

func TestReqProofJoinALateTakeMovesNothing(t *testing.T) {
	f := cxJoin(t, nil)
	b := f.supervisor("s1", "claude-code")
	for i := 0; i < 20 && !f.view(b.HelloID).State.Closed(); i++ {
		f.clock.add(20 * time.Minute)
		cxNoErr(t, f.store.Tick(context.Background()))
	}
	was := f.view(b.HelloID).State
	if !was.Closed() {
		t.Fatalf("the hello is still %s", was)
	}
	if _, err := f.store.Take(context.Background(), cxAs("s1"), b.HelloID, TakeInput{Via: ViaTool}); err == nil {
		t.Fatal("a late take was served")
	}
	f.tick()
	if f.canTake() || f.view(b.HelloID).State != was {
		t.Fatal("a late take moved something")
	}
}

func TestReqProofJoinRestartKeepsTheCounts(t *testing.T) {
	f := cxJoin(t, nil)
	f.supervisor("s1", "claude-code")
	for i := 0; i < 6; i++ { // one hour, restarting between checks
		f.clock.add(9 * time.Minute)
		f.open()
		cxNoErr(t, f.store.Tick(context.Background()))
	}
	if len(f.ty.notices) != 2 {
		t.Fatalf("2 per session each hour across restarts, got %d", len(f.ty.notices))
	}
}

func TestReqProofJoinUnprovenWorkerNeverProves(t *testing.T) {
	f := cxJoin(t, nil)
	f.supervisor("s3", "fake-worker")
	ask := f.ask("j1", cxJoinItems()...)
	for i := 0; i < 4; i++ {
		f.tick()
		f.open()
	}
	if v := f.view(ask); v.Holder != nil || v.ReasonCode != WhyTakeNotProven {
		t.Fatalf("%+v", v.Request)
	}
	if len(f.ty.notices)+len(f.ty.requests) != 0 {
		t.Fatalf("a hello was typed: %+v %+v", f.ty.notices, f.ty.requests)
	}
}

func TestReqProofJoinAcceptUnproven(t *testing.T) {
	f := cxJoin(t, nil)
	b := f.supervisor("s1", "claude-code")
	f.take("s1", b.HelloID)
	ask := f.ask("j1", DoneItem{ID: "c1", Statement: "A person saw it", From: "Write the file.", Method: "confirm"},
		DoneItem{ID: "c2", Statement: "A person read the detail", From: "SECRET-CXJ-7.", Method: "confirm"})
	f.take("s1", ask)
	ctx := context.Background()
	_, err := f.store.Accept(ctx, cxAs("s1"), ask, AcceptInput{})
	cxNoErr(t, err)
	_, err = f.store.Report(ctx, cxAs("s1"), ask, ReportInput{})
	cxNoErr(t, err)
	f.coverage(ask)
	in := ReasonInput{ReasonCode: "good_enough", Words: "fine"}
	_, err = f.store.AcceptUnproven(ctx, cxAs("s1"), ask, in)
	cxNoErr(t, err)
	f.tick()
	if v := f.view(ask); v.State != StateReported {
		t.Fatalf("the holder's own waiver closed it: %s", v.State)
	}
	_, err = f.store.AcceptUnproven(ctx, cxOp, ask, in)
	cxNoErr(t, err)
	f.tick()
	if v := f.view(ask); v.State != StateDone || v.ProofAs != ProofAcceptedUnproven {
		t.Fatalf("%s %q %+v", v.State, v.ProofAs, v.Verdict)
	}
}

// On a secure install the lists hold the request's words, so neither store may
// leave them readable.
func TestReqProofJoinNoPlainWordsOnASecureInstall(t *testing.T) {
	scan := func(key []byte) (found []string) {
		f := cxJoin(t, key)
		b := f.supervisor("s1", "claude-code")
		f.take("s1", b.HelloID)
		ask := f.ask("j1", cxJoinItems()...)
		f.take("s1", ask)
		f.tick()
		cxNoErr(t, filepath.WalkDir(f.data, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if strings.Contains(string(data), "SECRET-CXJ") {
				found = append(found, filepath.Base(path))
			}
			return err
		}))
		return found
	}
	if got := scan(nil); len(got) < 2 {
		t.Fatalf("the plain run must find the marker in the store and the lists: %v", got)
	}
	if got := scan([]byte("0123456789abcdef0123456789abcdef")); len(got) != 0 {
		t.Fatalf("readable on a secure install: %v", got)
	}
}
