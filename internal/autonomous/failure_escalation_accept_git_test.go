package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

// A branch name that starts with '-' is never handed to git log, where it
// would be read as an option even with the closing "--": --output=<file>
// would write that file. Both stored branch names reach the commit search.
func TestAcceptCheck_BranchNameStartingWithDashNotSearched(t *testing.T) {
	for name, set := range map[string]func(c *acceptCase, t *testing.T, ref string){
		"task branch": func(c *acceptCase, t *testing.T, ref string) {
			c.edit(t, func(tk *Task) { tk.WorktreeBranch = ref })
		},
		"automaton branch": func(c *acceptCase, _ *testing.T, ref string) {
			c.m.store.WithLock(func() { c.prd.Git.Branch = ref })
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := newAcceptCase(t)
			out := filepath.Join(t.TempDir(), "written-by-git-log")
			set(c, t, "--output="+out)
			c.wantRefused(t, "no task commit")
			if _, err := os.Lstat(out); !os.IsNotExist(err) {
				t.Fatalf("git read the branch name as an option and wrote %s (stat err %v)", out, err)
			}
		})
	}
}

// With log.showSignature set in the repository, the commit search and the
// outside-files read must not run the configured signature program.
func TestAcceptCheck_SignatureProgramNotRun(t *testing.T) {
	c := newAcceptCase(t)
	tools := t.TempDir()
	ran := filepath.Join(tools, "verified")
	gpg := filepath.Join(tools, "fake-gpg")
	// Signs with a fixed block; any other call is a verification and is recorded.
	script := "#!/bin/sh\ncase \"$*\" in\n" +
		"*-bsau*) cat >/dev/null; echo '[GNUPG:] SIG_CREATED ' >&2\n" +
		"  printf -- '-----BEGIN PGP SIGNATURE-----\\n\\nfake\\n-----END PGP SIGNATURE-----\\n' ;;\n" +
		"*) : > '" + ran + "' ;;\nesac\n"
	if err := os.WriteFile(gpg, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	snapTestGit(t, c.repo, "config", "gpg.program", gpg)
	c.edit(t, func(tk *Task) { tk.FilesPlanned = []string{"pkg/a.go"} })
	c.write(t, "pkg/a.go", "a\n")
	snapTestGit(t, c.repo, "add", "-f", "pkg/a.go")
	snapTestGit(t, c.repo, "commit", "-q", "-S", "-m", c.taskSubject())
	snapTestGit(t, c.repo, "config", "log.showSignature", "true")

	// The set-up must be able to run the program, or this test proves nothing.
	snapTestGit(t, c.repo, "log", "-n", "1", "--format=%H")
	if err := os.Remove(ran); err != nil {
		t.Fatalf("plain git log did not run the signature program: %v", err)
	}

	c.wantAccepted(t)
	if _, err := os.Lstat(ran); !os.IsNotExist(err) {
		t.Fatalf("accept ran the repository's signature program (stat err %v)", err)
	}
}
