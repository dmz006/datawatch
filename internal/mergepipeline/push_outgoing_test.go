package mergepipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/git"
)

// plantedToken is assembled at runtime so no credential-shaped text is
// committed to this repository.
func plantedToken() string { return "gh" + "p_" + strings.Repeat("aB3d", 7) }

func enforceDaemonScan(t *testing.T) {
	t.Helper()
	git.SetDaemonScanPolicy(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: git.ScanEnforce} })
	t.Cleanup(func() { git.SetDaemonScanPolicy(nil) })
}

// The merge push runs the outgoing check, for the branch and for the tag, and
// sends neither when it refuses.
func TestMergePushRefusesPlantedSecrets(t *testing.T) {
	for name, plant := range map[string]func(f pushGuardFixture){
		"in a file": func(f pushGuardFixture) {
			commitFile(t, f.local, "env", "GITHUB_TOKEN=\""+plantedToken()+"\"\n")
			pushGuardGit(t, f.local, "tag", "-a", "v0.0.1", "-m", "Release v0.0.1")
		},
		"in the tag message": func(f pushGuardFixture) {
			commitFile(t, f.local, "b.txt", "two\n")
			pushGuardGit(t, f.local, "tag", "-a", "v0.0.1", "-m", "Release "+plantedToken())
		},
	} {
		t.Run(name, func(t *testing.T) {
			enforceDaemonScan(t)
			f := newPushGuardFixture(t)
			before := f.remoteMain(t)
			plant(f)

			err := (ExecShipRunner{}).Push(context.Background(), f.local, "main", "v0.0.1")

			if err == nil || strings.Contains(err.Error(), plantedToken()) {
				t.Fatalf("want a redacted refusal, got %v", err)
			}
			if got := f.remoteMain(t); got != before {
				t.Fatalf("remote main moved to %s", got)
			}
			if got := pushGuardGit(t, f.bare, "tag", "-l"); got != "" {
				t.Fatalf("tag pushed: %s", got)
			}
		})
	}
}
