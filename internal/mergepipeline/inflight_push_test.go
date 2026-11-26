//go:build unix

package mergepipeline

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// A push the remote does not show yet may still arrive while the remote has not
// moved: it stays journalled, and is recorded when it does arrive.
func TestJournalledPushThatArrivesLateIsRecorded(t *testing.T) {
	for name, arrives := range map[string]bool{"it arrives": true, "the remote moves on instead": false} {
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			committerEnv(t)
			runGit(t, s.dir, "tag", "v1.2.3")
			var landed []string
			late := releaseRunner(t, s, DefaultDefinition(), realPusher{fake: true}, sandboxLanding{s: s, recorded: &landed}, &fakeNotify{})
			_, _ = releaseFeature(late)
			entries := pushJournal(t, s)
			if len(entries) != 1 {
				t.Fatalf("journal = %v", entries)
			}
			commit := journalCommit(t, entries[0])
			again := releaseRunner(t, s, DefaultDefinition(), realPusher{}, sandboxLanding{s: s, recorded: &landed}, &fakeNotify{})
			if res, err := again.ReconcilePushes(context.Background()); err != nil || len(res) != 1 || res[0].Pushed || res[0].Done {
				t.Fatalf("while it may still arrive: %+v %v", res, err)
			}
			if arrives {
				runGit(t, s.dir, "push", "-q", "--atomic", "origin", commit+":refs/heads/main", commit+":refs/tags/v1.2.4")
				res, err := again.ReconcilePushes(context.Background())
				if err != nil || len(res) != 1 || !res[0].Pushed || !res[0].Done {
					t.Fatalf("after it arrived: %+v %v", res, err)
				}
				assertReconciled(t, s, landed, commit)
				return
			}
			landOnRemoteMain(t, s.bare, "1.4.0")
			res, err := again.ReconcilePushes(context.Background())
			if err != nil || len(res) != 1 || res[0].Pushed || !res[0].Done || len(landed) != 0 {
				t.Fatalf("after the remote moved on: %+v %v landed=%v", res, err, landed)
			}
			if left := pushJournal(t, s); len(left) != 0 {
				t.Errorf("journal = %v", left)
			}
			if got := runGit(t, s.dir, "tag", "--list"); got != "v1.2.3" {
				t.Errorf("tags = %q", got)
			}
		})
	}
}

func journalCommit(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var p journalledPush
	if err := json.Unmarshal(data, &p); err != nil || p.Commit == "" {
		t.Fatalf("journal entry %s: %v", data, err)
	}
	return p.Commit
}
