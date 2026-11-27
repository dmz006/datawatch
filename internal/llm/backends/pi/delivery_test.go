// The datawatch pi extension, run under node against a fake pi and a fake
// datawatch. It shows the extension's own rules. It is not the proof that real
// pi raises these events: that is the sandbox proof run, which the built
// extension still has to pass.

package pi

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

var fkIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:+-]{0,127}$`)

type fkItem struct {
	ID       string
	State    string
	Content  string
	ReadOnly bool
	fetched  bool
	seen     bool
	accepted bool
}

type fkCall struct {
	Method, Path, Token string
	Body                map[string]string
}

// fkDaemon answers the request routes as the contract says.
type fkDaemon struct {
	mu        sync.Mutex
	items     []*fkItem
	calls     []fkCall
	seenFails int  // answer this many "seen" calls 503
	dropTake  int  // record this many takes and lose the reply
	badDigest bool // take answers with a digest of something else
	srv       *httptest.Server
}

func fkSum(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func newFkDaemon(t *testing.T) *fkDaemon {
	d := &fkDaemon{}
	d.srv = httptest.NewServer(http.HandlerFunc(d.serve))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *fkDaemon) add(id, content string, readOnly bool) *fkItem {
	d.mu.Lock()
	defer d.mu.Unlock()
	it := &fkItem{ID: id, State: "waiting", Content: content, ReadOnly: readOnly}
	d.items = append(d.items, it)
	return it
}

func (d *fkDaemon) serve(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := fkCall{Method: r.Method, Path: r.URL.Path, Token: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")}
	_ = json.NewDecoder(r.Body).Decode(&c.Body)
	d.calls = append(d.calls, c)
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	if r.URL.Path == "/api/requests/mine" && r.Method == http.MethodGet {
		items := []map[string]any{}
		for _, it := range d.items {
			items = append(items, map[string]any{"id": it.ID, "state": it.State})
		}
		reply(map[string]any{"session_id": "s1", "items": items})
		return
	}
	id, verb, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/requests/"), "/")
	for _, it := range d.items {
		if it.ID != id || r.Method != http.MethodPost {
			continue
		}
		switch verb {
		case "take":
			repeat := it.fetched
			it.fetched = true
			if d.dropTake > 0 {
				d.dropTake--
				http.Error(w, "lost", http.StatusInternalServerError)
				return
			}
			digest := fkSum(it.Content)
			if d.badDigest {
				digest = fkSum("other")
			}
			reply(map[string]any{"request": map[string]any{"id": it.ID}, "content": it.Content, "digest": digest,
				"repeat": repeat, "resend_ok": it.ReadOnly})
		case "seen":
			if d.seenFails > 0 {
				d.seenFails--
				http.Error(w, "cannot record", http.StatusServiceUnavailable)
				return
			}
			// The request store takes only an identifier as a version.
			if v := c.Body["reporter_version"]; v != "" && !fkIdentifier.MatchString(v) {
				http.Error(w, "a version is an identifier", http.StatusBadRequest)
				return
			}
			if !it.fetched || c.Body["digest"] != fkSum(it.Content) {
				http.Error(w, "state", http.StatusConflict)
				return
			}
			it.seen, it.State = true, "delivered"
			reply(map[string]any{"id": it.ID})
		case "accept":
			if c.Body["code"] != "c0de" {
				http.Error(w, "wrong code", http.StatusBadRequest)
				return
			}
			it.accepted = true
			reply(map[string]any{"id": it.ID})
		case "report":
			if !it.accepted {
				http.Error(w, "state", http.StatusConflict)
				return
			}
			it.State = "reported"
			reply(map[string]any{"id": it.ID})
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
		return
	}
	http.Error(w, "no such request", http.StatusNotFound)
}

func (d *fkDaemon) count(suffix string) (n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.calls {
		if strings.HasSuffix(c.Path, suffix) {
			n++
		}
	}
	return n
}

func (d *fkDaemon) last(suffix string) (out fkCall) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.calls {
		if strings.HasSuffix(c.Path, suffix) {
			out = c
		}
	}
	return out
}

// fkEnv is one session's files: the extension, its credential and reports.
type fkEnv struct {
	t                *testing.T
	dir, ext, credAt string
	cred             DeliveryCredential
}

func newFkEnv(t *testing.T, d *fkDaemon, deliverAs string) *fkEnv {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed: the extension was NOT tested")
	}
	dir := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	// pi supplies typebox to its extensions; here a small one stands in.
	tb := filepath.Join(dir, "node_modules", "typebox")
	must(os.MkdirAll(tb, 0o700))
	must(os.WriteFile(filepath.Join(tb, "package.json"), []byte(`{"name":"typebox","type":"module","exports":"./index.js"}`), 0o600))
	must(os.WriteFile(filepath.Join(tb, "index.js"), []byte(
		`export const Type = { Object: (p) => ({ type: "object", properties: p }), String: (o) => ({ type: "string", ...o }), Optional: (s) => ({ ...s, optional: true }) };`), 0o600))
	must(os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o600))
	ext, err := WriteDeliveryExtension(dir)
	must(err)
	e := &fkEnv{t: t, dir: dir, ext: ext, credAt: filepath.Join(dir, "s1", "credential.json"),
		cred: DeliveryCredential{APIURL: d.srv.URL, Token: "token-one",
			StateFile: filepath.Join(dir, "s1", "reports.json"), DeliverAs: deliverAs, PollMillis: 30}}
	e.save()
	return e
}

func (e *fkEnv) save() {
	e.t.Helper()
	if err := WriteDeliveryCredential(e.credAt, e.cred); err != nil {
		e.t.Fatal(err)
	}
}

type fkPi struct {
	t    *testing.T
	in   io.WriteCloser
	out  *bufio.Scanner
	stop func()
}

type fkReply struct {
	Error     string            `json:"error"`
	ToolError string            `json:"toolError"`
	Result    string            `json:"result"`
	OK        bool              `json:"ok"`
	Sections  map[string]string `json:"sections"`
	Tools     []string          `json:"tools"`
	Sent      []struct {
		Text      string `json:"text"`
		DeliverAs string `json:"deliverAs"`
	} `json:"sent"`
}

// start runs a pi process over e's files: a second one is a restarted pi.
func (e *fkEnv) start() *fkPi {
	e.t.Helper()
	cmd := exec.Command("node", "testdata/fake-pi.mjs", e.ext)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), DeliveryEnv + "=" + e.credAt}
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		e.t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		e.t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		e.t.Fatal(err)
	}
	var once sync.Once
	p := &fkPi{t: e.t, in: in, out: bufio.NewScanner(out)}
	p.out.Buffer(make([]byte, 1<<20), 1<<20)
	p.stop = func() { once.Do(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }) }
	e.t.Cleanup(p.stop)
	p.do(map[string]any{"cmd": "start"})
	return p
}

func (p *fkPi) do(cmd map[string]any) fkReply {
	p.t.Helper()
	b, _ := json.Marshal(cmd)
	if _, err := p.in.Write(append(b, '\n')); err != nil {
		p.t.Fatal(err)
	}
	if !p.out.Scan() {
		p.t.Fatalf("the fake pi stopped: %v", p.out.Err())
	}
	var r fkReply
	if err := json.Unmarshal(p.out.Bytes(), &r); err != nil {
		p.t.Fatal(err)
	}
	if r.Error != "" {
		p.t.Fatal(r.Error)
	}
	return r
}

// polls lets several polls pass.
func (p *fkPi) polls() fkReply { return p.do(map[string]any{"cmd": "sleep", "ms": 250}) }

func (p *fkPi) model(extra map[string]any) fkReply {
	cmd := map[string]any{"cmd": "model"}
	for k, v := range extra {
		cmd[k] = v
	}
	return p.do(cmd)
}

func fkWait(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

const fkContent = "datawatch request r-1 (ask)\n\nSECRET-PIX-9 write it\n\nReceipt code: c0de\n"

func TestReqPiBothTimingsDeliverAndReportSeen(t *testing.T) {
	for _, timing := range []string{DeliverSteer, DeliverFollowUp} {
		d := newFkDaemon(t)
		d.add("r-1", fkContent, false)
		e := newFkEnv(t, d, timing)
		p := e.start()
		r := p.polls()
		if len(r.Sent) != 1 || r.Sent[0].DeliverAs != timing ||
			r.Sent[0].Text != "[datawatch request r-1 begin]\n"+fkContent+"\n[datawatch request r-1 end]" {
			t.Fatalf("%s: %+v", timing, r.Sent)
		}
		if d.count("/seen") != 0 {
			t.Fatal("seen before any model call")
		}
		p.model(map[string]any{"parts": timing == DeliverFollowUp})
		got := d.last("/seen")
		if d.count("/seen") != 1 || got.Body["digest"] != fkSum(fkContent) || got.Body["via"] != "program" ||
			got.Body["reporter_version"] != DeliveryExtensionVersion || got.Token != "token-one" {
			t.Fatalf("%s: %+v", timing, got)
		}
		if take := d.last("/take"); take.Body["via"] != "program" || d.count("/take") != 1 {
			t.Fatalf("%+v", take)
		}
		p.model(nil)
		p.polls()
		if d.count("/seen") != 1 || d.count("/take") != 1 {
			t.Fatalf("again: %d takes, %d seen", d.count("/take"), d.count("/seen"))
		}
	}
}

// With no timing named in the credential the extension takes nothing, so
// nothing is marked fetched.
func TestReqPiWithNoTimingChosenTakesNothing(t *testing.T) {
	for _, timing := range []string{"", "sometime", "followup", "nextTurn"} {
		d := newFkDaemon(t)
		d.add("r-1", fkContent, true)
		p := newFkEnv(t, d, timing).start()
		r := p.polls()
		if len(r.Sent) != 0 || d.count("/take") != 0 || d.count("/mine") != 0 {
			t.Fatalf("%q: %+v, %d takes", timing, r.Sent, d.count("/take"))
		}
	}
}

func TestReqPiNoSeenWithoutACompletedModelCall(t *testing.T) {
	d := newFkDaemon(t)
	d.add("r-1", fkContent, false)
	p := newFkEnv(t, d, DeliverFollowUp).start()
	p.polls()
	for name, call := range map[string]map[string]any{
		"the text was removed":      {"strip": true},
		"the model server said 500": {"status": 500},
		"refused 429":               {"status": 429},
		"aborted":                   {"stopReason": "aborted"},
		"an error":                  {"stopReason": "error"},
		"no response seen":          {"noResponse": true},
		"not in a user message":     {"otherRole": "tool"},
	} {
		p.model(call)
		if d.count("/seen") != 0 {
			t.Fatalf("%s: reported seen", name)
		}
	}
	p.model(nil) // the next call that works holds it
	if d.count("/seen") != 1 {
		t.Fatalf("%d", d.count("/seen"))
	}
}

func TestReqPiAFailedReportIsKeptOnDiskAndRetried(t *testing.T) {
	d := newFkDaemon(t)
	d.add("r-1", fkContent, false)
	d.seenFails = 1 << 20
	e := newFkEnv(t, d, DeliverSteer)
	p := e.start()
	p.polls()
	p.model(nil)
	kept, err := os.ReadFile(e.cred.StateFile)
	if err != nil || !strings.Contains(string(kept), "r-1") || !strings.Contains(string(kept), fkSum(fkContent)) {
		t.Fatalf("not kept: %v %s", err, kept)
	}
	if strings.Contains(string(kept), "SECRET") || strings.Contains(string(kept), "token") {
		t.Fatalf("the report file holds content or a credential: %s", kept)
	}
	if st, _ := os.Stat(e.cred.StateFile); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	p.stop() // pi ends before datawatch took the report
	tries := d.count("/seen")

	// A restart mints a new token. The restarted pi retries the report.
	e.cred.Token = "token-two"
	e.save()
	d.mu.Lock()
	d.seenFails = 2
	d.mu.Unlock()
	p2 := e.start()
	fkWait(t, "the retried report", func() bool { d.mu.Lock(); defer d.mu.Unlock(); return d.items[0].seen })
	if got := d.last("/seen"); got.Token != "token-two" || d.count("/seen") < tries+3 {
		t.Fatalf("%+v after %d tries", got, d.count("/seen"))
	}
	r := p2.polls()
	if len(r.Sent) != 0 {
		t.Fatalf("it was injected again: %+v", r.Sent)
	}
	fkWait(t, "the report file to empty", func() bool {
		b, err := os.ReadFile(e.cred.StateFile)
		return os.IsNotExist(err) || err == nil && !strings.Contains(string(b), "r-1")
	})
}

func TestReqPiARepeatIsNotInjectedUnlessItMayBeSentAgain(t *testing.T) {
	d := newFkDaemon(t)
	d.add("r-work", fkContent, false)
	e := newFkEnv(t, d, DeliverFollowUp)
	first := e.start()
	if r := first.polls(); len(r.Sent) != 1 {
		t.Fatalf("%+v", r.Sent)
	}
	first.stop() // pi dies before any model call
	second := e.start()
	if r := second.polls(); len(r.Sent) != 0 {
		t.Fatalf("work was injected again after a restart: %+v", r.Sent)
	}
	if d.count("/take") != 2 {
		t.Fatalf("it must ask once and then leave it listed: %d takes", d.count("/take"))
	}
	second.stop()

	// Read-only work (a hello) is sent again.
	d.add("r-hello", "hello text", true)
	third := e.start()
	third.polls()
	third.stop()
	fourth := e.start()
	r := fourth.polls()
	if len(r.Sent) != 1 || !strings.Contains(r.Sent[0].Text, "r-hello") {
		t.Fatalf("%+v", r.Sent)
	}
}

// A take whose reply was lost leaves the request fetched. It is not injected:
// datawatch cannot know pi never had it (design question Q8).
func TestReqPiALostTakeReplyStaysListed(t *testing.T) {
	d := newFkDaemon(t)
	d.add("r-1", fkContent, false)
	d.dropTake = 1
	p := newFkEnv(t, d, DeliverSteer).start()
	r := p.polls()
	p.model(nil)
	if len(r.Sent) != 0 || d.count("/seen") != 0 || d.count("/take") != 2 {
		t.Fatalf("%+v, %d takes", r.Sent, d.count("/take"))
	}
}

func TestReqPiAWrongDigestIsNeverInjected(t *testing.T) {
	d := newFkDaemon(t)
	d.add("r-1", fkContent, true)
	d.badDigest = true
	p := newFkEnv(t, d, DeliverSteer).start()
	if r := p.polls(); len(r.Sent) != 0 || d.count("/take") != 1 {
		t.Fatalf("%+v, %d takes", r.Sent, d.count("/take"))
	}
}

func TestReqPiOnlyWaitingItemsAreTaken(t *testing.T) {
	d := newFkDaemon(t)
	for _, state := range []string{"delivered", "accepted", "reported"} {
		d.add("r-"+state, "x", true).State = state
	}
	p := newFkEnv(t, d, DeliverSteer).start()
	if r := p.polls(); len(r.Sent) != 0 || d.count("/take") != 0 {
		t.Fatalf("%+v", r.Sent)
	}
}

func TestReqPiTheCredentialIsReadForEveryCall(t *testing.T) {
	d := newFkDaemon(t)
	e := newFkEnv(t, d, DeliverSteer)
	p := e.start()
	p.polls()
	if d.last("/mine").Token != "token-one" {
		t.Fatalf("%+v", d.last("/mine"))
	}
	e.cred.Token = "token-two"
	e.save()
	fkWait(t, "the new token", func() bool { return d.last("/mine").Token == "token-two" })
}

// The operator's decision of 2026-11-27: pi says it finished with a report
// tool, as Claude does with request_report. A report only starts datawatch's
// check.
func TestReqPiTheReportToolReportsAndNeverSaysDone(t *testing.T) {
	d := newFkDaemon(t)
	d.add("r-1", fkContent, false)
	p := newFkEnv(t, d, DeliverFollowUp).start()
	tool := func(name string, params map[string]string) fkReply {
		return p.do(map[string]any{"cmd": "tool", "name": name, "params": params})
	}
	early := tool("datawatch_report", map[string]string{"request_id": "r-1"})
	if early.OK || !strings.Contains(early.ToolError, "datawatch_ack") {
		t.Fatalf("a report before the accept must say what to do first: %+v", early)
	}
	// In the real-pi run a refusal that only said "accept it first" made the
	// model retry on a hello and then look for another way to report.
	for _, want := range []string{"do not try again", "another way", "hello"} {
		if !strings.Contains(early.ToolError, want) {
			t.Fatalf("the refusal lacks %q: %q", want, early.ToolError)
		}
	}
	if r := tool("datawatch_ack", map[string]string{"request_id": "r-1", "code": "c0de"}); !r.OK {
		t.Fatalf("%+v", r)
	}
	r := tool("datawatch_report", map[string]string{"request_id": "r-1", "summary": "wrote the file"})
	if !r.OK || !strings.Contains(r.Result, "not done") || !strings.Contains(r.Result, "check") {
		t.Fatalf("%+v", r)
	}
	if got := d.last("/report"); got.Path != "/api/requests/r-1/report" || got.Body["text"] != "wrote the file" ||
		got.Token != "token-one" {
		t.Fatalf("%+v", got)
	}
	before := d.count("/report")
	for name, params := range map[string]map[string]string{
		"a made-up id":    {"request_id": "r-9"},
		"a path as an id": {"request_id": "r-1/../mine"},
	} {
		if r := tool("datawatch_report", params); r.OK || r.ToolError == "" {
			t.Errorf("%s: %+v", name, r)
		}
	}
	if d.count("/report") != before+1 {
		t.Fatalf("a path left the extension: %d", d.count("/report"))
	}
}

func TestReqPiTheReceiptToolAccepts(t *testing.T) {
	d := newFkDaemon(t)
	d.add("r-1", fkContent, false)
	p := newFkEnv(t, d, DeliverSteer).start()
	r := p.do(map[string]any{"cmd": "tool", "name": "datawatch_ack", "params": map[string]string{"request_id": "r-1", "code": "c0de"}})
	slices.Sort(r.Tools)
	if !r.OK || !slices.Equal(r.Tools, []string{"datawatch_ack", "datawatch_report"}) {
		t.Fatalf("%+v", r)
	}
	if got := d.last("/accept"); got.Path != "/api/requests/r-1/accept" || got.Body["code"] != "c0de" || got.Token != "token-one" {
		t.Fatalf("%+v", got)
	}
	// A model can miscopy an id (seen with real pi): the refusal says to check it.
	miscopied := p.do(map[string]any{"cmd": "tool", "name": "datawatch_ack", "params": map[string]string{"request_id": "r-l", "code": "c0de"}})
	if !strings.Contains(miscopied.ToolError, "check the request id and code") {
		t.Errorf("%+v", miscopied)
	}
	for name, params := range map[string]map[string]string{
		"a wrong code":    {"request_id": "r-1", "code": "nope"},
		"a made-up id":    {"request_id": "r-9", "code": "c0de"},
		"a path as an id": {"request_id": "r-1/../mine", "code": "c0de"},
	} {
		if r := p.do(map[string]any{"cmd": "tool", "name": "datawatch_ack", "params": params}); r.OK || r.ToolError == "" {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

func TestReqPiGuidanceOnlyWhenChosen(t *testing.T) {
	d := newFkDaemon(t)
	e := newFkEnv(t, d, DeliverSteer)
	p := e.start()
	if r := p.do(map[string]any{"cmd": "agent_start"}); len(r.Sections) != 0 {
		t.Fatalf("guidance nobody chose: %+v", r.Sections)
	}
	e.cred.Guidance = true
	e.save()
	note := p.do(map[string]any{"cmd": "agent_start"}).Sections["datawatch_requests"]
	for _, want := range []string{"[datawatch request <id> begin]", "real instruction", "datawatch_ack", "datawatch_report",
		"hello needs only datawatch_ack", "call once more", "another way"} {
		if !strings.Contains(note, want) {
			t.Fatalf("the note lacks %q: %q", want, note)
		}
	}
}

func TestReqPiNoCredentialDoesNothing(t *testing.T) {
	d := newFkDaemon(t)
	d.add("r-1", fkContent, true)
	e := newFkEnv(t, d, DeliverSteer)
	e.credAt = filepath.Join(e.dir, "missing.json")
	p := e.start()
	if r := p.polls(); len(r.Sent) != 0 || d.count("/mine") != 0 {
		t.Fatalf("%+v", r.Sent)
	}
}

func TestReqPiTheEmbeddedExtensionSaysItsVersion(t *testing.T) {
	if !fkIdentifier.MatchString(DeliveryExtensionVersion) {
		t.Fatalf("the request store refuses a seen report with the version %q", DeliveryExtensionVersion)
	}
	if !strings.Contains(string(deliveryExtension), `const VERSION = "`+DeliveryExtensionVersion+`";`) {
		t.Fatal("the extension and the daemon disagree on the version")
	}
	dir := t.TempDir()
	path, err := WriteDeliveryExtension(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := WriteDeliveryExtension(dir); err != nil || again != path {
		t.Fatalf("writing it again: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(deliveryExtension) {
		t.Fatal("the written file differs")
	}
}

// The interactive launch loads the extension when, and only when, datawatch
// wired the session for it. The command is run in real shells with a pi that
// only writes down what it was given.
func TestReqPiTheInteractiveLaunchLoadsTheExtensionWhenWired(t *testing.T) {
	dir := t.TempDir()
	got := filepath.Join(dir, "args")
	fake := filepath.Join(dir, "pi")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > '"+got+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ext := filepath.Join(dir, "a data folder", "it's", "datawatch-delivery.ts")
	run := func(shell, line string) []string {
		t.Helper()
		cmd := exec.Command(shell, "-c", line)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", shell, err, out)
		}
		data, err := os.ReadFile(got)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	}
	wired, err := interactiveCmd(fake, "m", "", "s", "task", dir, "cs-x", "", ext)
	if err != nil {
		t.Fatal(err)
	}
	shells := 0
	for _, shell := range []string{"sh", "bash", "zsh"} {
		if _, err := exec.LookPath(shell); err != nil {
			continue
		}
		shells++
		args := run(shell, wired)
		i := slices.Index(args, "-e")
		if i < 0 || i+1 >= len(args) || args[i+1] != ext {
			t.Fatalf("%s: wired, but pi got %q", shell, args)
		}
	}
	if shells < 2 {
		t.Fatalf("only %d shells ran", shells)
	}
	// Not wired: the command of a launch is exactly what it was.
	plain, err := interactiveCmd(fake, "m", "", "s", "task", dir, "cs-x", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(run("sh", plain), "-e") {
		t.Fatalf("not wired: %s", plain)
	}
	if _, ok := New("pi").(interface{ SetDeliveryExtension(string) }); !ok {
		t.Fatal("the interactive backend takes no extension: no launch would load it")
	}
	// One-shot pi is not part of delivery.
	if _, ok := NewPrompt("pi").(interface{ SetDeliveryExtension(string) }); ok {
		t.Fatal("the one-shot backend takes the extension")
	}
}
