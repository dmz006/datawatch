// Part A's first failing test 16: the wiring.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/acceptance"
	authpkg "github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/secfile"
	"github.com/dmz006/datawatch/internal/session"
)

const wireMarker = "ZQXWIRE91c"

const oldQueue = `[{"id":"a1","role":"r","state":"pending","payload":{"p":"` + wireMarker + `"},"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]`

type fakeHooks struct {
	pre, end func(*session.Session)
}

func (h *fakeHooks) SetOnPreLaunch(f func(*session.Session))  { h.pre = f }
func (h *fakeHooks) SetOnSessionEnd(f func(*session.Session)) { h.end = f }

// With Claude's channel off, pi still gets its own credential and its launch
// wiring, and no other session does.
func TestReqWirePiGetsATokenWithTheChannelOff(t *testing.T) {
	cfg := deliveryTestConfig(t)
	store, err := authpkg.NewSessionTokenStore(filepath.Join(cfg.DataDir, "session_tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]piDeliveryLaunch{}
	pi := piPreLaunch{cfg: cfg,
		mint:   func(id string) (string, error) { return store.Mint(id, []string{"queue:write"}) },
		setEnv: func(string, string, string) error { return nil },
		wire: func(s *session.Session, in piDeliveryLaunch) error {
			got[s.FullID] = in
			return nil
		}}
	hooks := &fakeHooks{}
	deliveryTestWiring().hooksWithoutChannel(hooks, pi)
	if hooks.pre == nil || hooks.end == nil {
		t.Fatal("the hooks are not set")
	}
	for _, fam := range []string{"pi", "claude-code", "opencode"} {
		hooks.pre(&session.Session{FullID: "host1-" + fam, BackendFamily: fam})
	}
	in, ok := got["host1-pi"]
	if !ok || len(got) != 1 {
		t.Fatalf("wired: %v", got)
	}
	if owner := store.SessionIDForToken(in.Token); owner != "host1-pi" {
		t.Fatalf("not this session's token: %q", owner)
	}
	if in.DataDir != cfg.DataDir || !strings.HasPrefix(in.APIURL, "http://") || in.SetSessionEnv == nil || in.Token == cfg.Server.Token {
		t.Fatalf("%+v", in)
	}
	// A token already minted for the session is kept: a second one would revoke it.
	pi.run(&session.Session{FullID: "host1-pi", BackendFamily: "pi"}, "already")
	if got["host1-pi"].Token != "already" {
		t.Fatal("minted again")
	}
	hooks.end(&session.Session{FullID: "host1-pi"}) // no store open: nothing happens
}

// The daemon sets the same wiring in both branches.
func TestReqWireDaemonSetsBothBranches(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"deliveryWire.hooksWithoutChannel(mgr, piLaunch)",
		`piLaunch.run(sess, sessionChannelEnv["DATAWATCH_TOKEN"])`,
		"deliveryWire.sessionEnded(sess.FullID)",
		"wire: wirePiDelivery",
		"mcpSrv.SetRequests(reqs)",
		"queueStore, err := openStartupQueue(queueStorePath(cfg))",
		"queue: queueStore}",
		"mcpQueueStore := daemonQueue()",
		"deliveryWire.pause = ps",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("main.go lacks %q", want)
		}
	}
	// Only openStartupQueue opens queue.json.
	if strings.Contains(string(src), "session.NewQueueStore(") {
		t.Error("main.go still opens the queue file itself")
	}
}

// The lists: the delivery key, no command runs, receipts and versions from the
// request records.
func TestReqWireListsUseTheDeliveryKey(t *testing.T) {
	cfg := deliveryTestConfig(t)
	cfg.Acceptance.Mode = acceptance.ModeReport
	key := []byte(strings.Repeat("k", 32))
	nothing := func() delivery.RequestBook { return nil }
	opts := acceptanceOptions(cfg, key, nothing)
	if _, ok := opts.Runner.(delivery.RefusingRunner); !ok {
		t.Fatalf("runner %T", opts.Runner)
	}
	if opts.Probes[acceptance.MethodReceipt] == nil || opts.Resolver == nil || string(opts.Key) != string(key) {
		t.Fatalf("%+v", opts)
	}
	d := deliveryDeps{cfg: cfg}
	if openLists(d, key, false, nothing) == nil {
		t.Fatal("not opened")
	}
	// A plain store on a secure install is refused, unless secure mode was
	// just turned on, and then it is sealed at once.
	if err := os.WriteFile(opts.Path, []byte(`{"lists":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if openLists(d, key, false, nothing) != nil {
		t.Fatal("a plain list store was accepted on a secure install")
	}
	if data, _ := os.ReadFile(opts.Path); string(data) != `{"lists":{}}` {
		t.Fatal("the refused store was changed")
	}
	if openLists(d, key, true, nothing) == nil {
		t.Fatal("not adopted")
	}
	if data, _ := os.ReadFile(opts.Path); !secfile.IsEncrypted(data) {
		t.Fatal("not sealed")
	}
	if openLists(d, []byte(strings.Repeat("x", 32)), false, nothing) != nil {
		t.Fatal("opened with another key")
	}
}

func wireTestManager(t *testing.T, cfg *config.Config) *session.Manager {
	t.Helper()
	m, err := session.NewManager(cfg.Hostname, cfg.DataDir, "claude", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

// The start opens the store, moves the queue, adds the source and seals what
// it writes; a restart serves the queue again.
func TestReqWireStartOpensTheStoreAndMovesTheQueue(t *testing.T) {
	cfg := deliveryTestConfig(t)
	path := queueStorePath(cfg)
	if err := os.WriteFile(path, []byte(oldQueue), 0o600); err != nil {
		t.Fatal(err)
	}
	start := func() (*deliveryWiring, *delivery.Engine, *session.QueueStore, context.CancelFunc) {
		t.Helper()
		w := deliveryTestWiring()
		if err := w.openKey(cfg, []byte("pw")); err != nil {
			t.Fatal(err)
		}
		q, err := openStartupQueue(path)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		e, err := w.start(ctx, deliveryDeps{cfg: cfg, sessions: wireTestManager(t, cfg), queue: q, firstPassDelay: time.Hour})
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		return w, e, q, cancel
	}
	w, e, q, cancel := start()
	defer cancel()
	if w.requestStore() == nil || w.requestBook() == nil {
		t.Fatal("no request store")
	}
	if body, _ := os.ReadFile(path); string(body) != delivery.QueueMarker {
		t.Fatalf("queue.json: %q", body)
	}
	// The older program stops on the marker instead of starting empty.
	if _, err := session.NewQueueStore(path); err == nil || !strings.Contains(err.Error(), "parse queue store") {
		t.Fatalf("old loader: %v", err)
	}
	if _, ok := q.Get("a1"); !ok {
		t.Fatal("the item was lost")
	}
	if _, err := q.Push("r", map[string]any{"p": wireMarker}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range e.Report().Sources {
		if s.Name == "requests" {
			found = s.OK
		}
	}
	if !found {
		t.Fatalf("no readable requests source: %+v", e.Report().Sources)
	}
	for name, body := range filesUnder(t, cfg.DataDir) {
		if strings.Contains(body, wireMarker) {
			t.Errorf("%s holds content in plain", name)
		}
	}
	cancel()

	_, _, q2, cancel2 := start()
	defer cancel2()
	if got := q2.List("r", ""); len(got) != 2 {
		t.Fatalf("after a restart: %d items", len(got))
	}
}

// A request store that cannot open stops the start and leaves queue.d and
// queue.json as they were.
func TestReqWireAFailedOpenChangesNothing(t *testing.T) {
	cfg := deliveryTestConfig(t)
	path := queueStorePath(cfg)
	if err := os.WriteFile(path, []byte(oldQueue), 0o600); err != nil {
		t.Fatal(err)
	}
	w := deliveryTestWiring()
	if err := w.openKey(cfg, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	if err := w.key.Commit(); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(cfg.DataDir, "queue.d", "meta.rec")
	if err := os.WriteFile(bad, []byte("DWDAT2\nnot a record\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	q, err := openStartupQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	before := filesUnder(t, filepath.Join(cfg.DataDir, "queue.d"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = w.start(ctx, deliveryDeps{cfg: cfg, sessions: wireTestManager(t, cfg), queue: q, firstPassDelay: time.Hour})
	// Damaged request storage never stops datawatch from starting (operator,
	// 2026-11-27): requests are off, and nothing is changed.
	if err != nil || w.requestStore() != nil {
		t.Fatalf("start: %v, requests %v", err, w.requestStore())
	}
	after := filesUnder(t, filepath.Join(cfg.DataDir, "queue.d"))
	if len(after) != len(before) || after["/meta.rec"] != before["/meta.rec"] {
		t.Fatalf("queue.d changed: %v", after)
	}
	if body, _ := os.ReadFile(path); string(body) != oldQueue {
		t.Fatal("queue.json changed")
	}
}

// With the check switched off, a queue that already moved is still served; one
// that has not moved is left alone.
func TestReqWireCheckOffKeepsTheQueueWorking(t *testing.T) {
	cfg := deliveryTestConfig(t)
	off := false
	path := queueStorePath(cfg)
	if err := os.WriteFile(path, []byte(oldQueue), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func() (*deliveryWiring, *session.QueueStore) {
		t.Helper()
		w := deliveryTestWiring()
		if err := w.openKey(cfg, nil); err != nil {
			t.Fatal(err)
		}
		q, err := openStartupQueue(path)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		if _, err := w.start(ctx, deliveryDeps{cfg: cfg, sessions: wireTestManager(t, cfg), queue: q, firstPassDelay: time.Hour}); err != nil {
			t.Fatal(err)
		}
		return w, q
	}
	cfg.Delivery.Enabled = &off
	w, q := run()
	if body, _ := os.ReadFile(path); string(body) != oldQueue || w.requestStore() != nil {
		t.Fatal("check off must leave an unmoved queue as before")
	}
	if _, ok := q.Get("a1"); !ok {
		t.Fatal("queue not served")
	}
	cfg.Delivery.Enabled = nil
	run() // moves it
	cfg.Delivery.Enabled = &off
	w, q = run()
	if _, ok := q.Get("a1"); !ok || w.requestStore() != nil {
		t.Fatal("a moved queue must be served with the check off")
	}
	if _, err := q.Push("r", nil); err != nil {
		t.Fatal(err)
	}
}

func TestReqWireSettingsAreReadLive(t *testing.T) {
	cfg := deliveryTestConfig(t)
	if s := requestSettings(cfg); s.RemindersPerSessionHour != 2 || s.RemindersPerHostHour != 20 {
		t.Fatalf("%+v", s)
	}
	five := 5
	cfg.Delivery.RemindersPerSessionHour = &five
	cfg.Delivery.RequestDueSecondsByKind = map[string]int{"bug": 60}
	if s := requestSettings(cfg); s.RemindersPerSessionHour != 5 || s.DueByKind[delivery.ReqBug] != time.Minute {
		t.Fatalf("%+v", s)
	}
}

func TestReqWireHoldsCoverEveryStop(t *testing.T) {
	sess := &session.Session{FullID: "h-1", BackendFamily: "claude-code", LLMRef: "claude-max", Model: "m", ProjectDir: "/p", State: session.StateRunning, PRDID: "prd1"}
	project := delivery.Scope{Kind: delivery.ScopeProject, ID: "p"}
	base := func() requestHolds {
		return requestHolds{
			session:             func(string) (*session.Session, bool) { c := *sess; return &c, true },
			paused:              func(string) bool { return false },
			usageBlocked:        func(string, string) bool { return false },
			budgetBlocked:       func(string) bool { return false },
			automationAtCeiling: func(string) bool { return false },
			automationOf:        func(id string) string { return map[string]string{"prd1": "auto1", "prd2": "auto2"}[id] },
		}
	}
	check := func(name string, h requestHolds, id string, scope delivery.Scope, want string) {
		t.Helper()
		cause, held := h.Held(id, scope)
		if cause != want || held != (want != "") {
			t.Errorf("%s: %q %v, want %q", name, cause, held, want)
		}
	}
	check("nothing", base(), "h-1", project, "")
	h := base()
	h.paused = func(dir string) bool { return dir == "/p" }
	check("pause", h, "h-1", project, delivery.WhyHeldPause)
	h = base()
	h.paused = func(string) bool { return true }
	check("pause, no holder yet", h, "", project, delivery.WhyHeldPause)
	h = base()
	h.session = func(string) (*session.Session, bool) {
		c := *sess
		c.State = session.StateRateLimited
		return &c, true
	}
	check("rate limit", h, "h-1", project, delivery.WhyHeldRateLimit)
	h = base()
	h.usageBlocked = func(b, m string) bool { return b == "claude-code" && m == "m" }
	check("back-off or burn guard", h, "h-1", project, delivery.WhyHeldUsage)
	h = base()
	h.budgetBlocked = func(b string) bool { return b == "claude-max" }
	check("budget", h, "h-1", project, delivery.WhyHeldUsage)
	h = base()
	h.automationAtCeiling = func(a string) bool { return a == "auto1" }
	check("the session's Automation at its ceiling", h, "h-1", project, delivery.WhyHeldUsage)
	h = base()
	h.automationAtCeiling = func(a string) bool { return a == "auto2" }
	check("an Automaton scope", h, "", delivery.Scope{Kind: delivery.ScopeAutomaton, ID: "prd2"}, delivery.WhyHeldUsage)
	check("an Automation scope", h, "", delivery.Scope{Kind: delivery.ScopeAutomation, ID: "auto2"}, delivery.WhyHeldUsage)
}

// The CLI queue and the standalone MCP server ask the daemon and open no file.
func TestReqWireCLIQueueGoesThroughTheDaemon(t *testing.T) {
	var seen []string
	d := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]session.QueueItem{{ID: "a1"}})
			return
		}
		_ = json.NewEncoder(w).Encode(session.QueueItem{ID: "n1", Role: "r"})
	}))
	defer d.Close()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DATAWATCH_TOKEN", "x")
	old := serverURL
	serverURL = d.URL
	defer func() { serverURL = old }()

	for _, args := range [][]string{{"push", "--role", "r"}, {"claim", "--role", "r"}, {"complete", "--id", "n1"}, {"fail", "--id", "n1"}, {"list"}} {
		cmd := newQueueCmd()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if len(seen) != 5 {
		t.Fatalf("%v", seen)
	}
	if files := filesUnder(t, home); len(files) != 0 {
		t.Fatalf("files opened or made: %v", files)
	}
}

func TestReqWireExportRefusesARunningDaemonAndNeedsPlain(t *testing.T) {
	cfg := deliveryTestConfig(t)
	path := queueStorePath(cfg)
	if err := os.WriteFile(path, []byte(oldQueue), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := exportQueue(cfg, nil, true); err == nil {
		t.Fatal("exported over a queue that never moved")
	}
	key := []byte(strings.Repeat("k", 32))
	reqs, err := delivery.OpenRequests(delivery.RequestOptions{Dir: filepath.Join(cfg.DataDir, "queue.d"), Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reqs.ImportQueue(path); err != nil {
		t.Fatal(err)
	}
	pid := filepath.Join(cfg.DataDir, "daemon.pid")
	if err := os.WriteFile(pid, []byte(strconv.Itoa(os.Getppid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := exportQueue(cfg, key, true); err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("with a daemon: %v", err)
	}
	if err := os.Remove(pid); err != nil {
		t.Fatal(err)
	}
	if _, err := exportQueue(cfg, key, false); !errors.Is(err, delivery.ErrExportPlain) {
		t.Fatalf("no --plain: %v", err)
	}
	if n, err := exportQueue(cfg, key, true); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	if _, err := session.NewQueueStore(path); err != nil {
		t.Fatalf("the older program cannot read the export: %v", err)
	}
}
