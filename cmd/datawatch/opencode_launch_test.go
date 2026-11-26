package main

// An opencode.json datawatch cannot safely read is never rewritten, and by
// default the launch fails and says why.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

const commentedOpencodeConfig = "{\n  // mine\n  \"model\": \"theirs/m\"\n}\n"

func opencodeProject(t *testing.T, body string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func opencodeSession(family, dir string) *session.Session {
	return &session.Session{ID: "oc01", FullID: "h-oc01", BackendFamily: family, ProjectDir: dir, Model: "ollama/chosen"}
}

func TestOpencodeLaunchFailsOnAConfigItCannotRead(t *testing.T) {
	for _, family := range []string{"opencode", "opencode-acp"} {
		dir, path := opencodeProject(t, commentedOpencodeConfig)
		err := opencodeLaunchCheck(&config.Config{}, true)(opencodeSession(family, dir))
		if err == nil {
			t.Fatalf("%s: the launch was allowed", family)
		}
		for _, want := range []string{"opencode.json", "model nobody chose", "left as it is", path} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: %q does not say %q", family, err, want)
			}
		}
		if got, _ := os.ReadFile(path); string(got) != commentedOpencodeConfig {
			t.Errorf("%s: rewritten: %s", family, got)
		}
	}
}

func TestOpencodeLaunchCanBeSetToWarnAndGoOn(t *testing.T) {
	dir, path := opencodeProject(t, commentedOpencodeConfig)
	var err error
	out := captureStdout(t, func() {
		err = opencodeLaunchCheck(&config.Config{}, false)(opencodeSession("opencode", dir))
	})
	if err != nil {
		t.Errorf("err = %v, want the launch to go on", err)
	}
	for _, want := range []string{"[warn]", "oc01", "left as it is"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log %q does not say %q", out, want)
		}
	}
	if got, _ := os.ReadFile(path); string(got) != commentedOpencodeConfig {
		t.Errorf("rewritten: %s", got)
	}
}

func TestOpencodeLaunchWritesAReadableConfig(t *testing.T) {
	dir, path := opencodeProject(t, `{"theme":"mine"}`)
	if err := opencodeLaunchCheck(&config.Config{}, true)(opencodeSession("opencode", dir)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	for _, want := range []string{"ollama/chosen", `"mine"`, "external_directory"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("%q missing: %s", want, got)
		}
	}
}

// Other backends are not stopped by a file they do not use.
func TestOpencodeLaunchCheckLeavesOtherBackendsAlone(t *testing.T) {
	for _, family := range []string{"claude-code", "opencode-prompt", "shell", ""} {
		dir, path := opencodeProject(t, commentedOpencodeConfig)
		if err := opencodeLaunchCheck(&config.Config{}, true)(opencodeSession(family, dir)); err != nil {
			t.Errorf("%q: %v", family, err)
		}
		if got, _ := os.ReadFile(path); string(got) != commentedOpencodeConfig {
			t.Errorf("%q: rewritten", family)
		}
	}
}

// The daemon sets the check, and the hook installer that never ran on main is
// still replaced by the later start callback.
func TestDaemonWiresTheLaunchCheck(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "mgr.SetLaunchCheck(opencodeLaunchCheck(cfg, opencodeConfigRefusalFailsLaunch))") {
		t.Error("the daemon does not set the opencode launch check")
	}
}
