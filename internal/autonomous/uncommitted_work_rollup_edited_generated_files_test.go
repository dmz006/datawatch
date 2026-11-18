package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

// writeOneGateReport writes the one gate report the daemon generates at
// finish time — really, through genartifacts.Write, with its fresh hash
// sidecar — into the rig's project directory, and returns the paths of the
// document and of its sidecar, relative to the project root.
func writeOneGateReport(t *testing.T, r *uncommittedRollupRig) (doc, sidecar string) {
	t.Helper()
	doc = "docs/plans/reports/" + r.prd.ID + "-gate.md"
	res, err := genartifacts.Write(genartifacts.GateReport, "All checks passed for this run.", doc, genartifacts.Options{Root: r.prd.ProjectDir})
	if err != nil {
		t.Fatalf("write gate report: %v", err)
	}
	if res.Status != genartifacts.Written {
		t.Fatalf("write gate report = %s (%s); want written", res.Status, res.Reason)
	}
	return doc, genartifacts.SidecarRelPath(doc)
}

// gateDocFull is the absolute path of a project-relative file of the rig's
// generated gate report pair.
func gateDocFull(r *uncommittedRollupRig, rel string) string {
	return filepath.Join(r.prd.ProjectDir, filepath.FromSlash(rel))
}

// mutateGatePath rewrites one file of the generated gate report pair in the
// rig's project directory — or removes it when content is nil.
func mutateGatePath(t *testing.T, r *uncommittedRollupRig, rel string, content []byte) {
	t.Helper()
	if content == nil {
		if err := os.Remove(gateDocFull(r, rel)); err != nil {
			t.Fatalf("remove %s: %v", rel, err)
		}
		return
	}
	if err := os.WriteFile(gateDocFull(r, rel), content, 0o644); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("rewrite %s: %v", rel, err)
	}
}

// setGateRollupDirty marks exactly the given project-relative paths as
// untracked ('??') in the rig's fake worktree, the way `git status` reports
// files the daemon has not staged.
func setGateRollupDirty(t *testing.T, r *uncommittedRollupRig, rels ...string) {
	t.Helper()
	dirty := map[string]string{}
	for _, rel := range rels {
		dirty[rel] = "??"
	}
	r.git.mu.Lock()
	r.git.dirty = dirty
	r.git.mu.Unlock()
}

// Each case starts from the fresh gate report and its hash sidecar the daemon
// writes at finish time, applies one disk change to that pair, and expects the
// change to count as uncommitted work again: the Automaton blocks, its hold
// record names the document, and the raised question names it too.
func TestUncommittedRollupGeneratedFilesIgnoredEditedOrUnverifiedCountsAsWork(t *testing.T) {
	const docBody = "All checks passed for this run.\n"

	// editedDoc is the daemon's document with an edited line right after the
	// marker line, the way a person's edit looks.
	editedDoc := genartifacts.Marker + "\n" + genartifacts.EditedMarker + "\n" + docBody

	// fakeHash is a well-formed but wrong sidecar hash line.
	fakeHash := strings.Repeat("e", 64) + "\n"

	cases := []struct {
		name   string
		mutate func(t *testing.T, r *uncommittedRollupRig, doc, sidecar string)
		onDisk func(doc, sidecar string) []string
	}{
		{
			name: "marker removed",
			mutate: func(t *testing.T, r *uncommittedRollupRig, doc, sidecar string) {
				b, err := os.ReadFile(gateDocFull(r, doc))
				if err != nil {
					t.Fatalf("read gate report: %v", err)
				}
				if _, rest, ok := strings.Cut(string(b), "\n"); !ok {
					t.Fatalf("gate report has no first line: %q", b)
				} else {
					mutateGatePath(t, r, doc, []byte(rest))
				}
			},
			onDisk: func(doc, sidecar string) []string { return []string{doc, sidecar} },
		},
		{
			name: "edited marker inserted after the marker",
			mutate: func(t *testing.T, r *uncommittedRollupRig, doc, sidecar string) {
				mutateGatePath(t, r, doc, []byte(editedDoc))
			},
			onDisk: func(doc, sidecar string) []string { return []string{doc, sidecar} },
		},
		{
			name: "body line changed, marker kept",
			mutate: func(t *testing.T, r *uncommittedRollupRig, doc, sidecar string) {
				mutateGatePath(t, r, doc, []byte(genartifacts.Marker+"\nA changed line no one generated.\n"))
			},
			onDisk: func(doc, sidecar string) []string { return []string{doc, sidecar} },
		},
		{
			name: "sidecar deleted, document edited",
			mutate: func(t *testing.T, r *uncommittedRollupRig, doc, sidecar string) {
				mutateGatePath(t, r, doc, []byte(editedDoc))
				mutateGatePath(t, r, sidecar, nil)
			},
			onDisk: func(doc, sidecar string) []string { return []string{doc} },
		},
		{
			name: "sidecar overwritten with a different hash",
			mutate: func(t *testing.T, r *uncommittedRollupRig, doc, sidecar string) {
				mutateGatePath(t, r, sidecar, []byte(fakeHash))
			},
			onDisk: func(doc, sidecar string) []string { return []string{doc, sidecar} },
		},
		{
			name:   "sidecar deleted, document unedited",
			mutate: func(t *testing.T, r *uncommittedRollupRig, doc, sidecar string) { mutateGatePath(t, r, sidecar, nil) },
			onDisk: func(doc, sidecar string) []string { return []string{doc} },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold, nil)
			doc, sidecar := writeOneGateReport(t, r)
			tc.mutate(t, r, doc, sidecar)

			rels := tc.onDisk(doc, sidecar)
			for _, rel := range rels {
				if _, err := os.Stat(gateDocFull(r, rel)); err != nil {
					t.Fatalf("dirty list names a path that is not on disk: %v", err)
				}
			}
			setGateRollupDirty(t, r, rels...)

			got := r.run(t)
			if got.Status != PRDBlocked {
				t.Fatalf("status = %s; want blocked (hold: %+v)", got.Status, got.UncommittedWork)
			}
			if got.UncommittedWork == nil || !containsString(got.UncommittedWork.Files, doc) {
				t.Fatalf("hold record lacks the document %q: %+v", doc, got.UncommittedWork)
			}
			qs := r.store.List(QuestionFilter{})
			if len(qs) != 1 {
				t.Fatalf("questions = %d; want 1", len(qs))
			}
			if !strings.Contains(qs[0].Text, doc) {
				t.Errorf("question lacks the document path %q:\n%s", doc, qs[0].Text)
			}
		})
	}
}
