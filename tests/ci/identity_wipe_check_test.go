package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// identityWipeTree is a throwaway tree holding a copy of
// scripts/check-no-identity-wipe.sh and the shell fixtures it scans, built
// under a t.TempDir() directory. The checker only reads files, and nothing
// here touches a real repository checkout, a daemon or the data directory.
type identityWipeTree struct {
	t    *testing.T
	root string
}

// newIdentityWipeTree copies the real checker (resolved relative to this test
// file) into <tmp>/scripts/ at mode 0755 and adds one clean fixture script, so
// the checker runs against a tree it can pass.
func newIdentityWipeTree(t *testing.T) *identityWipeTree {
	t.Helper()

	root := t.TempDir()

	src, err := filepath.Abs(filepath.Join("..", "..", "scripts", "check-no-identity-wipe.sh"))
	if err != nil {
		t.Fatalf("resolving check script: %v", err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading check script %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir for scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "check-no-identity-wipe.sh"), data, 0o755); err != nil {
		t.Fatalf("copying script into temp tree: %v", err)
	}

	x := &identityWipeTree{t: t, root: root}
	x.writeFile("scripts/clean.sh", "#!/usr/bin/env bash\ncurl -s \"$BASE/api/identity\"\n")
	return x
}

// writeFile writes content to path relative to the temp tree root, creating
// any missing parent directories.
func (x *identityWipeTree) writeFile(path, content string) {
	x.t.Helper()
	full := filepath.Join(x.root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		x.t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		x.t.Fatalf("writing %s: %v", path, err)
	}
}

// run executes the copied checker under bash with CHECK_ROOT pointing at the
// temp tree and TMUX and TMUX_TMPDIR removed from the environment.
func (x *identityWipeTree) run() (string, error) {
	x.t.Helper()
	cmd := exec.Command("bash", filepath.Join(x.root, "scripts", "check-no-identity-wipe.sh"))
	cmd.Env = append(withoutTmuxEnv(os.Environ()), "CHECK_ROOT="+x.root)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestIdentityWipeCheckPassesOnSafeWrites covers scripts that touch the
// identity route without wiping it: a read, a PATCH with content, a PUT of a
// saved body held in a variable, the reset route, a marked fixture line and a
// file outside the shell scripts the checker reads. The checker must exit 0.
func TestIdentityWipeCheckPassesOnSafeWrites(t *testing.T) {
	x := newIdentityWipeTree(t)
	x.writeFile("scripts/safe.sh", "#!/usr/bin/env bash\n"+
		"curl -s \"$BASE/api/identity\"\n"+
		"curl -X PATCH -H 'Content-Type: application/json' -d '{\"role\":\"x\"}' \"$BASE/api/identity\"\n"+
		"curl -X PUT -H 'Content-Type: application/json' --data-binary \"$SAVED\" \"$BASE/api/identity\"\n"+
		"curl -X POST \"$BASE/api/identity/reset\"\n"+
		"curl -X POST -d '{}' \"$BASE/api/identity/reset\"\n"+
		"curl -X PUT -d '{\"role\":\"\",\"values\":[]}' \"$BASE/api/identity\" # identity-wipe-ok: refusal assertion\n"+
		"# curl -X PUT -d '{}' \"$BASE/api/identity\"\n"+
		"curl -X PUT -d '{}' \"$BASE/api/other\"\n"+
		"curl -X DELETE -d '{}' \"$BASE/api/identity\"\n")
	x.writeFile("scripts/notes.txt", "curl -X PUT -d '{}' \"$BASE/api/identity\"\n")

	out, err := x.run()
	if err != nil {
		t.Fatalf("expected safe writes to pass, got error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OK: no empty writes to /api/identity") {
		t.Fatalf("expected the OK line in output, got:\n%s", out)
	}
}

// TestIdentityWipeCheckFailsOnEmptyWrite covers each shape of empty write the
// checker must catch, in each directory it scans. It must exit 1 and name the
// offending file.
func TestIdentityWipeCheckFailsOnEmptyWrite(t *testing.T) {
	cases := []struct {
		name      string
		path      string
		content   string
		wantNamed string
	}{
		{
			name:      "PUT empty object",
			path:      "scripts/bad.sh",
			content:   "#!/usr/bin/env bash\ncurl -X PUT -H 'Content-Type: application/json' -d '{}' \"$BASE/api/identity\"\n",
			wantNamed: "scripts/bad.sh",
		},
		{
			name:      "PATCH empty object in double quotes",
			path:      "tests/bad.sh",
			content:   "curl -s -X PATCH -d \"{}\" \"$BASE/api/identity\"\n",
			wantNamed: "tests/bad.sh",
		},
		{
			name:      "POST with data and no method",
			path:      "testdata/bad.sh",
			content:   "curl -s -d '{}' \"$BASE/api/identity\"\n",
			wantNamed: "testdata/bad.sh",
		},
		{
			name:      "empty string body",
			path:      ".datawatch/verify/bad.sh",
			content:   "curl -s -X PUT --data '' \"$BASE/api/identity\"\n",
			wantNamed: ".datawatch/verify/bad.sh",
		},
		{
			name:      "object whose every value is empty",
			path:      "scripts/bad.sh",
			content:   "curl -s -X PUT -d '{\"role\":\"\",\"north_star_goals\":[],\"values\":null}' \"$BASE/api/identity\"\n",
			wantNamed: "scripts/bad.sh",
		},
		{
			name:      "line continuation",
			path:      "scripts/bad.sh",
			content:   "curl -s -X PUT \\\n  -H 'Content-Type: application/json' \\\n  -d '{}' \\\n  \"$BASE/api/identity\"\n",
			wantNamed: "scripts/bad.sh",
		},
		{
			name:      "shebang file without an extension",
			path:      "scripts/badscript",
			content:   "#!/bin/bash\ncurl -s -X PUT -d '{}' \"$BASE/api/identity\"\n",
			wantNamed: "scripts/badscript",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := newIdentityWipeTree(t)
			x.writeFile(tc.path, tc.content)

			out, err := x.run()
			exit, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("expected the check to fail with exit code 1, got error: %v\n%s", err, out)
			}
			if code := exit.ExitCode(); code != 1 {
				t.Fatalf("expected exit code 1, got %d:\n%s", code, out)
			}
			if !strings.Contains(out, "  ✗ "+tc.wantNamed) {
				t.Fatalf("expected a violation line naming %s, got:\n%s", tc.wantNamed, out)
			}
		})
	}
}
