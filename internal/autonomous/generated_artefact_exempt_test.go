package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

const (
	exemptBranch = "automaton/abcd0001"
	exemptID     = "abcd0001"
)

// exemptRepo is a throwaway repository with one finished Automaton work tree
// whose branch is fully on trunk.
func exemptRepo(t *testing.T) (repo, wt string) {
	t.Helper()
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	uwGit(t, repo, "init", "-b", "trunk")
	exemptPut(t, repo, "a.txt", "a\n")
	uwGit(t, repo, "add", "a.txt")
	uwGit(t, repo, "commit", "-m", "init")
	wt = filepath.Join(root, "wt")
	uwGit(t, repo, "worktree", "add", "-b", exemptBranch, wt, "trunk")
	return repo, wt
}

func exemptPut(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// exemptGenerate writes one document with the daemon's own generator, which
// also writes the hash file beside it.
func exemptGenerate(t *testing.T, wt string, kind genartifacts.Kind) string {
	t.Helper()
	rel := strings.ReplaceAll(genartifacts.DefaultPaths()[kind], genartifacts.IDPlaceholder, exemptID)
	res, err := genartifacts.Write(kind, "generated body\n", rel, genartifacts.Options{Root: wt})
	if err != nil || res.Status != genartifacts.Written {
		t.Fatalf("generate %s: %v %+v", kind, err, res)
	}
	return rel
}

func exemptGenerateAll(t *testing.T, wt string) []string {
	t.Helper()
	var out []string
	for _, k := range genartifacts.Kinds() {
		rel := exemptGenerate(t, wt, k)
		out = append(out, rel, genartifacts.SidecarRelPath(rel))
	}
	return out
}

type exemptProbe struct{}

func (exemptProbe) AutomatonFinished(string) bool               { return true }
func (exemptProbe) BaseBranch(string) string                    { return "trunk" }
func (exemptProbe) WorktreeInUse(string, string) (bool, string) { return false, "" }
func (exemptProbe) EntryAge(string) (time.Duration, bool)       { return time.Hour, true }
func (exemptProbe) WorktreeSize(wt string) (int64, error)       { return dirSize(wt) }

// exemptRun runs the real safe-to-delete report and the real cleanup over it.
func exemptRun(t *testing.T, repo, mode string, audit WorktreeCleanupAudit) (SafeDeleteReport, WorktreeCleanupResult) {
	t.Helper()
	git := NewMergeGitRunner()
	report, err := SafeToDeleteReport(context.Background(), git, repo, "trunk", SafeDeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	res := CleanupWorktrees(WorktreeCleanupOptions{
		Repo: repo, Report: report, Mode: mode, MaxRemovals: 5,
		Git: git, Probe: exemptProbe{}, Audit: audit,
	})
	return report, res
}

func exemptMustExist(t *testing.T, wt string, rels []string) {
	t.Helper()
	for _, rel := range rels {
		if _, err := os.Stat(filepath.Join(wt, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("%s must be kept: %v", rel, err)
		}
	}
}

// A merged Automaton whose only untracked files are the daemon's own unedited
// documents is safe to delete, and the work tree and branch are removed.
func TestGeneratedArtefactsDoNotHoldAMergedWorktree(t *testing.T) {
	cases := map[string]func(t *testing.T, repo, wt string){
		// docs/ does not exist in the project: git reports one "?? docs/" line.
		"collapsed folder": func(*testing.T, string, string) {},
		// The folders hold tracked files: git reports one line per new file.
		"tracked neighbours": func(t *testing.T, _, wt string) {
			for _, d := range []string{"drafts", "reports", "parity-issues"} {
				exemptPut(t, wt, "docs/plans/"+d+"/kept.md", "tracked\n")
			}
			uwGit(t, wt, "add", "docs")
			uwGit(t, wt, "commit", "-m", "tracked docs")
		},
	}
	for name, prep := range cases {
		t.Run(name, func(t *testing.T) {
			repo, wt := exemptRepo(t)
			prep(t, repo, wt)
			uwGit(t, repo, "merge", "--ff-only", exemptBranch)
			files := exemptGenerateAll(t, wt)

			var audited []string
			report, res := exemptRun(t, repo, config.CleanupModeOn, func(_ string, d map[string]any) error {
				audited, _ = d["resolved_files"].([]string)
				return nil
			})
			if report.HeldDirty != 0 || len(report.Safe) != 1 {
				t.Fatalf("report: safe=%v held=%+v", report.Safe, report.Held)
			}
			if len(res.Removed) != 1 {
				t.Fatalf("removed=%+v skipped=%+v failed=%+v", res.Removed, res.Skipped, res.Failed)
			}
			if _, err := os.Stat(wt); !os.IsNotExist(err) {
				t.Fatal("work tree still present")
			}
			if out := uwGit(t, repo, "branch", "--list", exemptBranch); out != "" {
				t.Fatalf("branch still present: %q", out)
			}
			for _, rel := range files {
				if !containsString(res.Removed[0].ResolvedFiles, rel) || !containsString(audited, rel) {
					t.Fatalf("%s not recorded: result=%v audit=%v", rel, res.Removed[0].ResolvedFiles, audited)
				}
			}
		})
	}
}

// Anything the daemon cannot prove it wrote, unedited, still holds the work
// tree, and nothing in the tree is deleted.
func TestHandWrittenFilesStillHoldBesideGeneratedArtefacts(t *testing.T) {
	changelog := "docs/plans/drafts/" + exemptID + "-changelog.md"
	cases := map[string]struct {
		spoil func(t *testing.T, wt string)
		held  string
	}{
		"hand-written draft": {func(t *testing.T, wt string) {
			exemptPut(t, wt, "docs/plans/drafts/my-notes.md", "by hand\n")
		}, "docs/plans/drafts/my-notes.md"},
		"hand-written file under a generated name": {func(t *testing.T, wt string) {
			exemptPut(t, wt, "docs/plans/drafts/ffff0001-changelog.md", "by hand\n")
		}, "docs/plans/drafts/ffff0001-changelog.md"},
		"edited generated document": {func(t *testing.T, wt string) {
			exemptPut(t, wt, changelog, genartifacts.Marker+"\nrewritten by a person\n")
		}, changelog},
		"hash file without its document": {func(t *testing.T, wt string) {
			exemptPut(t, wt, "docs/plans/drafts/.gone.md.genhash", "0\n")
		}, "docs/plans/drafts/.gone.md.genhash"},
		"document that is a link": {func(t *testing.T, wt string) {
			full := filepath.Join(wt, filepath.FromSlash(changelog))
			real := filepath.Join(t.TempDir(), "real.md")
			b, err := os.ReadFile(full)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(real, b, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(full); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, full); err != nil {
				t.Fatal(err)
			}
		}, changelog},
		"tracked file changed": {func(t *testing.T, wt string) {
			exemptPut(t, wt, "a.txt", "changed\n")
		}, "a.txt"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			repo, wt := exemptRepo(t)
			files := exemptGenerateAll(t, wt)
			tc.spoil(t, wt)
			report, res := exemptRun(t, repo, config.CleanupModeOn, nil)
			if report.HeldDirty != 1 || len(res.Removed) != 0 {
				t.Fatalf("report=%+v removed=%+v", report, res.Removed)
			}
			exemptMustExist(t, wt, files)

			// The cleanup holds it on its own too, when handed a safe entry,
			// and names the file that holds it.
			direct := CleanupWorktrees(WorktreeCleanupOptions{
				Repo: repo, Mode: config.CleanupModeOn, MaxRemovals: 5,
				Report: SafeDeleteReport{Safe: []SafeDeleteEntry{{Branch: exemptBranch, Worktree: wt}}},
				Git:    NewMergeGitRunner(), Probe: exemptProbe{},
			})
			if len(direct.Removed) != 0 || len(direct.Skipped) != 1 || direct.Skipped[0].Reason != cleanupSkipDirty {
				t.Fatalf("removed=%+v skipped=%+v failed=%+v", direct.Removed, direct.Skipped, direct.Failed)
			}
			if !containsString(direct.Skipped[0].Files, tc.held) {
				t.Fatalf("held files %v do not name %s", direct.Skipped[0].Files, tc.held)
			}
			exemptMustExist(t, wt, files)
		})
	}
}

// Unmerged commits hold the work tree and its generated documents.
func TestUnmergedWorkStillHoldsGeneratedArtefacts(t *testing.T) {
	repo, wt := exemptRepo(t)
	exemptPut(t, wt, "new.txt", "unmerged\n")
	uwGit(t, wt, "add", "new.txt")
	uwGit(t, wt, "commit", "-m", "not on trunk")
	files := exemptGenerateAll(t, wt)
	_, res := exemptRun(t, repo, config.CleanupModeOn, nil)
	if len(res.Removed) != 0 {
		t.Fatalf("removed=%+v", res.Removed)
	}
	direct := CleanupWorktrees(WorktreeCleanupOptions{
		Repo: repo, Mode: config.CleanupModeOn, MaxRemovals: 5,
		Report: SafeDeleteReport{Safe: []SafeDeleteEntry{{Branch: exemptBranch, Worktree: wt}}},
		Git:    NewMergeGitRunner(), Probe: exemptProbe{},
	})
	if len(direct.Skipped) != 1 || direct.Skipped[0].Reason != cleanupSkipCommits {
		t.Fatalf("skipped=%+v failed=%+v", direct.Skipped, direct.Failed)
	}
	exemptMustExist(t, wt, files)
}

// Report mode names what would be removed and deletes nothing.
func TestGeneratedArtefactsReportModeDeletesNothing(t *testing.T) {
	repo, wt := exemptRepo(t)
	files := exemptGenerateAll(t, wt)
	_, res := exemptRun(t, repo, config.CleanupModeReport, nil)
	if len(res.WouldRemove) != 1 || len(res.WouldRemove[0].ResolvedFiles) != len(files) {
		t.Fatalf("would remove=%+v skipped=%+v", res.WouldRemove, res.Skipped)
	}
	exemptMustExist(t, wt, files)
}

// A person's edit that arrives between the status read and the removal is not
// destroyed: the entry fails and the tree keeps every file.
func TestGeneratedArtefactEditedBeforeRemovalIsKept(t *testing.T) {
	for name, harness := range map[string]bool{"strict removal": false, "with harness files": true} {
		t.Run(name, func(t *testing.T) {
			repo, wt := exemptRepo(t)
			files := exemptGenerateAll(t, wt)
			edited := files[0]
			if harness {
				// Hand the cleanup a tree that also holds a harness file, which
				// is the one case it removes with force.
				exemptPut(t, wt, ".mcp.json", "{}\n")
			}
			res := CleanupWorktrees(WorktreeCleanupOptions{
				Repo: repo, Mode: config.CleanupModeOn, MaxRemovals: 5,
				Report: SafeDeleteReport{Safe: []SafeDeleteEntry{{Branch: exemptBranch, Worktree: wt}}},
				Git:    NewMergeGitRunner(), Probe: exemptProbe{},
				Audit: func(string, map[string]any) error {
					exemptPut(t, wt, edited, "taken over by a person\n")
					return nil
				},
			})
			if len(res.Removed) != 0 || len(res.Failed) != 1 || res.Failed[0].Stage != cleanupStageRemove {
				t.Fatalf("removed=%+v skipped=%+v failed=%+v", res.Removed, res.Skipped, res.Failed)
			}
			exemptMustExist(t, wt, files)
		})
	}
}
