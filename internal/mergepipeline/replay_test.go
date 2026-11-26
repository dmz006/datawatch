//go:build unix

package mergepipeline

// The shadow of a hand merge, in sandboxes: a made-up repository with a local
// bare "remote", a made-up private pattern list, planted fake values and a
// scripted hand merge. Nothing here leaves the machine.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/mergeshadow"
)

const plantedWord = "zzqx-private-word"

const sandboxOutgoingCheck = `#!/bin/sh
list="$OUTGOING_PROJECT_DIR/.leak-patterns.local"
n=$(grep -c . "$list" 2>/dev/null)
[ "${n:-0}" -gt 0 ] || { echo "check-outgoing: the private pattern list is missing"; exit 2; }
if grep -qFf "$list" "$OUTGOING_MANIFEST_DIR/added.txt" || grep -rqFf "$list" "$OUTGOING_MANIFEST_DIR/commits"; then
	echo "check-outgoing: 1 finding(s); $n private pattern(s) loaded"
	exit 1
fi
echo "check-outgoing: clean ($n private pattern(s) loaded)"
`

// replayRig is a project whose base declares an outgoing check and reads an
// uncommitted private list, with one finished branch, "feature".
type replayRig struct {
	sandbox
	t      *testing.T
	ship   *fakeShip
	model  *countingModel
	asks   *fakeReleaseQuestions
	notify *fakeNotify
	def    Definition
	policy ConflictPolicy
}

func newReplayRig(t *testing.T) *replayRig {
	t.Helper()
	committerEnv(t)
	dir := newMergeRepo(t)
	putFile(t, dir, "Makefile", "check-outgoing:\n\t@sh scripts/check-outgoing.sh\n")
	putFile(t, dir, "scripts/check-outgoing.sh", sandboxOutgoingCheck)
	putFile(t, dir, ".gitignore", ".leak-patterns.local\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "outgoing check")
	putFile(t, dir, ".leak-patterns.local", plantedWord+"\n")
	runGit(t, dir, "branch", "-f", "feature", "main")
	runGit(t, dir, "checkout", "-q", "feature")
	putFile(t, dir, "feature.txt", "new\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "feature work")
	runGit(t, dir, "checkout", "-q", "main")
	g := &replayRig{
		sandbox: sandbox{dir: dir, bare: withOrigin(t, dir), notes: t.TempDir()}, t: t,
		ship: &fakeShip{}, model: &countingModel{}, asks: newFakeReleaseQuestions(), notify: &fakeNotify{},
		policy: PolicyStopAndAsk,
	}
	g.setChecks(Step{Name: "lint", Kind: KindLint, Command: "test -f feature.txt"}, check("go-test", "true"))
	return g
}

// setChecks makes the pipeline the default one, with no tag or deploy step and
// with checks before the release validator.
func (g *replayRig) setChecks(checks ...Step) {
	var steps []Step
	for _, s := range noTagSteps(checks...).Steps {
		if s.Name != "check-internal-refs" { // the sandbox has no such target
			steps = append(steps, s)
		}
	}
	g.def = Definition{Steps: steps}
	g.def.Shadow = &ShadowDefinition{PrivateFiles: []string{".leak-patterns.local"}}
}

// addCheck appends one more test step.
func (g *replayRig) addCheck(name, command string) {
	g.setChecks(Step{Name: "lint", Kind: KindLint, Command: "true"}, check("go-test", "true"), check(name, command))
}

type handOptions struct {
	// merge replaces the plain merge of the branch.
	merge func(dir string)
	// release runs after the version bump files are written, before the
	// release commit.
	release  func(dir string)
	date     string
	sentence string // the README release sentence
	body     string // text under the changelog heading
}

// handMerge is the scripted hand merge: merge the branch into main, bump the
// version, changelog and README, commit and push. It returns the release
// commit.
func (g *replayRig) handMerge(o handOptions) string {
	t := g.t
	t.Helper()
	if o.merge != nil {
		o.merge(g.dir)
	} else {
		runGit(t, g.dir, "merge", "-q", "--no-ff", "-m", "Merge branch 'feature'", "feature")
	}
	for _, f := range testVersionFiles {
		putFile(t, g.dir, f, "package x\n\nvar Version = \"1.2.4\"\n")
	}
	if o.date == "" {
		o.date = time.Now().UTC().Format("2006-01-02")
	}
	if o.sentence == "" {
		o.sentence = "Older notes."
	}
	entry := o.body
	if entry == "" {
		entry = "\n\n" // as the engine writes a heading with no text
	} else {
		entry += "\n"
	}
	putFile(t, g.dir, "CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n\n## v1.2.4 — Add the feature\n"+entry+"## v1.2.3 — older\n")
	putFile(t, g.dir, "README.md", "# Project\n\n**Current release: v1.2.4 ("+o.date+").** "+o.sentence+"\n")
	if o.release != nil {
		o.release(g.dir)
	}
	runGit(t, g.dir, "add", "-A")
	runGit(t, g.dir, "commit", "-q", "-m", "Set version 1.2.4")
	runGit(t, g.dir, "push", "-q", "origin", "main")
	return runGit(t, g.dir, "rev-parse", "HEAD")
}

func (g *replayRig) input(hand string) ReplayInput {
	t := g.t
	t.Helper()
	records := resolverWithRecord(t, g.def)
	return ReplayInput{
		Replay: mergeshadow.Replay{
			Key: mergeshadow.ReplayKey("a1", hand), AutomatonID: "a1", Kind: "bug", Project: "p", Branch: "feature",
			CreatedAt: time.Now(), Hand: mergeshadow.HandMerge{Commit: hand, At: time.Now()},
		},
		Repo: g.dir, WorkDir: filepath.Join(t.TempDir(), "work"), Title: "Add the feature",
		BranchHead: runGit(t, g.dir, "rev-parse", "feature"),
		Select: func(_ context.Context, tip string) *mergeshadow.ReplaySelection {
			return &mergeshadow.ReplaySelection{Eligible: true, Basis: "review_passed", Tip: tip}
		},
		Runner: func(copyDir, scratch string) *Runner {
			return &Runner{
				Resolver: records, VersionFiles: testVersionFiles, Dir: copyDir, Project: "p", ScratchRoot: scratch,
				Ship: g.ship, Model: g.model, Questions: g.asks, Notifier: g.notify, Recorder: &memRecorder{},
				ReleaseSize: "patch", ReleaseApproval: "ask",
				ConflictSettings: func() ConflictSettings {
					s := DefaultConflictSettings()
					s.Policy = g.policy
					return s
				},
				PostShip: PostShipSettings{Enabled: true, NotifyTemplate: "x",
					Hooks: []PostShipHook{{Name: "notify", Builtin: "notify", Enabled: true}}},
			}
		},
	}
}

// shadow runs the shadow of hand and proves it changed nothing in the project
// or on the remote, asked and sent nothing and removed its folder.
func (g *replayRig) shadow(hand string, edit ...func(*ReplayInput)) *mergeshadow.Replay {
	t := g.t
	t.Helper()
	in := g.input(hand)
	for _, e := range edit {
		e(&in)
	}
	before := g.state(t)
	r := ShadowHandMerge(context.Background(), in)
	g.assertUnchanged(t, before)
	if len(g.ship.calls) != 0 || g.model.calls != 0 || g.asks.raised != 0 || len(g.notify.sent()) != 0 {
		t.Errorf("outward calls: ship=%v model=%d questions=%d notices=%v", g.ship.calls, g.model.calls, g.asks.raised, g.notify.sent())
	}
	if _, err := os.Stat(in.WorkDir); !os.IsNotExist(err) {
		t.Errorf("the shadow's folder is still there: %v", err)
	}
	return r
}

func replayRow(t *testing.T, r *mergeshadow.Replay, n int) mergeshadow.Row {
	t.Helper()
	rows := r.Rows()
	if len(rows) != 7 {
		t.Fatalf("rows: %+v", rows)
	}
	return rows[n-1]
}

// wantMiss asserts the verdict is a miss and row n says why.
func wantMiss(t *testing.T, r *mergeshadow.Replay, n int, why string) {
	t.Helper()
	row := replayRow(t, r, n)
	if r.Verdict() != mergeshadow.VerdictDiffers || row.Result == mergeshadow.RowMatch || !strings.Contains(row.Detail, why) {
		t.Errorf("verdict %s, row %d %+v; want a miss mentioning %q\nrows %+v\nerror %q", r.Verdict(), n, row, why, r.Rows(), r.Error)
	}
}

func allPass(r *mergeshadow.Replay) {
	r.HandChecks = map[string]string{"lint": "pass", "go-test": "pass"}
}

func TestShadowHandMergeMatches(t *testing.T) {
	g := newReplayRig(t)
	base := runGit(t, g.dir, "rev-parse", "main")
	tip := runGit(t, g.dir, "rev-parse", "feature")
	hand := g.handMerge(handOptions{date: "2026-01-02"})
	r := g.shadow(hand)
	if r.State != mergeshadow.ReplayDone {
		t.Fatalf("state %s: %s", r.State, r.Error)
	}
	if r.Hand.Base != base || r.Hand.Tip != tip || r.Hand.Version != "1.2.4" || r.Hand.Size != "patch" {
		t.Errorf("hand = %+v, want base %s tip %s", r.Hand, base, tip)
	}
	if v := r.Verdict(); v != mergeshadow.VerdictWaiting {
		t.Errorf("before the hand results: %s %+v", v, r.Rows())
	}
	allPass(r)
	if v := r.Verdict(); v != mergeshadow.VerdictMatch {
		t.Fatalf("verdict %s: %+v\ndaemon %+v", v, r.Rows(), r.Daemon)
	}
	if len(r.Content.Allowed) != 1 || r.Content.Allowed[0] != "README.md" {
		t.Errorf("allowed = %v, want the README date only", r.Content.Allowed)
	}
	if r.Leak.PrivatePatterns != 1 || r.SettingsHash == "" || r.ChecksHash == "" {
		t.Errorf("leak %+v hashes %q %q", r.Leak, r.SettingsHash, r.ChecksHash)
	}
	// The fingerprints answer for the project as it is now.
	in := g.input(hand)
	now, err := SettingsFingerprint(in.Runner(g.dir, t.TempDir()), nil)
	if err != nil || now != r.SettingsHash {
		t.Errorf("settings now %q, replay %q: %v", now, r.SettingsHash, err)
	}
	checks, err := CheckFilesFingerprint(context.Background(), g.dir, "refs/heads/main", r.CheckFiles)
	if err != nil || checks != r.ChecksHash {
		t.Errorf("checks now %q, replay %q: %v", checks, r.ChecksHash, err)
	}
	// A pipeline change, a conflict setting and a check script change each
	// change a fingerprint.
	g.policy = PolicyMechanical
	if other, _ := SettingsFingerprint(g.input(hand).Runner(g.dir, t.TempDir()), nil); other == now {
		t.Error("a conflict policy change kept the settings fingerprint")
	}
	g.policy = PolicyStopAndAsk
	g.def.ConflictRules = []ConflictRule{{Path: "x", Rule: ConflictRuleAsk}}
	if other, _ := SettingsFingerprint(g.input(hand).Runner(g.dir, t.TempDir()), nil); other == now {
		t.Error("a conflict rule change kept the settings fingerprint")
	}
	if other, _ := SettingsFingerprint(in.Runner(g.dir, t.TempDir()), []byte("x")); other == now {
		t.Error("a merge setting change kept the settings fingerprint")
	}
	putFile(t, g.dir, "scripts/check-outgoing.sh", sandboxOutgoingCheck+"# changed\n")
	runGit(t, g.dir, "commit", "-q", "-am", "change a check")
	if other, _ := CheckFilesFingerprint(context.Background(), g.dir, "refs/heads/main", r.CheckFiles); other == checks {
		t.Error("a check script change kept the check fingerprint")
	}
}

func TestShadowHandMergeContentDifference(t *testing.T) {
	g := newReplayRig(t)
	hand := g.handMerge(handOptions{release: func(dir string) { putFile(t, dir, "feature.txt", "edited by hand\n") }})
	r := g.shadow(hand)
	allPass(r)
	wantMiss(t, r, mergeshadow.RowContent, "feature.txt")
}

func TestShadowHandMergeReleaseWording(t *testing.T) {
	g := newReplayRig(t)
	hand := g.handMerge(handOptions{sentence: "A sentence written by hand.", body: "\nNotes written by hand.\n"})
	r := g.shadow(hand)
	allPass(r)
	wantMiss(t, r, mergeshadow.RowContent, "CHANGELOG.md, README.md")

	g.def.Shadow.ReleaseText = ReleaseTextIgnore
	r = g.shadow(hand)
	allPass(r)
	if v := r.Verdict(); v != mergeshadow.VerdictMatch {
		t.Errorf("release_text ignore: %s %+v", v, r.Rows())
	}
}

// With release_text ignore, only the new release's own text is left out.
func TestShadowReleaseTextIgnoreStillComparesTheRest(t *testing.T) {
	g := newReplayRig(t)
	hand := g.handMerge(handOptions{release: func(dir string) {
		putFile(t, dir, "README.md", "# Project renamed\n\n**Current release: v1.2.4 (2026-01-02).** x\n")
		putFile(t, dir, "CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n\n## v1.2.4 — Add the feature\n\n## v1.2.3 — older, rewritten\n")
	}})
	g.def.Shadow.ReleaseText = ReleaseTextIgnore
	r := g.shadow(hand)
	allPass(r)
	wantMiss(t, r, mergeshadow.RowContent, "CHANGELOG.md, README.md")
}

// The changelog entry the Automaton drafted goes under the heading the engine
// writes, as the hand merge does with it.
func TestShadowUsesTheChangelogDraft(t *testing.T) {
	g := newReplayRig(t)
	runGit(t, g.dir, "checkout", "-q", "feature")
	putFile(t, g.dir, "docs/plans/drafts/a1-changelog.md", "## v0.0.0 — draft heading\n\n- the drafted line\n")
	runGit(t, g.dir, "add", "-A")
	runGit(t, g.dir, "commit", "-q", "-m", "changelog draft")
	runGit(t, g.dir, "checkout", "-q", "main")
	runGit(t, g.dir, "push", "-q", "origin", "feature")
	hand := g.handMerge(handOptions{body: "\n- the drafted line\n"})
	r := g.shadow(hand, func(in *ReplayInput) { in.Replay.AutomatonID = "a1" })
	allPass(r)
	if v := r.Verdict(); v != mergeshadow.VerdictMatch {
		t.Errorf("%s %+v", v, r.Rows())
	}
}

// ---- planted faults: every one must be held ----

func TestShadowCatchesAPlantedSecret(t *testing.T) {
	g := newReplayRig(t)
	runGit(t, g.dir, "checkout", "-q", "feature")
	putFile(t, g.dir, "feature.txt", "key = "+"AKIA"+"Q7ZC2LM9XK4TB8WN"+"\n")
	runGit(t, g.dir, "commit", "-q", "-am", "planted")
	runGit(t, g.dir, "checkout", "-q", "main")
	r := g.shadow(g.handMerge(handOptions{}))
	allPass(r)
	wantMiss(t, r, mergeshadow.RowLeak, "secret")
	if strings.Contains(r.Leak.Refusal+r.Leak.ProjectCheckOutput+r.Daemon.Reason+r.Daemon.StoppedAt, "Q7ZC2LM9XK4TB8WN") {
		t.Error("the stored record holds the planted value")
	}
}

func TestShadowCatchesAPrivateWordInACommitMessage(t *testing.T) {
	g := newReplayRig(t)
	runGit(t, g.dir, "checkout", "-q", "feature")
	runGit(t, g.dir, "commit", "-q", "--allow-empty", "-m", "see "+plantedWord)
	runGit(t, g.dir, "checkout", "-q", "main")
	r := g.shadow(g.handMerge(handOptions{}))
	allPass(r)
	wantMiss(t, r, mergeshadow.RowLeak, "check-outgoing")
}

func TestShadowCatchesAMissingPrivateList(t *testing.T) {
	g := newReplayRig(t)
	hand := g.handMerge(handOptions{})
	if err := os.Remove(filepath.Join(g.dir, ".leak-patterns.local")); err != nil {
		t.Fatal(err)
	}
	r := g.shadow(hand)
	allPass(r)
	wantMiss(t, r, mergeshadow.RowLeak, "missing")
}

func TestShadowCatchesAProjectWithNoLocalCheck(t *testing.T) {
	g := newReplayRig(t)
	runGit(t, g.dir, "rm", "-q", "Makefile")
	runGit(t, g.dir, "commit", "-q", "-m", "no check declared")
	runGit(t, g.dir, "push", "-q", "origin", "main")
	r := g.shadow(g.handMerge(handOptions{}))
	allPass(r)
	wantMiss(t, r, mergeshadow.RowLeak, "check-outgoing")
}

func TestShadowCatchesAFailingCheck(t *testing.T) {
	g := newReplayRig(t)
	g.setChecks(Step{Name: "lint", Kind: KindLint, Command: "true"}, check("go-test", "exit 1"))
	r := g.shadow(g.handMerge(handOptions{}))
	allPass(r)
	wantMiss(t, r, mergeshadow.RowChecks, "go-test")
	if !strings.Contains(r.Daemon.StoppedAt, "go-test") {
		t.Errorf("stopped at %q", r.Daemon.StoppedAt)
	}
}

// Candidate commands that try to leave: each reaches only the shadow's own
// copy, and each is reported.
func TestShadowCatchesACommandThatLeaves(t *testing.T) {
	for name, c := range map[string]struct{ command, why string }{
		"pushes to origin":        {"git push -q origin HEAD:refs/heads/leak", "refs/heads/leak"},
		"pushes main":             {"git push -q origin HEAD:main || true", "remote"},
		"tags":                    {"git tag planted-tag", "planted-tag"},
		"moves a branch":          {`cd "$MERGE_PROJECT_DIR" && git update-ref refs/heads/moved HEAD`, "refs/heads/moved"},
		"writes into the project": {`touch "$MERGE_PROJECT_DIR/left-behind"`, "left-behind"},
	} {
		t.Run(name, func(t *testing.T) {
			g := newReplayRig(t)
			g.addCheck("reach-out", c.command)
			r := g.shadow(g.handMerge(handOptions{}))
			r.HandChecks = map[string]string{"lint": "pass", "go-test": "pass", "reach-out": "pass"}
			wantMiss(t, r, mergeshadow.RowSideEffects, c.why)
		})
	}
}

// A command that knows the real remote's address and pushes there anyway is
// not prevented. It is seen.
func TestShadowSeesAPushToTheRealRemote(t *testing.T) {
	g := newReplayRig(t)
	g.addCheck("reach-out", "git push -q '"+g.bare+"' HEAD:refs/heads/escaped")
	hand := g.handMerge(handOptions{})
	in := g.input(hand)
	r := ShadowHandMerge(context.Background(), in)
	r.HandChecks = map[string]string{"lint": "pass", "go-test": "pass", "reach-out": "pass"}
	wantMiss(t, r, mergeshadow.RowSideEffects, "refs/heads/escaped")
}

func TestShadowCatchesAProcessLeftRunning(t *testing.T) {
	g := newReplayRig(t)
	pidFile := g.note("pid")
	// A bounded sleep in its own session, so the end of the step cannot stop it.
	g.addCheck("lingers", "setsid timeout 60 sleep 45 >/dev/null 2>&1 & echo $! > '"+pidFile+"'; sleep 1")
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid is not available")
	}
	t.Cleanup(func() {
		out, _ := exec.Command("pgrep", "-f", "timeout 60 sleep 45").Output()
		for _, f := range strings.Fields(string(out)) {
			var pid int
			for _, c := range f {
				pid = pid*10 + int(c-'0')
			}
			_ = syscall.Kill(-pid, syscall.SIGKILL) // end-reason-ok: ends the test's own bounded sleeper
		}
	})
	r := g.shadow(g.handMerge(handOptions{}))
	r.HandChecks = map[string]string{"lint": "pass", "go-test": "pass", "lingers": "pass"}
	wantMiss(t, r, mergeshadow.RowSideEffects, "process")
}

// Someone else's push while the shadow runs is listed apart.
func TestShadowListsOthersChangesApart(t *testing.T) {
	g := newReplayRig(t)
	other := filepath.Join(t.TempDir(), "other")
	g.addCheck("meanwhile", "test -e '"+g.note("done")+"' || { git -C '"+other+"' push -q origin main && touch '"+g.note("done")+"'; }")
	hand := g.handMerge(handOptions{})
	runGit(t, filepath.Dir(other), "clone", "-q", g.bare, other)
	runGit(t, other, "config", "user.email", "t@example.invalid")
	runGit(t, other, "config", "user.name", "Test")
	runGit(t, other, "config", "commit.gpgsign", "false")
	putFile(t, other, "elsewhere.txt", "x\n")
	runGit(t, other, "add", "-A")
	runGit(t, other, "commit", "-q", "-m", "someone else")
	r := ShadowHandMerge(context.Background(), g.input(hand))
	r.HandChecks = map[string]string{"lint": "pass", "go-test": "pass", "meanwhile": "pass"}
	if v := r.Verdict(); v != mergeshadow.VerdictMatch {
		t.Fatalf("%s %+v", v, r.Rows())
	}
	if len(r.SideEffects.Elsewhere) == 0 || !strings.Contains(strings.Join(r.SideEffects.Elsewhere, " "), "refs/heads/main") {
		t.Errorf("elsewhere = %v", r.SideEffects.Elsewhere)
	}
}

// The shadow's commands do not know where the real project or remote are.
func TestShadowCommandsSeeOnlyTheCopy(t *testing.T) {
	g := newReplayRig(t)
	g.addCheck("look", `{ git remote -v; echo "dir=$MERGE_PROJECT_DIR"; echo "ssh=$GIT_SSH_COMMAND"; cat "$MERGE_PROJECT_DIR/.git/config"; } > '`+g.note("seen")+`'`)
	g.shadow(g.handMerge(handOptions{}))
	seen := g.read("seen")
	if seen == "" || strings.Contains(seen, g.bare) || strings.Contains(seen, g.dir) || !strings.Contains(seen, "ssh=false") {
		t.Errorf("the command saw:\n%s", seen)
	}
}

// ---- conflicts ----

func conflicting(t *testing.T, g *replayRig) string {
	t.Helper()
	runGit(t, g.dir, "checkout", "-q", "feature")
	putFile(t, g.dir, "shared.txt", "branch side\n")
	runGit(t, g.dir, "commit", "-q", "-am", "branch edits shared")
	runGit(t, g.dir, "checkout", "-q", "main")
	putFile(t, g.dir, "shared.txt", "main side\n")
	runGit(t, g.dir, "commit", "-q", "-am", "main edits shared")
	runGit(t, g.dir, "push", "-q", "origin", "main")
	return g.handMerge(handOptions{merge: func(dir string) {
		cmd := exec.Command("git", "-C", dir, "merge", "--no-ff", "feature")
		_ = cmd.Run() // conflicts
		putFile(t, dir, "shared.txt", "resolved by hand\n")
		runGit(t, dir, "add", "-A")
		runGit(t, dir, "commit", "-q", "-m", "Merge branch 'feature'")
	}})
}

func TestShadowConflictItWouldAskAbout(t *testing.T) {
	g := newReplayRig(t)
	r := g.shadow(conflicting(t, g))
	if v := r.Verdict(); v != mergeshadow.VerdictAsked {
		t.Fatalf("%s %+v\ndaemon %+v err %q", v, r.Rows(), r.Daemon, r.Error)
	}
	if len(r.Hand.Conflicts) != 1 || r.Hand.Conflicts[0] != "shared.txt" || len(r.Daemon.Conflicts) != 1 {
		t.Errorf("conflicts hand %v daemon %v", r.Hand.Conflicts, r.Daemon.Conflicts)
	}
}

// ---- a shadow that cannot run says why ----

func TestShadowThatCannotRunSaysWhy(t *testing.T) {
	for name, c := range map[string]struct {
		hand func(g *replayRig) string
		why  string
	}{
		"no merge commit": {func(g *replayRig) string {
			runGit(g.t, g.dir, "merge", "-q", "--ff-only", "feature")
			return runGit(g.t, g.dir, "rev-parse", "HEAD")
		}, "no merge commit"},
		"the commit lacks the branch": {func(g *replayRig) string { return runGit(g.t, g.dir, "rev-parse", "main") }, "does not contain"},
		"the release carried other work": {func(g *replayRig) string {
			return g.handMerge(handOptions{release: nil, merge: func(dir string) {
				runGit(g.t, dir, "merge", "-q", "--no-ff", "-m", "Merge branch 'feature'", "feature")
				runGit(g.t, dir, "checkout", "-q", "-b", "other", "main~1")
				putFile(g.t, dir, "other.txt", "x\n")
				runGit(g.t, dir, "add", "-A")
				runGit(g.t, dir, "commit", "-q", "-m", "other work")
				runGit(g.t, dir, "checkout", "-q", "main")
				runGit(g.t, dir, "merge", "-q", "--no-ff", "-m", "Merge branch 'other'", "other")
			}})
		}, "other merges"},
		"an unknown commit": {func(*replayRig) string { return strings.Repeat("ab", 20) }, "not in the repository"},
	} {
		t.Run(name, func(t *testing.T) {
			g := newReplayRig(t)
			hand := c.hand(g)
			r := ShadowHandMerge(context.Background(), g.input(hand))
			if r.State != mergeshadow.ReplayError || !strings.Contains(r.Error, c.why) || r.Verdict() != mergeshadow.VerdictDiffers {
				t.Errorf("state %s error %q, want %q", r.State, r.Error, c.why)
			}
		})
	}
}

// A project that is a shallow clone is shadowed all the same.
func TestShadowOfAShallowProject(t *testing.T) {
	g := newReplayRig(t)
	hand := g.handMerge(handOptions{})
	// Cut the history behind the commit that declared the outgoing check.
	boundary := runGit(t, g.dir, "rev-list", "--max-count=1", "--grep=^outgoing check$", "main")
	if err := os.WriteFile(filepath.Join(g.dir, ".git", "shallow"), []byte(boundary+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, g.dir, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("fixture: shallow = %s", got)
	}
	r := g.shadow(hand)
	allPass(r)
	if v := r.Verdict(); v != mergeshadow.VerdictMatch {
		t.Errorf("%s: %q %+v", v, r.Error, r.Rows())
	}
}
