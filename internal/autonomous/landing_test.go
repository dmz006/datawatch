package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"
)

func landingPRD(t *testing.T, m *Manager, status PRDStatus, branch string) *PRD {
	t.Helper()
	p, err := m.store.CreatePRD("spec", "/p", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	m.store.WithLock(func() {
		p.Status = status
		p.Git.Branch = branch
		markLandingPendingLocked(p, "autonomous", "branch created")
	})
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLandingStates(t *testing.T) {
	cases := []struct {
		name        string
		status      PRDStatus
		apply       func(m *Manager, id string) error
		want        LandingState
		wantArchive bool
	}{
		{"pending completed refused", PRDCompleted, nil, LandingPending, false},
		{"pending rejected refused", PRDRejected, nil, LandingPending, false},
		{"pending cancelled refused", PRDCancelled, nil, LandingPending, false},
		{"landed via MarkPRDCompleted", PRDCancelled, func(m *Manager, id string) error {
			_, err := m.MarkPRDCompleted(id, "", time.Now(), "abc123", "merge_pipeline", "merged")
			return err
		}, LandingLanded, true},
		{"landed on already completed", PRDCompleted, func(m *Manager, id string) error {
			_, err := m.MarkPRDCompleted(id, "", time.Now(), "abc123", "merge_pipeline", "merged")
			return err
		}, LandingLanded, true},
		{"kept", PRDCancelled, func(m *Manager, id string) error {
			_, err := m.KeepLanding(id, "operator", "keep for reference")
			return err
		}, LandingKept, true},
		{"dropped", PRDRejected, func(m *Manager, id string) error {
			_, err := m.DropLanding(id, "operator", "superseded")
			return err
		}, LandingDropped, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := cmFixture(t)
			p := landingPRD(t, m, tc.status, "automaton/"+strings.ReplaceAll(tc.name, " ", "-"))
			if p.Landing == nil || p.Landing.State != LandingPending {
				t.Fatalf("new branch landing = %+v, want pending", p.Landing)
			}
			if tc.apply != nil {
				if err := tc.apply(m, p.ID); err != nil {
					t.Fatal(err)
				}
			}
			got, _ := m.store.GetPRD(p.ID)
			if got.Landing == nil || got.Landing.State != tc.want {
				t.Fatalf("landing = %+v, want %s", got.Landing, tc.want)
			}
			if tc.want == LandingLanded && got.Landing.Commit != "abc123" {
				t.Fatalf("landed commit = %q", got.Landing.Commit)
			}
			if tc.want == LandingKept || tc.want == LandingDropped {
				d := got.Decisions[len(got.Decisions)-1]
				if d.Kind != DecisionLandingKeep && d.Kind != DecisionLandingDrop {
					t.Fatalf("last decision = %+v", d)
				}
			}
			_, err := m.Archive(p.ID, "operator")
			if tc.wantArchive && err != nil {
				t.Fatalf("archive: %v", err)
			}
			if !tc.wantArchive {
				if err == nil {
					t.Fatal("archive of a pending landing succeeded")
				}
				msg := err.Error()
				for _, w := range []string{"Automaton", got.Git.Branch, "merge it", "keep or drop"} {
					if !strings.Contains(msg, w) {
						t.Fatalf("archive error %q missing %q", msg, w)
					}
				}
			}
		})
	}
}

func TestLandingKeepDropRequireActorAndReason(t *testing.T) {
	m := cmFixture(t)
	p := landingPRD(t, m, PRDCancelled, "automaton/x")
	if _, err := m.KeepLanding(p.ID, "", "r"); err == nil {
		t.Fatal("keep without actor accepted")
	}
	if _, err := m.DropLanding(p.ID, "operator", " "); err == nil {
		t.Fatal("drop without reason accepted")
	}
}

func TestLandingNoBranchNoRecord(t *testing.T) {
	m := cmFixture(t)
	p := landingPRD(t, m, PRDCancelled, "")
	if p.Landing != nil {
		t.Fatalf("landing set without a branch: %+v", p.Landing)
	}
	if _, err := m.Archive(p.ID, "operator"); err != nil {
		t.Fatal(err)
	}
}

func TestLandingMigration(t *testing.T) {
	m := cmFixture(t)
	mk := func(status PRDStatus, branch string) *PRD {
		p, err := m.store.CreatePRD("spec", "/p", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		p.Status = status
		p.Git.Branch = branch
		if err := m.store.SavePRD(p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	unmerged := mk(PRDArchived, "automaton/unmerged")
	merged := mk(PRDArchived, "automaton/merged")
	noBranch := mk(PRDArchived, "")
	notArchived := mk(PRDCancelled, "automaton/cancelled")
	git := newCMGit("automaton/merged")

	n, err := m.MigrateLanding(context.Background(), LandingMigrationOpts{Repo: "/repo", Git: git})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("marked %d, want 1", n)
	}
	got, _ := m.store.GetPRD(unmerged.ID)
	if got.Landing == nil || got.Landing.State != LandingPending || got.Status != PRDArchived || len(got.Decisions) != 0 {
		t.Fatalf("unmerged = status %s landing %+v decisions %d", got.Status, got.Landing, len(got.Decisions))
	}
	for _, p := range []*PRD{merged, noBranch, notArchived} {
		g, _ := m.store.GetPRD(p.ID)
		if g.Landing != nil {
			t.Fatalf("%s landing changed: %+v", p.Git.Branch, g.Landing)
		}
	}
	n, err = m.MigrateLanding(context.Background(), LandingMigrationOpts{Repo: "/repo", Git: git})
	if err != nil || n != 0 {
		t.Fatalf("second run = %d, %v; want 0, nil", n, err)
	}
}
