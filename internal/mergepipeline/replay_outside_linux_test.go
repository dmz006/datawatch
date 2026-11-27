//go:build linux

package mergepipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/mergeshadow"
)

// A check that asks a container engine for work does not read as matched, even
// when it ignores what it is told. The engine here is a stand-in that only
// notes that it ran; no real one is called.
func TestShadowCheckThatAsksForAContainerDoesNotReadMatched(t *testing.T) {
	limit := needTreeLimit(t)
	g := newReplayRig(t)
	ran := g.note("engine-ran")
	fakes := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakes, "docker"), []byte("#!/bin/sh\necho \"$*\" >>'"+ran+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakes+string(os.PathListSeparator)+os.Getenv("PATH"))
	g.addCheck("image", "docker build -t made-up . >/dev/null 2>&1 || true")
	r := g.shadow(g.handMerge(handOptions{}), func(in *ReplayInput) { in.Limit = limit })
	r.HandChecks = map[string]string{"lint": "pass", "go-test": "pass", "image": "pass"}
	wantMiss(t, r, mergeshadow.RowChecks, "docker")
	wantMiss(t, r, mergeshadow.RowSideEffects, "docker")
	if got, err := os.ReadFile(ran); err == nil {
		t.Errorf("the container engine was run: %s", got)
	}
}

// A check that asks systemd for a unit of its own does not read as matched
// either. What it asks for here is capped and does nothing.
func TestShadowCheckThatAsksForAScopeDoesNotReadMatched(t *testing.T) {
	limit := needTreeLimit(t)
	g := newReplayRig(t)
	mark := g.note("outside-ran")
	g.addCheck("own-scope", `XDG_RUNTIME_DIR=/run/user/$(id -u) systemd-run --user --scope --quiet --collect `+
		`-p MemoryMax=64M -p MemorySwapMax=0 -p RuntimeMaxSec=20 -- /usr/bin/touch '`+mark+`' >/dev/null 2>&1; true`)
	r := g.shadow(g.handMerge(handOptions{}), func(in *ReplayInput) { in.Limit = limit })
	r.HandChecks = map[string]string{"lint": "pass", "go-test": "pass", "own-scope": "pass"}
	wantMiss(t, r, mergeshadow.RowChecks, "systemd-run")
	wantMiss(t, r, mergeshadow.RowSideEffects, "systemd-run")
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Errorf("work ran in a unit outside the limit: %v", err)
	}
	if !strings.Contains(r.CommandLimit, "refused") {
		t.Errorf("the record does not say what is refused: %q", r.CommandLimit)
	}
}
