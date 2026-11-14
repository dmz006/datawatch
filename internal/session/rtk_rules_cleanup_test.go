package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- session start: RTK text goes to the untracked file ----

// rtkSessionStart runs a session-start guardrails write over a project dir that
// already holds committed-looking AGENT.md and CLAUDE.md, and returns both
// files' bytes before and after.
func rtkSessionStart(t *testing.T, dir string, opts GuardrailsOptions) (sess *Session, before, after map[string]string) {
	t.Helper()
	before = map[string]string{}
	for _, f := range []string{"AGENT.md", "CLAUDE.md"} {
		body := "# " + f + "\n\noriginal rules\n"
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		before[f] = body
	}
	tr, s, err := newGuardrailsTestSession(t.TempDir(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteSessionGuardrails("", s, opts); err != nil {
		t.Fatal(err)
	}
	after = map[string]string{}
	for _, f := range []string{"AGENT.md", "CLAUDE.md"} {
		after[f] = pcRead(t, filepath.Join(dir, f))
	}
	return s, before, after
}

func TestRTKSessionStart_RulesFilesByteIdentical_TextInUntrackedFile(t *testing.T) {
	dir := pcGitRepo(t)
	for _, f := range []string{"AGENT.md", "CLAUDE.md"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("# "+f+"\n\noriginal rules\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pcCommit(t, dir)

	sess, before, after := rtkSessionStart(t, dir, GuardrailsOptions{RTKEnabled: true})
	for f := range before {
		if before[f] != after[f] {
			t.Errorf("%s changed at session start:\n%s", f, after[f])
		}
	}
	if status := untrackedGit(t, dir, "status", "--porcelain", "--untracked-files=all"); strings.Contains(status, "AGENT.md") || strings.Contains(status, "CLAUDE.md ") {
		t.Errorf("tracked rules file shows in git status:\n%s", status)
	}
	if diff := untrackedGit(t, dir, "diff", "HEAD"); diff != "" {
		t.Errorf("git diff HEAD not empty:\n%s", diff)
	}

	local := pcRead(t, filepath.Join(dir, "CLAUDE.local.md"))
	if !strings.Contains(local, rtkInstructions()) {
		t.Errorf("CLAUDE.local.md lacks the RTK block:\n%s", local)
	}
	if !sess.RTKInstructionsDelivered {
		t.Error("RTKInstructionsDelivered not set")
	}

	// A second start does not duplicate the block.
	tr, s2, _ := newGuardrailsTestSession(t.TempDir(), dir)
	if err := tr.WriteSessionGuardrails("", s2, GuardrailsOptions{RTKEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(pcRead(t, filepath.Join(dir, "CLAUDE.local.md")), rtkBeginMarker); n != 1 {
		t.Errorf("block count after second start = %d, want 1", n)
	}
}

func TestRTKSessionStart_KeepsPlatformContextBesideRTK(t *testing.T) {
	dir := t.TempDir()
	s, _, _ := rtkSessionStart(t, dir, GuardrailsOptions{RTKEnabled: true, PlatformContextEnabled: true})
	local := pcRead(t, filepath.Join(dir, "CLAUDE.local.md"))
	if !strings.Contains(local, rtkBeginMarker) || !strings.Contains(local, pcMarker) {
		t.Errorf("want both blocks in CLAUDE.local.md:\n%s", local)
	}
	if !s.RTKInstructionsDelivered || !s.PlatformContextLocalFile {
		t.Errorf("flags: rtk=%v platform=%v", s.RTKInstructionsDelivered, s.PlatformContextLocalFile)
	}
}

func TestRTKSessionStart_DisabledWritesNothingAndRemovesStaleBlock(t *testing.T) {
	dir := t.TempDir()
	rtkSessionStart(t, dir, GuardrailsOptions{RTKEnabled: true, PlatformContextEnabled: true})
	s, _, _ := rtkSessionStart(t, dir, GuardrailsOptions{PlatformContextEnabled: true})
	local := pcRead(t, filepath.Join(dir, "CLAUDE.local.md"))
	if strings.Contains(local, rtkBeginMarker) {
		t.Errorf("RTK block should be gone when RTK is disabled:\n%s", local)
	}
	if !strings.Contains(local, pcMarker) {
		t.Errorf("platform context block must survive:\n%s", local)
	}
	if s.RTKInstructionsDelivered {
		t.Error("RTKInstructionsDelivered should be false when RTK is disabled")
	}
}

func TestRTKSessionStart_WriteRulesFilesTrueRestoresAppend(t *testing.T) {
	dir := t.TempDir()
	s, before, after := rtkSessionStart(t, dir, GuardrailsOptions{RTKEnabled: true, RTKWriteRulesFiles: true})
	// The target is AGENT.md when it exists; CLAUDE.md is the fallback.
	if after["AGENT.md"] != before["AGENT.md"]+"\n\n"+rtkInstructions() {
		t.Errorf("AGENT.md is not the old append:\n%s", after["AGENT.md"])
	}
	if after["CLAUDE.md"] != before["CLAUDE.md"] {
		t.Errorf("CLAUDE.md must not change:\n%s", after["CLAUDE.md"])
	}
	if !s.RTKInstructionsDelivered {
		t.Error("RTKInstructionsDelivered not set for the legacy write")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "CLAUDE.local.md")); err == nil && strings.Contains(string(b), rtkBeginMarker) {
		t.Errorf("legacy mode must not also write the untracked file:\n%s", b)
	}
}

// ---- startup cleanup, with a fake git runner and alert sink ----

// fakeGit answers the cleanup's git calls from fixed state and counts calls.
type fakeGit struct {
	tracked  map[string]bool   // file name -> tracked
	status   map[string]string // file name -> porcelain line
	head     map[string]string // file name -> committed content
	commands []string
}

func (f *fakeGit) Git(dir string, args ...string) (string, error) {
	f.commands = append(f.commands, strings.Join(args, " "))
	name := args[len(args)-1]
	switch args[0] {
	case "ls-files":
		if !f.tracked[name] {
			return "", fmt.Errorf("exit status 1")
		}
		return name + "\n", nil
	case "status":
		return f.status[name], nil
	case "show":
		n := strings.TrimPrefix(name, "HEAD:")
		h, ok := f.head[n]
		if !ok {
			return "", fmt.Errorf("exit status 128")
		}
		return h, nil
	}
	return "", fmt.Errorf("unexpected git %v", args)
}

type alertLog struct{ got []string }

func (a *alertLog) sink(level, title, body string) { a.got = append(a.got, level+"|"+title+"|"+body) }

func TestCleanupRTKRulesFiles_SafeCaseRemovesBlock(t *testing.T) {
	dir := t.TempDir()
	committed := "# AGENT.md\n\nrules\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(committed+"\n\n"+rtkInstructions()), 0o640); err != nil {
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
		t.Errorf("AGENT.md = %q, want the committed %q", got, committed)
	}
	if len(res.Cleaned) != 1 || len(res.Unsafe) != 0 || res.Alerts != 0 || len(al.got) != 0 {
		t.Errorf("result = %+v, alerts = %v", res, al.got)
	}
	if info, _ := os.Stat(filepath.Join(dir, "AGENT.md")); info.Mode().Perm() != 0o640 {
		t.Errorf("file mode changed to %v", info.Mode().Perm())
	}

	// Idempotent: a second pass finds no block and does nothing.
	g.commands = nil
	res = CleanupRTKRulesFiles([]string{dir}, g, al.sink)
	if len(res.Cleaned) != 0 || len(g.commands) != 0 || len(al.got) != 0 {
		t.Errorf("second pass: %+v, git calls %v", res, g.commands)
	}
}

func TestCleanupRTKRulesFiles_SafeCaseNoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	committed := "# CLAUDE.md\nlast line without newline"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(committed+"\n\n"+rtkInstructions()), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &fakeGit{
		tracked: map[string]bool{"CLAUDE.md": true},
		status:  map[string]string{"CLAUDE.md": " M CLAUDE.md\n"},
		head:    map[string]string{"CLAUDE.md": committed},
	}
	CleanupRTKRulesFiles([]string{dir}, g, nil)
	if got := pcRead(t, filepath.Join(dir, "CLAUDE.md")); got != committed {
		t.Errorf("CLAUDE.md = %q, want %q", got, committed)
	}
}

func TestCleanupRTKRulesFiles_UnsafeCasesLeaveFileAndAlertOnce(t *testing.T) {
	committed := "# AGENT.md\n\nrules\n"
	withBlock := committed + "\n\n" + rtkInstructions()

	cases := []struct {
		name    string
		content string
		git     *fakeGit
	}{
		{"untracked", withBlock, &fakeGit{
			tracked: map[string]bool{},
			status:  map[string]string{},
			head:    map[string]string{"AGENT.md": committed},
		}},
		{"other uncommitted change", withBlock + "\nextra operator edit\n", &fakeGit{
			tracked: map[string]bool{"AGENT.md": true},
			status:  map[string]string{"AGENT.md": " M AGENT.md\n"},
			head:    map[string]string{"AGENT.md": committed},
		}},
		{"other change before the block", "# AGENT.md\n\nrules EDITED\n\n\n" + rtkInstructions(), &fakeGit{
			tracked: map[string]bool{"AGENT.md": true},
			status:  map[string]string{"AGENT.md": " M AGENT.md\n"},
			head:    map[string]string{"AGENT.md": committed},
		}},
		{"block is committed", withBlock, &fakeGit{
			tracked: map[string]bool{"AGENT.md": true},
			status:  map[string]string{"AGENT.md": ""},
			head:    map[string]string{"AGENT.md": withBlock},
		}},
		{"staged change", withBlock, &fakeGit{
			tracked: map[string]bool{"AGENT.md": true},
			status:  map[string]string{"AGENT.md": "MM AGENT.md\n"},
			head:    map[string]string{"AGENT.md": committed},
		}},
		{"git show fails", withBlock, &fakeGit{
			tracked: map[string]bool{"AGENT.md": true},
			status:  map[string]string{"AGENT.md": " M AGENT.md\n"},
			head:    map[string]string{},
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "AGENT.md")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			al := &alertLog{}
			res := CleanupRTKRulesFiles([]string{dir}, tc.git, al.sink)
			if got := pcRead(t, path); got != tc.content {
				t.Errorf("file changed:\n%s", got)
			}
			if len(al.got) != 1 || res.Alerts != 1 {
				t.Fatalf("alerts = %d (%v), want exactly 1", len(al.got), al.got)
			}
			if !strings.HasPrefix(al.got[0], "warn|") || !strings.Contains(al.got[0], dir) {
				t.Errorf("alert = %q", al.got[0])
			}
			if len(res.Unsafe) != 1 || len(res.Cleaned) != 0 {
				t.Errorf("result = %+v", res)
			}
		})
	}
}

func TestCleanupRTKRulesFiles_OneAlertPerWorktreeEvenWithTwoUnsafeFiles(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"AGENT.md", "CLAUDE.md"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x\n\n"+rtkInstructions()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g := &fakeGit{tracked: map[string]bool{}}
	al := &alertLog{}
	res := CleanupRTKRulesFiles([]string{dir, dir}, g, al.sink)
	if len(al.got) != 1 || len(res.Unsafe) != 2 {
		t.Errorf("alerts = %v, result = %+v", al.got, res)
	}
}

func TestCleanupRTKRulesFiles_NoBlockNoGitNoAlert(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("# plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &fakeGit{}
	al := &alertLog{}
	res := CleanupRTKRulesFiles([]string{dir, filepath.Join(dir, "missing"), ""}, g, al.sink)
	if len(g.commands) != 0 || len(al.got) != 0 || len(res.Cleaned)+len(res.Unsafe) != 0 {
		t.Errorf("unexpected activity: git=%v alerts=%v res=%+v", g.commands, al.got, res)
	}
}

func TestRemoveRTKBlock_IgnoresInlineMention(t *testing.T) {
	s := "see the `<!-- rtk-instructions -->` marker in docs\n"
	if out, ok := removeRTKBlock(s); ok || out != s {
		t.Errorf("inline mention treated as a block: ok=%v out=%q", ok, out)
	}
}

// TestCleanupRTKRulesFiles_RealGit exercises the real git runner on a
// throwaway repository: the legacy append is undone byte for byte.
func TestCleanupRTKRulesFiles_RealGit(t *testing.T) {
	dir := pcGitRepo(t)
	committed := "# AGENT.md\n\nrules\n"
	path := filepath.Join(dir, "AGENT.md")
	if err := os.WriteFile(path, []byte(committed), 0o644); err != nil {
		t.Fatal(err)
	}
	pcCommit(t, dir)
	if err := os.WriteFile(path, []byte(committed+"\n\n"+rtkInstructions()), 0o644); err != nil {
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

// With rtk.write_rules_files on and the block already in the rules file, the
// file is untouched and the delivered flag still reports the block as present.
func TestRTKSessionStart_WriteRulesFilesTrueBlockAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	body := "# AGENT.md\n\n" + rtkInstructions() + "\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tr, s, err := newGuardrailsTestSession(t.TempDir(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteSessionGuardrails("", s, GuardrailsOptions{RTKEnabled: true, RTKWriteRulesFiles: true}); err != nil {
		t.Fatal(err)
	}
	if got := pcRead(t, filepath.Join(dir, "AGENT.md")); got != body {
		t.Errorf("AGENT.md changed:\n%s", got)
	}
	if !s.RTKInstructionsDelivered {
		t.Error("RTKInstructionsDelivered must be true when the block is already in the rules file")
	}
}

// RTKInstructionsDelivered survives a Store save and reload, like
// PlatformContextLocalFile.
func TestRTKInstructionsDelivered_PersistsThroughStore(t *testing.T) {
	st, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := &Session{ID: "ab12", FullID: "host-ab12", RTKInstructionsDelivered: true, PlatformContextLocalFile: true}
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	got, ok := st.Get("host-ab12")
	if !ok || !got.RTKInstructionsDelivered || !got.PlatformContextLocalFile {
		t.Fatalf("flags lost: ok=%v %+v", ok, got)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"rtk_instructions_delivered":true`) {
		t.Errorf("json tag missing: %s", b)
	}
}
