//go:build linux

package proclimit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEngine makes a folder with a stand-in container engine that only notes
// how it was called and which engine address it was given. No real engine is
// called by these tests.
func fakeEngine(t *testing.T) (dir, notes string) {
	t.Helper()
	dir, notes = t.TempDir(), filepath.Join(t.TempDir(), "engine-calls")
	script := "#!/bin/sh\necho \"$*|$DOCKER_HOST|$CONTAINER_HOST|${DOCKER_CONTEXT-unset}\" >>'" + notes + "'\n"
	for _, name := range []string{"docker", "podman"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return dir, notes
}

// A tool that would run work outside the limit is not run when it is called by
// name, and the command reads as failed although it ignores the answer.
func TestAToolThatRunsWorkElsewhereIsRefusedByName(t *testing.T) {
	ctx, tally := needScope(t, 256<<20)
	fakes, notes := fakeEngine(t)
	for name, script := range map[string]string{
		"docker": "docker run made-up >/dev/null 2>&1 || true",
		"podman": "DOCKER_HOST= CONTAINER_HOST= sh -c 'podman run made-up' >/dev/null 2>&1; true",
		// Only asking its version: nothing could be made by it even if run.
		"systemd-run": "systemd-run --version >/dev/null 2>&1; true",
	} {
		t.Run(name, func(t *testing.T) {
			cmd := sh(ctx, script)
			cmd.Env = []string{"PATH=" + fakes + string(os.PathListSeparator) + os.Getenv("PATH")}
			err := Run(ctx, cmd)
			var over *LimitError
			if !errors.As(err, &over) || !strings.Contains(over.Why, "refused: the command asked for "+name) {
				t.Errorf("err = %v", err)
			}
		})
	}
	if got, err := os.ReadFile(notes); err == nil {
		t.Errorf("an engine was run: %s", got)
	}
	if got := strings.Join(tally.Refused(), ","); got != "docker,podman,systemd-run" {
		t.Errorf("kept: %q", got)
	}
}

// Called by its full path, an engine's tool is given an address where nothing
// listens, whatever the daemon's own environment says.
func TestAnEngineCalledByItsPathGetsNoAddressThatAnswers(t *testing.T) {
	ctx, tally := needScope(t, 256<<20)
	fakes, notes := fakeEngine(t)
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	t.Setenv("DOCKER_CONTEXT", "default")
	cmd := sh(ctx, `"$1" info`, filepath.Join(fakes, "docker"))
	if err := Run(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(notes)
	if err != nil {
		t.Fatal(err)
	}
	if want := "info|" + noEngine + "|" + noEngine + "|unset\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if len(tally.Refused()) != 0 {
		t.Errorf("kept: %v", tally.Refused())
	}
}

// Looking a tool up is not asking for it, a tool that is not there stays so,
// and the stand-ins go when the command ends.
func TestRefusingChangesNothingElse(t *testing.T) {
	ctx, tally := needScope(t, 256<<20)
	fakes, _ := fakeEngine(t)
	cmd := sh(ctx, `command -v docker; command -v nerdctl || echo "no nerdctl"`)
	// Only the stand-in engine and the shell's own folder are searched.
	only := t.TempDir()
	if err := os.Symlink("/bin/sh", filepath.Join(only, "sh")); err != nil {
		t.Fatal(err)
	}
	cmd.Env = []string{"PATH=" + fakes + string(os.PathListSeparator) + only}
	out, err := CombinedOutput(ctx, cmd)
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	lines := strings.Fields(string(out))
	if len(lines) != 3 || !strings.Contains(lines[0], "dw-refuse-") || lines[1]+" "+lines[2] != "no nerdctl" {
		t.Fatalf("output %q", out)
	}
	if _, err := os.Stat(filepath.Dir(lines[0])); !os.IsNotExist(err) {
		t.Errorf("the stand-ins are still there: %v", err)
	}
	if len(tally.Refused()) != 0 {
		t.Errorf("kept: %v", tally.Refused())
	}
}

func TestTheScopeOfACommandsLimitIsKnownByItsControlGroup(t *testing.T) {
	for data, want := range map[string]string{
		"0::/user.slice/user-1.slice/app.slice/" + UnitPrefix + "ab.scope\n":     UnitPrefix + "ab.scope",
		"0::/system.slice/" + UnitPrefix + "cd.scope/below\n":                    UnitPrefix + "cd.scope",
		"0::/user.slice/app.slice/run-123.scope\n":                               "",
		"0::/user.slice/app.slice/datawatch.service\n":                           "",
		"0::/system.slice/" + UnitPrefix + "not-a-scope.service\n":               "",
		"1:memory:/" + UnitPrefix + "ef.scope\n0::/user.slice/session-1.scope\n": "",
		"": "",
	} {
		if got := limitScopeOf(data); got != want {
			t.Errorf("%q: %q, want %q", data, got, want)
		}
	}
}
