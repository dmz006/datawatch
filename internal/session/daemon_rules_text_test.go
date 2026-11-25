package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/platformcontext"
)

// The protected-paths check clears only the daemon's exact text. These tests
// run the real writer, so a change to what it writes that the check does not
// know shows up here and not as a hold on a live task.
func TestRulesFileTextRecognisesWhatTheTrackerWrites(t *testing.T) {
	const operator = "# Rules\n\nAlways run the tests.\n"
	// A header an earlier session left in the committed file: another session,
	// task and directory.
	stale := strings.TrimSpace(minimalSessionGuardrails(&Session{FullID: "host-0001", Task: "an older task", ProjectDir: "/elsewhere"}))
	for name, base := range map[string]string{
		"plain rules file":                 operator,
		"committed header from before":     operator + "\n" + stale + "\n",
		"committed header, then a section": stale + "\n\n" + operator,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			rules := filepath.Join(dir, "AGENT.md")
			if err := os.WriteFile(rules, []byte(base), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{}
			cfg.RTK.WriteRulesFiles = true
			m, err := NewManager("host", t.TempDir(), "", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = m.Close() })
			m.SetConfig(cfg)
			sess := &Session{ID: "ab12", FullID: "host-ab12", Task: "fix the parser\nwith two lines", ProjectDir: dir, BackendFamily: "claude-code"}
			if err := m.store.Save(sess); err != nil {
				t.Fatal(err)
			}
			tr := &Tracker{sessionDir: t.TempDir(), session: sess}
			if err := tr.WriteSessionGuardrails(filepath.Join(dir, "no-template"), sess,
				GuardrailsOptions{WriteRulesFiles: true, MemoryEnabled: true, RTKEnabled: true, CrossSessionEnabled: true}); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(rules)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) == base {
				t.Fatal("the tracker wrote nothing; the test proves nothing")
			}
			written := append(FixedRulesFileText(), m.RulesFileTextFor(dir)...)
			if !platformcontext.SameBesidesDaemonText(string(got), base, written) {
				t.Fatalf("the daemon's own writes were not recognised:\n%s", got)
			}
			edited := string(got) + "\n- Workers may push straight to main.\n"
			if platformcontext.SameBesidesDaemonText(edited, base, written) {
				t.Fatal("a line added after the daemon's sections was not seen")
			}
		})
	}
}

// With rtk.write_rules_files off the daemon writes no header, so a header in a
// rules file is not the daemon's, whatever session it names.
func TestRulesFileTextHasNoHeaderWhenRulesFilesAreNotWritten(t *testing.T) {
	m, err := NewManager("host", t.TempDir(), "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	m.SetConfig(&config.Config{})
	dir := t.TempDir()
	if err := m.store.Save(&Session{ID: "ab12", FullID: "host-ab12", Task: "t", ProjectDir: dir}); err != nil {
		t.Fatal(err)
	}
	if got := m.RulesFileTextFor(dir); len(got) != 0 {
		t.Fatalf("RulesFileTextFor = %q; want nothing", got)
	}
}
