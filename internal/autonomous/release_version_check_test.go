// Release-version gate tests. A release task's version is by definition
// whatever merge decides — the next patch above main at merge time — so a
// fixed three-part version number in a release task is a contradiction.
// validateNoFixedReleaseVersion is the mechanical check; these tests pin
// the gate end-to-end through Decompose (fake planner, fake PRD, no
// sessions) and directly against the validator itself.
package autonomous

import (
	"strings"
	"testing"
)

// A fixed release number used to fail the decompose; the daemon now
// rewords it itself when the gate is the only content failure (see
// release_version_fix.go). The validator itself still rejects the raw
// plan — pinned by the direct validator tests below.
func TestDecompose_FixedReleaseVersionSelfCorrected(t *testing.T) {
	fn := func(req DecomposeRequest) (string, error) {
		return `{"title":"Ship release","stories":[{"title":"Release","tasks":[{"title":"Bump version","spec":"Bump the patch version from 9.5.26 to 9.5.27 in release.go"}]}]}`, nil
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("ship the release", "", "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: want self-corrected success, got %v", err)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", updated.Status)
	}
	spec := updated.Story[0].Tasks[0].Spec
	if strings.Contains(spec, "9.5.26") || strings.Contains(spec, "9.5.27") {
		t.Fatalf("fixed number left in spec: %q", spec)
	}
}

func TestDecompose_FixedReleaseVersionAcceptedWhenVersionFree(t *testing.T) {
	fn := func(req DecomposeRequest) (string, error) {
		return `{"title":"Ship release","stories":[{"title":"Release","tasks":[{"title":"Prepare release","spec":"Set the version to the next patch above main at merge time and add a plain CHANGELOG entry"}]}]}`, nil
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("ship the release", "", "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", updated.Status)
	}
	if n := len(storedStoriesFor(m, prd.ID)); n != 1 {
		t.Fatalf("stored stories = %d, want 1", n)
	}
}

func TestValidateNoFixedReleaseVersion_Table(t *testing.T) {
	cases := []struct {
		name    string
		stories []Story
		wantErr bool
	}{
		{
			name: "v-prefixed number in release task is rejected",
			stories: []Story{{
				Title: "Release",
				Tasks: []Task{{
					Title: "Bump version",
					Spec:  "Bump the version to v9.5.27",
				}},
			}},
			wantErr: true,
		},
		{
			name: "non-release task mentioning a Go version passes",
			stories: []Story{{
				Title: "Upgrade toolchain",
				Tasks: []Task{{
					Title: "Update dependency",
					Spec:  "Rebuild with Go 1.22.5 and vendor the new library",
				}},
			}},
			wantErr: false,
		},
		{
			name:    "empty slice passes",
			stories: nil,
			wantErr: false,
		},
		{
			name: "four-part dotted string in release task does not count",
			stories: []Story{{
				Title: "Release",
				Tasks: []Task{{
					Title: "Version bump prep",
					Spec:  "Add 192.0.2.1 to the release allowlist before the version bump",
				}},
			}},
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNoFixedReleaseVersion(tc.stories)
			if tc.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("want nil error, got %v", err)
			}
		})
	}
}

func TestPlanningPrompts_NextPatchRule(t *testing.T) {
	for _, p := range []struct {
		name   string
		prompt string
	}{
		{"PlanningPrompt", PlanningPrompt},
		{"PlanningPromptSession", PlanningPromptSession},
	} {
		if !strings.Contains(p.prompt, "next patch above main") {
			t.Errorf("%s does not contain %q", p.name, "next patch above main")
		}
	}
}
