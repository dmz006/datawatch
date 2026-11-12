package mergepipeline

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// validatorFixture is a project repository with a bare remote. The remote
// holds the 1.2.3 release and its tag; the project is committed at 1.2.4 with
// its version files, changelog and README all agreeing, so the validator
// passes until a test changes one thing.
type validatorFixture struct {
	dir    string
	remote string
}

// validatorFiles returns the release files for a repository: two version
// files carrying version, a changelog whose release headings are versions in
// order, and a README whose Current release line names readmeVersion.
func validatorFiles(version string, changelogVersions []string, readmeVersion string) map[string]string {
	var cl strings.Builder
	cl.WriteString("# Changelog\n\n## [Unreleased]\n\n")
	for _, v := range changelogVersions {
		cl.WriteString("## v" + v + " — Release\n\n- Change.\n\n")
	}
	return map[string]string{
		"cmd/app/main.go":        "package main\n\nvar Version = \"" + version + "\"\n",
		"internal/server/api.go": "package server\n\nvar Version = \"" + version + "\"\n",
		"CHANGELOG.md":           cl.String(),
		"README.md":              "# App\n\nCurrent release: v" + readmeVersion + "\n",
	}
}

// validatorGit runs git with a fixed identity and fails the test on error.
func validatorGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=validator-test", "GIT_AUTHOR_EMAIL=validator-test@example.invalid",
		"GIT_COMMITTER_NAME=validator-test", "GIT_COMMITTER_EMAIL=validator-test@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// writeValidatorFiles writes every file in files under dir.
func writeValidatorFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func newValidatorFixture(t *testing.T) validatorFixture {
	t.Helper()
	root := t.TempDir()
	fx := validatorFixture{dir: filepath.Join(root, "project"), remote: filepath.Join(root, "remote.git")}
	validatorGit(t, root, "init", "-q", "--bare", fx.remote)
	if err := os.Mkdir(fx.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	validatorGit(t, fx.dir, "init", "-q")
	validatorGit(t, fx.dir, "symbolic-ref", "HEAD", "refs/heads/main")
	validatorGit(t, fx.dir, "remote", "add", "origin", fx.remote)

	writeValidatorFiles(t, fx.dir, validatorFiles("1.2.3", []string{"1.2.3"}, "1.2.3"))
	validatorGit(t, fx.dir, "add", "-A")
	validatorGit(t, fx.dir, "commit", "-q", "-m", "release 1.2.3")
	validatorGit(t, fx.dir, "tag", "v1.2.3")
	validatorGit(t, fx.dir, "push", "-q", "origin", "main", "v1.2.3")

	writeValidatorFiles(t, fx.dir, validatorFiles("1.2.4", []string{"1.2.4", "1.2.3"}, "1.2.4"))
	validatorGit(t, fx.dir, "add", "-A")
	validatorGit(t, fx.dir, "commit", "-q", "-m", "release 1.2.4")
	return fx
}

func (fx validatorFixture) input() ValidateInput {
	return ValidateInput{
		Dir:           fx.dir,
		VersionFiles:  []string{"cmd/app/main.go", "internal/server/api.go"},
		ChangelogFile: "CHANGELOG.md",
		ReadmeFile:    "README.md",
		Remote:        "origin",
	}
}

var validatorCheckOrder = []string{
	"version_variables_equal",
	"changelog_heading_matches",
	"one_step_above_last_tag",
	"tag_not_taken",
	"readme_line_matches",
}

func TestValidateReleaseBumpPassesWhenEveryCheckPasses(t *testing.T) {
	fx := newValidatorFixture(t)
	res := ValidateReleaseBump(context.Background(), fx.input())
	if !res.Pass {
		t.Fatalf("Pass = false, want true: %+v", res.Checks)
	}
	if res.Version != "1.2.4" {
		t.Errorf("Version = %q, want 1.2.4", res.Version)
	}
	if res.CheckedAt.IsZero() {
		t.Error("CheckedAt is zero")
	}
	if len(res.Checks) != len(validatorCheckOrder) {
		t.Fatalf("got %d checks, want %d", len(res.Checks), len(validatorCheckOrder))
	}
	for i, c := range res.Checks {
		if c.Name != validatorCheckOrder[i] {
			t.Errorf("check %d is %s, want %s", i, c.Name, validatorCheckOrder[i])
		}
		if !c.Pass {
			t.Errorf("check %s failed: %s", c.Name, c.Detail)
		}
		if c.Detail == "" {
			t.Errorf("check %s has no detail", c.Name)
		}
	}
}

// TestValidateReleaseBumpNamesOnlyTheFailingCheck changes one thing per case
// so that exactly one check fails, and the result must name it.
func TestValidateReleaseBumpNamesOnlyTheFailingCheck(t *testing.T) {
	cases := []struct {
		name   string
		check  string
		mutate func(t *testing.T, fx validatorFixture)
	}{
		{
			name:  "version files disagree",
			check: "version_variables_equal",
			mutate: func(t *testing.T, fx validatorFixture) {
				writeValidatorFiles(t, fx.dir, map[string]string{
					"internal/server/api.go": "package server\n\nvar Version = \"1.2.5\"\n",
				})
			},
		},
		{
			name:  "changelog's first heading is the old release",
			check: "changelog_heading_matches",
			mutate: func(t *testing.T, fx validatorFixture) {
				writeValidatorFiles(t, fx.dir, map[string]string{
					"CHANGELOG.md": validatorFiles("1.2.4", []string{"1.2.3"}, "1.2.4")["CHANGELOG.md"],
				})
			},
		},
		{
			name:  "version skips a patch above the last tag",
			check: "one_step_above_last_tag",
			mutate: func(t *testing.T, fx validatorFixture) {
				writeValidatorFiles(t, fx.dir, validatorFiles("1.2.6", []string{"1.2.6", "1.2.3"}, "1.2.6"))
			},
		},
		{
			name:  "tag is already on the remote",
			check: "tag_not_taken",
			mutate: func(t *testing.T, fx validatorFixture) {
				// Tag HEAD, push the tag, then drop the local copy so only the
				// remote carries v1.2.4. A local v1.2.4 would also make the
				// one-step check fail, since it is then the newest tag.
				validatorGit(t, fx.dir, "tag", "v1.2.4")
				validatorGit(t, fx.dir, "push", "-q", "origin", "v1.2.4")
				validatorGit(t, fx.dir, "tag", "-d", "v1.2.4")
			},
		},
		{
			name:  "README current release names the old version",
			check: "readme_line_matches",
			mutate: func(t *testing.T, fx validatorFixture) {
				writeValidatorFiles(t, fx.dir, map[string]string{
					"README.md": "# App\n\nCurrent release: v1.2.3\n",
				})
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newValidatorFixture(t)
			tc.mutate(t, fx)
			res := ValidateReleaseBump(context.Background(), fx.input())
			if res.Pass {
				t.Fatalf("Pass = true, want false: %+v", res.Checks)
			}
			failed := []string{}
			for _, c := range res.Checks {
				if !c.Pass {
					failed = append(failed, c.Name)
				}
			}
			if len(failed) != 1 || failed[0] != tc.check {
				t.Fatalf("failing checks = %v, want only %s; checks: %+v", failed, tc.check, res.Checks)
			}
		})
	}
}

// TestValidateReleaseBumpLocalTagElsewhere covers a local tag of the version
// that does not point at HEAD. The tag check must fail; the one-step check
// also fails, because that tag is then the newest one, so only the tag check
// is asserted here.
func TestValidateReleaseBumpLocalTagElsewhere(t *testing.T) {
	fx := newValidatorFixture(t)
	validatorGit(t, fx.dir, "tag", "v1.2.4", "HEAD~1")
	res := ValidateReleaseBump(context.Background(), fx.input())
	if res.Pass {
		t.Fatal("Pass = true, want false")
	}
	for _, c := range res.Checks {
		if c.Name == "tag_not_taken" && c.Pass {
			t.Fatalf("tag_not_taken passed with a local tag off HEAD: %s", c.Detail)
		}
	}
}

// TestValidateReleaseBumpRunTwiceChangesNothing runs the validator twice on a
// passing and on a failing repository. Both runs must report the same result
// (CheckedAt aside), and neither run may change a file, a ref or the remote.
func TestValidateReleaseBumpRunTwiceChangesNothing(t *testing.T) {
	for _, mutate := range []bool{false, true} {
		fx := newValidatorFixture(t)
		if mutate {
			writeValidatorFiles(t, fx.dir, map[string]string{
				"README.md": "# App\n\nCurrent release: v1.2.3\n",
			})
		}
		before := snapshotValidatorRepo(t, fx)
		first := ValidateReleaseBump(context.Background(), fx.input())
		second := ValidateReleaseBump(context.Background(), fx.input())
		if first.CheckedAt.IsZero() || second.CheckedAt.IsZero() {
			t.Fatal("CheckedAt is zero")
		}
		// CheckedAt is the only field that differs between runs; every
		// other field must match.
		if first.Pass != second.Pass || first.Version != second.Version || len(first.Checks) != len(second.Checks) {
			t.Fatalf("runs disagree: first=%+v second=%+v", first, second)
		}
		for i := range first.Checks {
			if first.Checks[i] != second.Checks[i] {
				t.Fatalf("check %d differs between runs: %+v vs %+v", i, first.Checks[i], second.Checks[i])
			}
		}
		if after := snapshotValidatorRepo(t, fx); after != before {
			t.Fatalf("a run changed the repository (mutate=%v):\nbefore:\n%s\nafter:\n%s", mutate, before, after)
		}
	}
}

// snapshotValidatorRepo renders every file in the project (not .git), the
// project's refs and status, and the remote's refs, so a change to any of
// them shows as a difference.
func snapshotValidatorRepo(t *testing.T, fx validatorFixture) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(fx.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fx.dir, p)
		b.WriteString(rel + " " + info.Mode().String() + "\n" + string(body) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b.WriteString(validatorGit(t, fx.dir, "for-each-ref", "--format=%(refname) %(objectname)"))
	b.WriteString(validatorGit(t, fx.dir, "status", "--porcelain"))
	b.WriteString(validatorGit(t, fx.dir, "ls-remote", "origin"))
	return b.String()
}

func TestValidateReleaseBumpRefusesPathsOutsideTheRepository(t *testing.T) {
	fx := newValidatorFixture(t)
	in := fx.input()
	in.VersionFiles = []string{"../outside/main.go"}
	in.ChangelogFile = "/etc/CHANGELOG.md"
	res := ValidateReleaseBump(context.Background(), in)
	if res.Pass {
		t.Fatal("Pass = true, want false")
	}
	for _, c := range res.Checks {
		if c.Name == "version_variables_equal" && c.Pass {
			t.Fatal("version check passed for a path outside the repository")
		}
		if c.Name == "changelog_heading_matches" && c.Pass {
			t.Fatal("changelog check passed for an absolute path")
		}
	}
}

func TestValidateReleaseBumpFailsEveryCheckWithoutAProjectDirectory(t *testing.T) {
	res := ValidateReleaseBump(context.Background(), ValidateInput{})
	if res.Pass || len(res.Checks) != len(validatorCheckOrder) {
		t.Fatalf("got Pass=%v with %d checks, want a failing result with all five", res.Pass, len(res.Checks))
	}
	for _, c := range res.Checks {
		if c.Pass {
			t.Errorf("check %s passed with no project directory", c.Name)
		}
	}
}

func TestRequireOneStep(t *testing.T) {
	cases := []struct {
		last, next string
		ok         bool
	}{
		{"1.2.3", "1.2.4", true},
		{"1.2.3", "1.3.0", true},
		{"1.2.3", "2.0.0", true},
		{"1.2.3", "1.2.3", false},
		{"1.2.3", "1.2.5", false},
		{"1.2.3", "1.3.1", false},
		{"1.2.3", "1.4.0", false},
		{"1.2.3", "2.0.1", false},
		{"1.2.3", "2.1.0", false},
		{"1.2.3", "1.2.2", false},
	}
	for _, tc := range cases {
		err := requireOneStep(tc.last, tc.next)
		if (err == nil) != tc.ok {
			t.Errorf("requireOneStep(%s, %s) = %v, want ok=%v", tc.last, tc.next, err, tc.ok)
		}
	}
}

func TestNewestReleaseTagComparesNumerically(t *testing.T) {
	fx := newValidatorFixture(t)
	for _, tag := range []string{"v1.9.0", "v1.10.0", "v1.9.5", "release-notes"} {
		validatorGit(t, fx.dir, "tag", tag, "HEAD~1")
	}
	got, ok, err := newestReleaseTag(context.Background(), fx.dir, "")
	if err != nil || !ok {
		t.Fatalf("newestReleaseTag = (%q, %v, %v)", got, ok, err)
	}
	if got != "1.10.0" {
		t.Fatalf("newestReleaseTag = %q, want 1.10.0", got)
	}
}

// A local tag of the version being released, made by the run itself, is not
// the previous release: excluding it keeps the one-step check passing.
func TestValidateReleaseBumpExcludeTagSkipsOwnLocalTag(t *testing.T) {
	fx := newValidatorFixture(t)
	validatorGit(t, fx.dir, "tag", "v1.2.4")

	in := fx.input()
	if res := ValidateReleaseBump(context.Background(), in); res.Pass {
		t.Fatal("without ExcludeTag the own local tag should fail one_step_above_last_tag")
	}
	in.ExcludeTag = "v1.2.4"
	res := ValidateReleaseBump(context.Background(), in)
	if !res.Pass {
		t.Fatalf("with ExcludeTag the validator should pass: %+v", res.Checks)
	}
}
