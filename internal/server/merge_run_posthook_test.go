// Post-ship hooks on the merge run route: the runner takes the operator's
// post-ship block and the daemon's alert sink from the Manager, and a failed
// hook is written to the owning Automaton's triage record via the existing
// note API — nothing leaves the daemon except through the daemon's own
// sender.

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergepipeline"
)

// postShipGit runs one git command in dir, with the test's identity.
func postShipGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newPostShipRepo makes a temporary repository with one version-carrying
// file, the merge scripts, and one feature branch that adds a file — the
// same layout the pipeline's default steps expect.
func newPostShipRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	postShipGit(t, dir, "init", "-q", "-b", "main")
	postShipGit(t, dir, "config", "user.email", "t@example.invalid")
	postShipGit(t, dir, "config", "user.name", "Test")
	postShipGit(t, dir, "config", "commit.gpgsign", "false")
	ver := "package datawatch\n\nvar Version = \"1.2.3\"\n"
	for _, rel := range []string{"cmd/datawatch/main.go", "internal/server/api.go"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(ver), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	changelog := "# Changelog\n\n## [Unreleased]\n\n## v1.2.3 — older\n"
	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte(changelog), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"check-merge-version.sh", "merge-branches-bump-version.sh", "set-release-version.sh"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "scripts", s))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "scripts", s), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	postShipGit(t, dir, "add", "-A")
	postShipGit(t, dir, "commit", "-q", "-m", "base")
	// The base is a published 1.2.3 release on an origin remote, so the
	// release-bump validator has a previous tag and a remote to check.
	postShipGit(t, dir, "tag", "v1.2.3")
	origin := filepath.Join(t.TempDir(), "origin.git")
	postShipGit(t, dir, "init", "-q", "--bare", origin)
	postShipGit(t, dir, "remote", "add", "origin", origin)
	postShipGit(t, dir, "push", "-q", "origin", "main", "v1.2.3")
	postShipGit(t, dir, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The branch carries the README's Current release line for the release it
	// ships, in the vX.Y.Z (YYYY-MM-DD) shape the release line expects, which
	// the release-bump validator checks.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# App\n\nCurrent release: v1.2.4 (2026-11-01)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	postShipGit(t, dir, "add", "-A")
	postShipGit(t, dir, "commit", "-q", "-m", "feature work")
	postShipGit(t, dir, "checkout", "-q", "main")
	return dir
}

// postShipSpyShip is a ShipRunner double: every ship-side call succeeds and
// is recorded, so a run reaches the post-ship hooks without touching a real
// remote.
type postShipSpyShip struct {
	calls []string
}

func (f *postShipSpyShip) note(c string) { f.calls = append(f.calls, c) }
func (f *postShipSpyShip) Tag(_ context.Context, _, tag, _ string) error {
	f.note("tag " + tag)
	return nil
}
func (f *postShipSpyShip) Push(_ context.Context, _, b, tag string) error {
	f.note("push " + b + " " + tag)
	return nil
}
func (f *postShipSpyShip) Deploy(_ context.Context, _ string) error {
	f.note("deploy")
	return nil
}
func (f *postShipSpyShip) Hook(ctx context.Context, dir string, st mergepipeline.Step) (string, error) {
	f.note("hook " + st.Name)
	var b strings.Builder
	cmd := exec.CommandContext(ctx, "sh", "-c", st.Command)
	cmd.Dir = dir
	cmd.Stdout = &b
	cmd.Stderr = &b
	err := cmd.Run()
	if err != nil {
		return b.String(), fmt.Errorf("sh exited: %v", err)
	}
	return b.String(), nil
}
func (f *postShipSpyShip) FileIssue(_ context.Context, _, repo, title, _ string) error {
	f.note("issue " + repo + " " + title)
	return nil
}

// postShipServer wires a run route against the doubles: the spy Manager
// (recorder, notes, alert sink), an in-memory config, and the fake ship.
func postShipServer(t *testing.T, repo string, ship *postShipSpyShip, mp config.MergePipelineConfig) (*mergeRunSpy, *Server) {
	t.Helper()
	spy := &mergeRunSpy{repo: repo}
	s := &Server{
		autonomousMgr: spy,
		mergeShip:     ship,
		cfg:           &config.Config{Autonomous: config.AutonomousConfig{MergePipeline: mp}},
	}
	return spy, s
}

func postShipSteps(run autonomous.MergePipelineRun) []autonomous.MergeStepRun {
	var out []autonomous.MergeStepRun
	for _, s := range run.Steps {
		if s.Source == "post_ship" {
			out = append(out, s)
		}
	}
	return out
}

// One failing hook and one working notify: the hooks run in order after the
// ship, the notifier gets the rendered message through the daemon's alert
// sink, the failed hook lands as one triage note with source "post_ship" on
// the named Automaton, and the run itself stays ok.
func TestMergeRunRoute_PostShipNotifyAndTriageNote(t *testing.T) {
	repository := newPostShipRepo(t)
	ship := &postShipSpyShip{}
	spy, s := postShipServer(t, repository, ship, config.MergePipelineConfig{
		VersionFiles: []string{"cmd/datawatch/main.go", "internal/server/api.go"},
		PostShip: config.PostShipConfig{
			Enabled: true,
			Hooks: []config.PostShipHook{
				{Name: "rollout-flag", Command: "exit 3", Enabled: true},
				{Name: "notify-release", Builtin: "notify", Enabled: true},
			},
			NotifyTemplate: "shipped {project} {version}",
		},
	})
	rec := postMergeRun(s, http.MethodPost, `{"title":"Ship the feature","project":"psx","branches":["feature"],"automaton":"prd-1"}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Run   autonomous.MergePipelineRun `json:"run"`
		Error string                      `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "" || out.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run: result %q error %q", out.Run.Result, out.Error)
	}
	// the runner ran through the ship: tag, push, deploy, then the hooks.
	for _, want := range []string{"tag v1.2.4", "push main v1.2.4", "deploy", "hook rollout-flag"} {
		found := false
		for _, c := range ship.calls {
			if c == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ship calls miss %q; got %v", want, ship.calls)
		}
	}
	hooks := postShipSteps(out.Run)
	if len(hooks) != 2 || hooks[0].Step != "rollout-flag" || hooks[1].Step != "notify-release" {
		t.Fatalf("post-ship hooks in order: %+v", hooks)
	}
	if hooks[0].Result != autonomous.MergeStepFailed || !strings.Contains(hooks[0].Detail, "exit status 3") {
		t.Errorf("command hook should fail on the step: %+v", hooks[0])
	}
	if hooks[1].Result != autonomous.MergeStepOK {
		t.Errorf("notify hook should run after the failed one: %+v", hooks[1])
	}
	if len(spy.alerts) != 1 || spy.alerts[0].body != "shipped psx 1.2.4" {
		t.Fatalf("notifier calls: %+v", spy.alerts)
	}
	if len(spy.notes) != 1 || spy.notes[0].prdID != "prd-1" || spy.notes[0].source != "post_ship" {
		t.Fatalf("triage notes: %+v", spy.notes)
	}
	if !strings.Contains(spy.notes[0].text, "rollout-flag") {
		t.Errorf("triage note should name the failed hook: %q", spy.notes[0].text)
	}
	if len(spy.runs) == 0 || spy.runs[len(spy.runs)-1].ID != out.Run.ID {
		t.Fatalf("recorder should keep the run, got %d entries", len(spy.runs))
	}
}

// The same run with no Automaton named: the hook still fails and the
// notifier still sends, but nothing is written to a triage record.
func TestMergeRunRoute_PostShipNoAutomatonNoNotes(t *testing.T) {
	repository := newPostShipRepo(t)
	ship := &postShipSpyShip{}
	spy, s := postShipServer(t, repository, ship, config.MergePipelineConfig{
		VersionFiles: []string{"cmd/datawatch/main.go", "internal/server/api.go"},
		PostShip: config.PostShipConfig{
			Enabled:        true,
			Hooks:          []config.PostShipHook{{Name: "rollout-flag", Command: "exit 3", Enabled: true}},
			NotifyTemplate: "shipped {project} {version}",
		},
	})
	rec := postMergeRun(s, http.MethodPost, `{"title":"Ship the feature","branches":["feature"]}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Run   autonomous.MergePipelineRun `json:"run"`
		Error string                      `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "" || out.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run: result %q error %q", out.Run.Result, out.Error)
	}
	hooks := postShipSteps(out.Run)
	if len(hooks) != 1 || hooks[0].Result != autonomous.MergeStepFailed {
		t.Fatalf("post-ship hooks: %+v", hooks)
	}
	if len(spy.alerts) != 0 {
		t.Errorf("no notify hook configured, so the sender must not fire: %+v", spy.alerts)
	}
	if len(spy.notes) != 0 {
		t.Errorf("no Automaton named: no triage notes, got %+v", spy.notes)
	}
}

// With no alert sink wired the notify hook fails rather than drop the
// message; the run still stays ok and the failure is noted on the Automaton.
func TestMergeRunRoute_PostShipNoAlertSink(t *testing.T) {
	repository := newPostShipRepo(t)
	ship := &postShipSpyShip{}
	spy, s := postShipServer(t, repository, ship, config.MergePipelineConfig{
		VersionFiles: []string{"cmd/datawatch/main.go", "internal/server/api.go"},
		PostShip: config.PostShipConfig{
			Enabled:        true,
			Hooks:          []config.PostShipHook{{Name: "notify-release", Builtin: "notify", Enabled: true}},
			NotifyTemplate: "shipped {project} {version}",
		},
	})
	spy.SinkOff = true
	rec := postMergeRun(s, http.MethodPost, `{"title":"Ship the feature","branches":["feature"],"automaton":"prd-1"}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Run   autonomous.MergePipelineRun `json:"run"`
		Error string                      `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "" || out.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run: result %q error %q", out.Run.Result, out.Error)
	}
	hooks := postShipSteps(out.Run)
	if len(hooks) != 1 || hooks[0].Result != autonomous.MergeStepFailed {
		t.Fatalf("notify hook without a sink must fail on its step: %+v", hooks)
	}
	if len(spy.alerts) != 0 {
		t.Errorf("no sink wired: nothing may be sent, got %+v", spy.alerts)
	}
	if len(spy.notes) != 1 || spy.notes[0].prdID != "prd-1" || spy.notes[0].source != "post_ship" {
		t.Fatalf("failed hook must be noted on the Automaton: %+v", spy.notes)
	}
}

// The block off: not one post-ship step runs, no matter the request.
func TestMergeRunRoute_PostShipOffRunsNothing(t *testing.T) {
	repository := newPostShipRepo(t)
	ship := &postShipSpyShip{}
	spy, s := postShipServer(t, repository, ship, config.MergePipelineConfig{
		VersionFiles: []string{"cmd/datawatch/main.go", "internal/server/api.go"},
	})
	rec := postMergeRun(s, http.MethodPost, `{"title":"Ship the feature","branches":["feature"],"automaton":"prd-1"}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Run   autonomous.MergePipelineRun `json:"run"`
		Error string                      `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "" || out.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run: result %q error %q", out.Run.Result, out.Error)
	}
	if hooks := postShipSteps(out.Run); len(hooks) != 0 {
		t.Fatalf("block off: no post-ship steps, got %+v", hooks)
	}
	if len(spy.alerts) != 0 || len(spy.notes) != 0 {
		t.Fatalf("block off: nothing sent or noted, got alerts=%+v notes=%+v", spy.alerts, spy.notes)
	}
}
