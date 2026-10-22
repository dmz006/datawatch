package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// The validation gate decides whether an ad-hoc one-shot may commit. These
// tests pin its three outcomes: no commands declared (unvalidated), a passing
// command (passed), and a failing command (failed). They don't need tmux.

func TestValidateOneShot_NoCommandsIsUnvalidated(t *testing.T) {
	m := &Manager{}
	sess := &Session{OneShot: true, ProjectDir: t.TempDir()}
	err := m.validateOneShot(sess)
	if err == nil {
		t.Fatal("expected an error when no validate commands are declared")
	}
	if !strings.HasPrefix(sess.ValidationResult, "unvalidated") {
		t.Fatalf("ValidationResult = %q, want an unvalidated prefix", sess.ValidationResult)
	}
}

func TestValidateOneShot_PassingCommandPasses(t *testing.T) {
	m := &Manager{}
	sess := &Session{OneShot: true, ProjectDir: t.TempDir(), ValidateCommands: []string{"true"}}
	if err := m.validateOneShot(sess); err != nil {
		t.Fatalf("passing validation returned error: %v", err)
	}
	if sess.ValidationResult != "passed" {
		t.Fatalf("ValidationResult = %q, want passed", sess.ValidationResult)
	}
}

func TestValidateOneShot_FailingCommandBlocks(t *testing.T) {
	m := &Manager{}
	sess := &Session{OneShot: true, ProjectDir: t.TempDir(), ValidateCommands: []string{"true", "false"}}
	err := m.validateOneShot(sess)
	if err == nil {
		t.Fatal("expected failing validation to return an error")
	}
	if !strings.HasPrefix(sess.ValidationResult, "failed") {
		t.Fatalf("ValidationResult = %q, want a failed prefix", sess.ValidationResult)
	}
}

func TestOneShotCompletionMarker_IgnoresEchoedCommand(t *testing.T) {
	// The echoed launch command carries the marker mid-line, after "echo '".
	// Only a line that starts with the marker counts as finished.
	echoed := "cd '/x' && pi --print ...; echo 'DATAWATCH_COMPLETE: pi done'"
	if strings.HasPrefix(strings.TrimSpace(echoed), oneShotCompletionMarker) {
		t.Fatal("echoed command line must not count as completion")
	}
	printed := "DATAWATCH_COMPLETE: pi done"
	if !strings.HasPrefix(strings.TrimSpace(printed), oneShotCompletionMarker) {
		t.Fatal("printed marker line must count as completion")
	}
}

func TestSessionRetentionDays_DefaultAndOverride(t *testing.T) {
	m := &Manager{}
	if got := m.sessionRetentionDays(); got != defaultSessionRetentionDays {
		t.Fatalf("default retention = %d, want %d", got, defaultSessionRetentionDays)
	}
	m.cfg = &config.Config{}
	m.cfg.Session.SessionRetentionDays = 7
	if got := m.sessionRetentionDays(); got != 7 {
		t.Fatalf("configured retention = %d, want 7", got)
	}
}

func TestRemoveOneShotScratch_RemovesOnlyThatSession(t *testing.T) {
	mine := filepath.Join(os.TempDir(), "datawatch-pi-task-cs-testhost-gate1-111.txt")
	other := filepath.Join(os.TempDir(), "datawatch-pi-task-cs-testhost-gate2-222.txt")
	for _, f := range []string{mine, other} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if err := os.Remove(other); err != nil && !os.IsNotExist(err) {
			t.Errorf("cleanup: remove %s: %v", other, err)
		}
	})
	removeOneShotScratch("cs-testhost-gate1")
	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Fatal("scratch file for the closed session should be removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("another session's scratch file must not be touched")
	}
}

// Retention deletes only finished sessions older than the window, and never a
// live one. The store record is kept either way.
func TestPruneSessionFolders_DeletesOnlyOldFinishedSessions(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	now := time.Now()
	mk := func(id string, state State, age time.Duration) string {
		sess := &Session{ID: id, FullID: "testhost-" + id, State: state, UpdatedAt: now.Add(-age)}
		if err := mgr.store.Save(sess); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(mgr.dataDir, "sessions", sess.FullID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	oldDone := mk("old1", StateComplete, 45*24*time.Hour)
	newDone := mk("new1", StateComplete, 2*24*time.Hour)
	oldLive := mk("live1", StateRunning, 90*24*time.Hour)

	removed := mgr.PruneSessionFolders(now)

	if len(removed) != 1 || removed[0] != "testhost-old1" {
		t.Fatalf("removed = %v, want only testhost-old1", removed)
	}
	if _, err := os.Stat(oldDone); !os.IsNotExist(err) {
		t.Fatal("old finished session folder should be deleted")
	}
	for name, dir := range map[string]string{"recent finished": newDone, "old but live": oldLive} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("%s session folder must be kept: %v", name, err)
		}
	}
	if _, ok := mgr.store.Get("testhost-old1"); !ok {
		t.Fatal("the store record must stay after its folder is pruned")
	}
}
