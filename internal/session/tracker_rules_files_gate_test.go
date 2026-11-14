package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

// Every option the tracker can act on, switched on at once. With the gate
// (rtk.write_rules_files) off, none of them may reach AGENT.md or CLAUDE.md.
func allGuardrailOptions(write bool) GuardrailsOptions {
	return GuardrailsOptions{
		MemoryEnabled:          true,
		RTKEnabled:             true,
		CrossSessionEnabled:    true,
		PlatformContextEnabled: true,
		WriteRulesFiles:        write,
	}
}

// staleHeaderRules is a rules file whose "# Session Guardrails" header differs
// from what the current session would stamp, so the old header refresh would
// rewrite it.
func staleHeaderRules(name string) string {
	return "# Session Guardrails\n\nSession: host-old | Task: an earlier unrelated task\n\n# " + name + "\n\noriginal rules\n"
}

// The default session start leaves AGENT.md and CLAUDE.md byte-identical and
// the worktree clean even with the memory, cross-session, RTK and platform
// context options all on and a stale session header in the file.
func TestSessionStart_Default_AllOptionsOn_RulesFilesByteIdentical(t *testing.T) {
	dir := pcGitRepo(t)
	before := map[string]string{}
	for _, f := range []string{"AGENT.md", "CLAUDE.md"} {
		before[f] = staleHeaderRules(f)
		if err := os.WriteFile(filepath.Join(dir, f), []byte(before[f]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pcCommit(t, dir)

	for round := 0; round < 2; round++ {
		tr, sess, err := newGuardrailsTestSession(t.TempDir(), dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.WriteSessionGuardrails("", sess, allGuardrailOptions(false)); err != nil {
			t.Fatal(err)
		}
		for f, want := range before {
			if got := pcRead(t, filepath.Join(dir, f)); got != want {
				t.Fatalf("round %d: %s changed at session start:\n%s", round, f, got)
			}
		}
		if diff := untrackedGit(t, dir, "diff", "HEAD"); diff != "" {
			t.Fatalf("round %d: git diff HEAD not empty:\n%s", round, diff)
		}
		if st := untrackedGit(t, dir, "status", "--porcelain", "--untracked-files=all"); strings.TrimSpace(st) != "" {
			t.Fatalf("round %d: worktree not clean:\n%s", round, st)
		}
	}

	// Every block still reaches the session, through the untracked file, once.
	local := pcRead(t, filepath.Join(dir, "CLAUDE.local.md"))
	for name, marker := range map[string]string{
		"rtk":           "<!-- rtk-instructions -->",
		"memory":        "<!-- datawatch:memory-instructions -->",
		"cross-session": "<!-- cross-session-communication-rule -->",
		"platform":      pcMarker,
	} {
		if n := strings.Count(local, marker); n != 1 {
			t.Errorf("%s block count in CLAUDE.local.md = %d, want 1:\n%s", name, n, local)
		}
	}
	if !strings.Contains(local, "memory_recall") || !strings.Contains(local, "reply_to_parent") {
		t.Errorf("CLAUDE.local.md lacks the memory or cross-session text:\n%s", local)
	}
}

// With no rules file in the project, the default session start creates none.
func TestSessionStart_Default_CreatesNoRulesFile(t *testing.T) {
	dir := t.TempDir()
	tr, sess, err := newGuardrailsTestSession(t.TempDir(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteSessionGuardrails("", sess, allGuardrailOptions(false)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"AGENT.md", "CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("%s was created by the session start (err=%v)", f, err)
		}
	}
	if !fileExists(filepath.Join(dir, "CLAUDE.local.md")) {
		t.Error("CLAUDE.local.md not written")
	}
}

// Turning the options off removes their blocks from the untracked file and
// leaves the other blocks and operator text alone.
func TestSessionStart_Default_DisablingRemovesLocalBlocks(t *testing.T) {
	dir := t.TempDir()
	tr, sess, _ := newGuardrailsTestSession(t.TempDir(), dir)
	if err := tr.WriteSessionGuardrails("", sess, allGuardrailOptions(false)); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(dir, "CLAUDE.local.md")
	if err := os.WriteFile(local, []byte(pcRead(t, local)+"\noperator note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr2, sess2, _ := newGuardrailsTestSession(t.TempDir(), dir)
	if err := tr2.WriteSessionGuardrails("", sess2, GuardrailsOptions{RTKEnabled: true, PlatformContextEnabled: true}); err != nil {
		t.Fatal(err)
	}
	got := pcRead(t, local)
	if strings.Contains(got, "datawatch:memory-instructions") || strings.Contains(got, "cross-session-communication-rule") {
		t.Errorf("disabled blocks still present:\n%s", got)
	}
	for _, keep := range []string{"<!-- rtk-instructions -->", pcMarker, "operator note"} {
		if !strings.Contains(got, keep) {
			t.Errorf("%q lost:\n%s", keep, got)
		}
	}
}

// A tracked CLAUDE.local.md is never written, and the rules files stay intact.
func TestSessionStart_Default_TrackedLocalFileRefusedRulesFilesUntouched(t *testing.T) {
	dir := pcGitRepo(t)
	files := map[string]string{"AGENT.md": staleHeaderRules("AGENT.md"), "CLAUDE.local.md": "tracked local\n"}
	for f, body := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pcCommit(t, dir)
	tr, sess, _ := newGuardrailsTestSession(t.TempDir(), dir)
	if err := tr.WriteSessionGuardrails("", sess, allGuardrailOptions(false)); err != nil {
		t.Fatal(err)
	}
	for f, want := range files {
		if got := pcRead(t, filepath.Join(dir, f)); got != want {
			t.Errorf("%s changed:\n%s", f, got)
		}
	}
}

// With the gate on, the legacy merge runs: header refresh plus all three
// appended sections in the rules file, and no duplicate in the untracked file.
func TestSessionStart_WriteRulesFilesOn_LegacyMergeAllSections(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(staleHeaderRules("AGENT.md")), 0o644); err != nil {
		t.Fatal(err)
	}
	// A previous default-mode start left blocks in the untracked file.
	tr0, s0, _ := newGuardrailsTestSession(t.TempDir(), dir)
	if err := tr0.WriteSessionGuardrails("", s0, allGuardrailOptions(false)); err != nil {
		t.Fatal(err)
	}

	tr, sess, _ := newGuardrailsTestSession(t.TempDir(), dir)
	if err := tr.WriteSessionGuardrails("", sess, allGuardrailOptions(true)); err != nil {
		t.Fatal(err)
	}
	got := pcRead(t, filepath.Join(dir, "AGENT.md"))
	if strings.Contains(got, "host-old") || !strings.Contains(got, "host-cccc") {
		t.Errorf("header not refreshed:\n%s", got)
	}
	for _, want := range []string{"# Memory & Knowledge (datawatch)", "<!-- rtk-instructions -->", "<!-- cross-session-communication-rule -->"} {
		if strings.Count(got, want) != 1 {
			t.Errorf("AGENT.md: %q count = %d, want 1:\n%s", want, strings.Count(got, want), got)
		}
	}
	local := pcRead(t, filepath.Join(dir, "CLAUDE.local.md"))
	for _, gone := range []string{"<!-- rtk-instructions -->", "datawatch:memory-instructions", "cross-session-communication-rule"} {
		if strings.Contains(local, gone) {
			t.Errorf("legacy mode left %q in CLAUDE.local.md:\n%s", gone, local)
		}
	}
	if !sess.RTKInstructionsDelivered {
		t.Error("RTKInstructionsDelivered not set")
	}
}

// ---- startup cleanup of the non-RTK blocks ----

// The scenario from the worktree under review: the rules file carries the RTK
// block and the cross-session block as its only differences from HEAD. HEAD
// already holds the memory section, so that one must stay.
func TestCleanupRTKRulesFiles_RTKAndCrossSessionBlocksRemoved_HeadKeepsMemory(t *testing.T) {
	dir := t.TempDir()
	committed := "# AGENT.md\n\nrules\n\n" + memoryInstructions() + "\n"
	work := committed + "\n\n" + rtkInstructions() + "\n\n" + crossSessionCommunicationRule()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(work), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &fakeGit{
		tracked: map[string]bool{"AGENT.md": true},
		status:  map[string]string{"AGENT.md": " M AGENT.md\n"},
		head:    map[string]string{"AGENT.md": committed},
	}
	al := &alertLog{}
	res := CleanupRTKRulesFiles([]string{dir}, g, al.sink)
	if got := pcRead(t, filepath.Join(dir, "AGENT.md")); got != committed {
		t.Errorf("AGENT.md = %q, want the committed version", got)
	}
	if len(res.Cleaned) != 1 || len(res.Unsafe) != 0 || len(al.got) != 0 {
		t.Errorf("res = %+v alerts = %v", res, al.got)
	}
}

// Only the cross-session block differs from HEAD: it is cleaned too.
func TestCleanupRTKRulesFiles_CrossSessionOnlyRemoved(t *testing.T) {
	dir := t.TempDir()
	committed := "# CLAUDE.md\n\nrules\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(committed+"\n\n"+crossSessionCommunicationRule()), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &fakeGit{
		tracked: map[string]bool{"CLAUDE.md": true},
		status:  map[string]string{"CLAUDE.md": " M CLAUDE.md\n"},
		head:    map[string]string{"CLAUDE.md": committed},
	}
	CleanupRTKRulesFiles([]string{dir}, g, nil)
	if got := pcRead(t, filepath.Join(dir, "CLAUDE.md")); got != committed {
		t.Errorf("CLAUDE.md = %q", got)
	}
}

// A file that differs from HEAD by something other than the blocks (here a
// refreshed session header) is left exactly as is and alerted once.
func TestCleanupRTKRulesFiles_HeaderRefreshChangeIsUnsafe(t *testing.T) {
	dir := t.TempDir()
	committed := staleHeaderRules("AGENT.md")
	refreshed := strings.Replace(committed, "host-old", "host-new", 1)
	work := refreshed + "\n\n" + crossSessionCommunicationRule()
	path := filepath.Join(dir, "AGENT.md")
	if err := os.WriteFile(path, []byte(work), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &fakeGit{
		tracked: map[string]bool{"AGENT.md": true},
		status:  map[string]string{"AGENT.md": " M AGENT.md\n"},
		head:    map[string]string{"AGENT.md": committed},
	}
	al := &alertLog{}
	res := CleanupRTKRulesFiles([]string{dir}, g, al.sink)
	if got := pcRead(t, path); got != work {
		t.Errorf("file changed:\n%s", got)
	}
	if len(al.got) != 1 || res.Alerts != 1 || len(res.Unsafe) != 1 {
		t.Errorf("alerts = %v res = %+v", al.got, res)
	}
}

// A cross-session or memory block that is committed (clean file) is the
// project's own text, not a leftover: no change and no alert.
func TestCleanupRTKRulesFiles_CommittedNonRTKBlocksAreSilent(t *testing.T) {
	dir := t.TempDir()
	committed := "# AGENT.md\n\n" + memoryInstructions() + "\n\n" + crossSessionCommunicationRule()
	path := filepath.Join(dir, "AGENT.md")
	if err := os.WriteFile(path, []byte(committed), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &fakeGit{
		tracked: map[string]bool{"AGENT.md": true},
		status:  map[string]string{"AGENT.md": ""},
		head:    map[string]string{"AGENT.md": committed},
	}
	al := &alertLog{}
	res := CleanupRTKRulesFiles([]string{dir}, g, al.sink)
	if got := pcRead(t, path); got != committed || len(al.got) != 0 || len(res.Unsafe)+len(res.Cleaned) != 0 {
		t.Errorf("unexpected activity: res=%+v alerts=%v", res, al.got)
	}
}

// Real git: all three blocks appended by an old daemon are undone byte for
// byte, and the worktree is clean afterwards.
func TestCleanupRTKRulesFiles_RealGit_AllThreeBlocks(t *testing.T) {
	dir := pcGitRepo(t)
	committed := "# AGENT.md\n\nrules\n"
	path := filepath.Join(dir, "AGENT.md")
	if err := os.WriteFile(path, []byte(committed), 0o644); err != nil {
		t.Fatal(err)
	}
	pcCommit(t, dir)
	work := committed + "\n\n" + memoryInstructions() + "\n\n" + rtkInstructions() + "\n\n" + crossSessionCommunicationRule()
	if err := os.WriteFile(path, []byte(work), 0o644); err != nil {
		t.Fatal(err)
	}
	al := &alertLog{}
	res := CleanupRTKRulesFiles([]string{dir}, nil, al.sink)
	if len(res.Cleaned) != 1 || len(al.got) != 0 {
		t.Fatalf("res = %+v alerts = %v", res, al.got)
	}
	if got := pcRead(t, path); got != committed {
		t.Errorf("AGENT.md = %q", got)
	}
	if diff := untrackedGit(t, dir, "diff", "HEAD"); diff != "" {
		t.Errorf("git diff HEAD not empty:\n%s", diff)
	}
}

// What the tracker writes into the untracked file is something the strip helper
// recognises, so a rules file never inherits it by accident.
func TestStripDaemonSections_RemovesLocalMemoryBlock(t *testing.T) {
	dir := t.TempDir()
	if _, err := platformcontext.WriteMemoryLocalFile(dir, memoryInstructions()); err != nil {
		t.Fatal(err)
	}
	got := platformcontext.StripDaemonSections(pcRead(t, filepath.Join(dir, "CLAUDE.local.md")))
	if strings.TrimSpace(got) != "" {
		t.Errorf("memory block not fully stripped: %q", got)
	}
}
