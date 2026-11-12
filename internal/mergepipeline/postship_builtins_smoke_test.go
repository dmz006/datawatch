// Smoke for the built-in post-ship hooks, end to end in process: a full
// runner pass over a temporary repository with a fake ship runner whose
// hosting-service half is an httptest server, a fake notifier, and a fixed
// clock. Nothing here reaches a real remote, a real model or a daemon.

package mergepipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// smokeIssue is one issue the fake hosting service accepted or refused.
type smokeIssue struct {
	Repo  string
	Title string
	Body  string
}

// smokeShip stands in for the ship runner: every call that would leave the
// machine is logged; the hosting-service half (filing an issue) goes over
// HTTP to an httptest server so the network path is exercised in process.
type smokeShip struct {
	mu          sync.Mutex
	calls       []string
	issues      []smokeIssue
	refuseIssue bool // the next filing gets a 500 from the fake service
	endpoint    string
}

func (f *smokeShip) record(c string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
}

func (f *smokeShip) Tag(_ context.Context, _, tag, _ string) error {
	f.record("tag " + tag)
	return nil
}
func (f *smokeShip) Push(_ context.Context, _, branch, tag string) error {
	f.record("push " + branch + " " + tag)
	return nil
}
func (f *smokeShip) Deploy(_ context.Context, _ string) error {
	f.record("deploy")
	return nil
}
func (f *smokeShip) Hook(_ context.Context, _ string, s Step) (string, error) {
	f.record("hook " + s.Name)
	return "the hook ran", nil
}
func (f *smokeShip) FileIssue(_ context.Context, _, repo, title, body string) error {
	f.record("issue " + repo)
	payload, err := json.Marshal(map[string]string{"repo": repo, "title": title, "body": body})
	if err != nil {
		return err
	}
	f.mu.Lock()
	endpoint := f.endpoint
	f.mu.Unlock()
	resp, err := http.Post(endpoint+"/issues", "application/json", bytes.NewReader(payload)) // #nosec G115 -- test loopback endpoint
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("the hosting service answered %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// newFakeHostingService starts the httptest server the fake ship runner's
// issue filings land on.
func newFakeHostingService(t *testing.T, f *smokeShip) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method != http.MethodPost || r.URL.Path != "/issues" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if f.refuseIssue {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "the service is down")
			return
		}
		var in struct {
			Repo  string `json:"repo"`
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &in); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.issues = append(f.issues, smokeIssue{Repo: in.Repo, Title: in.Title, Body: in.Body})
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	f.endpoint = srv.URL
	return srv
}

// smokeNotifier is the fake for the notify built-in's sink; failNote makes
// the next delivery fail.
type smokeNotifier struct {
	mu       sync.Mutex
	texts    []string
	failNote string
}

func (n *smokeNotifier) Notify(_ context.Context, text string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.failNote != "" {
		e := errors.New(n.failNote)
		n.failNote = ""
		return e
	}
	n.texts = append(n.texts, text)
	return nil
}

func (n *smokeNotifier) delivered() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string{}, n.texts...)
}

// smokeRepo is the temporary repository: a clean main with a version, a
// changelog, a feature branch and a registry file one row matches in.
func smokeRepo(t *testing.T) string {
	t.Helper()
	dir := newMergeRepo(t)
	putFile(t, dir, "registry.md", "| project | status |\n| smoke | open |\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "registry")
	return dir
}

// smokePipeline is the minimal ship pipeline: merge, push, deploy. The
// push-release step succeeds, so the post-ship hooks are due, and the
// deploy step sits between the ship and the hooks.
func smokePipeline(t *testing.T) *Resolver {
	t.Helper()
	def := Definition{Steps: []Step{
		{Name: "merge", Kind: KindShip, Action: ActionMergeBranches},
		{Name: "push-release", Kind: KindShip, Action: ActionPushRelease},
		{Name: "deploy-latest", Kind: KindDeploy, Action: ActionDeployLatest},
	}}
	rs, err := NewRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Set("smoke", def); err != nil {
		t.Fatal(err)
	}
	return &Resolver{Records: rs, ProjectDir: func(string) (string, bool) { return "", false }}
}

// The four built-in post-ship hooks run after the deploy, in configured
// order, reach their wiring (the fake hosting service, the fake ship runner,
// the project's registry file, the fake notifier) with rendered settings,
// and leave the ship's result untouched.
func TestSmokeBuiltinPostShipHooksRunAfterDeploy(t *testing.T) {
	dir := smokeRepo(t)
	fixed := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	ship := &smokeShip{}
	newFakeHostingService(t, ship)
	nf := &smokeNotifier{}

	settings := PostShipSettings{
		Enabled:             true,
		RegistryFile:        "registry.md",
		RegistryRowPattern:  "| smoke |",
		CompanionRepository: "org/companion",
		IssueTitleTemplate:  "Shipped {project} {version}",
		NotifyTemplate:      "{project} {version} is live",
		Hooks: []PostShipHook{
			{Name: "companion-issue", Builtin: "parity_issue", Enabled: true},
			{Name: "release-tag", Builtin: "tag", Enabled: true},
			{Name: "registry-row", Builtin: "registry_row", Enabled: true},
			{Name: "say", Builtin: "notify", Enabled: true},
		},
	}

	r := &Runner{
		VersionFiles: testVersionFiles, Dir: dir, Project: "smoke",
		Resolver: smokePipeline(t), Ship: ship, Notifier: nf, PostShip: settings,
		Now: func() time.Time { return fixed },
	}
	res, err := r.Run(context.Background(), RunRequest{Title: "smoke the hooks", Branches: []string{"feature"}})
	if err != nil || res.Run.Result != "ok" {
		t.Fatalf("the ship must succeed: %q err %v", res.Run.Result, err)
	}

	// 1) every step ran, the hooks last, in configured order
	var names []string
	for _, s := range res.Run.Steps {
		names = append(names, s.Step)
	}
	wantOrder := "merge|push-release|deploy-latest|companion-issue|release-tag|registry-row|say"
	if strings.Join(names, "|") != wantOrder {
		t.Fatalf("steps %v, want %v: the hooks run after the deploy, in order", names, wantOrder)
	}

	// 2) the leaving-the-machine calls really happened through the fakes,
	// each after the deploy
	deployIdx, issueIdx, tagIdx := -1, -1, -1
	for i, c := range ship.calls {
		switch {
		case c == "deploy":
			deployIdx = i
		case strings.HasPrefix(c, "issue "):
			issueIdx = i
		case strings.HasPrefix(c, "tag "):
			tagIdx = i
		}
	}
	if deployIdx == -1 || issueIdx == -1 || tagIdx == -1 || issueIdx < deployIdx || tagIdx < deployIdx {
		t.Errorf("ship calls out of order (want deploy before the hook calls): %v", ship.calls)
	}

	// 3) every built-in reached its wiring with rendered settings
	if got := ship.issueCount(); got != 1 {
		t.Errorf("want exactly one filed issue, got %d", got)
	} else {
		in := ship.issues[0]
		if in.Title != "Shipped smoke 1.2.3" {
			t.Errorf("issue title %q: placeholders must be rendered", in.Title)
		}
	}
	if texts := nf.delivered(); len(texts) != 1 || texts[0] != "smoke 1.2.3 is live" {
		t.Errorf("notifications %v: the notify built-in must deliver the rendered template once", texts)
	}
	reg := getFile(t, dir, "registry.md")
	if !strings.Contains(reg, "| smoke | Shipped 1.2.3 |") {
		t.Errorf("registry not marked shipped: %q", reg)
	}

	// 4) the fixed clock is what the record carries
	if !res.Run.Start.Equal(fixed) {
		t.Errorf("run start %v, want the fixed clock %v", res.Run.Start, fixed)
	}
	for _, s := range res.Run.Steps {
		if s.End == nil || !s.End.Equal(fixed) {
			t.Errorf("step %s has an off-clock end: %+v", s.Step, s)
		}
	}
}

func (s *smokeShip) issueCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.issues)
}

// A failing hook posts an error to the fake wiring (a 500 from the hosting
// service, a refused delivery from the notifier). The failure stays on the
// hook's own recorded step: the ship's result is untouched, the failure
// detail is present, and hooks listed after it still run.
func TestSmokeFailingBuiltinHooksAreRecordedWithoutFailingTheShip(t *testing.T) {
	dir := smokeRepo(t)
	ship := &smokeShip{refuseIssue: true}
	newFakeHostingService(t, ship)
	nf := &smokeNotifier{failNote: "the comm line is down"}

	settings := PostShipSettings{
		Enabled:             true,
		RegistryFile:        "registry.md",
		RegistryRowPattern:  "| smoke |",
		CompanionRepository: "org/companion",
		IssueTitleTemplate:  "Shipped {project} {version}",
		NotifyTemplate:      "{project} {version} is live",
		Hooks: []PostShipHook{
			{Name: "companion-issue", Builtin: "parity_issue", Enabled: true}, // 500 from the fake service
			{Name: "say", Builtin: "notify", Enabled: true},                   // refused by the fake notifier
			{Name: "registry-row", Builtin: "registry_row", Enabled: true},    // listed after both: must still run
		},
	}
	r := &Runner{
		VersionFiles: testVersionFiles, Dir: dir, Project: "smoke",
		Resolver: smokePipeline(t), Ship: ship, Notifier: nf, PostShip: settings,
	}
	res, err := r.Run(context.Background(), RunRequest{Title: "smoke failures", Branches: []string{"feature"}})
	if err != nil || res.Run.Result != "ok" {
		t.Fatalf("failing hooks must not fail or stop the ship: %q err %v", res.Run.Result, err)
	}
	got := stepResults(res.Run)
	if got["companion-issue"] != "failed" {
		t.Errorf("the refused filing must be recorded as a failed step: %v", got)
	}
	if got["say"] != "failed" {
		t.Errorf("the refused delivery must be recorded as a failed step: %v", got)
	}
	if got["registry-row"] != "ok" {
		t.Errorf("a hook listed after a failing one must still run: %v", got)
	}
	if ship.issueCount() != 0 {
		t.Errorf("the fake hosting service must not hold a refused issue: %v", ship.issues)
	}
	if texts := nf.delivered(); len(texts) != 0 {
		t.Errorf("a refused notification must not be delivered: %v", texts)
	}
	if !strings.Contains(getFile(t, dir, "registry.md"), "Shipped") {
		t.Errorf("the later hook's work is missing from the registry: %q", getFile(t, dir, "registry.md"))
	}
	// the failed steps carry a detail (redacted) and a finished end
	for _, s := range res.Run.Steps {
		if (s.Step == "companion-issue" || s.Step == "say") && (s.Detail == "" || s.End == nil) {
			t.Errorf("failed hook step %s is incomplete: %+v", s.Step, s)
		}
	}
}

// A hook that is not enabled does nothing: no recorded step, no call to any
// fake wiring — while the enabled hooks around it run normally.
func TestSmokeDisabledBuiltinHooksDoNothing(t *testing.T) {
	dir := smokeRepo(t)
	ship := &smokeShip{}
	newFakeHostingService(t, ship)
	nf := &smokeNotifier{}

	settings := PostShipSettings{
		Enabled:             true,
		RegistryFile:        "registry.md",
		RegistryRowPattern:  "| smoke |",
		CompanionRepository: "org/companion",
		IssueTitleTemplate:  "Shipped {project} {version}",
		NotifyTemplate:      "{project} {version} is live",
		Hooks: []PostShipHook{
			{Name: "companion-issue", Builtin: "parity_issue", Enabled: false},
			{Name: "say", Builtin: "notify", Enabled: false},
			{Name: "registry-row", Builtin: "registry_row", Enabled: true},
		},
	}
	r := &Runner{
		VersionFiles: testVersionFiles, Dir: dir, Project: "smoke",
		Resolver: smokePipeline(t), Ship: ship, Notifier: nf, PostShip: settings,
	}
	res, err := r.Run(context.Background(), RunRequest{Title: "smoke disabled", Branches: []string{"feature"}})
	if err != nil || res.Run.Result != "ok" {
		t.Fatalf("ship %q err %v", res.Run.Result, err)
	}
	got := stepResults(res.Run)
	if _, ran := got["companion-issue"]; ran {
		t.Errorf("a disabled hook must record no step: %v", got)
	}
	if _, ran := got["say"]; ran {
		t.Errorf("a disabled hook must record no step: %v", got)
	}
	if got["registry-row"] != "ok" {
		t.Errorf("the enabled hook must run: %v", got)
	}
	for _, c := range ship.calls {
		if strings.HasPrefix(c, "issue ") {
			t.Errorf("a disabled hook reached the hosting service: %v", ship.calls)
		}
	}
	if ship.issueCount() != 0 {
		t.Errorf("the fake hosting service must hold nothing: %v", ship.issues)
	}
	if texts := nf.delivered(); len(texts) != 0 {
		t.Errorf("the fake notifier must hold nothing: %v", texts)
	}
}
