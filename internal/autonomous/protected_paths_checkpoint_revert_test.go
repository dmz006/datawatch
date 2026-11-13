package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/platformcontext"
)

// workerEntryBody is a worker-added checkpoint entry: a dated heading, a
// blank line, and bullet lines, separated from the surrounding base content
// by blank lines.
const workerEntryBody = "\n\n## 2026-11-09 14:14 — DONE — rewrote the picker\n\n- [x] replaced the chip row with the dropdown\n- [x] migrated the picker tests\n"

// strippedEqual reports whether the two texts strip to the same daemon-free
// text — the equality the gate requires after a revert.
func strippedEqual(t *testing.T, a, b string) bool {
	t.Helper()
	return platformcontext.StripDaemonSections(a) == platformcontext.StripDaemonSections(b)
}

// rulesPath joins the rules file name under the rig's project directory.
func rulesPath(r *gateRig, name string) string { return filepath.Join(r.prd.ProjectDir, name) }

// setupEntryChange puts the working file and the fake git state in place for
// a change to the named rules file: the working copy is base plus the worker
// entry, the changed set lists the file, and the base version is the base
// content.
func setupEntryChange(t *testing.T, r *gateRig, name, base, added string) string {
	t.Helper()
	p := rulesPath(r, name)
	if err := os.WriteFile(p, []byte(base+added), 0o644); err != nil {
		t.Fatal(err)
	}
	r.git.diff = name + "\n"
	r.git.show = map[string]string{gateBaseSHA + ":" + name: base}
	return p
}

// assertEntryReverted checks the gate-level outcome of a clean revert: no
// hold, no question, no alert, the task untouched, and exactly one
// protected_paths_checkpoint_reverted decision.
func assertEntryReverted(t *testing.T, r *gateRig) {
	t.Helper()
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status == TaskBlocked || tk.ProtectedPathHold != nil {
		t.Errorf("status=%s hold=%+v; the task must not be held", tk.Status, tk.ProtectedPathHold)
	}
	if n := r.questionCount(); n != 0 {
		t.Errorf("questions = %d; want none (no operator question for an entry-only change)", n)
	}
	if a := r.alertTitles(); len(a) != 0 {
		t.Errorf("alerts = %v; want none", a)
	}
	decs := r.decisions(checkpointEntryRevertedDecisionKind)
	if len(decs) != 1 {
		t.Fatalf("revert decisions = %+v; want exactly one", decs)
	}
	if want := "task " + r.task.ID + ": removed a worker checkpoint entry from "; !strings.HasPrefix(decs[0].Note, want) {
		t.Errorf("note = %q; want a prefix %q…", decs[0].Note, want)
	}
}

// TestCheckpointRevertEntryOnlyRevertsAgentMd is spec case 1: AGENT.md whose
// only change is an appended DONE entry passes the gate, the working file is
// restored to the base content, and exactly one revert decision is recorded.
func TestCheckpointRevertEntryOnlyRevertsAgentMd(t *testing.T) {
	r := newGateRig(t)
	p := setupEntryChange(t, r, "AGENT.md", baseRulesContent, workerEntryBody)

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; an entry-only change must not hold", held, err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strippedEqual(t, s, baseRulesContent) {
		t.Errorf("the working file must strip to the base content again;\ngot:  %q\nbase: %q", s, baseRulesContent)
	}
	if strings.Contains(s, "rewrote the picker") {
		t.Error("the entry must be gone")
	}
	assertEntryReverted(t, r)

	// The gate must still issue only read-only git commands.
	readOnly := map[string]bool{"rev-parse": true, "merge-base": true, "diff": true, "ls-files": true, "log": true, "symbolic-ref": true, "show": true}
	for _, c := range r.git.calls {
		if !readOnly[c[0]] {
			t.Errorf("the gate ran a git command that may write: %v", c)
		}
	}
}

// TestCheckpointRevertEntryOnlyRevertsClaudeMd is spec case 2: the same for
// CLAUDE.md, with the changed set listing CLAUDE.md and the base version
// served under the CLAUDE.md key.
func TestCheckpointRevertEntryOnlyRevertsClaudeMd(t *testing.T) {
	r := newGateRig(t)
	r.m.SetConfig(withProtected(r.m.Config(), []string{"CLAUDE.md"}, config.ProtectedPathsActionHold))
	p := setupEntryChange(t, r, "CLAUDE.md", baseRulesContent, workerEntryBody)

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; an entry-only change to CLAUDE.md must not hold", held, err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strippedEqual(t, s, baseRulesContent) {
		t.Errorf("CLAUDE.md must strip to the base content again;\ngot:  %q\nbase: %q", s, baseRulesContent)
	}
	if strings.Contains(s, "rewrote the picker") {
		t.Error("the entry must be gone")
	}
	assertEntryReverted(t, r)
	if want := "removed a worker checkpoint entry from CLAUDE.md"; !strings.Contains(r.decisions(checkpointEntryRevertedDecisionKind)[0].Note, want) {
		t.Errorf("note = %q; want it naming CLAUDE.md", r.decisions(checkpointEntryRevertedDecisionKind)[0].Note)
	}
}

// TestCheckpointRevertEntryUnderNewCheckpointsHeading is spec case 3: the
// worker added its first entry together with a fresh `## Checkpoints`
// heading; both are reverted.
func TestCheckpointRevertEntryUnderNewCheckpointsHeading(t *testing.T) {
	r := newGateRig(t)
	p := setupEntryChange(t, r, "AGENT.md", baseRulesContent, "\n\n## Checkpoints\n\n## 2026-11-09\n\n- [x] step one\n")

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; an entry under a fresh ## Checkpoints heading must not hold", held, err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if !strippedEqual(t, s, baseRulesContent) {
		t.Errorf("the file must strip to the base content again (heading and entry both reverted);\ngot:  %q\nbase: %q", s, baseRulesContent)
	}
	if strings.Contains(s, "- [x] step one") || strings.Contains(s, "## Checkpoints") {
		t.Error("the heading and the entry must be gone")
	}
	assertEntryReverted(t, r)
}

// TestCheckpointRevertEntryRidesAlongWithDaemonSections is spec case 4: the
// entry sits among the daemon's own sections; the entry is removed and the
// daemon sections survive the write-back byte for byte.
func TestCheckpointRevertEntryRidesAlongWithDaemonSections(t *testing.T) {
	r := newGateRig(t)
	writeRulesFile(t, r.prd.ProjectDir, baseRulesContent+workerEntryBody+daemonSectionsBlock)
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; an entry-only change with daemon sections must not hold", held, err)
	}
	b, err := os.ReadFile(rulesPath(r, "AGENT.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strippedEqual(t, s, baseRulesContent) {
		t.Errorf("the file must strip to the base content;\ngot:  %q\nbase: %q", s, baseRulesContent)
	}
	if !strings.Contains(s, daemonSectionsBlock) {
		t.Errorf("the daemon sections must be preserved byte for byte;\ngot:\n%s\nwant to contain:\n%s", s, daemonSectionsBlock)
	}
	if strings.Contains(s, "rewrote the picker") {
		t.Error("the entry must be gone")
	}
	assertEntryReverted(t, r)
}

// TestCheckpointRevertEntryPlusRealRuleEditHoldsFileUntouched is spec case 5:
// a checkpoint entry and a real rule edit in the same file are
// indistinguishable from a rules edit — held exactly as one, and the file is
// not written.
func TestCheckpointRevertEntryPlusRealRuleEditHoldsFileUntouched(t *testing.T) {
	r := newGateRig(t)
	file := "# Agent Rules\n\nWrap every command, plus tests, with timestamps.\n" + workerEntryBody
	writeRulesFile(t, r.prd.ProjectDir, file)
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; an entry plus a real edit must still hold", held, err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskBlocked || tk.ProtectedPathHold == nil || strings.Join(tk.ProtectedPathHold.Paths, ",") != "AGENT.md" {
		t.Fatalf("status=%s hold=%+v; want blocked with a hold on AGENT.md", tk.Status, tk.ProtectedPathHold)
	}
	if r.questionCount() != 1 {
		t.Errorf("questions = %d; want the protected-paths question", r.questionCount())
	}
	b, _ := os.ReadFile(rulesPath(r, "AGENT.md"))
	if string(b) != file {
		t.Errorf("the working copy must be byte for byte unchanged on a failed revert;\ngot:  %q\nwant: %q", string(b), file)
	}
	if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 0 {
		t.Errorf("revert decisions = %+v; want none when nothing was reverted", r.decisions(checkpointEntryRevertedDecisionKind))
	}
}

// TestCheckpointRevertEntryPlusOtherProtectedFileHoldsOnlyThatFile is spec
// case 6: a checkpoint entry in AGENT.md plus a real change to another
// protected file; only the other file is held and AGENT.md is cleaned.
func TestCheckpointRevertEntryPlusOtherProtectedFileHoldsOnlyThatFile(t *testing.T) {
	r := newGateRig(t)
	writeRulesFile(t, r.prd.ProjectDir, baseRulesContent+workerEntryBody)
	r.git.diff = "AGENT.md\n.github/workflows/ci.yml\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; the real change must still hold", held, err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskBlocked {
		t.Errorf("status = %s; want blocked", tk.Status)
	}
	if h := tk.ProtectedPathHold; h == nil || strings.Join(h.Paths, ",") != ".github/workflows/ci.yml" {
		t.Fatalf("hold = %+v; want only .github/workflows/ci.yml held", h)
	}
	qs := r.store.List(QuestionFilter{})
	if len(qs) != 1 {
		t.Fatalf("questions = %+v; want exactly one", qs)
	}
	if !strings.Contains(qs[0].Text, ".github/workflows/ci.yml") || strings.Contains(qs[0].Text, "AGENT.md") {
		t.Errorf("question = %q; want it naming the workflow file only, not AGENT.md", qs[0].Text)
	}
	b, _ := os.ReadFile(rulesPath(r, "AGENT.md"))
	if !strippedEqual(t, string(b), baseRulesContent) || strings.Contains(string(b), "rewrote the picker") {
		t.Errorf("AGENT.md must be cleaned of the entry;\ngot:  %q\nbase: %q", string(b), baseRulesContent)
	}
	if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 1 {
		t.Errorf("revert decisions = %+v; want one for the cleaned AGENT.md", r.decisions(checkpointEntryRevertedDecisionKind))
	}
	if n := len(r.decisions("protected_paths_held")); n != 1 {
		t.Errorf("held decisions = %+v; want one", r.decisions("protected_paths_held"))
	}
}

// TestCheckpointRevertBlockActionHoldsNonCheckpointEdit is spec case 7: the
// hold/warn/block actions are unaffected for a non-checkpoint edit
// (spot-check: block parks without a question).
func TestCheckpointRevertBlockActionHoldsNonCheckpointEdit(t *testing.T) {
	r := newGateRig(t)
	r.m.SetConfig(withProtected(r.m.Config(), nil, config.ProtectedPathsActionBlock))
	file := "# Agent Rules\n\nWrap every command, plus tests, with timestamps.\n"
	writeRulesFile(t, r.prd.ProjectDir, file)
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; block parks without a hold", held, err)
	}
	if r.questionCount() != 0 {
		t.Errorf("block raised %d questions; want none", r.questionCount())
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskBlocked || !strings.HasPrefix(tk.Error, "protected_path_blocked: ") || !strings.Contains(tk.Error, "AGENT.md") {
		t.Errorf("task = %s error=%q; want blocked naming AGENT.md", tk.Status, tk.Error)
	}
	if tk.ProtectedPathHold != nil {
		t.Errorf("block recorded a hold: %+v", tk.ProtectedPathHold)
	}
	if n := len(r.decisions("protected_paths_blocked")); n != 1 {
		t.Errorf("blocked decisions = %+v; want one", r.decisions("protected_paths_blocked"))
	}
	if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 0 {
		t.Errorf("revert decisions = %+v; a real edit must not pass the revert", r.decisions(checkpointEntryRevertedDecisionKind))
	}
	b, _ := os.ReadFile(rulesPath(r, "AGENT.md"))
	if string(b) != file {
		t.Errorf("the working copy must be untouched by a block: %q", string(b))
	}
}

// TestCheckpointRevertFailClosedKeepsHitAndWritesNothing is spec case 8: a
// line removed from the base, a new file absent at base, a missing working
// copy, and a symlink all keep the hit and write nothing.
func TestCheckpointRevertFailClosedKeepsHitAndWritesNothing(t *testing.T) {
	t.Run("line_removed_from_base", func(t *testing.T) {
		r := newGateRig(t)
		file := "# Agent Rules\n\n## 2026-11-09\n\n- [x] step\n"
		writeRulesFile(t, r.prd.ProjectDir, file)
		r.git.diff = "AGENT.md\n"
		r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

		if held, err := r.check(t); err != nil || !held {
			t.Fatalf("held=%v err=%v; a removed base line must keep the hit", held, err)
		}
		b, _ := os.ReadFile(rulesPath(r, "AGENT.md"))
		if string(b) != file {
			t.Errorf("the working copy must be untouched;\ngot:  %q\nwant: %q", string(b), file)
		}
		if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 0 {
			t.Errorf("revert decisions = %+v; want none", r.decisions(checkpointEntryRevertedDecisionKind))
		}
		if r.questionCount() != 1 {
			t.Errorf("questions = %d; want the protected-paths question", r.questionCount())
		}
	})

	t.Run("new_file_absent_at_base", func(t *testing.T) {
		r := newGateRig(t)
		file := "# Agent Rules\n" + workerEntryBody
		writeRulesFile(t, r.prd.ProjectDir, file)
		r.git.diff = "AGENT.md\n"
		// r.git.show has no entry for AGENT.md: git shows the path absent at
		// the base commit, so the base version is unreadable.

		if held, err := r.check(t); err != nil || !held {
			t.Fatalf("held=%v err=%v; a base without the file must fail closed", held, err)
		}
		b, _ := os.ReadFile(rulesPath(r, "AGENT.md"))
		if !strings.Contains(string(b), "rewrote the picker") {
			t.Error("the working copy must be untouched")
		}
		if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 0 {
			t.Errorf("revert decisions = %+v; want none", r.decisions(checkpointEntryRevertedDecisionKind))
		}
	})

	t.Run("missing_working_copy", func(t *testing.T) {
		r := newGateRig(t)
		// git lists AGENT.md as changed and the base has it, but the working
		// tree no longer does: the gate keeps the hit.
		r.git.diff = "AGENT.md\n"
		r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

		if held, err := r.check(t); err != nil || !held {
			t.Fatalf("held=%v err=%v; a deleted rules file must keep the hit", held, err)
		}
		if r.questionCount() != 1 {
			t.Errorf("questions = %d; want the protected-paths question", r.questionCount())
		}
		if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 0 {
			t.Errorf("revert decisions = %+v; want none", r.decisions(checkpointEntryRevertedDecisionKind))
		}
	})

	t.Run("symlink", func(t *testing.T) {
		r := newGateRig(t)
		target := filepath.Join(r.prd.ProjectDir, "target.md")
		file := baseRulesContent + workerEntryBody
		if err := os.WriteFile(target, []byte(file), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, rulesPath(r, "AGENT.md")); err != nil {
			t.Skip("symlinks not supported here")
		}
		r.git.diff = "AGENT.md\n"
		r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

		if held, err := r.check(t); err != nil || !held {
			t.Fatalf("held=%v err=%v; a symlinked rules file must fail closed", held, err)
		}
		b, _ := os.ReadFile(target)
		if string(b) != file {
			t.Errorf("the symlink target must be untouched;\ngot:  %q\nwant: %q", string(b), file)
		}
		if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 0 {
			t.Errorf("revert decisions = %+v; want none", r.decisions(checkpointEntryRevertedDecisionKind))
		}
	})
}

// TestCheckpointRevertSecondGateRunIsNoOp is spec case 9: after a clean
// revert the file equals the base, so a second gate run is a no-op that
// records nothing further.
func TestCheckpointRevertSecondGateRunIsNoOp(t *testing.T) {
	r := newGateRig(t)
	setupEntryChange(t, r, "AGENT.md", baseRulesContent, workerEntryBody)

	for i := 0; i < 2; i++ {
		if held, err := r.check(t); err != nil || held {
			t.Fatalf("run %d: held=%v err=%v", i+1, held, err)
		}
	}
	if n := r.questionCount(); n != 0 {
		t.Errorf("questions = %d; want none after a clean revert", n)
	}
	if a := r.alertTitles(); len(a) != 0 {
		t.Errorf("alerts = %v; want none", a)
	}
	if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 1 {
		t.Errorf("revert decisions = %+v; want exactly one (the second run must not duplicate)", r.decisions(checkpointEntryRevertedDecisionKind))
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status == TaskBlocked || tk.ProtectedPathHold != nil {
		t.Errorf("status=%s hold=%+v; the task must not be held", tk.Status, tk.ProtectedPathHold)
	}
}

// TestCheckpointRevertNonDateHeadingIsNotAnEntry is spec case 10: a heading
// that is not date-shaped is not a checkpoint entry, so the addition is a
// real edit and the file is held.
func TestCheckpointRevertNonDateHeadingIsNotAnEntry(t *testing.T) {
	r := newGateRig(t)
	file := baseRulesContent + "\n\n## Notes\n\n- [x] step\n"
	writeRulesFile(t, r.prd.ProjectDir, file)
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	if held, err := r.check(t); err != nil || !held {
		t.Fatalf("held=%v err=%v; a non-dated heading is not a checkpoint entry", held, err)
	}
	b, _ := os.ReadFile(rulesPath(r, "AGENT.md"))
	if string(b) != file {
		t.Errorf("the working copy must be untouched;\ngot:  %q\nwant: %q", string(b), file)
	}
	if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 0 {
		t.Errorf("revert decisions = %+v; want none", r.decisions(checkpointEntryRevertedDecisionKind))
	}
	if n := len(r.decisions("protected_paths_held")); n != 1 {
		t.Errorf("held decisions = %+v; want one", r.decisions("protected_paths_held"))
	}
}

// TestCheckpointRevertStripWorkerCheckpointEntries is spec case 11: direct
// table-driven tests of the pure helper — multi-bullet entries, two entries
// back to back, an entry at the end of file without a trailing newline, and
// an entry followed by another `## ` section (the section is kept).
func TestCheckpointRevertStripWorkerCheckpointEntries(t *testing.T) {
	base := baseRulesContent
	cases := []struct {
		name string
		base string
		cur  string
		want bool
		keep string // text the cleaned output must still contain when ok
	}{
		{
			name: "multi-bullet entry",
			cur:  base + "\n\n## 2026-11-09\n\n- [x] one\n- [x] two\n- [x] three\n",
			want: true,
		},
		{
			name: "two entries back to back",
			cur:  base + "\n\n## 2026-11-08\n\n- [x] a\n\n## 2026-11-09\n\n- [x] b\n",
			want: true,
		},
		{
			name: "entry at end of file without a trailing newline",
			cur:  base + "\n\n## 2026-11-09\n\n- [x] a\n- [x] b",
			want: true,
		},
		{
			name: "entry followed by another ## section (the section is kept)",
			base: "## One\n\nx\n\n## Two\n\ny\n",
			cur:  "## One\n\nx\n\n## 2026-11-09\n\n- [x] step\n\n## Two\n\ny\n",
			want: true,
			keep: "## Two\n\ny",
		},
		{
			name: "entry under a fresh ## Checkpoints heading",
			cur:  base + "\n\n## Checkpoints\n\n## 2026-11-09\n\n- [x] a\n",
			want: true,
		},
		{
			name: "heading with time and title",
			cur:  base + "\n\n## 2026-11-09 14:14 — DONE — rewrote the picker\n\n- [x] step\n",
			want: true,
		},
		{
			name: "entry body line identical to a base line",
			cur:  base + "\n\n## 2026-11-09\nWrap every shell command with a start and end timestamp.\n",
			want: true,
		},
		{
			name: "line removed from the base keeps the hit",
			cur:  "# Agent Rules\n\n## 2026-11-09\n\n- [x] step\n",
		},
		{
			name: "worker edited a base line",
			cur:  "# Agent Rules\n\nWrap every command with timestamps.\n\n## 2026-11-09\n\n- [x] step\n",
		},
		{
			name: "added text outside any entry",
			cur:  base + "\n\nfixed the picker.\n",
		},
		{
			name: "heading that is not date-shaped is not an entry",
			cur:  base + "\n\n## Notes\n\n- [x] a\n",
		},
		{
			name: "second-level dated heading is not an entry",
			cur:  base + "\n\n### 2026-11-09\n\n- [x] step\n",
		},
		{
			name: "two added Checkpoints headings",
			cur:  base + "\n\n## Checkpoints\n\n## 2026-11-09\n\n- [x] a\n\n## Checkpoints\n\n## 2026-11-10\n\n- [x] b\n",
		},
		{
			name: "entry under a Checkpoints heading the base already carried",
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
		baseFor := base
		if tc.base != "" {
			baseFor = tc.base
		}
		t.Run(tc.name, func(t *testing.T) {
			got, ok := stripWorkerCheckpointEntries(baseFor, tc.cur)
			if ok != tc.want {
				t.Fatalf("ok=%v; want %v", ok, tc.want)
			}
			if !ok {
				return
			}
			if !strippedEqual(t, got, baseFor) {
				t.Fatalf("cleaned %q must strip to the base %q", got, baseFor)
			}
			if tc.keep != "" && !strings.Contains(got, tc.keep) {
				t.Fatalf("cleaned %q must keep %q", got, tc.keep)
			}
			if strings.Contains(got, "rewrote the picker") || strings.Contains(got, "- [x] one") || strings.Contains(got, "- [x] a") {
				t.Fatalf("cleaned %q still carries an entry line", got)
			}
			if strings.Contains(baseFor, "- [x]") && !strings.Contains(got, "- [x]") {
				t.Errorf("base entry content lost: %q", got)
			}
		})
	}
}

// TestCheckpointRevertStripHeadingFormats pins the date heading match: the
// entry heading needs a `## YYYY-MM-DD`, optionally ` HH:MM(:SS)`, with an
// optional title tail.
func TestCheckpointRevertStripHeadingFormats(t *testing.T) {
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

// TestCheckpointRevertStripPerservesDaemonSectionsByteForByte is the
// pure-helper version of spec case 4: the daemon's own sections are never
// among the added lines, so they survive the cleanup byte for byte while the
// entry lines disappear.
func TestCheckpointRevertStripPerservesDaemonSectionsByteForByte(t *testing.T) {
	base := baseRulesContent + daemonSectionsBlock
	basePlain := baseRulesContent
	cur := basePlain + workerEntryBody + daemonSectionsBlock

	got, ok := stripWorkerCheckpointEntries(base, cur)
	if !ok {
		t.Fatal("ok=false; want the entry reverted among the daemon sections")
	}
	if !strippedEqual(t, got, basePlain) {
		t.Fatalf("cleaned %q must strip to the base %q", got, basePlain)
	}
	if !strings.Contains(got, daemonSectionsBlock) {
		t.Fatalf("the daemon sections must be preserved byte for byte;\ngot:\n%s\nwant to contain:\n%s", got, daemonSectionsBlock)
	}
	if strings.Contains(got, "rewrote the picker") {
		t.Error("the entry must be gone")
	}
}

// TestCheckpointRevertEntryUnderDaemonSectionsNeedsNoRevert: an entry
// appended after the daemon's own header sections is inside the region the
// daemon rewrites on every session start, so the daemon-filter pass — not the
// checkpoint revert — already drops the hit and nothing is written.
func TestCheckpointRevertEntryUnderDaemonSectionsNeedsNoRevert(t *testing.T) {
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
	b, _ := os.ReadFile(rulesPath(r, "AGENT.md"))
	if !strings.Contains(string(b), "rewrote the picker") {
		t.Error("the working copy must be left exactly as found")
	}
}

// TestCheckpointRevertNonCandidateFileIsNotTouched: only AGENT.md, CLAUDE.md
// and the configured rules file are candidates; any other hit passes the
// revert untouched and is held.
func TestCheckpointRevertNonCandidateFileIsNotTouched(t *testing.T) {
	r := newGateRig(t)
	r.m.SetConfig(withProtected(r.m.Config(), []string{"NOTES.md"}, config.ProtectedPathsActionHold))
	p := filepath.Join(r.prd.ProjectDir, "NOTES.md")
	if err := os.WriteFile(p, []byte(baseRulesContent+workerEntryBody), 0o644); err != nil {
		t.Fatal(err)
	}
	r.git.diff = "NOTES.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":NOTES.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; a non-candidate file must still hold", held, err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "rewrote the picker") {
		t.Error("a non-candidate file must be left alone")
	}
	if n := len(r.decisions(checkpointEntryRevertedDecisionKind)); n != 0 {
		t.Errorf("revert decisions = %+v; want none", r.decisions(checkpointEntryRevertedDecisionKind))
	}
}

// TestCheckpointRevertRecordsOncePerTaskAndPath: repeat reverts of the same
// (task, path) pair record exactly one decision.
func TestCheckpointRevertRecordsOncePerTaskAndPath(t *testing.T) {
	r := newGateRig(t)
	for i := 0; i < 2; i++ {
		// Re-attach the entry each round so the second check also has
		// something to revert.
		writeRulesFile(t, r.prd.ProjectDir, baseRulesContent+workerEntryBody)
		r.git.diff = "AGENT.md\n"
		r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}
		if held, err := r.check(t); err != nil || held {
			t.Fatalf("round %d: held=%v err=%v", i+1, held, err)
		}
	}
	r.prd, _ = r.m.Store().GetPRD(r.prd.ID)
	decs := r.decisions(checkpointEntryRevertedDecisionKind)
	if len(decs) != 1 {
		t.Fatalf("revert decisions = %+v; want exactly one (deduped per task+path)", decs)
	}
}
