//go:build unix

package server

// A hand ship record starts the shadow of the hand merge, and the shadow read
// carries the operator's turn-on report. Sandboxes with a local bare
// "remote", a made-up private list and a scripted hand merge.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergepipeline"
	"github.com/dmz006/datawatch/internal/mergeshadow"
)

const handCheckScript = `#!/bin/sh
list="$OUTGOING_PROJECT_DIR/.leak-patterns.local"
n=$(grep -c . "$list" 2>/dev/null)
[ "${n:-0}" -gt 0 ] || exit 2
echo "clean ($n private pattern(s) loaded)"
`

type handRig struct {
	t    *testing.T
	s    *Server
	mgr  *autonomous.Manager
	repo string
	def  mergepipeline.Definition
}

func newHandRig(t *testing.T) *handRig {
	t.Helper()
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.invalid")
	s, mgr, repo, _, _, _ := switchServer(t)
	for name, body := range map[string]string{
		"Makefile": "check-outgoing:\n\t@sh scripts/check-outgoing.sh\n", "scripts/check-outgoing.sh": handCheckScript,
		".gitignore": ".leak-patterns.local\n",
	} {
		g := &handRig{t: t, repo: repo}
		g.put(name, body)
	}
	postShipGit(t, repo, "add", "-A")
	postShipGit(t, repo, "commit", "-q", "-m", "outgoing check")
	postShipGit(t, repo, "push", "-q", "origin", "main")
	g := &handRig{t: t, s: s, mgr: mgr, repo: repo}
	g.put(".leak-patterns.local", "zzqx-private-word\n")
	var steps []mergepipeline.Step
	for _, st := range mergepipeline.DefaultDefinition().Steps {
		switch {
		case st.Action == mergepipeline.ActionTagRelease, st.Action == mergepipeline.ActionDeployLatest,
			st.Action == mergepipeline.ActionSetReadmeReleaseLine, st.Name == "check-internal-refs":
			continue
		case st.Action == mergepipeline.ActionValidateReleaseBump:
			steps = append(steps, mergepipeline.Step{Name: "lint", Kind: mergepipeline.KindLint, Command: "true"},
				mergepipeline.Step{Name: "unit", Kind: mergepipeline.KindTest, Command: "true"})
		}
		// The sandbox has no README, which the release validator reads.
		if st.Action == mergepipeline.ActionValidateReleaseBump {
			continue
		}
		steps = append(steps, st)
	}
	g.def = mergepipeline.Definition{Steps: steps, Shadow: &mergepipeline.ShadowDefinition{PrivateFiles: []string{".leak-patterns.local"}}}
	g.record(g.def)
	return g
}

func (g *handRig) put(rel, body string) {
	g.t.Helper()
	p := filepath.Join(g.repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		g.t.Fatal(err)
	}
}

func (g *handRig) record(def mergepipeline.Definition) {
	g.t.Helper()
	rs, err := mergepipeline.NewRecordStore(g.s.statePath())
	if err != nil {
		g.t.Fatal(err)
	}
	if _, err := rs.Set(filepath.Base(g.repo), def); err != nil {
		g.t.Fatal(err)
	}
}

// automaton makes an accepted Automaton on a new branch off main.
func (g *handRig) automaton(branch, kind string) *autonomous.PRD {
	t := g.t
	t.Helper()
	postShipGit(t, g.repo, "checkout", "-q", "-B", branch, "main")
	g.put(branch+".txt", "work\n")
	postShipGit(t, g.repo, "add", "-A")
	postShipGit(t, g.repo, "commit", "-q", "-m", "work on "+branch)
	tip := postShipGit(t, g.repo, "rev-parse", "HEAD")
	postShipGit(t, g.repo, "checkout", "-q", "main")
	p := selAutomaton(t, g.mgr, autonomous.PRDNeedsReview, branch)
	p.Title, p.Kind = "Add "+branch, kind
	selAccept(t, g.mgr, p, tip)
	return p
}

// handMerge merges p's branch by hand as a datawatch run would, releases
// version and pushes. It returns the release commit.
func (g *handRig) handMerge(p *autonomous.PRD, from, version string) string {
	t := g.t
	t.Helper()
	postShipGit(t, g.repo, "merge", "-q", "--no-ff", "-m", "Merge branch '"+p.Git.Branch+"'", p.Git.Branch)
	for _, f := range []string{"cmd/datawatch/main.go", "internal/server/api.go"} {
		data, err := os.ReadFile(filepath.Join(g.repo, filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		g.put(f, strings.Replace(string(data), from, version, 1))
	}
	log, err := os.ReadFile(filepath.Join(g.repo, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	g.put("CHANGELOG.md", strings.Replace(string(log), "## [Unreleased]\n", "## [Unreleased]\n\n## v"+version+" — "+p.Title+"\n\n", 1))
	postShipGit(t, g.repo, "commit", "-q", "-am", "Set version "+version)
	postShipGit(t, g.repo, "push", "-q", "origin", "main")
	return postShipGit(t, g.repo, "rev-parse", "HEAD")
}

func (g *handRig) shipped(id string, body map[string]any) (int, string) {
	g.t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		g.t.Fatal(err)
	}
	rr := liveDo(g.s.handleAutonomousPRDs, http.MethodPost, "/api/autonomous/prds/"+id+"/shipped", string(data))
	waitHandShadows()
	return rr.Code, rr.Body.String()
}

func (g *handRig) ship(p *autonomous.PRD, commit string, extra ...map[string]any) {
	g.t.Helper()
	body := map[string]any{"merge_commit": commit, "merged_in_release": "1.2.4", "source": "merge_step"}
	for _, m := range extra {
		for k, v := range m {
			body[k] = v
		}
	}
	if code, out := g.shipped(p.ID, body); code != http.StatusOK {
		g.t.Fatalf("shipped: %d %s", code, out)
	}
}

func (g *handRig) replays() []*mergeshadow.Replay {
	g.t.Helper()
	out, err := mergeshadow.ListReplays(g.s.statePath())
	if err != nil {
		g.t.Fatal(err)
	}
	return out
}

type turnOnWire struct {
	TurnOn *struct {
		Project   string                   `json:"project"`
		MergingOn bool                     `json:"merging_on"`
		Target    int                      `json:"target"`
		Met       bool                     `json:"met"`
		Streak    mergeshadow.StreakResult `json:"streak"`
		Merges    []struct {
			AutomatonID string            `json:"automaton_id"`
			Verdict     string            `json:"verdict"`
			Rows        []mergeshadow.Row `json:"rows"`
		} `json:"merges"`
		Eligible      []autonomous.MergeSelectionEntry `json:"eligible"`
		Excluded      []autonomous.MergeSelectionEntry `json:"excluded"`
		NextPush      []string                         `json:"next_push"`
		Actions       []string                         `json:"actions"`
		StillToApply  []string                         `json:"still_to_apply"`
		PlantedFaults []string                         `json:"planted_faults"`
		TurnOff       string                           `json:"turn_off"`
		Lines         []string                         `json:"lines"`
	} `json:"turn_on"`
}

func (g *handRig) turnOn() turnOnWire {
	g.t.Helper()
	rr := liveDo(g.s.handleAutonomousMergeShadow, http.MethodGet, "/api/autonomous/merge/shadow", "")
	var out turnOnWire
	if rr.Code != http.StatusOK {
		g.t.Fatalf("shadow read: %d %s", rr.Code, rr.Body)
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		g.t.Fatal(err)
	}
	if out.TurnOn == nil {
		g.t.Fatalf("no turn-on report: %s", rr.Body)
	}
	return out
}

var handChecks = map[string]string{"lint": "pass", "unit": "pass"}

func TestHandShipRecordMakesOneSavedShadowAndTwoAutomataMakeAStreakOfTwo(t *testing.T) {
	g := newHandRig(t)
	one := g.automaton("one", "bug")
	first := g.handMerge(one, "1.2.3", "1.2.4")
	refs := switchHead(t, g.repo)
	g.ship(one, first, map[string]any{"hand_checks": handChecks})
	got := g.replays()
	if len(got) != 1 || got[0].AutomatonID != one.ID || got[0].Verdict() != mergeshadow.VerdictMatch {
		for _, r := range got {
			t.Logf("%s %s: %+v\n%+v", r.State, r.Error, r.Rows(), r.Daemon)
		}
		t.Fatalf("replays = %d", len(got))
	}
	if switchHead(t, g.repo) != refs {
		t.Errorf("the shadow changed the project's refs")
	}
	// The same record again neither adds nor reruns.
	at := got[0].UpdatedAt
	g.ship(one, first)
	if again := g.replays(); len(again) != 1 || !again[0].UpdatedAt.Equal(at) || again[0].Verdict() != mergeshadow.VerdictMatch {
		t.Errorf("recording again changed the shadow: %+v", again)
	}

	two := g.automaton("two", "feature")
	two.ReleaseSize = "patch"
	if err := g.mgr.Store().SavePRD(two); err != nil {
		t.Fatal(err)
	}
	second := g.handMerge(two, "1.2.4", "1.2.5")
	g.ship(two, second)
	// Without the hand results it waits, and does not count.
	rep := g.turnOn().TurnOn
	if rep.Streak.Length != 0 || len(rep.Merges) != 2 || rep.Merges[0].Verdict != mergeshadow.VerdictWaiting {
		t.Fatalf("waiting: %+v %+v", rep.Streak, rep.Merges)
	}
	g.ship(two, second, map[string]any{"hand_checks": handChecks})

	waiting := g.automaton("waiting", "bug")
	rep = g.turnOn().TurnOn
	project := filepath.Base(g.repo)
	if rep.Project != project || rep.Streak.Length != 2 || rep.Streak.Total != 2 || rep.Streak.Bugs != 1 || rep.Streak.Features != 1 {
		for _, m := range rep.Merges {
			t.Logf("%+v", m)
		}
		t.Fatalf("project %q streak %+v", rep.Project, rep.Streak)
	}
	for _, r := range g.replays() {
		if r.Project != project {
			t.Errorf("project key %q", r.Project)
		}
	}
	if rep.MergingOn || rep.Target != config.DefaultMergeShadowAgreementStreak || rep.Met {
		t.Errorf("on=%v target=%d met=%v", rep.MergingOn, rep.Target, rep.Met)
	}
	for _, m := range rep.Merges {
		if len(m.Rows) != 7 {
			t.Errorf("a merge without seven results: %+v", m)
		}
	}
	listed := map[string]bool{}
	for _, e := range append(rep.Eligible, rep.Excluded...) {
		listed[e.AutomatonID] = true
		if !e.Eligible && len(e.Reasons) == 0 {
			t.Errorf("excluded with no reason: %+v", e)
		}
	}
	_ = waiting
	all := strings.Join(rep.Lines, "\n")
	for _, want := range []string{"2 of 5", "Eligible", "Excluded", "next push", "push", "autonomous.merge_deploy_run_enabled", "Planted faults", "turn it off"} {
		if !strings.Contains(strings.ToLower(all), strings.ToLower(want)) {
			t.Errorf("the report lacks %q:\n%s", want, all)
		}
	}
	if len(rep.Actions) == 0 || len(rep.StillToApply) == 0 || len(rep.PlantedFaults) == 0 || rep.TurnOff == "" || len(rep.NextPush) == 0 {
		t.Errorf("sections: actions %v apply %v faults %v off %q next %v", rep.Actions, rep.StillToApply, rep.PlantedFaults, rep.TurnOff, rep.NextPush)
	}

	// A target the streak reaches is met.
	n := 2
	g.s.cfg.Autonomous.MergeShadow.AgreementStreak = &n
	if rep := g.turnOn().TurnOn; !rep.Met {
		t.Errorf("2 of 2 is not met: %+v", rep.Streak)
	}
	// A change of the pipeline record restarts the count.
	changed := g.def
	changed.Steps = append([]mergepipeline.Step(nil), g.def.Steps...)
	changed.Steps = append(changed.Steps, mergepipeline.Step{Name: "extra", Kind: mergepipeline.KindCustom, Command: "true"})
	g.record(changed)
	if rep := g.turnOn().TurnOn; rep.Streak.Length != 0 || !strings.Contains(rep.Streak.StoppedBy, "settings") || rep.Met {
		t.Errorf("after a pipeline change: %+v", rep.Streak)
	}
	g.record(g.def)
	// So does a merge setting, and a check script changed on main.
	other := "another"
	g.s.cfg.Autonomous.MergePipeline.ConflictModel = &other
	if rep := g.turnOn().TurnOn; rep.Streak.Length != 0 {
		t.Errorf("after a merge setting change: %+v", rep.Streak)
	}
	g.s.cfg.Autonomous.MergePipeline.ConflictModel = nil
	if rep := g.turnOn().TurnOn; rep.Streak.Length != 2 {
		t.Fatalf("back to the same settings: %+v", rep.Streak)
	}
	g.put("scripts/check-outgoing.sh", handCheckScript+"# changed\n")
	postShipGit(t, g.repo, "commit", "-q", "-am", "change a check script")
	if rep := g.turnOn().TurnOn; rep.Streak.Length != 0 || !strings.Contains(rep.Streak.StoppedBy, "check") {
		t.Errorf("after a check script change: %+v", rep.Streak)
	}
}

func TestHandShipShadowOnlyForHandMergeRecords(t *testing.T) {
	g := newHandRig(t)
	p := g.automaton("one", "bug")
	commit := g.handMerge(p, "1.2.3", "1.2.4")

	for name, checks := range map[string]map[string]string{"unknown result": {"lint": "ok"}, "bad name": {"../x": "pass"}} {
		code, _ := g.shipped(p.ID, map[string]any{"merge_commit": commit, "source": "merge_step", "hand_checks": checks})
		if code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, code)
		}
	}
	if got, _ := g.mgr.Store().GetPRD(p.ID); got.IsShipped() {
		t.Fatal("a refused request recorded the shipped state")
	}
	code, _ := g.shipped(p.ID, map[string]any{"merge_commit": commit, "source": "backfill"})
	if code != http.StatusOK || len(g.replays()) != 0 {
		t.Errorf("backfill: %d, %d shadow(s)", code, len(g.replays()))
	}

	off := false
	g.s.cfg.Autonomous.MergeShadow.Enabled = &off
	q := g.automaton("two", "bug")
	g.ship(q, g.handMerge(q, "1.2.4", "1.2.5"))
	if len(g.replays()) != 0 {
		t.Errorf("a shadow ran with merge_shadow off: %d", len(g.replays()))
	}
}

// A shadow a daemon restart cut off reads as a miss with its reason, and
// recording the shipped state again runs it again. One that cannot run keeps
// its reason.
func TestHandShipShadowInterruptedOrUnrunnable(t *testing.T) {
	g := newHandRig(t)
	p := g.automaton("one", "bug")
	commit := g.handMerge(p, "1.2.3", "1.2.4")
	cut := &mergeshadow.Replay{Key: mergeshadow.ReplayKey(p.ID, commit), AutomatonID: p.ID, Project: filepath.Base(g.repo),
		Branch: "one", State: mergeshadow.ReplayRunning, CreatedAt: time.Now(), Hand: mergeshadow.HandMerge{Commit: commit}}
	if err := mergeshadow.SaveReplay(g.s.statePath(), cut); err != nil {
		t.Fatal(err)
	}
	rep := g.turnOn().TurnOn
	if len(rep.Merges) != 1 || rep.Merges[0].Verdict != mergeshadow.VerdictDiffers || !strings.Contains(rep.Merges[0].Rows[0].Detail, "interrupted") {
		t.Fatalf("a cut-off shadow reads %+v", rep.Merges)
	}
	g.ship(p, commit, map[string]any{"hand_checks": handChecks})
	got := g.replays()
	if len(got) != 1 || got[0].Verdict() != mergeshadow.VerdictMatch {
		t.Fatalf("rerun: %s %s %+v", got[0].State, got[0].Error, got[0].Rows())
	}

	// Fast-forwarded: there is no base to build from, and the reason is kept.
	q := g.automaton("two", "bug")
	postShipGit(t, g.repo, "merge", "-q", "--ff-only", "two")
	postShipGit(t, g.repo, "push", "-q", "origin", "main")
	g.ship(q, postShipGit(t, g.repo, "rev-parse", "HEAD"))
	for _, r := range g.replays() {
		if r.AutomatonID == q.ID && (r.State != mergeshadow.ReplayError || !strings.Contains(r.Error, "no merge commit")) {
			t.Errorf("state %s, reason %q", r.State, r.Error)
		}
	}
	if len(g.replays()) != 2 {
		t.Errorf("replays = %d, want the failed one stored too", len(g.replays()))
	}
}
