package main

import (
	"os"
	"strings"
	"testing"
)

// The protected-paths check needs the session manager's rules-file texts
// before the autonomous API starts, which releases false-positive holds.
func TestDaemonRulesTextWiredBeforeAPIStart(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	made := strings.Index(src, "autonomouspkg.NewManager(")
	wired := strings.Index(src, "amgr.SetDaemonRulesTextFn(mgr.RulesFileTextFor)")
	started := strings.Index(src, "amgr.StartLandingMigration()")
	if made < 0 || wired < made || started < wired {
		t.Fatalf("SetDaemonRulesTextFn wiring: manager at %d, wiring at %d, start work at %d", made, wired, started)
	}
}
