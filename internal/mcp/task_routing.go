// Task routing MCP tools: task_routing_get, task_routing_set, the profile
// tools, task_routing_automaton and task_routing_dry_run. Each proxies the
// REST routes under /api/autonomous/task-routing (and the Automaton's own
// task-routing path) over the loopback, so every surface answers from the
// same engine code. No tool starts, stops or signals a session.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

const taskRoutingPath = "/api/autonomous/task-routing"

func (s *Server) toolTaskRoutingGet() mcpsdk.Tool {
	return mcpsdk.NewTool("task_routing_get",
		mcpsdk.WithDescription("Read the task routing table: whether routing is on, the active profile, every profile's rows "+
			"(task type to worker LLM, verifier LLM, effort, second choice) and the validation result. "+
			"When on, the table picks the LLM and model for each task of an Automaton by its task type."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleTaskRoutingGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet(taskRoutingPath, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTaskRoutingSet() mcpsdk.Tool {
	return mcpsdk.NewTool("task_routing_set",
		mcpsdk.WithDescription("Update the task routing table. Every field is optional: a field you leave out keeps its current value. "+
			"The result is validated first and a bad table is refused with every problem listed, nothing applied "+
			"(an LLM the registry does not know, a model that does not pair with its LLM, or an Opus or Fable model on a row "+
			"that does not allow premium models). Only tasks created afterwards read the new table."),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Turn task routing on or off")),
		mcpsdk.WithString("active_profile", mcpsdk.Description("Name of the profile to make active (it must exist)")),
		mcpsdk.WithString("profiles", mcpsdk.Description(`JSON object of profiles by name, replacing the current profiles: {"default":{"docs_set":{"worker_backend":"","worker_model":"","verification_backend":"","verification_model":"","effort":"","second_choice":{"backend":"","model":""},"allow_premium_model":false}}}`)),
	)
}

func (s *Server) handleTaskRoutingSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	args := req.GetArguments()
	cur, err := s.proxyGet(taskRoutingPath, nil)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(cur, &body); err != nil {
		return nil, fmt.Errorf("task routing table: %w", err)
	}
	// Only the writable members go back.
	delete(body, "validation")
	delete(body, "task_types")
	if v, ok := args["enabled"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			return nil, fmt.Errorf("enabled: must be true or false")
		}
		body["enabled"], _ = json.Marshal(b)
	}
	if v := req.GetString("active_profile", ""); v != "" {
		body["active_profile"], _ = json.Marshal(v)
	}
	if v := req.GetString("profiles", ""); v != "" {
		if !json.Valid([]byte(v)) {
			return nil, fmt.Errorf("profiles: invalid JSON")
		}
		body["profiles"] = json.RawMessage(v)
	}
	raw, _ := json.Marshal(body)
	out, err := s.proxyJSON(http.MethodPut, taskRoutingPath, raw)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTaskRoutingProfileList() mcpsdk.Tool {
	return mcpsdk.NewTool("task_routing_profile_list",
		mcpsdk.WithDescription("List the saved task routing profiles and which one is active."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleTaskRoutingProfileList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet(taskRoutingPath+"/profiles", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTaskRoutingProfileSave() mcpsdk.Tool {
	return mcpsdk.NewTool("task_routing_profile_save",
		mcpsdk.WithDescription("Save a task routing profile under a name. With no rows it copies the active profile's current table; "+
			"with rows it stores those. An existing profile of that name is replaced. The table is validated first."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Profile name")),
		mcpsdk.WithString("rows", mcpsdk.Description("Optional JSON object of rows by task type; omit to copy the active profile")),
	)
}

func (s *Server) handleTaskRoutingProfileSave(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{"name": req.GetString("name", "")}
	if v := req.GetString("rows", ""); v != "" {
		if !json.Valid([]byte(v)) {
			return nil, fmt.Errorf("rows: invalid JSON")
		}
		body["rows"] = json.RawMessage(v)
	}
	raw, _ := json.Marshal(body)
	out, err := s.proxyJSON(http.MethodPost, taskRoutingPath+"/profiles", raw)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTaskRoutingProfileActivate() mcpsdk.Tool {
	return mcpsdk.NewTool("task_routing_profile_activate",
		mcpsdk.WithDescription("Make a saved task routing profile the active one. Only tasks created afterwards use it; no existing task or session changes."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Profile name")),
	)
}

func (s *Server) handleTaskRoutingProfileActivate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPut, taskRoutingPath+"/active-profile", map[string]any{"name": req.GetString("name", "")})
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTaskRoutingProfileDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("task_routing_profile_delete",
		mcpsdk.WithDescription("Delete a saved task routing profile. The active profile cannot be deleted."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Profile name")),
	)
}

func (s *Server) handleTaskRoutingProfileDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	out, err := s.proxyJSON(http.MethodDelete, taskRoutingPath+"/profiles/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTaskRoutingProfileExport() mcpsdk.Tool {
	return mcpsdk.NewTool("task_routing_profile_export",
		mcpsdk.WithDescription("Export one task routing profile as a JSON document (the same file the web app downloads). "+
			"Pass the document to task_routing_profile_import on another server to copy the profile."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Profile name")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleTaskRoutingProfileExport(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	out, err := s.proxyGet(taskRoutingPath+"/profiles/"+url.PathEscape(name)+"/export", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTaskRoutingProfileImport() mcpsdk.Tool {
	return mcpsdk.NewTool("task_routing_profile_import",
		mcpsdk.WithDescription("Import a task routing profile from an exported document. The profile is validated and refused with every problem listed if it is not valid. "+
			"An existing profile of the same name is replaced only when overwrite is true."),
		mcpsdk.WithString("document", mcpsdk.Required(), mcpsdk.Description("The exported profile document (JSON text)")),
		mcpsdk.WithString("name", mcpsdk.Description("Store it under this name instead of the document's own name")),
		mcpsdk.WithBoolean("overwrite", mcpsdk.Description("Replace an existing profile of the same name")),
	)
}

func (s *Server) handleTaskRoutingProfileImport(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	doc := req.GetString("document", "")
	if !json.Valid([]byte(doc)) {
		return nil, fmt.Errorf("document: invalid JSON")
	}
	q := url.Values{}
	if n := strings.TrimSpace(req.GetString("name", "")); n != "" {
		q.Set("name", n)
	}
	if req.GetBool("overwrite", false) {
		q.Set("overwrite", "true")
	}
	path := taskRoutingPath + "/profiles/import"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	out, err := s.proxyJSON(http.MethodPost, path, []byte(doc))
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTaskRoutingAutomaton() mcpsdk.Tool {
	return mcpsdk.NewTool("task_routing_automaton",
		mcpsdk.WithDescription("Per-Automaton task routing read: for every task, its task type, how it was classified, "+
			"which table row decided its LLM (or why none did) and the LLM it will run with. Writes nothing."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton id")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleTaskRoutingAutomaton(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	out, err := s.proxyGet("/api/autonomous/prds/"+url.PathEscape(id)+"/task-routing", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTaskRoutingDryRun() mcpsdk.Tool {
	return mcpsdk.NewTool("task_routing_dry_run",
		mcpsdk.WithDescription("Dry run for a draft Automaton: show what the active (or the named) routing profile would assign to each task. "+
			"Nothing is written, no decision is logged and no LLM is called."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton id")),
		mcpsdk.WithString("profile", mcpsdk.Description("Profile name; defaults to the active profile")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleTaskRoutingDryRun(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(id)+"/task-routing/dry-run",
		map[string]any{"profile": req.GetString("profile", "")})
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
