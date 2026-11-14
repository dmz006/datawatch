// MCP-surface tests: project_upsert must forward a non-empty description to
// the daemon (POST /api/projects body) and must not drop or blank a stored
// description when the caller omits the field.

package mcp

import (
	"testing"
)

func TestProjectDescription_UpsertForwardsDescription(t *testing.T) {
	cap, s, done := newRecordServer(t, `{"name":"acme","description":"the billing service of the Automata"}`)
	defer done()

	res := callTool(t, s.handleProjectUpsert, map[string]any{
		"name":        "acme",
		"description": "the billing service of the Automata",
		"dir":         "/tmp/acme",
	})
	if res == nil || res.IsError {
		t.Fatalf("project_upsert failed: %+v", res)
	}
	if cap.calls != 1 || cap.method != "POST" || cap.path != "/api/projects" {
		t.Fatalf("daemon call = %s %s (%d calls), want POST /api/projects (1 call)", cap.method, cap.path, cap.calls)
	}
	if got, _ := cap.body["description"].(string); got != "the billing service of the Automata" {
		t.Errorf("forwarded description = %q, want %q", got, "the billing service of the Automata")
	}
	if got, _ := cap.body["name"].(string); got != "acme" {
		t.Errorf("forwarded name = %q, want acme", got)
	}
	if got, _ := cap.body["dir"].(string); got != "/tmp/acme" {
		t.Errorf("forwarded dir = %q, want /tmp/acme", got)
	}
}

// LAYER-FINDING: the saved project description is dropped in the MCP layer.
//
// The operator has already saved a description for project "acme"; the daemon
// returns it on GET /api/projects/acme. The LLM then makes a legitimate
// partial update — project_upsert with only name and dir, no description —
// and the stored description must survive, exactly as the CLI
// (`datawatch projects set`) and the comm channel (`project set`) preserve it:
// both read the stored record first and carry description/default_backend
// over, because "the upsert overwrites description, default_backend and
// policy whenever they are sent empty".
//
// handleProjectUpsert does not read the stored record and not carry the
// fields over. It unconditionally forwards
//
//	"description":     req.GetString("description", ""),
//
// so an omitted description arrives at the daemon as an empty string and the
// server overwrites the saved value with "" — the description the operator
// saved is silently wiped.
//
// The test below fails today: the forwarded body contains an empty
// description even though the caller never mentioned one. The correct
// behavior (matching CLI and comm channel) is that the forwarded body either
// omits the description key or carries the stored value — never blanks it.
func TestProjectDescription_UpsertWithoutDescriptionKeepsStoredDescription(t *testing.T) {
	// Daemon stub: the stored record has the saved description; a POST
	// succeeds. A GET could return the record — the point is what the tool
	// sends on POST.
	storedDescription := "the billing service of the Automata"
	cap, s, done := newRecordServer(t, `{"ok":true}`)
	defer done()

	res := callTool(t, s.handleProjectUpsert, map[string]any{
		"name": "acme",
		"dir":  "/tmp/acme",
		// description intentionally omitted: a partial update that must not
		// wipe what the operator saved.
	})
	if res == nil || res.IsError {
		t.Fatalf("project_upsert failed: %+v", res)
	}
	if cap.method != "POST" || cap.path != "/api/projects" {
		t.Fatalf("daemon call = %s %s, want POST /api/projects", cap.method, cap.path)
	}

	got, present := cap.body["description"]
	gotStr, _ := got.(string)
	if present && gotStr == "" {
		t.Errorf("project_upsert forwarded description=%q (blank) after the caller omitted the field; "+
			"the daemon would overwrite the stored description (%q) with the empty string. "+
			"MCP must not blank stored fields on a partial update: omit the key or carry the stored value (as the CLI and comm channel do).",
			gotStr, storedDescription)
	}
	if present && gotStr != "" && gotStr != storedDescription {
		t.Errorf("forwarded description = %q, want either omitted or the stored value %q", gotStr, storedDescription)
	}
	if got, _ := cap.body["default_backend"].(string); got == "" {
		if _, ok := cap.body["default_backend"]; ok {
			t.Errorf("project_upsert also blanks default_backend on a partial update (same defect class)")
		}
	}
}
