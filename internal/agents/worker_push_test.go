// A container worker's push obeys the parent's push settings, never moves a
// default or base branch, and goes only through the outgoing check. Every
// remote is a local bare repository (or an unused loopback port).

package agents

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
)

func wgit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=w", "-c", "user.email=w@example.com"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func wcommit(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	wgit(t, dir, "add", "-A")
	wgit(t, dir, "commit", "-q", "-m", "add "+name)
}

// workerRepo is a bare remote whose default branch is def, plus the worker's
// clone of it checked out on branch. makefile, when set, is on the default
// branch.
func workerRepo(t *testing.T, def, branch, makefile string) (bare, clone string) {
	t.Helper()
	root := t.TempDir()
	bare, clone = filepath.Join(root, "remote.git"), filepath.Join(root, "clone")
	seed := filepath.Join(root, "seed")
	wgit(t, root, "init", "-q", "--bare", "-b", def, bare)
	wgit(t, root, "init", "-q", "-b", def, seed)
	wcommit(t, seed, "README.md", "seed\n")
	if makefile != "" {
		wcommit(t, seed, "Makefile", makefile)
	}
	wgit(t, seed, "push", "-q", bare, def)
	wgit(t, root, "clone", "-q", bare, clone)
	wgit(t, clone, "config", "user.name", "w")
	wgit(t, clone, "config", "user.email", "w@example.com")
	if branch != def {
		wgit(t, clone, "checkout", "-q", "-b", branch)
	}
	wcommit(t, clone, "work.txt", "task work\n")
	return bare, clone
}

func workerResp(url, branch, base string) *BootstrapResponse {
	return &BootstrapResponse{Git: BootstrapGit{
		URL: url, Branch: branch, CreateBranch: true, BaseBranch: base,
		Push: &BootstrapPush{Policy: "branch", SecretScanMode: "enforce"},
	}}
}

func bareRefList(t *testing.T, bare string) string {
	t.Helper()
	return wgit(t, bare, "for-each-ref", "--format=%(refname) %(objectname)")
}

func TestWorkerPush_CleanWorkBranchIsPushed(t *testing.T) {
	bare, clone := workerRepo(t, "main", "automaton/x", "")
	branch, sha, err := PushOnCompletion(context.Background(), clone, workerResp(bare, "automaton/x", "develop"))
	if err != nil || branch != "automaton/x" {
		t.Fatalf("branch=%q err=%v", branch, err)
	}
	if got := wgit(t, bare, "rev-parse", "refs/heads/automaton/x"); got != sha {
		t.Errorf("remote %s, reported %s", got, sha)
	}
	if got := wgit(t, clone, "remote", "get-url", "origin"); got != bare {
		t.Errorf("origin not restored: %s", got)
	}
}

// refusedWorkerPush asserts nothing reached the remote and the reason says why.
func refusedWorkerPush(t *testing.T, bare, clone string, resp *BootstrapResponse, why string) error {
	t.Helper()
	before := bareRefList(t, bare)
	branch, sha, err := PushOnCompletion(context.Background(), clone, resp)
	if got := bareRefList(t, bare); got != before {
		t.Errorf("remote changed:\nbefore %s\nafter  %s", before, got)
	}
	if err == nil || branch != "" || sha != "" {
		t.Fatalf("want a refusal and no reported branch, got branch=%q sha=%q err=%v", branch, sha, err)
	}
	if !strings.Contains(err.Error(), why) {
		t.Errorf("reason %q does not say %q", err, why)
	}
	return err
}

func TestWorkerPush_ObeysTheParentPushSettings(t *testing.T) {
	for name, tc := range map[string]struct {
		push *BootstrapPush
		why  string
	}{
		"no settings sent": {nil, "no push settings"},
		"never":            {&BootstrapPush{Policy: "never", SecretScanMode: "enforce"}, "never"},
		"default_branch":   {&BootstrapPush{Policy: "default_branch", SecretScanMode: "enforce"}, "default branch"},
		"unknown policy":   {&BootstrapPush{Policy: "everything", SecretScanMode: "enforce"}, "unknown push policy"},
		"empty policy":     {&BootstrapPush{SecretScanMode: "enforce"}, "no push settings"},
		"no scan setting":  {&BootstrapPush{Policy: "branch"}, "secret scan setting"},
	} {
		t.Run(name, func(t *testing.T) {
			bare, clone := workerRepo(t, "main", "automaton/x", "")
			resp := workerResp(bare, "automaton/x", "develop")
			resp.Git.Push = tc.push
			_ = refusedWorkerPush(t, bare, clone, resp, tc.why)
		})
	}
}

func TestWorkerPush_NeverMovesADefaultOrBaseBranch(t *testing.T) {
	t.Run("main", func(t *testing.T) {
		bare, clone := workerRepo(t, "main", "main", "")
		resp := workerResp(bare, "", "")
		resp.Git.CreateBranch = false
		_ = refusedWorkerPush(t, bare, clone, resp, "default branch")
	})
	t.Run("a default branch with another name", func(t *testing.T) {
		bare, clone := workerRepo(t, "trunk", "trunk", "")
		resp := workerResp(bare, "", "")
		resp.Git.CreateBranch = false
		_ = refusedWorkerPush(t, bare, clone, resp, "default branch")
	})
	t.Run("the profile base branch", func(t *testing.T) {
		bare, clone := workerRepo(t, "main", "develop", "")
		resp := workerResp(bare, "develop", "develop")
		resp.Git.CreateBranch = false
		_ = refusedWorkerPush(t, bare, clone, resp, "base branch")
	})
	t.Run("the task switched to the base branch", func(t *testing.T) {
		bare, clone := workerRepo(t, "main", "develop", "")
		_ = refusedWorkerPush(t, bare, clone, workerResp(bare, "automaton/x", "develop"), "base branch")
	})
	t.Run("an existing branch named by an older parent", func(t *testing.T) {
		bare, clone := workerRepo(t, "main", "develop", "")
		resp := workerResp(bare, "develop", "")
		resp.Git.CreateBranch = false
		_ = refusedWorkerPush(t, bare, clone, resp, "base branch")
	})
	t.Run("detached head", func(t *testing.T) {
		bare, clone := workerRepo(t, "main", "automaton/x", "")
		wgit(t, clone, "checkout", "-q", "--detach")
		_ = refusedWorkerPush(t, bare, clone, workerResp(bare, "automaton/x", ""), "detached")
	})
}

func TestWorkerPush_RunsTheOutgoingCheck(t *testing.T) {
	t.Run("planted secret", func(t *testing.T) {
		bare, clone := workerRepo(t, "main", "automaton/x", "")
		material := "gh" + "p_" + strings.Repeat("aB3d", 7)
		wcommit(t, clone, "env", "GITHUB_TOKEN=\""+material+"\"\n")
		err := refusedWorkerPush(t, bare, clone, workerResp(bare, "automaton/x", ""), "secret_found")
		if strings.Contains(err.Error(), material) {
			t.Error("the reason holds the secret")
		}
	})
	// A file that is never committed (a private pattern list) is not in a
	// container; one the task wrote into its own clone does not count.
	t.Run("project check that needs an uncommitted file", func(t *testing.T) {
		bare, clone := workerRepo(t, "main", "automaton/x",
			"check-outgoing:\n\t@test -f \"$$OUTGOING_PROJECT_DIR/.private\" || { echo the private list is not here; exit 1; }\n")
		if err := os.WriteFile(filepath.Join(clone, ".private"), []byte("made up by the task\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := refusedWorkerPush(t, bare, clone, workerResp(bare, "automaton/x", ""), "the private list is not here")
		if !strings.Contains(err.Error(), "container worker") {
			t.Errorf("reason does not say why a container cannot run it: %v", err)
		}
	})
	t.Run("self-contained project check runs and passes", func(t *testing.T) {
		bare, clone := workerRepo(t, "main", "automaton/x", "check-outgoing:\n\t@test -s \"$$OUTGOING_MANIFEST_DIR/added.txt\"\n")
		if _, _, err := PushOnCompletion(context.Background(), clone, workerResp(bare, "automaton/x", "")); err != nil {
			t.Fatalf("push: %v", err)
		}
		if bareRefList(t, bare) == "" {
			t.Fatal("nothing pushed")
		}
	})
}

// The token never reaches the reason, and origin is put back.
func TestWorkerPush_TokenStaysOutOfTheReason(t *testing.T) {
	_, clone := workerRepo(t, "main", "automaton/x", "")
	const url = "http://127.0.0.1:1/x.git"
	wgit(t, clone, "remote", "set-url", "origin", url)
	resp := workerResp(url, "automaton/x", "")
	resp.Git.Token = "tok-" + "abcdef123456"
	_, _, err := PushOnCompletion(context.Background(), clone, resp)
	if err == nil || strings.Contains(err.Error(), resp.Git.Token) {
		t.Fatalf("err=%v", err)
	}
	if got := wgit(t, clone, "remote", "get-url", "origin"); got != url {
		t.Errorf("origin left as %q", strings.ReplaceAll(got, resp.Git.Token, "<token>"))
	}
}

// The docker teardown step cannot run the outgoing check from outside the
// worker, so it pushes nothing and says what stays behind.
func TestDockerTeardown_DoesNotPushUnchecked(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pushed  bool // the work already reached the remote
		wantAll bool // report says nothing is left behind
	}{{"unpushed commits", false, false}, {"nothing unpushed", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			bare, clone := workerRepo(t, "main", "automaton/x", "")
			if tc.pushed {
				wgit(t, clone, "push", "-q", "origin", "automaton/x")
			}
			before := bareRefList(t, bare)
			bin := filepath.Join(t.TempDir(), "docker")
			if err := os.WriteFile(bin, []byte(fakeDockerScript), 0o755); err != nil {
				t.Fatal(err)
			}
			logFile := filepath.Join(t.TempDir(), "calls.log")
			t.Setenv("FAKE_DOCKER_CWD", clone)
			t.Setenv("FAKE_DOCKER_LOG", logFile)
			d := NewDockerDriver(bin, "", "v0", "http://127.0.0.1:9")

			rep, err := d.PushUnpushed(context.Background(), &Agent{ID: "a", DriverInstance: "cid", project: &profile.ProjectProfile{Git: profile.GitSpec{URL: bare}}})

			if err != nil {
				t.Fatal(err)
			}
			if got := bareRefList(t, bare); got != before {
				t.Errorf("teardown pushed:\nbefore %s\nafter  %s", before, got)
			}
			if rep.Pushed != tc.wantAll || rep.Branch != "automaton/x" {
				t.Errorf("report %+v", rep)
			}
			if !tc.wantAll && (rep.Count != 1 || !strings.Contains(rep.Reason, "outgoing check")) {
				t.Errorf("report does not say what stays behind and why: %+v", rep)
			}
			calls, _ := os.ReadFile(logFile)
			if strings.Contains(string(calls), " push ") {
				t.Errorf("the in-container script still pushes:\n%s", calls)
			}
		})
	}
}

// A push address the task added to its clone does not take the work elsewhere.
func TestWorkerPush_IgnoresAPushAddressTheTaskAdded(t *testing.T) {
	bare, clone := workerRepo(t, "main", "automaton/x", "")
	other := filepath.Join(t.TempDir(), "other.git")
	wgit(t, clone, "init", "-q", "--bare", other)
	wgit(t, clone, "remote", "set-url", "--push", "origin", other)

	if _, _, err := PushOnCompletion(context.Background(), clone, workerResp(bare, "automaton/x", "")); err != nil {
		t.Fatal(err)
	}
	if got := bareRefList(t, other); got != "" {
		t.Errorf("work went to the added address: %s", got)
	}
	if !strings.Contains(bareRefList(t, bare), "automaton/x") {
		t.Error("work did not reach the bootstrap repository")
	}
}
