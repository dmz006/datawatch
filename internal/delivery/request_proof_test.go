package delivery

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/acceptance"
)

type cxRunner struct{ ran int }

func (r *cxRunner) Run(context.Context, acceptance.CheckSpec, acceptance.Subject, time.Duration) acceptance.RunOutcome {
	r.ran++
	return acceptance.RunOutcome{Finished: true}
}

type cxProofFx struct {
	t       *testing.T
	b       *cxBook
	clock   *cxClock
	mode    string
	repo    string
	storeAt string
	lists   *acceptance.Service
	v       Verifier
	runner  *cxRunner
}

func cxProof(t *testing.T) *cxProofFx {
	t.Helper()
	f := &cxProofFx{t: t, b: cxNewBook(), clock: &cxClock{t: cxStart}, mode: acceptance.ModeReport,
		repo: t.TempDir(), storeAt: filepath.Join(t.TempDir(), "store"), runner: &cxRunner{}}
	f.git("init", "-q", "-b", "main")
	f.commit("README", "start")
	book := func() RequestBook { return f.b }
	lists, err := acceptance.Open(acceptance.Options{
		Path: filepath.Join(f.storeAt, "acceptance.json"), Mode: func() string { return f.mode },
		Runner: f.runner, Resolver: NewRequestResolver(book),
		Probes: map[string]acceptance.Probe{acceptance.MethodReceipt: NewReceiptProbe(book)},
		Now:    f.clock.now,
	})
	cxNoErr(t, err)
	f.lists, f.v = lists, NewListsVerifier(lists)
	return f
}

func (f *cxProofFx) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", f.repo, "-c", "user.name=t", "-c", "user.email=t@example.invalid",
		"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *cxProofFx) commit(name, text string) {
	f.t.Helper()
	cxNoErr(f.t, os.WriteFile(filepath.Join(f.repo, name), []byte(text), 0o600))
	f.git("add", name)
	f.git("commit", "-q", "--allow-empty", "-m", "c")
}

var cxFileItem = DoneItem{ID: "f1", Statement: "out.txt says ready", From: "Write the file.", Method: "state:file",
	Path: "out.txt", Contains: "ready", Effect: "work"}

// create does what part A does: the list is prepared before the request is
// saved.
func (f *cxProofFx) create(id string, kind RequestKind, items ...DoneItem) Request {
	f.t.Helper()
	r := Request{ID: id, Kind: kind, Role: RoleSupervisor, Scope: cxProject("p1"), State: StateWaiting, Created: f.clock.now()}
	c := RequestContent{Ask: "Write the file.", ProjectDir: f.repo, Items: items}
	cxNoErr(f.t, f.v.Prepare(context.Background(), r, c))
	f.b.put(r, c)
	return r
}

// holds gives the request to a session, as assignment does.
func (f *cxProofFx) holds(id, session, family string) {
	f.t.Helper()
	cxNoErr(f.t, f.v.Holder(context.Background(), id, session))
	r := f.b.reqs[id]
	r.Holder = &Holder{SessionID: session, Family: family, Since: f.clock.now()}
	r.Delivery = DeliveryRecord{}
}

// take records a take by the holder's own credential.
func (f *cxProofFx) take(id string) {
	f.clock.add(1500 * time.Microsecond)
	r := f.b.reqs[id]
	at := f.clock.now()
	r.Delivery.FetchedAt = &at
	r.Calls = append(r.Calls, CallRecord{At: at, Verb: VerbTake, Observed: "session:" + r.Holder.SessionID})
}

func (f *cxProofFx) seen(id string) {
	f.clock.add(time.Millisecond)
	r := f.b.reqs[id]
	at := f.clock.now()
	r.Delivery.InModelCallAt = &at
	r.Calls = append(r.Calls, CallRecord{At: at, Verb: VerbSeen, Observed: "session:" + r.Holder.SessionID})
}

func (f *cxProofFx) check(id string) Verdict {
	f.t.Helper()
	f.clock.add(time.Second)
	r, _ := f.b.Request(id)
	v, err := f.v.Check(context.Background(), r)
	cxNoErr(f.t, err)
	for _, code := range []string{v.Status, v.Reason} {
		if code != "" && CheckReasonCode(code) != nil {
			f.t.Fatalf("status and reason must be codes: %+v", v)
		}
	}
	return v
}

func (f *cxProofFx) list(id string) *acceptance.List {
	f.t.Helper()
	l, err := f.lists.Get(id)
	cxNoErr(f.t, err)
	return l
}

var cxPerson = acceptance.Caller{Observed: "admin-token"}

func (f *cxProofFx) coverage(id string) {
	f.t.Helper()
	_, err := f.lists.Confirm(id, "list", true, "it covers the ask", "", f.list(id).Hash(), cxPerson)
	cxNoErr(f.t, err)
}

func TestReqProofTheListIsMadeAndFixedBeforeDelivery(t *testing.T) {
	f := cxProof(t)
	f.create("r-1", ReqAsk, cxFileItem)
	l := f.list("r-1")
	if !l.Frozen() || !l.SourceKnown || l.Occurrence != "" || l.Scheduled || l.History[0].By.Observed != "datawatch" {
		t.Fatalf("%+v", l)
	}
	var file *acceptance.Item
	for i := range l.Items {
		if l.Items[i].ID == "f1" {
			file = &l.Items[i]
		}
	}
	if file == nil || !file.Change || file.Before != acceptance.BeforeDiffer {
		t.Fatalf("a file item must show a change: %+v", file)
	}
	if st := l.State["f1"]; st == nil || st.Start == nil || !st.Start.Recorded {
		t.Fatalf("the start value was not saved: %+v", l.State["f1"])
	}
	// Preparing datawatch's own list again is harmless.
	cxNoErr(t, f.v.Prepare(context.Background(), *f.b.reqs["r-1"], f.b.content["r-1"]))
}

func TestReqProofAnOldFileOrCommitDoesNotPass(t *testing.T) {
	f := cxProof(t)
	f.commit("out.txt", "ready")
	old := cxFileItem
	f.create("r-1", ReqAsk, old)
	f.holds("r-1", "s1", "claude-code")
	f.coverage("r-1")
	if v := f.check("r-1"); v.Closes {
		t.Fatalf("an old file passed: %+v", v)
	}
	f.commit("other.txt", "unrelated")
	if v := f.check("r-1"); v.Closes {
		t.Fatalf("an unrelated commit passed it: %+v", v)
	}
}

func TestReqProofAChangeNeedsACommitAndCoverage(t *testing.T) {
	f := cxProof(t)
	f.create("r-1", ReqAsk, cxFileItem)
	f.holds("r-1", "s1", "claude-code")
	cxNoErr(t, os.WriteFile(filepath.Join(f.repo, "out.txt"), []byte("ready"), 0o600))
	if v := f.check("r-1"); v.Closes || !v.NotMet || v.Reason != WhyProofFailed {
		t.Fatalf("an uncommitted file passed: %+v", v)
	}
	f.commit("out.txt", "ready")
	v := f.check("r-1")
	if v.Closes || v.NotMet || v.Status != string(acceptance.StatusNotReviewed) || v.Reason != WhyWaitingOnYou {
		t.Fatalf("without coverage it reads not reviewed, not done: %+v", v)
	}
	f.coverage("r-1")
	if v := f.check("r-1"); !v.Closes || v.Status != string(acceptance.StatusDone) {
		t.Fatalf("%+v", v)
	}
	if f.runner.ran != 0 {
		t.Fatalf("%d checks ran", f.runner.ran)
	}
}

func TestReqProofARefEffectReadsThatBranch(t *testing.T) {
	f := cxProof(t)
	f.git("branch", "release")
	item := cxFileItem
	item.Effect = "ref:release"
	f.create("r-1", ReqAsk, item)
	f.holds("r-1", "s1", "claude-code")
	f.coverage("r-1")
	f.commit("out.txt", "ready") // on main only
	if v := f.check("r-1"); v.Closes {
		t.Fatalf("main passed an item about release: %+v", v)
	}
	f.git("branch", "-f", "release", "main")
	if v := f.check("r-1"); !v.Closes {
		t.Fatalf("%+v", v)
	}
}

func TestReqProofNothingADoerSaysCloses(t *testing.T) {
	f := cxProof(t)
	f.create("r-1", ReqAsk, DoneItem{ID: "c1", Statement: "A person saw it", From: "Write the file.", Method: "confirm"})
	f.holds("r-1", "s1", "claude-code")
	for name, c := range map[string]acceptance.Caller{
		"the holder":                              {Observed: "session:s1", SessionID: "s1"},
		"the daemon token naming the holder":      {Observed: "admin-token from session:s1", SessionID: "s1"},
		"an unnamed session":                      {Observed: "session:unknown", ViaTool: true},
		"the holder naming itself another person": {Claimed: "operator", Observed: "session:s1", SessionID: "s1"},
	} {
		_, err := f.lists.Confirm("r-1", "c1", true, "done", "", "", c)
		cxNoErr(t, err)
		if v := f.check("r-1"); v.Closes {
			t.Fatalf("%s closed it", name)
		}
	}
	f.coverage("r-1")
	ctx := context.Background()
	r, _ := f.b.Request("r-1")
	in := ReasonInput{ReasonCode: "good_enough", Words: "it is fine"}
	for name, c := range map[string]Caller{
		"the holder":                    {Kind: CallerSession, SessionID: "s1", Surface: SurfaceMCP},
		"an unnamed session":            {Kind: CallerSessionUnknown, Surface: SurfaceREST},
		"the token naming the holder":   {Kind: CallerOperator, Surface: SurfaceREST, ClaimedSession: "s1"},
		"the token through a tool call": {Kind: CallerOperator, Surface: SurfaceMCP},
		// A claimed session never raises what counts.
		"a tool call naming a stranger": {Kind: CallerOperator, Surface: SurfaceMCP, ClaimedSession: "s9"},
	} {
		v, err := f.v.Waive(ctx, c, r, in)
		cxNoErr(t, err)
		if v.Closes || f.check("r-1").Closes {
			t.Fatalf("%s waived it: %+v", name, v)
		}
	}
	if _, err := f.v.Waive(ctx, cxOp, r, ReasonInput{ReasonCode: "x"}); !errors.Is(err, ErrRequestInput) {
		t.Fatalf("a waiver keeps the words: %v", err)
	}
	v, err := f.v.Waive(ctx, cxOp, r, in)
	cxNoErr(t, err)
	if got := f.check("r-1"); !got.Closes || got.Status != string(acceptance.StatusDoneAccepted) || got.Status != v.Status {
		t.Fatalf("%+v %+v", v, got)
	}
	// A later claim does not take the counted acceptance away.
	_, err = f.v.Waive(ctx, Caller{Kind: CallerSession, SessionID: "s1"}, r, in)
	cxNoErr(t, err)
	if !f.check("r-1").Closes {
		t.Fatal("a doer's claim replaced a counted acceptance")
	}
}

func TestReqProofDeliveryOnlyClosesOnACountedReceipt(t *testing.T) {
	for _, family := range []string{"claude-code", "pi"} {
		f := cxProof(t)
		f.create("r-1", ReqHello)
		if l := f.list("r-1"); l.Ask.Text != "" || len(l.Items) != 1 || l.Items[0].Method != "receipt" {
			t.Fatalf("a delivery-only list has an empty ask: %+v", l.Items)
		}
		f.holds("r-1", "s1", family)
		if v := f.check("r-1"); v.Closes {
			t.Fatalf("%s: closed before anything: %+v", family, v)
		}
		typed := f.clock.now()
		f.b.reqs["r-1"].Delivery.ContentTypedAt = &typed
		if v := f.check("r-1"); v.Closes {
			t.Fatalf("%s: typing closed it: %+v", family, v)
		}
		f.take("r-1")
		if family == "pi" {
			if v := f.check("r-1"); v.Closes || v.NotMet {
				t.Fatalf("a pi fetch alone: %+v", v)
			}
			f.seen("r-1")
		}
		saved := f.b.reqs["r-1"].Calls
		f.b.reqs["r-1"].Calls = []CallRecord{{Verb: VerbTake, Observed: "admin-token"},
			{Verb: VerbTake, Observed: "session:s1", Refused: "not_holder"},
			{Verb: VerbSeen, Observed: "session:s2"}}
		if v := f.check("r-1"); v.Closes {
			t.Fatalf("%s: no observed caller: %+v", family, v)
		}
		f.b.reqs["r-1"].Calls = saved
		v := f.check("r-1")
		if !v.Closes || v.Status != string(acceptance.StatusDelivered) {
			t.Fatalf("%s: %+v", family, v)
		}
	}
}

func TestReqProofReceiptReading(t *testing.T) {
	f := cxProof(t)
	f.create("r-1", ReqHello)
	f.holds("r-1", "s1", "claude-code")
	f.take("r-1")
	probe := NewReceiptProbe(func() RequestBook { return f.b })
	got, err := probe.Read(context.Background(), acceptance.ProbeRequest{RequestID: "r-1"})
	cxNoErr(t, err)
	if !got.Answered || got.Value != "fetched" || got.By != "s1" || got.Occurrence != "" ||
		!got.SourceAt.Equal(*f.b.reqs["r-1"].Delivery.FetchedAt) || got.SourceAt.Nanosecond() == 0 {
		t.Fatalf("%+v", got)
	}
	for _, p := range []acceptance.Probe{probe, NewReceiptProbe(func() RequestBook { return nil })} {
		if got, err := p.Read(context.Background(), acceptance.ProbeRequest{RequestID: "none"}); err != nil || got.Answered {
			t.Fatalf("%+v %v", got, err)
		}
	}
	f.b.reqs["r-1"].Holder.Family = "pi" // fetched is not enough there
	if got, _ := probe.Read(context.Background(), acceptance.ProbeRequest{RequestID: "r-1"}); got.Value == "fetched" || got.Value == "confirmed" {
		t.Fatalf("a pi fetch read as a receipt: %+v", got)
	}
}

func TestReqProofANewHolderStartsAgain(t *testing.T) {
	f := cxProof(t)
	f.create("r-1", ReqHello)
	f.holds("r-1", "s1", "claude-code")
	f.take("r-1")
	oldAt := *f.b.reqs["r-1"].Delivery.FetchedAt
	if !f.check("r-1").Closes {
		t.Fatal("setup")
	}
	f.clock.add(time.Minute)
	f.holds("r-1", "s2", "claude-code")
	l := f.list("r-1")
	if len(l.Holders) != 1 || l.Holders[0] != "s2" || len(l.FormerHolders) != 1 || l.FormerHolders[0] != "s1" || l.ReplacedAt.IsZero() {
		t.Fatalf("%+v %+v %v", l.Holders, l.FormerHolders, l.ReplacedAt)
	}
	r := f.b.reqs["r-1"]
	r.Delivery = DeliveryRecord{FetchedAt: &oldAt}
	r.Calls = []CallRecord{{Verb: VerbTake, Observed: "session:s2"}}
	if v := f.check("r-1"); v.Closes {
		t.Fatalf("the old receipt counted: %+v", v)
	}
	f.take("r-1")
	if !f.check("r-1").Closes {
		t.Fatal("the new holder's own receipt must count")
	}
}

func TestReqProofAFormerHolderStaysADoer(t *testing.T) {
	f := cxProof(t)
	f.create("r-1", ReqAsk, DoneItem{ID: "c1", Statement: "A person saw it", From: "Write the file.", Method: "confirm"})
	// The lists were off when s1 held it, so only the request knows.
	f.holds("r-1", "s2", "claude-code")
	f.b.reqs["r-1"].FormerHolders = []string{"s1"}
	f.check("r-1")
	_, err := f.lists.Confirm("r-1", "c1", true, "yes", "", "", acceptance.Caller{Observed: "session:s1", SessionID: "s1"})
	cxNoErr(t, err)
	f.coverage("r-1")
	if v := f.check("r-1"); v.Closes {
		t.Fatalf("a former holder's answer counted: %+v", v)
	}
	if l := f.list("r-1"); len(l.Holders) != 1 || l.Holders[0] != "s2" {
		t.Fatalf("%+v", l.Holders)
	}
}

func TestReqProofListsOffNeverCloses(t *testing.T) {
	ctx := context.Background()
	r := Request{ID: "r-1", Kind: ReqHello}
	for name, v := range map[string]Verifier{"no lists": NewListsVerifier(nil), "off": nil} {
		if v == nil {
			f := cxProof(t)
			f.mode = acceptance.ModeOff
			v = f.v
		}
		cxNoErr(t, v.Prepare(ctx, r, RequestContent{}))
		cxNoErr(t, v.Holder(ctx, "r-1", "s1"))
		cxNoErr(t, v.Started(ctx, "r-1", ReqHello))
		got, err := v.Check(ctx, r)
		cxNoErr(t, err)
		if got.Closes || got.NotMet || got.Reason != WhyListsOff {
			t.Fatalf("%s: %+v", name, got)
		}
		if _, err := v.Waive(ctx, cxOp, r, ReasonInput{ReasonCode: "x", Words: "y"}); !errors.Is(err, ErrRequestState) {
			t.Fatalf("%s: %v", name, err)
		}
		if ok, _ := v.Rework(ctx, r); ok {
			t.Fatal(name)
		}
	}
	// Turned on after the request was made: it has no list and never closes.
	f := cxProof(t)
	f.mode = acceptance.ModeOff
	f.create("r-2", ReqHello)
	f.mode = acceptance.ModeReport
	f.holds("r-2", "s1", "claude-code")
	f.take("r-2")
	if v := f.check("r-2"); v.Closes || v.Reason != WhyNoList {
		t.Fatalf("%+v", v)
	}
}

func TestReqProofCommandItemsAreRefusedAndNothingRuns(t *testing.T) {
	f := cxProof(t)
	for _, method := range []string{"command", "project:tests", "state:live", "state:build_commit", "", "script"} {
		for _, mode := range []string{acceptance.ModeReport, acceptance.ModeOff} {
			f.mode = mode
			err := f.v.Prepare(context.Background(), Request{ID: "r-1", Kind: ReqAsk},
				RequestContent{Ask: "x", ProjectDir: f.repo, Items: []DoneItem{{Statement: "x", Method: method}}})
			if !errors.Is(err, ErrCheckMethod) {
				t.Fatalf("%q (%s): %v", method, mode, err)
			}
		}
	}
	f.mode = acceptance.ModeReport
	if _, err := f.lists.Get("r-1"); !errors.Is(err, acceptance.ErrNotFound) {
		t.Fatalf("a refused item left a list: %v", err)
	}
	if f.runner.ran != 0 {
		t.Fatalf("%d processes", f.runner.ran)
	}
	if out := (RefusingRunner{}).Run(context.Background(), acceptance.CheckSpec{Command: "true"}, acceptance.Subject{}, time.Second); out.Finished {
		t.Fatal("the refusing runner finished a run")
	}
}

func TestReqProofAListDatawatchDidNotMakeIsRefused(t *testing.T) {
	f := cxProof(t)
	ctx := context.Background()
	_, err := f.lists.Edit(ctx, acceptance.EditInput{RequestID: "r-1", Ask: "anything"}, cxPerson)
	cxNoErr(t, err)
	r := Request{ID: "r-1", Kind: ReqHello}
	if err := f.v.Prepare(ctx, r, RequestContent{}); !errors.Is(err, ErrRequestState) {
		t.Fatalf("%v", err)
	}
	cxNoErr(t, f.v.Holder(ctx, "r-1", "s1"))
	if l := f.list("r-1"); len(l.Holders) != 0 {
		t.Fatalf("it changed a list that is not its own: %+v", l.Holders)
	}
	got, err := f.v.Check(ctx, r)
	cxNoErr(t, err)
	if got.Closes || got.Reason != WhyNoList {
		t.Fatalf("%+v", got)
	}
}

func TestReqProofASaveThatFailsIsCannotRecord(t *testing.T) {
	f := cxProof(t)
	cxNoErr(t, os.WriteFile(f.storeAt, []byte("a file where the folder should be"), 0o600))
	err := f.v.Prepare(context.Background(), Request{ID: "r-1", Kind: ReqAsk}, RequestContent{Ask: "SECRET-CX-9"})
	if !errors.Is(err, ErrCannotRecord) || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("%v", err)
	}
}

func TestReqProofResolverOnlyAnswersForRequests(t *testing.T) {
	f := cxProof(t)
	res := NewRequestResolver(func() RequestBook { return f.b })
	if _, err := res.Resolve(context.Background(), "bug-17", ""); err == nil {
		t.Fatal("a list that is not a request got a version")
	}
	if _, err := NewRequestResolver(func() RequestBook { return nil }).Resolve(context.Background(), "r-1", ""); err == nil {
		t.Fatal("no store")
	}
	f.create("r-1", ReqAsk, cxFileItem)
	sub, err := res.Resolve(context.Background(), "r-1", "")
	cxNoErr(t, err)
	if sub.Commit != f.git("rev-parse", "HEAD") || sub.Repo != f.repo {
		t.Fatalf("%+v", sub)
	}
	for _, effect := range []string{"the running system", "ref:", "ref:--help", "ref:nothing"} {
		if _, err := res.Resolve(context.Background(), "r-1", effect); err == nil {
			t.Errorf("%q got a version", effect)
		}
	}
	c := f.b.content["r-1"]
	c.ProjectDir = ""
	f.b.content["r-1"] = c
	if _, err := res.Resolve(context.Background(), "r-1", ""); err == nil {
		t.Fatal("no project folder")
	}
}

func TestReqProofReworkUsesTheListsLimit(t *testing.T) {
	f := cxProof(t)
	f.create("r-1", ReqAsk, cxFileItem)
	f.holds("r-1", "s1", "claude-code")
	r, _ := f.b.Request("r-1")
	rework := func() bool {
		ok, err := f.v.Rework(context.Background(), r)
		cxNoErr(t, err)
		return ok
	}
	f.commit("out.txt", "wrong 1")
	if !f.check("r-1").NotMet || !rework() {
		t.Fatal("the first failed check goes back")
	}
	f.check("r-1")
	if rework() {
		t.Fatal("the same version does not go back twice")
	}
	f.commit("out.txt", "wrong 2")
	f.check("r-1")
	if !rework() {
		t.Fatal("a changed version goes back")
	}
	f.commit("out.txt", "wrong 3")
	f.check("r-1")
	if rework() {
		t.Fatal("the list's limit of 2 rounds")
	}
}
