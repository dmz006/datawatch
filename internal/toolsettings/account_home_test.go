package toolsettings

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func TestAccountHomeIsTheUserDatabaseOneByDefault(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	t.Setenv("HOME", t.TempDir()) // the lookup must not follow HOME
	if got := AccountHome(); got != u.HomeDir {
		t.Fatalf("AccountHome() = %q, want the user database's %q", got, u.HomeDir)
	}
}

// HOME can be moved; the account's own home keeps the user's own settings, so
// both are refused.
func TestRefuseUserClaudeRefusesTheAccountHomeWhenHOMEIsElsewhere(t *testing.T) {
	account := t.TempDir()
	old := AccountHome
	AccountHome = func() string { return account }
	t.Cleanup(func() { AccountHome = old })
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if err := os.MkdirAll(filepath.Join(account, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := RefuseUserClaude(account, "settings.json"); err == nil {
		t.Error("the account home was accepted as a project folder")
	}
	linked := t.TempDir()
	if err := os.Symlink(filepath.Join(account, ".claude"), filepath.Join(linked, ".claude")); err != nil {
		t.Skip(err)
	}
	if err := RefuseUserClaude(linked, "settings.json"); err == nil {
		t.Error("a project whose .claude is the account's was accepted")
	}
	if err := RefuseUserClaude(t.TempDir(), "settings.json"); err != nil {
		t.Errorf("an ordinary project folder was refused: %v", err)
	}
}
