package autonomous

// MainCheckout guard tests: Approve and Run refuse an Automaton whose
// ProjectDir is the main working tree of the daemon's own repository, the
// refusal names the remedy, and an empty or a non-main ProjectDir still
// approves. The mainCheckoutCheck seam carries fakes — no real git.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mainCheckoutPRD returns an Automaton in status with ProjectDir set.
func mainCheckoutPRD(t *testing.T, m *Manager, status PRDStatus, projectDir string) *PRD {
	t.Helper()
	prd := newEmitPRD(t, m, PRDDraft)
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = status
	prd.ProjectDir = projectDir
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	return got
}

// TestApproveRefusedOnMainCheckout — Approve refuses an Automaton pointed
// at the daemon's own main working tree and leaves it where it was.
func TestApproveRefusedOnMainCheckout(t *testing.T) {
	m := newEmitManager(t, DefaultConfig(), nil)
	m.SetMainCheckoutCheck(func(*PRD) bool { return true })
	prd := mainCheckoutPRD(t, m, PRDNeedsReview, "/repo/main")

	_, err := m.Approve(prd.ID, "operator", "")
	if err == nil {
		t.Fatal("Approve on the daemon's main checkout must be refused")
	}
	if got, _ := m.Store().GetPRD(prd.ID); got.Status != PRDNeedsReview {
		t.Fatalf("a refused approval changed the status to %s", got.Status)
	}
}

// TestRunRefusedOnMainCheckout — Run refuses before doing anything.
func TestRunRefusedOnMainCheckout(t *testing.T) {
	m := newEmitManager(t, DefaultConfig(), nil)
	m.SetMainCheckoutCheck(func(*PRD) bool { return true })
	prd := mainCheckoutPRD(t, m, PRDApproved, "/repo/main")

	err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	if err == nil {
		t.Fatal("Run on the daemon's main checkout must be refused")
	}
	if got, _ := m.Store().GetPRD(prd.ID); got.Status != PRDApproved {
		t.Fatalf("a refused run changed the status to %s", got.Status)
	}
}

// TestMainCheckoutRefusalNamesRemedy — the refusal an operator sees names
// the remedy: recreate the Automaton without a project dir.
func TestMainCheckoutRefusalNamesRemedy(t *testing.T) {
	m := newEmitManager(t, DefaultConfig(), nil)
	m.SetMainCheckoutCheck(func(*PRD) bool { return true })
	want := "recreate the Automaton without a project dir"

	apr := mainCheckoutPRD(t, m, PRDNeedsReview, "/repo/main")
	_, aerr := m.Approve(apr.ID, "operator", "")
	if aerr == nil || !strings.Contains(aerr.Error(), want) {
		t.Fatalf("Approve refusal %v must contain %q", aerr, want)
	}

	rpr := mainCheckoutPRD(t, m, PRDApproved, "/repo/main")
	rerr := m.Run(context.Background(), rpr.ID, fakeSpawn, fakeVerify)
	if rerr == nil || !strings.Contains(rerr.Error(), want) {
		t.Fatalf("Run refusal %v must contain %q", rerr, want)
	}
}

// TestApproveAllowedWithEmptyProjectDir — a ProjectDir-less Automaton is
// never a main checkout, even when the check seam says yes anyway.
func TestApproveAllowedMainCheckoutEmptyProjectDir(t *testing.T) {
	m := newEmitManager(t, DefaultConfig(), nil)
	m.SetMainCheckoutCheck(func(*PRD) bool { return true })
	prd := mainCheckoutPRD(t, m, PRDNeedsReview, "")

	updated, err := m.Approve(prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("Approve with an empty project dir must not be refused: %v", err)
	}
	if updated.Status != PRDApproved {
		t.Fatalf("status = %s, want approved", updated.Status)
	}
}

// TestRunAllowedOnNonMainDirectory — a ProjectDir the check says is not
// the main working tree is not refused.
func TestRunAllowedMainCheckoutNonMainDirectory(t *testing.T) {
	m := newEmitManager(t, DefaultConfig(), nil)
	m.SetMainCheckoutCheck(func(*PRD) bool { return false })
	prd := mainCheckoutPRD(t, m, PRDApproved, "/elsewhere/worktree")

	// The guard must not be the blocker: anything earlier or later may
	// still refuse, but not for the main-checkout reason.
	err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	if err != nil && strings.Contains(err.Error(), "main working tree") {
		t.Fatalf("a non-main project dir was refused as a main checkout: %v", err)
	}
}

// TestIsMainCheckoutStatRules — the real, stat-based helper (no git
// commands): equal resolved paths with a DIRECTORY .git is the main
// checkout; a FILE .git (linked worktree), a missing .git, unreadable
// paths, empty arguments and unequal paths are not.
func TestIsMainCheckoutStatRules(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	worktree := filepath.Join(root, "wt")
	if err := os.MkdirAll(filepath.Join(main, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+filepath.Join(main, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(root, "bare")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		projectDir string
		baseRepo   string
		want       bool
	}{
		{"main checkout", main, main, true},
		{"symlink-resolved match", filepath.Join(root, "dots", "..", "main"), main, true},
		{"linked worktree has a .git file", worktree, main, false},
		{"no .git entry", bare, bare, false},
		{"empty project dir", "", main, false},
		{"blank project dir", "   ", main, false},
		{"empty base repo", main, "", false},
		{"paths differ", worktree, bare, false},
		{"unreadable project dir", filepath.Join(root, "missing"), main, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isMainCheckout(c.projectDir, c.baseRepo); got != c.want {
				t.Fatalf("isMainCheckout(%q, %q) = %v, want %v", c.projectDir, c.baseRepo, got, c.want)
			}
		})
	}
}

// TestApproveRealCheckRefusesMainCheckout — with no seam wired, the
// default real check refuses Approve when the project dir is a configured
// base repo whose .git is a directory, and allows it when the base repo is
// unconfigured (nothing can be the daemon's main checkout).
func TestApproveRealCheckRefusesMainCheckout(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	if err := os.MkdirAll(filepath.Join(main, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = main
	m := newEmitManager(t, cfg, nil)
	prd := mainCheckoutPRD(t, m, PRDNeedsReview, main)
	_, err := m.Approve(prd.ID, "operator", "")
	if err == nil {
		t.Fatal("Approve on the configured base repo's main checkout must be refused")
	}
	if !strings.Contains(err.Error(), "recreate the Automaton without a project dir") {
		t.Fatalf("refusal %v must name the remedy", err)
	}

	m2 := newEmitManager(t, DefaultConfig(), nil) // no base repo configured
	prd2 := mainCheckoutPRD(t, m2, PRDNeedsReview, main)
	if updated, err := m2.Approve(prd2.ID, "operator", ""); err != nil || updated.Status != PRDApproved {
		t.Fatalf("with no base repo configured, approve should pass: %v (status %s)", err, updated.Status)
	}
}
