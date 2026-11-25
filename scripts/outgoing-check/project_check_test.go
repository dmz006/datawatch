// This repository's check-outgoing target, run the way the daemon runs it:
// from the base commit of a sandbox project whose remote is a local bare
// repository. Every planted value is assembled at runtime, so no leak-shaped
// text is committed here.

package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/git"
)

// privateWord is the sandbox's private pattern. It must never appear in any
// refusal or log line.
var privateWord = "zebra" + "planet-" + "internal"

var (
	plantAddress = strings.Join([]string{"192", "168", "7", "9"}, ".")
	plantHome    = "/ho" + "me/" + "somebody/work"
	plantEmail   = "some.person" + "@" + "a-real-domain.io"
)

type sandbox struct {
	t          *testing.T
	bare, work string
}

func (s sandbox) git(args ...string) string {
	s.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = s.work
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (s sandbox) write(name, body string) {
	s.t.Helper()
	p := filepath.Join(s.work, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

func (s sandbox) commit(name, body, message string) {
	s.t.Helper()
	s.write(name, body)
	s.git("add", "-A")
	s.git("commit", "-q", "-m", message)
}

// newSandbox makes a project that uses this repository's real check scripts,
// with them on the remote's main and a private list in the working tree.
func newSandbox(t *testing.T) sandbox {
	t.Helper()
	root := t.TempDir()
	s := sandbox{t: t, bare: filepath.Join(root, "remote.git"), work: filepath.Join(root, "work")}
	if err := os.MkdirAll(s.work, 0o755); err != nil {
		t.Fatal(err)
	}
	s.git("init", "-q", "--bare", "-b", "main", s.bare)
	s.git("init", "-q", "-b", "main", ".")
	s.git("config", "user.name", "Tester")
	s.git("config", "user.email", "tester@example.com")
	s.git("remote", "add", "origin", s.bare)
	for _, f := range []string{"check-outgoing.sh", "check-no-local-leaks.sh", "leak-check.patterns",
		"leak-check.allowed-emails", "leak-check.allowed-addresses", "leak-check.exclude"} {
		body, err := os.ReadFile(filepath.Join("..", f))
		if err != nil {
			t.Fatalf("read scripts/%s: %v", f, err)
		}
		s.write("scripts/"+f, string(body))
	}
	s.write(".gitignore", ".leak-patterns.local\n")
	s.commit("Makefile", "check-outgoing:\n\t@bash scripts/check-outgoing.sh\n", "project setup")
	s.git("push", "-q", "origin", "main")
	s.git("checkout", "-q", "-b", "work")
	s.write(".leak-patterns.local", "# private\n"+privateWord+"\n")
	return s
}

func (s sandbox) push() (git.PushResult, string) {
	s.t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	res := git.PushWorkBranch(context.Background(), s.work, "origin", "work", git.SecretScanPolicy{Mode: git.ScanEnforce})
	return res, buf.String()
}

func (s sandbox) onRemote() string {
	s.t.Helper()
	return s.git("ls-remote", "--heads", s.bare, "work")
}

// refused asserts the push was refused by the project check naming want, that
// the remote has nothing, and that none of hidden was printed.
func (s sandbox) refused(want string, hidden ...string) git.PushResult {
	s.t.Helper()
	res, logged := s.push()
	if res.Kind != git.PushFailCheck {
		s.t.Fatalf("want a check refusal, got %+v", res)
	}
	if got := s.onRemote(); got != "" {
		s.t.Fatalf("the branch reached the remote: %s", got)
	}
	if !strings.Contains(res.Reason, want) {
		s.t.Errorf("reason lacks %q:\n%s", want, res.Reason)
	}
	for _, h := range append(hidden, privateWord) {
		if strings.Contains(strings.ToLower(res.Reason+logged), strings.ToLower(h)) {
			s.t.Errorf("a matched value was printed:\n%s\n%s", res.Reason, logged)
		}
	}
	return res
}

func TestOutgoingCheck_CleanWorkPushes(t *testing.T) {
	s := newSandbox(t)
	s.commit("a.txt", "clean\n", "clean work\n\nCo-Authored-By: A Model <noreply@anthropic.com>")
	res, _ := s.push()
	if !res.Pushed || s.onRemote() == "" {
		t.Fatalf("clean work must push: %+v", res)
	}
}

func TestOutgoingCheck_EveryLeakClass(t *testing.T) {
	for name, tc := range map[string]struct {
		value, want string
	}{
		"private word": {privateWord, "local"},
		"address":      {"host " + plantAddress + " ", "address"},
		"home path":    {"see " + plantHome + " ", "home"},
		"email":        {"ask " + plantEmail + " ", "email"},
	} {
		t.Run(name+" in a file", func(t *testing.T) {
			s := newSandbox(t)
			s.commit("notes.txt", "first line\n"+tc.value+"\n", "work")
			s.refused("notes.txt:2: "+tc.want, strings.TrimSpace(tc.value))
		})
		t.Run(name+" in a commit message", func(t *testing.T) {
			s := newSandbox(t)
			s.commit("a.txt", "clean\n", "work\n\n"+tc.value)
			s.refused(": "+tc.want, strings.TrimSpace(tc.value))
		})
	}
}

func TestOutgoingCheck_TagMessage(t *testing.T) {
	s := newSandbox(t)
	s.git("tag", "-a", "v1.0.0", "-m", "built for "+privateWord)
	err := git.PushTag(context.Background(), s.work, "origin", "v1.0.0")
	if err == nil || strings.Contains(err.Error(), privateWord) {
		t.Fatalf("want a refusal that does not print the word: %v", err)
	}
	if got := s.git("ls-remote", "--tags", s.bare); got != "" {
		t.Fatalf("tag pushed: %s", got)
	}
}

func TestOutgoingCheck_MergeResolutionLine(t *testing.T) {
	s := newSandbox(t)
	s.commit("c.txt", "work side\n", "work edit")
	s.git("checkout", "-q", "main")
	s.commit("c.txt", "main side\n", "main edit")
	s.git("push", "-q", "origin", "main")
	s.git("checkout", "-q", "work")
	cmd := exec.Command("git", "merge", "-q", "main")
	cmd.Dir = s.work
	if cmd.Run() == nil {
		t.Fatal("fixture: expected a conflict")
	}
	s.commit("c.txt", "resolved for "+plantAddress+" \n", "merge main")
	s.refused("c.txt:1: address", plantAddress)
}

// A private word in a file or branch name is reported without the name.
func TestOutgoingCheck_NamesAreNotPrinted(t *testing.T) {
	t.Run("new file name", func(t *testing.T) {
		s := newSandbox(t)
		s.commit("docs/"+privateWord+".md", "see "+plantAddress+" \n", "work")
		s.refused("local")
	})
	t.Run("commit that only adds the name", func(t *testing.T) {
		s := newSandbox(t)
		s.commit(privateWord+".txt", "", "work")
		s.refused("file name")
	})
}

func TestOutgoingCheck_AuthorAndCommitter(t *testing.T) {
	for name, identity := range map[string]string{
		"unlisted address":           plantEmail,
		"machine default, no domain": "somebody" + "@" + "somebox",
		"empty":                      "",
	} {
		for _, role := range []string{"AUTHOR", "COMMITTER"} {
			t.Run(name+" "+role, func(t *testing.T) {
				s := newSandbox(t)
				s.write("a.txt", "clean\n")
				s.git("add", "-A")
				t.Setenv("GIT_"+role+"_EMAIL", identity)
				s.git("commit", "-q", "-m", "work")
				var hidden []string
				if identity != "" {
					hidden = append(hidden, identity)
				}
				s.refused(strings.ToLower(role)+": address not on the allowed list", hidden...)
			})
		}
	}
}

func TestOutgoingCheck_PrivateListIsRequired(t *testing.T) {
	for name, prepare := range map[string]func(s sandbox){
		"missing": func(s sandbox) {
			if err := os.Remove(filepath.Join(s.work, ".leak-patterns.local")); err != nil {
				s.t.Fatal(err)
			}
		},
		"only comments": func(s sandbox) { s.write(".leak-patterns.local", "# nothing\n\n") },
	} {
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			s.commit("a.txt", "clean\n", "work")
			prepare(s)
			s.refused("private pattern list")
		})
	}
}

func TestOutgoingCheck_BranchCannotWeakenTheScript(t *testing.T) {
	s := newSandbox(t)
	s.commit("scripts/check-outgoing.sh", "#!/usr/bin/env bash\nexit 0\n", "relax the check")
	s.commit("a.txt", "host "+plantAddress+" \n", "work")
	s.refused("a.txt:1: address", plantAddress)
}

// Bytes that are not valid UTF-8 earlier in the same push must not hide a
// later line, and a binary file is still held to the private list.
func TestOutgoingCheck_OddBytesHideNothing(t *testing.T) {
	t.Run("latin-1 before the leak", func(t *testing.T) {
		s := newSandbox(t)
		s.commit("a.txt", "caf\xe9\n", "latin-1")
		s.commit("b.txt", "host "+plantAddress+" \n", "work")
		s.refused("b.txt:1: address", plantAddress)
	})
	t.Run("control byte in another commit message", func(t *testing.T) {
		s := newSandbox(t)
		s.commit("a.txt", "clean\n", "odd \x01 message")
		s.commit("b.txt", "clean too\n", "work\n\n"+privateWord)
		s.refused("local")
	})
	t.Run("private word in a binary file", func(t *testing.T) {
		s := newSandbox(t)
		s.commit("blob.bin", "\x00\x01"+privateWord+"\x00\n", "binary")
		s.refused("local")
	})
}
