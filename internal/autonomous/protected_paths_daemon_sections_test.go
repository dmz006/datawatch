package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// baseRulesContent is the operator-written part of the rules file, as it was
// at the base commit.
const baseRulesContent = "# Agent Rules\n\nWrap every shell command with a start and end timestamp.\n"

// daemonSectionsBlock is everything the daemon writes itself into the rules
// file: the platform-context block, the RTK block, the cross-session-block,
// and the Memory and Session Guardrails header sections.
const daemonSectionsBlock = `
<!-- datawatch:platform-context v1 -->
This session runs on datawatch.
<!-- /datawatch:platform-context -->

<!-- rtk-instructions -->
Always prefix commands with rtk.
<!-- /rtk-instructions -->

<!-- cross-session-communication-rule -->
Use datawatch tools for durable facts.
<!-- /cross-session-communication-rule -->

# Memory & Knowledge (datawatch)

Use memory_recall before answering project questions.

# Session Guardrails

Never stop running sessions.
`

// writeRulesFile puts the given rules-file content in the task's project
// directory under its protected name (AGENT.md in the default rig config).
func writeRulesFile(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "AGENT.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProtectedPathsDaemonSectionsOnlyChangeRaisesNothing(t *testing.T) {
	r := newGateRig(t)
	// The working tree gained exactly the daemon's own sections: the
	// platform-context block, a changed Session Guardrails header section,
	// and appended memory, RTK and cross-session sections.
	writeRulesFile(t, r.prd.ProjectDir, baseRulesContent+daemonSectionsBlock)
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; a daemon-only change must not hold", held, err)
	}
	if n := r.questionCount(); n != 0 {
		t.Errorf("questions = %d; want none (no operator question for a daemon-only change)", n)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status == TaskBlocked || tk.ProtectedPathHold != nil {
		t.Errorf("status = %s hold=%+v; the task must not be held", tk.Status, tk.ProtectedPathHold)
	}
	if len(r.decisions("protected_paths_held")) != 0 {
		t.Errorf("held decisions = %+v; want none", r.decisions("protected_paths_held"))
	}
	if a := r.alertTitles(); len(a) != 0 {
		t.Errorf("alerts = %v; want none", a)
	}
}

func TestProtectedPathsDaemonSectionsWorkerEditStillHolds(t *testing.T) {
	r := newGateRig(t)
	// A worker edit to the rule line, plus the daemon's sections riding along.
	writeRulesFile(t, r.prd.ProjectDir,
		"# Agent Rules\n\nWrap every shell command, and every test run, with timestamps.\n"+daemonSectionsBlock)
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; a real edit must still hold", held, err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskBlocked {
		t.Errorf("status = %s; want blocked", tk.Status)
	}
	if h := tk.ProtectedPathHold; h == nil || strings.Join(h.Paths, ",") != "AGENT.md" || h.Action != config.ProtectedPathsActionHold || h.QuestionID == "" {
		t.Fatalf("hold = %+v", h)
	}
	qs := r.store.List(QuestionFilter{})
	if len(qs) != 1 {
		t.Fatalf("questions = %+v; want exactly one", qs)
	}
	if qs[0].Label != ProtectedPathsQuestionLabel || qs[0].Default != "hold" || !strings.Contains(qs[0].Text, "AGENT.md") {
		t.Errorf("question = %+v; want the protected-paths question defaulting to hold, naming AGENT.md", qs[0])
	}
}

func TestProtectedPathsDaemonSectionsWorkerEditBlockAction(t *testing.T) {
	r := newGateRig(t)
	r.m.SetConfig(withProtected(r.m.Config(), nil, config.ProtectedPathsActionBlock))
	writeRulesFile(t, r.prd.ProjectDir,
		"# Agent Rules\n\nWrap every shell command, and every test run, with timestamps.\n"+daemonSectionsBlock)
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; block parks without a hold", held, err)
	}
	if n := r.questionCount(); n != 0 {
		t.Errorf("block raised %d questions; want none", n)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskBlocked || !strings.HasPrefix(tk.Error, "protected_path_blocked: ") || !strings.Contains(tk.Error, "AGENT.md") {
		t.Errorf("task = %s error=%q; want blocked naming AGENT.md", tk.Status, tk.Error)
	}
	if tk.ProtectedPathHold != nil {
		t.Errorf("block recorded a hold: %+v", tk.ProtectedPathHold)
	}
	if decs := r.decisions("protected_paths_blocked"); len(decs) != 1 {
		t.Errorf("blocked decisions = %+v; want one", decs)
	}
}

func TestProtectedPathsDaemonSectionsWorkerEditWarnAction(t *testing.T) {
	r := newGateRig(t)
	r.m.SetConfig(withProtected(r.m.Config(), nil, config.ProtectedPathsActionWarn))
	writeRulesFile(t, r.prd.ProjectDir,
		"# Agent Rules\n\nWrap every shell command, and every test run, with timestamps.\n"+daemonSectionsBlock)
	r.git.diff = "AGENT.md\n"
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || held {
		t.Fatalf("held=%v err=%v; warn must let the task continue", held, err)
	}
	if n := r.questionCount(); n != 0 {
		t.Errorf("warn raised %d questions; want none", n)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskInProgress || tk.ProtectedPathHold != nil {
		t.Errorf("task = %s hold=%+v; warn must leave it running", tk.Status, tk.ProtectedPathHold)
	}
	if decs := r.decisions("protected_paths_warned"); len(decs) != 1 {
		t.Errorf("warned decisions = %+v; want one", decs)
	}
	if a := r.alertTitles(); len(a) != 1 || !strings.HasPrefix(a[0], "warn|Protected paths warning|") {
		t.Errorf("alerts = %v; want one warn alert", a)
	}
}

func TestProtectedPathsDaemonSectionsDeletedRulesFileStillHits(t *testing.T) {
	r := newGateRig(t)
	// git diff --no-renames reports the delete as AGENT.md (with any copy);
	// the working tree no longer has the file.
	r.git.diff = "AGENT.md\n"
	// The base version is present, but the working tree read fails and the
	// gate must fail closed on a delete.
	r.git.show = map[string]string{gateBaseSHA + ":AGENT.md": baseRulesContent}

	held, err := r.check(t)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v; deleting the rules file is still a hit", held, err)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskBlocked || tk.ProtectedPathHold == nil || strings.Join(tk.ProtectedPathHold.Paths, ",") != "AGENT.md" {
		t.Fatalf("hold = %+v; the delete must be held", tk.ProtectedPathHold)
	}
	if n := r.questionCount(); n != 1 {
		t.Errorf("questions = %d; want one", n)
	}
}
