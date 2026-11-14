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
