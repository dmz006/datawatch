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
	// The readback may issue a GET for the omitted default_backend before the
	// POST; what matters is that the final call is the upsert itself.
	if cap.method != "POST" || cap.path != "/api/projects" || cap.calls < 1 {
		t.Fatalf("daemon call = %s %s (%d calls), want POST /api/projects last", cap.method, cap.path, cap.calls)
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

// The stored-value read must not depend on which field the caller omits: an
// update that SETS the description (leaving the backend omitted) must still
// carry the stored default_backend over — the daemon's whole-record contract
// would otherwise blank it. Before the fix the readback only fired when the
// description itself was omitted, so this body silently dropped
// default_backend and the server blanked the stored value.
func TestProjectDescription_UpsertWithDescriptionKeepsStoredBackend(t *testing.T) {
	cap, s, done := newRecordServer(t, `{"name":"acme","description":"old note","default_backend":"claude-code"}`)
	defer done()

	res := callTool(t, s.handleProjectUpsert, map[string]any{
		"name":        "acme",
		"dir":         "/tmp/acme",
		"description": "billing v2",
		// default_backend intentionally omitted: the stored backend must
		// survive, not be blanked, by this partial update.
	})
	if res == nil || res.IsError {
		t.Fatalf("project_upsert failed: %+v", res)
	}
	if cap.method != "POST" || cap.path != "/api/projects" {
		t.Fatalf("daemon call = %s %s, want POST /api/projects (capture keeps the last call)", cap.method, cap.path)
	}
	if got, _ := cap.body["description"].(string); got != "billing v2" {
		t.Errorf("forwarded description = %q, want billing v2 (body=%v)", got, cap.body)
	}
	if got, _ := cap.body["default_backend"].(string); got != "claude-code" {
		t.Errorf("forwarded default_backend = %v (present=%v), want the stored claude-code carried over — omitting the field must not blank it (body=%v)", got, cap.body["default_backend"] != nil, cap.body)
	}
}

// The symmetric direction: setting the backend with the description omitted
// keeps the stored description.
func TestProjectDescription_UpsertWithBackendKeepsStoredDescription(t *testing.T) {
	cap, s, done := newRecordServer(t, `{"name":"acme","description":"old note","default_backend":"claude-code"}`)
	defer done()

	res := callTool(t, s.handleProjectUpsert, map[string]any{
		"name":            "acme",
		"dir":             "/tmp/acme",
		"default_backend": "pi",
		// description intentionally omitted.
	})
	if res == nil || res.IsError {
		t.Fatalf("project_upsert failed: %+v", res)
	}
	if got, _ := cap.body["default_backend"].(string); got != "pi" {
		t.Errorf("forwarded default_backend = %q, want pi (body=%v)", got, cap.body)
	}
	if got, _ := cap.body["description"].(string); got != "old note" {
		t.Errorf("forwarded description = %q, want the stored old note carried over (body=%v)", got, cap.body)
	}
}

// An explicit empty string still clears the stored value — even in the same
// call where the other field was omitted and had to be read back.
func TestProjectDescription_UpsertEmptyDescriptionWithOmittedBackend(t *testing.T) {
	cap, s, done := newRecordServer(t, `{"name":"acme","description":"old note","default_backend":"claude-code"}`)
	defer done()

	res := callTool(t, s.handleProjectUpsert, map[string]any{
		"name":        "acme",
		"dir":         "/tmp/acme",
		"description": "", // explicit clear
		// default_backend omitted, must be carried over.
	})
	if res == nil || res.IsError {
		t.Fatalf("project_upsert failed: %+v", res)
	}
	v, present := cap.body["description"]
	if !present || v != "" {
		t.Errorf("forwarded description = %v (present=%v), want the explicit empty string to clear (body=%v)", v, present, cap.body)
	}
	if got, _ := cap.body["default_backend"].(string); got != "claude-code" {
		t.Errorf("forwarded default_backend = %v, want the stored claude-code (body=%v)", got, cap.body)
	}
}

// A brand-new registration (the daemon 404s the readback) must still
// succeed: no stored values exist to carry, and the upsert is not rejected.
func TestProjectDescription_UpsertNewProjectOmitsStoredFields(t *testing.T) {
	cap, s, done := newRecordServer(t, `{"error":"project not found"}`)
	defer done()

	res := callTool(t, s.handleProjectUpsert, map[string]any{
		"name": "nova",
		"dir":  "/srv/nova",
	})
	if res == nil || res.IsError {
		t.Fatalf("project_upsert of a new project failed: %+v", res)
	}
	if cap.method != "POST" || cap.path != "/api/projects" {
		t.Fatalf("last daemon call = %s %s, want POST /api/projects", cap.method, cap.path)
	}
	if got, _ := cap.body["name"].(string); got != "nova" {
		t.Errorf("forwarded name = %q, want nova (body=%v)", got, cap.body)
	}
	if got, _ := cap.body["description"].(string); got != "" {
		t.Errorf("forwarded description = %q, want empty (body=%v)", got, cap.body)
	}
}
