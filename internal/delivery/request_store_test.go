// Part A's first failing tests 8-15: sealing, keys, tampering, import, a full
// disk, clocks and holds, other computers, and the invariants.

package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const marker = "ZQXMARKER7f3a"

func markedContent() RequestContent {
	m := func(s string) string { return s + " " + marker }
	return RequestContent{Ask: m("ask"), Detail: m("detail"), ProjectDir: m("/p"),
		Params: map[string]string{marker + "k": m("v")},
		Items: []DoneItem{{ID: marker + "id", Statement: m("s"), From: m("f"), Method: "state:file", Path: m("path"),
			Contains: m("c"), Effect: m("e"), Before: m("b"), Target: m("t")}}}
}

// runEveryState puts the marker into every content field through the calls and
// leaves a request in every state.
func runEveryState(f *reqFx) {
	t := f.t
	t.Helper()
	f.supervise("a", "claude-code")
	liar := sessCaller("a")
	liar.ClaimedName = "claimed " + marker
	mk := func(mod ...func(*CreateInput)) RequestView {
		return f.create(ReqAsk, append([]func(*CreateInput){func(in *CreateInput) {
			askSeq++
			in.Key, in.Content = "operator:"+marker+itoa(askSeq), markedContent()
		}}, mod...)...)
	}
	waiting := mk()
	delivered, accepted, reported, done, failed, expired, cancelled := mk(), mk(), mk(), mk(), mk(),
		mk(func(in *CreateInput) {
			in.Role, in.Scope, in.Due = RoleSupervisor, Scope{Kind: ScopeProject, ID: "nobody"}, time.Minute
		}), mk()
	for _, v := range []RequestView{delivered, accepted, reported, done, failed} {
		if _, err := f.s.Take(bg, liar, v.ID, TakeInput{Via: ViaTool}); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []RequestView{accepted, reported, done} {
		f.accept(v.ID, "a")
	}
	for _, v := range []RequestView{reported, done} {
		if _, err := f.s.Report(bg, liar, v.ID, ReportInput{Text: "report " + marker, ResultID: marker}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.s.AcceptUnproven(bg, opCaller, reported.ID, ReasonInput{ReasonCode: "r", Words: "words " + marker}); err != nil {
		t.Fatal(err)
	}
	f.ver.set(done.ID, Verdict{Status: "done", Closes: true})
	if _, err := f.s.Decline(bg, liar, failed.ID, DeclineInput{ReasonCode: "no", Text: "decline " + marker}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Drop(bg, liar, cancelled.ID, ReasonInput{ReasonCode: "no", Words: "drop " + marker}); err != nil {
		t.Fatal(err)
	}
	f.run(10 * time.Minute)
	if _, err := f.s.Reissue(bg, opCaller, cancelled.ID, ReasonInput{ReasonCode: "again", Words: marker}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.s.Observe(bg, Condition{ID: "c-" + marker, Value: "A" + marker}); err != nil {
		t.Fatal(err)
	}
	want := map[string]RequestState{waiting.ID: StateWaiting, delivered.ID: StateDelivered, accepted.ID: StateAccepted,
		reported.ID: StateReported, done.ID: StateDone, failed.ID: StateFailed, expired.ID: StateExpired,
		cancelled.ID: StateCancelled}
	for id, st := range want {
		f.want(id, st)
	}
	if err := f.s.AddNotice(RequestNotice{At: f.now, SessionID: "a", RequestIDs: []string{waiting.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SaveQueueItem("item1", []byte(`{"id":"item1","payload":{"p":"`+marker+`"}}`)); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	f.tick(time.Minute)
	queue := filepath.Join(f.data, "queue.json")
	if err := os.WriteFile(queue, []byte(`[{"id":"old1","role":"r","state":"pending","payload":{"p":"`+marker+`"}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ImportQueue(queue); err != nil {
		t.Fatal(err)
	}
	f.reopen()
}

func (f *reqFx) markerHits() []string {
	var hits []string
	for name, body := range f.files() {
		if strings.Contains(body, marker) || strings.Contains(name, marker) {
			hits = append(hits, name)
		}
	}
	sort.Strings(hits)
	return hits
}

// 8. No new plain copy on a secure install; the scan finds it on a plain one.
func TestReqStoreMarkerScan(t *testing.T) {
	secure := newReqFx(t, testKey(7))
	runEveryState(secure)
	if hits := secure.markerHits(); len(hits) != 0 {
		t.Fatalf("plain content on a secure install: %v", hits)
	}
	list, err := secure.s.Candidates(bg, secure.now)
	if err != nil {
		t.Fatal(err)
	}
	rest, err := json.Marshal([]any{list, secure.recorded, secure.s.Requests(RequestFilter{})})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rest), marker) {
		t.Fatalf("the list, an alarm or an index holds content:\n%s", rest)
	}
	plain := newReqFx(t, nil)
	runEveryState(plain)
	if len(plain.markerHits()) == 0 {
		t.Fatal("the scan finds nothing on a plain install, so it proves nothing")
	}
}

// 9. The store has its own key.
func TestReqStoreKeyIsItsOwnAndFailsClosed(t *testing.T) {
	data := t.TempDir()
	params := KeyParams{Time: 1, MemoryKiB: 64, Threads: 1}
	open := func(password string) (*StoreKey, error) {
		return OpenStoreKey(data, []byte(password), params)
	}
	k, err := open("pw")
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Commit(); err != nil {
		t.Fatal(err)
	}
	f := newReqFx(t, nil)
	f.data, f.key = data, k.Key()
	f.open()
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	// The config file's salt changes.
	if err := os.WriteFile(filepath.Join(data, "enc.salt"), []byte("another salt"), 0o600); err != nil {
		t.Fatal(err)
	}
	k2, err := open("pw")
	if err != nil {
		t.Fatalf("a changed config salt broke the store key: %v", err)
	}
	f.key = k2.Key()
	f.reopen()
	f.want(v.ID, StateWaiting)

	before := f.files()
	if _, err := open("wrong"); !errors.Is(err, ErrStoreKey) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := OpenStoreKey(data, nil, params); !errors.Is(err, ErrStoreKey) {
		t.Fatalf("no password: %v", err)
	}
	for name, key := range map[string][]byte{"no key": nil, "wrong key": testKey(9)} {
		o := f.options()
		o.Key = key
		if _, err := OpenRequests(o); !errors.Is(err, ErrRecords) {
			t.Errorf("%s: %v", name, err)
		}
	}
	same(t, before, f.files())
	// Opening with the right key writes nothing either.
	f.reopen()
	same(t, before, f.files())

	// store.key missing with only request records: never recreated.
	if err := os.Remove(filepath.Join(data, "queue.d", metaFileName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(data, filepath.FromSlash(KeyFile))); err != nil {
		t.Fatal(err)
	}
	if recs, _ := filepath.Glob(filepath.Join(data, "queue.d", "r-*.rec")); len(recs) == 0 {
		t.Fatal("no request record to test with")
	}
	if _, err := open("pw"); !errors.Is(err, ErrStoreKey) {
		t.Fatalf("a new key was made beside sealed request records: %v", err)
	}
}

func same(t *testing.T, a, b map[string]string) {
	t.Helper()
	for name, body := range a {
		if b[name] != body {
			t.Errorf("%s changed or went", name)
		}
	}
	for name := range b {
		if _, ok := a[name]; !ok {
			t.Errorf("%s appeared", name)
		}
	}
}

// Secure mode just turned on: what was plain is sealed.
func TestReqStoreAdoptsPlainOnceWhenTheKeyIsNew(t *testing.T) {
	f := newReqFx(t, nil)
	runEveryState(f)
	f.key = testKey(5)
	if _, err := OpenRequests(f.options()); !errors.Is(err, ErrRecords) {
		t.Fatalf("plain store under a key with no adoption: %v", err)
	}
	o := f.options()
	o.AdoptPlain = true
	s, err := OpenRequests(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SealAdopted(); err != nil {
		t.Fatal(err)
	}
	// queue.json was already the marker; nothing under the folder is plain.
	if hits := f.markerHits(); len(hits) != 0 {
		t.Fatalf("still plain: %v", hits)
	}
	f.reopen()
	if len(f.s.Requests(RequestFilter{})) == 0 {
		t.Fatal("lost")
	}
}

// 10. A foreign write is red, never believed and never overwritten.
func TestReqStoreForeignWritesAreRed(t *testing.T) {
	for _, secure := range []bool{true, false} {
		key := []byte(nil)
		if secure {
			key = testKey(4)
		}
		cases := map[string]struct {
			plant func(t *testing.T, path, dir string, old []byte)
			want  []string
		}{
			"edited index": {func(t *testing.T, path, _ string, old []byte) {
				write(t, path, strings.Replace(string(old), `"state":"waiting"`, `"state":"done"`, 1))
			}, []string{"I12"}},
			"plain record": {func(t *testing.T, path, _ string, _ []byte) {
				write(t, path, `{"v":1,"index":{"id":"x"},"content":{}}`)
			}, []string{"I12"}},
			"removed": {func(t *testing.T, path, _ string, _ []byte) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}, []string{"I12", "missing"}},
			"unknown file": {func(t *testing.T, _, dir string, old []byte) {
				write(t, filepath.Join(dir, "r-planted.rec"), string(old))
			}, []string{"I12", "unknown"}},
			"unopenable": {func(t *testing.T, path, _ string, _ []byte) {
				write(t, path, `{"v":1,"index":{},"sealed":"DWDAT2\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n"}`)
			}, []string{"I17"}},
		}
		for name, tc := range cases {
			f := newReqFx(t, key)
			f.supervise("a", "claude-code")
			v := f.create(ReqAsk, func(in *CreateInput) { in.Key = "operator:the-one" })
			dir := filepath.Join(f.data, "queue.d")
			path := filepath.Join(dir, v.ID+".rec")
			old, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.s.Candidates(bg, f.now); err != nil {
				t.Fatalf("%s: red before the fault: %v", name, err)
			}
			tc.plant(t, path, dir, old)
			f.reopen()
			planted := f.files()
			_, err = f.s.Candidates(bg, f.now)
			if !errors.Is(err, ErrRecords) {
				t.Errorf("secure=%v %s: not red: %v", secure, name, err)
				continue
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("secure=%v %s: %q lacks %q", secure, name, err, w)
				}
			}
			if err := f.s.Tick(bg); !errors.Is(err, ErrRecords) {
				t.Errorf("%s: tick: %v", name, err)
			}
			if len(f.recorded) == 0 {
				t.Errorf("%s: no alarm", name)
			}
			if _, _, err := f.s.Create(bg, opCaller, CreateInput{Kind: ReqAsk, Role: RoleSupervisor, Scope: projScope, Key: "operator:new"}); !errors.Is(err, ErrRecords) {
				t.Errorf("%s: create: %v", name, err)
			}
			if name != "unknown file" {
				// It never reads "not there".
				if _, err := f.s.Get(bg, opCaller, v.ID); !errors.Is(err, ErrRecords) {
					t.Errorf("%s: get: %v", name, err)
				}
				if has, err := f.s.Has(bg, "operator:the-one"); !has && err == nil {
					t.Errorf("%s: has said there is none", name)
				}
				if _, err := f.s.LoadQueue(); !errors.Is(err, ErrRecords) {
					t.Errorf("%s: queue: %v", name, err)
				}
			}
			same(t, planted, f.files())
		}
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 11. The import loses nothing, survives an interruption, and an export of a
// secure store needs the plain flag.
func TestReqImportAndExport(t *testing.T) {
	const old = `[{"id":"a1","role":"r","state":"pending","payload":{"n":1},"created_at":"2026-01-01T00:00:00Z"},
{"id":"b2","role":"r","state":"claimed","claimed_by":"s","payload":{},"created_at":"2026-01-02T00:00:00Z","future_field":true}]`
	f := newReqFx(t, testKey(6))
	queue := filepath.Join(f.data, "queue.json")
	write(t, queue, old)
	// Interrupted after the first item.
	items := 0
	f.fail = func(path string) error {
		if strings.HasPrefix(filepath.Base(path), queuePrefix) {
			items++
			if items > 1 {
				return errors.New("no space")
			}
		}
		return nil
	}
	f.reopen()
	if _, err := f.s.ImportQueue(queue); !errors.Is(err, ErrCannotRecord) {
		t.Fatalf("interrupted: %v", err)
	}
	if body, _ := os.ReadFile(queue); string(body) != old {
		t.Fatal("queue.json changed before everything was read back")
	}
	f.fail = nil
	f.reopen()
	n, err := f.s.ImportQueue(queue)
	if err != nil || n != 2 {
		t.Fatalf("import: %d %v", n, err)
	}
	if body, _ := os.ReadFile(queue); string(body) != QueueMarker {
		t.Fatalf("queue.json: %q", body)
	}
	if json.Valid([]byte(QueueMarker)) {
		t.Fatal("the marker must not parse")
	}
	f.reopen()
	if n, err := f.s.ImportQueue(queue); err != nil || n != 0 {
		t.Fatalf("again: %d %v", n, err)
	}
	got, err := f.s.LoadQueue()
	if err != nil || len(got) != 2 || !strings.Contains(string(got[1]), "future_field") {
		t.Fatalf("lost: %s %v", got, err)
	}
	if _, err := f.s.ExportQueue(queue, false); err == nil {
		t.Fatal("a secure export without the plain flag")
	}
	if body, _ := os.ReadFile(queue); string(body) != QueueMarker {
		t.Fatal("the refused export wrote")
	}
	if n, err := f.s.ExportQueue(queue, true); err != nil || n != 2 {
		t.Fatalf("export: %d %v", n, err)
	}
	var back []map[string]any
	body, _ := os.ReadFile(queue)
	if err := json.Unmarshal(body, &back); err != nil || len(back) != 2 || back[0]["id"] != "a1" {
		t.Fatalf("exported %s: %v", body, err)
	}
	// The older program ran and changed its queue: its file wins.
	write(t, queue, `[{"id":"c3","role":"r","state":"pending","payload":{}}]`)
	if n, err := f.s.ImportQueue(queue); err != nil || n != 1 {
		t.Fatalf("import again: %d %v", n, err)
	}
	if got, _ := f.s.LoadQueue(); len(got) != 1 {
		t.Fatalf("items deleted by the older program came back: %s", got)
	}
	// A damaged queue.json stops the import and stays.
	write(t, queue, "[{")
	if _, err := f.s.ImportQueue(queue); err == nil {
		t.Fatal("imported a damaged file")
	}
	if got, _ := f.s.LoadQueue(); len(got) != 1 {
		t.Fatal("a damaged file emptied the queue")
	}
	// With no queue.json the marker is still put there.
	g := newReqFx(t, nil)
	path := filepath.Join(g.data, "queue.json")
	if _, err := g.s.ImportQueue(path); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(path); string(body) != QueueMarker {
		t.Fatal("no marker: an older datawatch would start with an empty queue")
	}
}

// 12. A save that fails: nothing happens.
func TestReqStoreCannotRecord(t *testing.T) {
	f := newReqFx(t, testKey(8))
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	full := func(string) error { return errors.New("no space left on device") }
	f.fail = full
	calls := f.notif.calls
	_, created, err := f.s.Create(bg, opCaller, CreateInput{Kind: ReqAsk, Role: RoleSupervisor, Scope: projScope, Key: "operator:full"})
	if !errors.Is(err, ErrCannotRecord) || created {
		t.Fatalf("create: %v %v", created, err)
	}
	if has, err := f.s.Has(bg, "operator:full"); has || err != nil {
		t.Fatalf("has: %v %v", has, err)
	}
	if _, err := f.s.Take(bg, sessCaller("a"), v.ID, TakeInput{}); !errors.Is(err, ErrCannotRecord) {
		t.Fatalf("take: %v", err)
	}
	if r := f.want(v.ID, StateWaiting); r.Delivery.FetchedAt != nil {
		t.Fatal("an unsaved take happened")
	}
	if _, err := f.s.Change(v.ID, func(r *Request) error { r.Delivery.TypeClaimAt = &f.now; return nil }); !errors.Is(err, ErrCannotRecord) {
		t.Fatalf("change (a typing claim): %v", err)
	}
	if err := f.s.AddNotice(RequestNotice{At: f.now, SessionID: "a"}); !errors.Is(err, ErrCannotRecord) {
		t.Fatalf("notice: %v", err)
	}
	f.now = f.now.Add(5 * time.Minute)
	if err := f.s.Tick(bg); !errors.Is(err, ErrCannotRecord) {
		t.Fatalf("tick: %v", err)
	}
	if f.notif.calls != calls {
		t.Fatal("reminders ran while nothing can be saved")
	}
	alarms := 0
	for _, c := range f.recorded {
		if c.Kind == KindAlarm && c.ID == "requests_cannot_record" {
			alarms++
		}
	}
	if alarms != 1 {
		t.Fatalf("%d alarms, want 1", alarms)
	}
	if err := f.s.Healthy(); !errors.Is(err, ErrCannotRecord) {
		t.Fatalf("healthy: %v", err)
	}
	// The heartbeat goes stale.
	e, err := New(Options{Dir: filepath.Join(f.data, "delivery"), Now: func() time.Time { return f.now }, Healthy: f.s.Healthy})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pass(context.Background()); !errors.Is(err, ErrCannotRecord) {
		t.Fatalf("pass: %v", err)
	}
	beat := filepath.Join(f.data, "delivery", HeartbeatFile)
	if _, err := os.Stat(beat); err == nil {
		t.Fatal("a heartbeat was written while requests cannot be saved")
	}
	f.fail = nil
	if err := f.s.Healthy(); err != nil {
		t.Fatalf("after the disk has room: %v", err)
	}
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(beat); err != nil {
		t.Fatal(err)
	}
}

// A write that fails after the commit point stands, and is finished later,
// also by a restart.
func TestReqStoreInterruptedWriteIsFinished(t *testing.T) {
	f := newReqFx(t, testKey(8))
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	f.fail = func(path string) error {
		if filepath.Base(path) != metaFileName {
			return errors.New("no space")
		}
		return nil
	}
	f.take(v.ID, "a")
	f.want(v.ID, StateDelivered)
	if err := f.s.Healthy(); !errors.Is(err, ErrCannotRecord) {
		t.Fatalf("healthy: %v", err)
	}
	f.reopen() // a restart before the files were written
	f.want(v.ID, StateDelivered)
	f.fail = nil
	f.tick(time.Minute)
	f.reopen()
	f.want(v.ID, StateDelivered)
	if _, err := f.s.Candidates(bg, f.now); err != nil {
		t.Fatal(err)
	}
}

// 13. A clock jump or a hold never mass-expires.
func TestReqStateClockJumpAndMassExpiry(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	f.tick(5 * time.Minute)
	before := f.get(v.ID).AgeSeconds
	f.tick(72 * time.Hour)
	if got := f.get(v.ID).AgeSeconds - before; got != 600 {
		t.Fatalf("a 3-day jump added %d s, want two intervals", got)
	}
	f.tick(-96 * time.Hour)
	if got := f.get(v.ID).AgeSeconds - before; got != 600 {
		t.Fatalf("a backward jump changed the age: %d", got)
	}

	g := newReqFx(t, nil)
	g.supervise("a", "claude-code")
	var ids []string
	for i := 0; i < maxExpiringPerPass+1; i++ {
		ids = append(ids, g.create(ReqAsk, func(in *CreateInput) { in.Due = 7 * time.Minute }).ID)
	}
	g.run(30 * time.Minute)
	for _, id := range ids {
		g.want(id, StateWaiting)
	}
	alarms := 0
	for _, c := range g.recorded {
		if c.ID == "requests_mass_expiry" {
			alarms++
		}
	}
	if alarms != 1 {
		t.Fatalf("%d mass-expiry alarms, want 1", alarms)
	}
	if l := g.listed(); l[ids[0]].Problem != WhyDuePassed {
		t.Fatalf("held-back requests are listed: %+v", l[ids[0]])
	}
	// One fewer may expire.
	if _, err := g.s.Drop(bg, opCaller, ids[0], ReasonInput{ReasonCode: "x"}); err != nil {
		t.Fatal(err)
	}
	g.tick(5 * time.Minute)
	g.want(ids[1], StateExpired)
}

func TestReqStateAHoldStopsTheClocks(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	x, y := f.create(ReqAsk), f.create(ReqAsk)
	f.holds.cause["a"] = WhyHeldUsage
	f.run(30 * time.Hour)
	f.want(x.ID, StateWaiting)
	f.want(y.ID, StateWaiting)
	l := f.listed()
	if len(l) != 1 || l["held:"+WhyHeldUsage].Count != 2 || l["held:"+WhyHeldUsage].Problem != WhyHeldUsage {
		t.Fatalf("one grouped item per cause: %+v", l)
	}
	if r := f.get(x.ID); r.HeldSince == nil || r.HeldCause != WhyHeldUsage {
		t.Fatalf("%+v", r)
	}
	delete(f.holds.cause, "a")
	f.tick(5 * time.Minute)
	if r := f.want(x.ID, StateWaiting); r.HeldSince != nil || r.Overdue {
		t.Fatalf("%+v", r)
	}
	f.run(25 * time.Hour)
	f.want(x.ID, StateExpired)
}

// Never-accepted work past due expires; accepted work is only marked overdue.
func TestReqStateDuePassed(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	w, d, a := f.create(ReqAsk), f.create(ReqAsk), f.create(ReqAsk)
	f.take(d.ID, "a")
	f.take(a.ID, "a")
	f.accept(a.ID, "a")
	f.run(25 * time.Hour)
	f.want(w.ID, StateExpired)
	f.want(d.ID, StateExpired)
	if r := f.want(a.ID, StateAccepted); !r.Overdue {
		t.Fatalf("%+v", r)
	}
	if l := f.listed(); l[a.ID].Problem != WhyDuePassed || l[a.ID].WaitsOn != "session:a" || l[w.ID].Problem != WhyDuePassed {
		t.Fatalf("%+v", l)
	}
}

// 14. Other computers, parents and the backlog.
func TestReqStateOtherComputersAreRefused(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	before := f.files()
	in := CreateInput{Kind: ReqAsk, Role: RoleSupervisor, Scope: projScope, Key: "operator:far", Host: "there"}
	if _, _, err := f.s.Create(bg, opCaller, in); !errors.Is(err, ErrCrossHost) {
		t.Fatalf("other host: %v", err)
	}
	peer := Caller{Kind: CallerPeer, Peer: "p", SessionID: "a", Surface: SurfaceREST}
	in.Host = ""
	calls := map[string]func() error{
		"create": func() error { _, _, e := f.s.Create(bg, peer, in); return e },
		"take":   func() error { _, e := f.s.Take(bg, peer, v.ID, TakeInput{}); return e },
		"seen":   func() error { _, e := f.s.Seen(bg, peer, v.ID, SeenInput{}); return e },
		"accept": func() error { _, e := f.s.Accept(bg, peer, v.ID, AcceptInput{}); return e },
		"report": func() error { _, e := f.s.Report(bg, peer, v.ID, ReportInput{}); return e },
		"decline": func() error {
			_, e := f.s.Decline(bg, peer, v.ID, DeclineInput{ReasonCode: "x"})
			return e
		},
		"mine":  func() error { _, e := f.s.Mine(bg, peer); return e },
		"get":   func() error { _, e := f.s.Get(bg, peer, v.ID); return e },
		"list":  func() error { _, e := f.s.List(bg, peer, RequestFilter{}); return e },
		"roles": func() error { _, e := f.s.Roles(bg, peer); return e },
		"give": func() error {
			_, e := f.s.GiveRole(bg, peer, RoleInput{Role: RoleSupervisor, Scope: projScope, SessionID: "a"})
			return e
		},
		"release": func() error { return f.s.ReleaseRole(bg, peer, RoleInput{Role: RoleSupervisor, Scope: projScope}) },
		"reissue": func() error { _, e := f.s.Reissue(bg, peer, v.ID, ReasonInput{ReasonCode: "x"}); return e },
		"drop":    func() error { _, e := f.s.Drop(bg, peer, v.ID, ReasonInput{ReasonCode: "x"}); return e },
		"force":   func() error { _, e := f.s.Force(bg, peer, v.ID, ForceInput{ReasonCode: "x"}); return e },
		"unproven": func() error {
			_, e := f.s.AcceptUnproven(bg, peer, v.ID, ReasonInput{ReasonCode: "x"})
			return e
		},
		"backlog": func() error {
			_, _, e := f.s.FromBacklog(bg, peer, BacklogInput{EntryKey: "bug:1:open", Role: RoleSupervisor, Scope: projScope})
			return e
		},
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, ErrCrossHost) {
			t.Errorf("%s: %v", name, err)
		}
	}
	same(t, before, f.files())
}

func TestReqStateParentAndBacklog(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	parent := f.create(ReqAsk)
	child := f.create(ReqAsk, func(in *CreateInput) { in.After = parent.ID })
	grand := f.create(ReqAsk, func(in *CreateInput) { in.After = child.ID })
	if r := f.want(child.ID, StateWaiting); r.Holder != nil {
		t.Fatal("a child went out before its parent was done")
	}
	if _, err := f.s.Drop(bg, opCaller, parent.ID, ReasonInput{ReasonCode: "x"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{child.ID, grand.ID} {
		if r := f.want(id, StateFailed); r.ReasonCode != WhyParentClosed {
			t.Fatalf("%+v", r)
		}
		if _, ok := f.listed()[id]; !ok {
			t.Fatal("the failed child is not listed")
		}
	}
	late := f.create(ReqAsk, func(in *CreateInput) { in.After = parent.ID })
	f.want(late.ID, StateFailed)
	if _, _, err := f.s.Create(bg, opCaller, CreateInput{Kind: ReqAsk, Role: RoleSupervisor, Scope: projScope, Key: "operator:orphan", After: "r-none"}); !errors.Is(err, ErrRequestInput) {
		t.Fatalf("%v", err)
	}
	// A done parent releases its child.
	p2 := f.create(ReqAsk)
	c2 := f.create(ReqAsk, func(in *CreateInput) { in.After = p2.ID })
	f.take(p2.ID, "a")
	f.accept(p2.ID, "a")
	f.report(p2.ID, "a")
	f.ver.set(p2.ID, Verdict{Status: "done", Closes: true})
	f.tick(time.Minute)
	if r := f.want(c2.ID, StateWaiting); r.Holder == nil {
		t.Fatal("the child of a done request stays unassigned")
	}

	f.backlog["bug:42:open"] = 3
	in := BacklogInput{EntryKey: "bug:42:open", Role: RoleSupervisor, Scope: projScope, Content: RequestContent{Ask: "look"}}
	a, made, err := f.s.FromBacklog(bg, opCaller, in)
	if err != nil || !made {
		t.Fatalf("%v %v", made, err)
	}
	delete(f.backlog, "bug:42:open") // the entry left the list
	b, made, err := f.s.FromBacklog(bg, opCaller, in)
	if err != nil || made || b.ID != a.ID {
		t.Fatalf("twice: %v %v %s", made, err, b.ID)
	}
	in.EntryKey = "bug:none:open"
	if _, _, err := f.s.FromBacklog(bg, opCaller, in); !errors.Is(err, ErrRequestInput) {
		t.Fatalf("an unknown entry: %v", err)
	}
}

func TestReqStoreRefusesWhatCannotBeReadable(t *testing.T) {
	f := newReqFx(t, nil)
	base := func() CreateInput {
		return CreateInput{Kind: ReqAsk, Role: RoleSupervisor, Scope: projScope, Key: "operator:v"}
	}
	for name, tc := range map[string]struct {
		mod  func(*CreateInput)
		want error
	}{
		"scope with a path":   {func(in *CreateInput) { in.Scope.ID = "/srv/x" }, ErrRequestInput},
		"source with words":   {func(in *CreateInput) { in.Source = SourceRef{Type: "t", ID: "some words"} }, ErrRequestInput},
		"unknown kind":        {func(in *CreateInput) { in.Kind = "nope" }, ErrRequestInput},
		"no key":              {func(in *CreateInput) { in.Key = "" }, ErrRequestInput},
		"negative due":        {func(in *CreateInput) { in.Due = -1 }, ErrRequestInput},
		"a command item":      {func(in *CreateInput) { in.Content.Items = []DoneItem{{Method: "command"}} }, ErrCheckMethod},
		"session role, other": {func(in *CreateInput) { in.Role = RoleSession }, ErrRequestInput},
	} {
		in := base()
		tc.mod(&in)
		if _, _, err := f.s.Create(bg, opCaller, in); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	f.ver.prepErr = ErrCheckMethod
	if _, _, err := f.s.Create(bg, opCaller, base()); !errors.Is(err, ErrCheckMethod) {
		t.Fatalf("a list that cannot be made: %v", err)
	}
	if n := len(f.s.Requests(RequestFilter{})); n != 0 {
		t.Fatalf("%d saved", n)
	}
	if _, err := OccurrenceKey("schedule", "s1", "two words"); err == nil {
		t.Fatal("words in a key")
	}
	if _, err := OccurrenceKey("schedule"); err == nil {
		t.Fatal("an empty key")
	}
	if k, err := OccurrenceKey("schedule", "s1", "100"); err != nil || k != "schedule:s1:100" {
		t.Fatalf("%q %v", k, err)
	}
}

// 15. Each invariant is red on a planted fault and green without it.
func TestReqStateInvariants(t *testing.T) {
	plant := map[string]struct {
		rule  string
		fault func(f *reqFx, id string)
	}{
		"I1 no deadline":  {"I1", func(f *reqFx, id string) { f.s.reqs[id].index.DueSeconds = 0 }},
		"I2 past take-by": {"I2", func(f *reqFx, id string) { f.s.reqs[id].index.AgeSeconds = 7200 }},
		"I3 failed": {"I3", func(f *reqFx, id string) {
			f.s.reqs[id].index.State = StateFailed
		}},
		"I3 settled by a session": {"I3", func(f *reqFx, id string) {
			r := &f.s.reqs[id].index
			r.State, r.SettledAt, r.SettledBy = StateFailed, &f.now, "session:a"
		}},
		"I4 done with no proof": {"I4", func(f *reqFx, id string) { f.s.reqs[id].index.State = StateDone }},
		"I6 ended holder": {"I6", func(f *reqFx, id string) {
			f.s.reqs[id].index.State = StateDelivered
			f.sessions.end("a")
		}},
		"I7 two holders of a role": {"I7", func(f *reqFx, _ string) {
			b := f.s.meta.Bindings[0]
			b.SessionID = "other"
			f.s.meta.Bindings = append(f.s.meta.Bindings, b)
		}},
		"I7 a call from somebody else": {"I7", func(f *reqFx, id string) {
			r := &f.s.reqs[id].index
			r.Calls = append(r.Calls, CallRecord{Verb: VerbReport, Observed: "session:z"})
		}},
		"I8 no holder": {"I8", func(f *reqFx, id string) {
			r := &f.s.reqs[id].index
			r.Holder, r.AgeSeconds = nil, 7200
		}},
		"I9 two with one key": {"I9", func(f *reqFx, id string) {
			c := f.s.reqs[id].clone()
			c.index.ID = "r-copy"
			f.s.reqs["r-copy"] = c
		}},
		"I13 too many reminders": {"I13", func(f *reqFx, id string) { f.s.reqs[id].index.Delivery.Reminders = 3 }},
		"I13 typed with no claim": {"I13", func(f *reqFx, id string) {
			f.s.reqs[id].index.Delivery.ContentTypedAt = &f.now
		}},
		"I13 over the hourly limit": {"I13", func(f *reqFx, _ string) {
			for i := 0; i < 3; i++ {
				f.s.meta.Notices = append(f.s.meta.Notices, RequestNotice{At: f.now, SessionID: "a"})
			}
		}},
	}
	for name, p := range plant {
		f := newReqFx(t, nil)
		f.supervise("a", "claude-code")
		v := f.create(ReqAsk)
		if l := f.listed(); len(l) != 0 {
			t.Fatalf("%s: red without the fault: %+v", name, l)
		}
		p.fault(f, v.ID)
		found := false
		cs, err := f.s.Candidates(bg, f.now)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cs {
			found = found || c.Invariant == p.rule
		}
		if !found {
			t.Errorf("%s: no %s entry in %+v", name, p.rule, cs)
		}
	}
}

// The second guard, also where the manifest agrees: a readable index that
// differs from the sealed copy is never believed.
func TestReqStoreReadableIndexMustMatchTheSealedCopy(t *testing.T) {
	f := newReqFx(t, testKey(4))
	v := f.create(ReqAsk)
	data, err := os.ReadFile(filepath.Join(f.data, "queue.d", v.ID+".rec"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := f.s.decodeRecord(data); err != nil {
		t.Fatalf("before the edit: %v", err)
	}
	edited := strings.Replace(string(data), `"state":"waiting"`, `"state":"done"`, 1)
	if _, _, rule, err := f.s.decodeRecord([]byte(edited)); err == nil || rule != "I12" {
		t.Fatalf("believed an edited index: %q %v", rule, err)
	}
}
