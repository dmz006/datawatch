//go:build unix

package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/mergeshadow"
)

// A shadow whose key is not a store key makes and clears no folder.
func TestRunHandShadowRefusesABadKey(t *testing.T) {
	g := newHandRig(t)
	p := g.automaton("one", "bug")
	commit := g.handMerge(p, "1.2.3", "1.2.4")
	data := g.s.statePath()
	keep := filepath.Join(data, "keep", "marker")
	if err := os.MkdirAll(filepath.Dir(keep), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := g.s.autonomousMgr.(mergeShadowSource)
	subject, found := src.MergeShadowSubject(t.Context(), p.ID)
	if !found || subject.BranchHead == "" {
		t.Fatalf("no subject: %+v", subject)
	}
	rec := mergeshadow.Replay{
		Key: "../../keep", AutomatonID: p.ID, Branch: subject.Branch, CreatedAt: time.Now().UTC(),
		Hand: mergeshadow.HandMerge{Commit: commit, At: time.Now().UTC()},
	}
	runHandShadow(src, g.s.mergePipelineConfig(), g.repo, data, rec, subject, nil)
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("a folder outside the shadows' own was cleared: %v", err)
	}
}
