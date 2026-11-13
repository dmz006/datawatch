package autonomous

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/platformcontext"
)

// workerEntryBody is checkpoint entry text a worker added to the rules file
// (a dated heading, a blank line, a body bullet, separated from the base by
// blank lines).
const workerEntryBody = "\n\n## 2026-11-09 14:14 — DONE — rewrote the picker\n\nReplaced the chip row with the dropdown.\n"

// strippedEqual reports whether the two texts strip to the same daemon-free
// text, the equality the gate requires after a revert.
func strippedEqual(t *testing.T, a, b string) bool {
	t.Helper()
	return platformcontext.StripDaemonSections(a) == platformcontext.StripDaemonSections(b)
}

func TestStripWorkerCheckpointEntriesEntriesOnly(t *testing.T) {
	base := baseRulesContent
	cases := []struct {
		name string
		cur  string
		want bool
	}{
		{
			name: "one entry at the end",
			cur:  base + "\n\n## 2026-11-09\n\n- [x] step one\n- [x] step two\n",
			want: true,
		},
		{
			name: "entry with heading section the base did not use",
			cur:  base + "\n\n## Checkpoints\n\n## 2026-11-09\n\n- [x] step one\n",
			want: true,
		},
		{
			name: "several dated entries",
			cur:  base + "\n\n## 2026-11-08\n\n- [x] first\n\n## 2026-11-09\n\n- [x] second\n",
			want: true,
		},
		{
			name: "entry body line identical to a base line",
			cur:  base + "\n\n## 2026-11-09\nWrap every shell command with a start and end timestamp.\n",
			want: true,
		},
		{
			name: "entry in the middle of the file",
			cur:  "## One\n\nx\n\n## 2026-11-09\n\n- [x] task\n\n## Two\n\ny\n",
			want: true,
		},
		{
			name: "heading with time and title",
			cur:  base + "\n\n## 2026-11-09 14:14 — DONE — rewrote the picker\n\n- [x] step\n",
			want: true,
		},
		{
			name: "second-level heading only",
			cur:  base + "\n\n### 2026-11-09\n\n- [x] step\n",
			want: false,
		},
	}
	// A base with no entry at all.
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseFor := base
			if tc.name == "entry in the middle of the file" {
				baseFor = "## One\n\nx\n\n## Two\n\ny\n"
			}
			got, ok := stripWorkerCheckpointEntries(baseFor, tc.cur)
			if ok != tc.want {
				t.Fatalf("ok=%v; want %v", ok, tc.want)
			}
			if tc.want && !strippedEqual(t, got, baseFor) {
				t.Fatalf("cleaned %q must strip to the base %q", got, baseFor)
			}
		})
	}
}

func TestStripWorkerCheckpointEntriesFailClosed(t *testing.T) {
	base := baseRulesContent
	cases := []struct {
		name string
		base string
		cur  string
	}{
		{
			name: "worker edited a base line",
			cur:  "# Agent Rules\n\nWrap every command with timestamps.\n\n## 2026-11-09\n\n- [x] step\n",
		},
		{
			name: "worker removed a base line",
			cur:  "# Agent Rules\n\n## 2026-11-09\n\n- [x] step\n",
		},
		{
			name: "added text outside any entry",
			cur:  base + "\n\nFixed a bug in the picker.\n",
		},
		{
			name: "entry contains a non-dated section",
			cur:  base + "\n\n## Checkpoints\n\n## 2026-11-09\n\n- [x] a\n\n## Notes\n\nsomething\n",
		},
		{
			name: "two added Checkpoints headings",
			cur:  base + "\n\n## Checkpoints\n\n## 2026-11-09\n\n- [x] a\n\n## Checkpoints\n\n## 2026-11-10\n\n- [x] b\n",
		},
		{
			name: "Checkpoints heading already in the base",
			base: "# Agent Rules\n\n## Checkpoints\n",
			cur:  "# Agent Rules\n\n## Checkpoints\n\n## Checkpoints\n\n## 2026-11-09\n\n- [x] a\n",
		},
		{
			name: "blank lines but no entry",
			cur:  base + "\n\n\n\n",
		},
		{
			name: "no additions at all",
			cur:  base,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseFor := base
			if tc.base != "" {
				baseFor = tc.base
			}
			if got, ok := stripWorkerCheckpointEntries(baseFor, tc.cur); ok {
				t.Fatalf("ok=true cleaned=%q; want fail closed", got)
			}
		})
	}
}

func TestStripWorkerCheckpointEntriesDaemonSectionsPreserved(t *testing.T) {
	base := baseRulesContent + daemonSectionsBlock
	// The entry sits before the daemon's own sections, so it stays in the
	// stripped space the gate diffs; the daemon sections must survive the
	// write-back verbatim.
	cur := baseRulesContent + "\n\n## 2026-11-09 14:14 — DONE — rewrote the picker\n\nReplaced the chip row with the dropdown.\n" + daemonSectionsBlock

	got, ok := stripWorkerCheckpointEntries(base, cur)
	if !ok {
		t.Fatal("ok=false; want the entry reverted")
	}
	if !strippedEqual(t, got, base) {
		t.Fatalf("cleaned %q must strip to the base", got)
	}
	for _, marker := range []string{"<!-- datawatch:platform-context v1 -->", "<!-- rtk-instructions -->", "# Memory & Knowledge (datawatch)", "# Session Guardrails"} {
		if !contains(got, marker) {
			t.Errorf("the cleaned file lost a daemon section marker %q", marker)
		}
	}
	if contains(got, "rewrote the picker") {
		t.Error("the entry survived the revert")
	}
}

func TestCheckpointRevertEntryUnderDaemonSectionsNeedsNoRevert(t *testing.T) {
	// The daemon's own header sections run to the next `# ` h1 or EOF, so an
	// entry appended after them is owned by the daemon (it rewrites that
	// region on every session start): the daemon-filter pass already drops
	// the hit and nothing is touched.
	r := newGateRig(t)
	writeRulesFile(t, r.prd.ProjectDir, baseRulesContent+daemonSectionsBlock+workerEntryBody)
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; an entry under the daemon sections is not a hit against the base", held, err)
	}
	if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 0 {
		t.Errorf("revert decisions = %d; want none (the daemon-filter path handles it)", n)
	}
	b, _ := os.ReadFile(filepath.Join(r.prd.ProjectDir, "AGENT.md"))
	if !contains(string(b), "rewrote the picker") {
		t.Error("the working copy must be left exactly as found")
	}
}

func TestStripWorkerCheckpointEntriesHeadingFormats(t *testing.T) {
	base := baseRulesContent
	cases := []struct {
		heading string
		want    bool
	}{
		{"## 20261109", false},
		{"## 2026-11-9", false},
		{"## 2026-11-09x", false},
		{"## 2026-11-09 9:1", true}, // any text after the date plus a space may be the title
		{"## 2026-11-09 14:1", true},
		{"## " + "2026-11-09", true},
		{"## " + "2026-11-09 14:14", true},
		{"## " + "2026-11-09 14:14:09", true},
	}
	for _, tc := range cases {
		_, ok := stripWorkerCheckpointEntries(base, base+"\n\n"+tc.heading+"\nbody line\n")
		if ok != tc.want {
			t.Errorf("heading %q: ok=%v; want %v", tc.heading, ok, tc.want)
		}
	}
}

func TestCheckpointRevertEntryOnlyHoldsNothing(t *testing.T) {
	r := newGateRig(t)
	writeRulesFile(t, r.prd.ProjectDir, baseRulesContent+workerEntryBody)
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; an entry-only change must not hold", held, err)
	}
	p := filepath.Join(r.prd.ProjectDir, "AGENT.md")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strippedEqual(t, string(b), baseRulesContent) {
		t.Errorf("the file %q must strip to the base %q", string(b), baseRulesContent)
	}
	decs := r.decisions(checkpointEntryRevertedDecisionKind)
	if len(decs) != 1 {
		t.Fatalf("revert decisions = %+v; want exactly one", decs)
	}
	if want := "task " + r.task.ID + ": removed a worker checkpoint entry from AGENT.md"; decs[0].Note != want {
		t.Errorf("note = %q; want %q", decs[0].Note, want)
	}
	if decs[0].Actor != "autonomous" {
		t.Errorf("actor = %q; want autonomous", decs[0].Actor)
	}
	if n := r.questionCount(); n != 0 {
		t.Errorf("questions = %d; want none (no operator question for an entry-only change)", n)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status == TaskBlocked || tk.ProtectedPathHold != nil {
		t.Errorf("status=%s hold=%+v; the task must not be held", tk.Status, tk.ProtectedPathHold)
	}
	if a := r.alertTitles(); len(a) != 0 {
		t.Errorf("alerts = %v; want none", a)
	}
	// The gate must still issue only read-only git commands.
	readOnly := map[string]bool{"rev-parse": true, "merge-base": true, "diff": true, "ls-files": true, "log": true, "symbolic-ref": true, "show": true}
	for _, c := range r.git.calls {
		if !readOnly[c[0]] {
			t.Errorf("the gate ran a git command that may write: %v", c)
		}
	}
}

func TestCheckpointRevertPreservesDaemonSectionsAndHeading(t *testing.T) {
	r := newGateRig(t)
	// Entry and heading before the daemon block: both must be reverted while
	// the daemon sections survive the write-back.
	writeRulesFile(t, r.prd.ProjectDir, baseRulesContent+"\n\n## Checkpoints\n"+workerEntryBody+daemonSectionsBlock)
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; an entry-only change with daemon sections must not hold", held, err)
	}
	b, err := os.ReadFile(filepath.Join(r.prd.ProjectDir, "AGENT.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strippedEqual(t, string(b), baseRulesContent) {
		t.Errorf("the file must strip to the base")
	}
	if !contains(string(b), "<!-- rtk-instructions -->") {
		t.Error("the daemon RTK section must survive the revert")
	}
	if !contains(string(b), "# Memory & Knowledge (datawatch)") {
		t.Error("the daemon Memory section must survive the revert")
	}
	if contains(string(b), "rewrote the picker") {
		t.Error("the entry must be gone")
	}
}

func TestCheckpointRevertWorkerEditStillHolds(t *testing.T) {
	r := newGateRig(t)
	file := "# Agent Rules\n\nWrap every command, plus tests, with timestamps.\n" + workerEntryBody
	writeRulesFile(t, r.prd.ProjectDir, file)
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; entry plus a real edit must still hold", held, err)
	}
	b, _ := os.ReadFile(filepath.Join(r.prd.ProjectDir, "AGENT.md"))
	if !contains(string(b), "Wrap every command, plus tests, with timestamps.") {
		t.Error("the working copy must be untouched when the revert is not clean")
	}
	if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 0 {
		t.Errorf("revert decisions = %d; want none when nothing was reverted", n)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskBlocked || tk.ProtectedPathHold == nil {
		t.Errorf("status=%s hold=%+v; want blocked with a hold", tk.Status, tk.ProtectedPathHold)
	}
}

func TestCheckpointRevertBaseUnreadableFailsClosed(t *testing.T) {
	r := newGateRig(t)
	// A new file at the hit path: the base version is absent from git, so the
	// gate must fail closed and hold.
	writeRulesFile(t, r.prd.ProjectDir, "\n## 2026-11-09\n\n- [x] step\n")
	r.git.diff = "AGENT.md\n"
	// r.git.show has no entry for AGENT.md: git shows that it is absent.

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; a base without the file must fail closed", held, err)
	}
	b, _ := os.ReadFile(filepath.Join(r.prd.ProjectDir, "AGENT.md"))
	if !contains(string(b), "- [x] step") {
		t.Error("the working copy must be untouched")
	}
	if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 0 {
		t.Errorf("revert decisions = %d; want none", n)
	}
	if n := r.questionCount(); n != 1 {
		t.Errorf("questions = %d; want the protected-paths question", n)
	}
}

func TestCheckpointRevertDeletedFileStillHolds(t *testing.T) {
	r := newGateRig(t)
	// The working tree no longer has the file: os.Lstat fails and the gate
	// keeps the hit.
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; deleting the rules file is still a hit", held, err)
	}
}

func TestCheckpointRevertSymlinkFailsClosed(t *testing.T) {
	r := newGateRig(t)
	target := filepath.Join(r.prd.ProjectDir, "target.md")
	if err := os.WriteFile(target, []byte(baseRulesContent+workerEntryBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(r.prd.ProjectDir, "AGENT.md")); err != nil {
		t.Skip("symlinks not supported here")
	}
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; a symlinked rules file must fail closed", held, err)
	}
	b, _ := os.ReadFile(target)
	if !contains(string(b), "rewrote the picker") {
		t.Error("the symlink target must be untouched")
	}
}

func TestCheckpointRevertCandidateCLAUDEAndRulesFile(t *testing.T) {
	// CLAUDE.md is a candidate by name.
	r := newGateRig(t)
	r.m.SetConfig(withProtected(r.m.Config(), []string{"CLAUDE.md"}, config.ProtectedPathsActionHold))
	claude := filepath.Join(r.prd.ProjectDir, "CLAUDE.md")
	if err := os.WriteFile(claude, []byte(baseRulesContent+workerEntryBody), 0o644); err != nil {
		t.Fatal(err)
	}
	r.git.diff = "CLAUDE.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":CLAUDE.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; an entry-only change to CLAUDE.md must not hold", held, err)
	}
	b, _ := os.ReadFile(claude)
	if !strippedEqual(t, string(b), baseRulesContent) {
		t.Error("CLAUDE.md must strip to the base")
	}
	if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 1 {
		t.Errorf("revert decisions = %+v; want one", r.decisions(checkpointEntryRevertedDecisionKind))
	}

	// A non-candidate hit (not AGENT.md / CLAUDE.md / the rules file) is
	// passed through untouched and held.
	r2 := newGateRig(t)
	other := filepath.Join(r2.prd.ProjectDir, "NOTES.md")
	if err := os.WriteFile(other, []byte(baseRulesContent+workerEntryBody), 0o644); err != nil {
		t.Fatal(err)
	}
	r2.m.SetConfig(withProtected(r2.m.Config(), []string{"NOTES.md"}, config.ProtectedPathsActionHold))
	r2.git.diff = "NOTES.md\n"
	r2.git.show = map[string]string{gateBaseSHA + ":NOTES.md": baseRulesContent}

	held, err = r2.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; a non-candidate file must still hold", held, err)
	}
	b2, _ := os.ReadFile(other)
	if !contains(string(b2), "rewrote the picker") {
		t.Error("a non-candidate file must be left alone")
	}
}

func TestCheckpointRevertRecordsOncePerTaskAndPath(t *testing.T) {
	r := newGateRig(t)
	writeRulesFile(t, r.prd.ProjectDir, baseRulesContent+workerEntryBody)
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	for i := 0; i < 2; i++ {
		// Re-attach the entry each round so the second check also has
		// something to revert.
		writeRulesFile(t, r.prd.ProjectDir, baseRulesContent+workerEntryBody)
		if held, err := r.check(t); err != nil || held {
			t.Fatalf("round %d: held=%v err=%v", i+1, held, err)
		}
	}
	// The decision list is per-Automaton; two reverts of the identical
	// (task, path, note) still record exactly one entry.
	r.prd, _ = r.m.Store().GetPRD(r.prd.ID)
	decs := r.decisions(checkpointEntryRevertedDecisionKind)
	if len(decs) != 1 {
		t.Fatalf("revert decisions = %+v; want exactly one (deduped per task+path)", decs)
	}
}
