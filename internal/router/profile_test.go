package router

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
)

// ── Parse grammar tests (pure, fast) ────────────────────────────────────

func TestParse_ProfileCommands(t *testing.T) {
	cases := []struct {
		in       string
		wantKind string
		wantVerb string
		wantName string
		wantText string
	}{
		{"profile project list", ProfileKindProject, ProfileVerbList, "", ""},
		{"profile cluster list", ProfileKindCluster, ProfileVerbList, "", ""},
		{"profile project show my-proj", ProfileKindProject, ProfileVerbShow, "my-proj", ""},
		{"profile cluster smoke testing", ProfileKindCluster, ProfileVerbSmoke, "testing", ""},
		{"profile", "", "", "", ""},
		{"profile project", ProfileKindProject, "", "", ""},
		{"profile namespace list", "", "", "", "invalid kind: namespace"},
		{"profile project show", ProfileKindProject, ProfileVerbShow, "", "show requires a profile name"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := Parse(c.in)
			if got.Type != CmdProfile {
				t.Fatalf("Type=%v want profile", got.Type)
			}
			if got.ProfileKind != c.wantKind {
				t.Errorf("Kind=%q want %q", got.ProfileKind, c.wantKind)
			}
			if got.ProfileVerb != c.wantVerb {
				t.Errorf("Verb=%q want %q", got.ProfileVerb, c.wantVerb)
			}
			if got.ProfileName != c.wantName {
				t.Errorf("Name=%q want %q", got.ProfileName, c.wantName)
			}
			if c.wantText != "" && !strings.Contains(got.Text, c.wantText) {
				t.Errorf("Text=%q want contains %q", got.Text, c.wantText)
			}
		})
	}
}

// ── Handler tests via HandleTestMessage (synchronous capture) ──────────

// setupProfileRouter wires real Project+Cluster stores and returns
// the router ready for HandleTestMessage-driven exercises.
func setupProfileRouter(t *testing.T) *Router {
	t.Helper()
	r := &Router{hostname: "testhost"}
	// Parent fields HandleTestMessage depends on
	r.backend = &captureBackend{name: "test", capture: func(string) {}}
	ps, err := profile.NewProjectStore(filepath.Join(t.TempDir(), "p.json"))
	if err != nil {
		t.Fatal(err)
	}
	cs, err := profile.NewClusterStore(filepath.Join(t.TempDir(), "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	r.SetProjectStore(ps)
	r.SetClusterStore(cs)
	return r
}

func TestProfile_List_Empty(t *testing.T) {
	r := setupProfileRouter(t)
	responses := r.HandleTestMessage("profile project list")
	if len(responses) == 0 || !strings.Contains(responses[0], "(none)") {
		t.Errorf("empty list should say (none), got %v", responses)
	}
}

func TestProfile_List_Project_WithEntries(t *testing.T) {
	r := setupProfileRouter(t)
	_ = r.projectStore.Create(&profile.ProjectProfile{
		Name:      "foo",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude", Sidecar: "lang-go"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = r.projectStore.Create(&profile.ProjectProfile{
		Name:      "solo",
		Git:       profile.GitSpec{URL: "https://example.com/x"},
		ImagePair: profile.ImagePair{Agent: "agent-aider"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	responses := r.HandleTestMessage("profile project list")
	if len(responses) == 0 {
		t.Fatal("no response")
	}
	out := responses[0]
	for _, want := range []string{"foo", "solo", "agent-claude", "agent-aider", "(solo)"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q: %s", want, out)
		}
	}
}

func TestProfile_Show_NotFound(t *testing.T) {
	r := setupProfileRouter(t)
	responses := r.HandleTestMessage("profile project show nope")
	if len(responses) == 0 || !strings.Contains(responses[0], "not found") {
		t.Errorf("missing not-found message: %v", responses)
	}
}

func TestProfile_Show_Project(t *testing.T) {
	r := setupProfileRouter(t)
	_ = r.projectStore.Create(&profile.ProjectProfile{
		Name:        "foo",
		Description: "test desc",
		Git:         profile.GitSpec{URL: "https://github.com/x/y", Branch: "main"},
		ImagePair:   profile.ImagePair{Agent: "agent-claude", Sidecar: "lang-go"},
		Memory:      profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	responses := r.HandleTestMessage("profile project show foo")
	if len(responses) == 0 {
		t.Fatal("no response")
	}
	out := responses[0]
	for _, want := range []string{"foo", "test desc", "agent-claude + lang-go", "sync-back"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestProfile_Smoke_Pass(t *testing.T) {
	r := setupProfileRouter(t)
	_ = r.projectStore.Create(&profile.ProjectProfile{
		Name:      "foo",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude", Sidecar: "lang-go"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	responses := r.HandleTestMessage("profile project smoke foo")
	if len(responses) == 0 {
		t.Fatal("no response")
	}
	if !strings.Contains(responses[0], "PASS") {
		t.Errorf("smoke pass missing PASS: %s", responses[0])
	}
}

func TestProfile_Help_UnknownKind(t *testing.T) {
	r := setupProfileRouter(t)
	responses := r.HandleTestMessage("profile namespace list")
	if len(responses) == 0 {
		t.Fatal("no response")
	}
	if !strings.Contains(responses[0], "usage") || !strings.Contains(responses[0], "invalid kind") {
		t.Errorf("help should mention usage + echo the note: %s", responses[0])
	}
}

func TestProfile_ClusterList(t *testing.T) {
	r := setupProfileRouter(t)
	_ = r.clusterStore.Create(&profile.ClusterProfile{
		Name:    "testing",
		Kind:    profile.ClusterK8s,
		Context: "k8s-testing",
	})
	responses := r.HandleTestMessage("profile cluster list")
	if len(responses) == 0 {
		t.Fatal("no response")
	}
	for _, want := range []string{"testing", "kind=k8s", "context=k8s-testing", "ns=default"} {
		if !strings.Contains(responses[0], want) {
			t.Errorf("cluster list missing %q: %s", want, responses[0])
		}
	}
}
