// CLI-surface test: `projects upsert <name> --description ...` must forward
// the description to the daemon (POST /api/projects body), and `projects show`
// must display the stored description. The description is not dropped anywhere
// in the CLI round-trip.

package main

import (
	"strings"
	"testing"
)

func TestProjectDescription_UpsertForwardsDescription(t *testing.T) {
	f := &fakeProjectsDaemon{}
	err := runProjectsCmd(t, f, "upsert", "acme",
		"--dir", "/srv/acme",
		"--backend", "claude-code",
		"--description", "the billing service of the Automata")
	if err != nil {
		t.Fatalf("projects upsert failed: %v", err)
	}
	if f.postCount() != 1 {
		t.Fatalf("post count = %d, want 1", f.postCount())
	}
	body := f.lastPost(t)
	if got, _ := body["description"].(string); got != "the billing service of the Automata" {
		t.Errorf("posted description = %q (body=%v), want %q", got, body, "the billing service of the Automata")
	}
	if got, _ := body["name"].(string); got != "acme" {
		t.Errorf("posted name = %q, want acme", got)
	}
	if got, _ := body["dir"].(string); got != "/srv/acme" {
		t.Errorf("posted dir = %q, want /srv/acme", got)
	}
	if got, _ := body["default_backend"].(string); got != "claude-code" {
		t.Errorf("posted default_backend = %q, want claude-code", got)
	}
}

// A record update that only changes the backend must still carry over the
// stored description instead of clearing it.
func TestProjectDescription_SetKeepsStoredDescription(t *testing.T) {
	f := &fakeProjectsDaemon{}
	err := runProjectsCmd(t, f, "set", "acme", "--owner", "infra")
	if err != nil {
		t.Fatalf("projects set failed: %v", err)
	}
	body := f.lastPost(t)
	if got, _ := body["description"].(string); got != "Acme work" {
		t.Errorf("posted description = %q (body=%v), want the stored description carried over", got, body)
	}
	if got, _ := body["default_backend"].(string); got != "claude-code" {
		t.Errorf("posted default_backend = %q, want claude-code (carried over)", got)
	}
	if policy, ok := body["policy"].(map[string]any); !ok || policy["plan_review_mode"] != "block" {
		t.Errorf("posted policy = %v, want the stored policy preserved", body["policy"])
	}
}

func TestProjectDescription_ShowPrintsDescription(t *testing.T) {
	out, err := renderProjectShow([]byte(acmeRecord), []byte(acmeRollup))
	if err != nil {
		t.Fatalf("renderProjectShow: %v", err)
	}
	if !strings.Contains(string(out), "Acme work") {
		t.Errorf("show output missing the stored description:\n%s", out)
	}
}

// The manage `upsert` is a partial-update surface for the operator: a flag
// that was not passed must not blank the stored value (the daemon's upsert
// overwrites description and default_backend whenever they are absent or
// empty). `upsert` must read the stored record first and carry the omitted
// field over — exactly what `projects set` already does.
func TestProjectDescription_UpsertWithDescriptionCarriesStoredBackend(t *testing.T) {
	f := &fakeProjectsDaemon{}
	err := runProjectsCmd(t, f, "upsert", "acme",
		"--dir", "/srv/acme",
		"--description", "new note",
		// --backend intentionally not given: the stored claude-code must
		// survive this update, not be blanked.
	)
	if err != nil {
		t.Fatalf("projects upsert failed: %v", err)
	}
	body := f.lastPost(t)
	if got, _ := body["description"].(string); got != "new note" {
		t.Errorf("posted description = %q (body=%v), want new note", got, body)
	}
	if got, ok := body["default_backend"].(string); !ok || got != "claude-code" {
		t.Errorf("posted default_backend = %v (present=%v) (body=%v), want the stored claude-code carried over — omitting the flag must not blank it", got, ok, body)
	}
}

// The symmetric direction: updating the backend with the description flag
// omitted keeps the stored description.
func TestProjectDescription_UpsertWithBackendCarriesStoredDescription(t *testing.T) {
	f := &fakeProjectsDaemon{}
	err := runProjectsCmd(t, f, "upsert", "acme",
		"--dir", "/srv/acme",
		"--backend", "pi",
	)
	if err != nil {
		t.Fatalf("projects upsert failed: %v", err)
	}
	body := f.lastPost(t)
	if got, _ := body["default_backend"].(string); got != "pi" {
		t.Errorf("posted default_backend = %q (body=%v), want pi", got, body)
	}
	if got, _ := body["description"].(string); got != "Acme work" {
		t.Errorf("posted description = %q (body=%v), want the stored Acme work carried over", got, body)
	}
}

// An explicit empty --description still clears — and in the same call an
// omitted backend is still carried over from the stored record.
func TestProjectDescription_UpsertExplicitEmptyDescriptionClears(t *testing.T) {
	f := &fakeProjectsDaemon{}
	err := runProjectsCmd(t, f, "upsert", "acme",
		"--dir", "/srv/acme",
		"--description", "", // explicit clear
	)
	if err != nil {
		t.Fatalf("projects upsert failed: %v", err)
	}
	body := f.lastPost(t)
	if v, present := body["description"]; !present || v != "" {
		t.Errorf("posted description = %v (present=%v), want the explicit empty string to clear (body=%v)", v, present, body)
	}
	if got, _ := body["default_backend"].(string); got != "claude-code" {
		t.Errorf("posted default_backend = %q (body=%v), want the stored claude-code carried over", got, body)
	}
}

// Registering a project the daemon does not have yet (the readback 404s)
// must still succeed — no stored values exist to carry, and the upsert must
// not fail because of the failed read.
func TestProjectDescription_UpsertNewProjectReadback404s(t *testing.T) {
	f := &fakeProjectsDaemon{}
	err := runProjectsCmd(t, f, "upsert", "nova",
		"--dir", "/srv/nova",
	)
	if err != nil {
		t.Fatalf("projects upsert of a new project failed: %v", err)
	}
	body := f.lastPost(t)
	if got, _ := body["name"].(string); got != "nova" {
		t.Errorf("posted name = %q (body=%v), want nova", got, body)
	}
	if got, _ := body["dir"].(string); got != "/srv/nova" {
		t.Errorf("posted dir = %q (body=%v), want /srv/nova", got, body)
	}
}
