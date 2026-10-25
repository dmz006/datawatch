// MCP tools for the Automata tag registry and per-Automaton tag assignment.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolAutonomousTagList() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_tag_list",
		mcpsdk.WithDescription("List Automata tags (a tree via parent_id) with the number of Automata carrying each tag."),
		mcpsdk.WithBoolean("include_sub", mcpsdk.Description("Roll each tag's Automata count up to include its descendant tags (default false)")),
		mcpsdk.WithBoolean("counts", mcpsdk.Description("Add subtree and direct counts for Automata and sessions to each tag, plus an untagged total (default false)")),
	)
}
func (s *Server) handleAutonomousTagList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if req.GetBool("include_sub", false) {
		q.Set("include_sub", "true")
	}
	if req.GetBool("counts", false) {
		q.Set("counts", "true")
	}
	out, err := s.proxyGet("/api/autonomous/tags", q)
	if err != nil {
		return toolError("autonomous_tag_list", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousTagCreate() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_tag_create",
		mcpsdk.WithDescription("Create an Automata tag, optionally nested under a parent tag."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Tag name")),
		mcpsdk.WithString("parent_id", mcpsdk.Description("Parent tag ID (empty = root tag)")),
		mcpsdk.WithString("color", mcpsdk.Description("Optional CSS color (e.g. '#6366f1')")),
		mcpsdk.WithString("description", mcpsdk.Description("Optional description")),
	)
}
func (s *Server) handleAutonomousTagCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body, _ := json.Marshal(map[string]string{
		"name":        req.GetString("name", ""),
		"parent_id":   req.GetString("parent_id", ""),
		"color":       req.GetString("color", ""),
		"description": req.GetString("description", ""),
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/tags", body)
	if err != nil {
		return toolError("autonomous_tag_create", err), nil
	}
	return textOK(string(out)), nil
}

// patchTag sends a single-field PATCH for a tag.
func (s *Server) patchTag(tool, id string, fields map[string]string) (*mcpsdk.CallToolResult, error) {
	body, _ := json.Marshal(fields)
	out, err := s.proxyJSON(http.MethodPatch, "/api/autonomous/tags/"+url.PathEscape(id), body)
	if err != nil {
		return toolError(tool, err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousTagRename() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_tag_rename",
		mcpsdk.WithDescription("Rename an Automata tag."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Tag ID")),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("New name")),
	)
}
func (s *Server) handleAutonomousTagRename(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.patchTag("autonomous_tag_rename", req.GetString("id", ""),
		map[string]string{"name": req.GetString("name", "")})
}

func (s *Server) toolAutonomousTagSetParent() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_tag_set_parent",
		mcpsdk.WithDescription("Move an Automata tag under a new parent (empty parent_id = make it a root). Refused if the move would make the tag its own ancestor."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Tag ID")),
		mcpsdk.WithString("parent_id", mcpsdk.Description("New parent tag ID (empty = root)")),
	)
}
func (s *Server) handleAutonomousTagSetParent(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.patchTag("autonomous_tag_set_parent", req.GetString("id", ""),
		map[string]string{"parent_id": req.GetString("parent_id", "")})
}

func (s *Server) toolAutonomousTagSetColor() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_tag_set_color",
		mcpsdk.WithDescription("Set an Automata tag's color."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Tag ID")),
		mcpsdk.WithString("color", mcpsdk.Required(), mcpsdk.Description("CSS color (e.g. '#6366f1'); empty clears it")),
	)
}
func (s *Server) handleAutonomousTagSetColor(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.patchTag("autonomous_tag_set_color", req.GetString("id", ""),
		map[string]string{"color": req.GetString("color", "")})
}

func (s *Server) toolAutonomousTagDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_tag_delete",
		mcpsdk.WithDescription("Delete an Automata tag. Refused (409) while the tag has child tags or is still assigned to Automata; the refusal reports how many Automata use it."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Tag ID")),
	)
}
func (s *Server) handleAutonomousTagDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodDelete, "/api/autonomous/tags/"+url.PathEscape(req.GetString("id", "")), nil)
	if err != nil {
		return toolError("autonomous_tag_delete", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDSetTags() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_tags",
		mcpsdk.WithDescription("Replace the set of tags on an Automaton (PRD). Pass an empty array to clear. Pass primary_tag_id alone to change only the primary tag and keep the current tags; an empty primary_tag_id clears the primary."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("tag_ids_json", mcpsdk.Description(`JSON array of tag IDs, e.g. ["1a2b3c4d","5e6f7a8b"]. Required unless primary_tag_id is given.`)),
		mcpsdk.WithString("primary_tag_id", mcpsdk.Description("Optional primary tag ID, one of the Automaton's tags, used for sorting only. Empty clears it. Omit to keep the current primary.")),
	)
}
func (s *Server) handleAutonomousPRDSetTags(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	args := req.GetArguments()
	body := map[string]any{}
	if _, given := args["tag_ids_json"]; given {
		tagIDs := []string{}
		if err := json.Unmarshal([]byte(req.GetString("tag_ids_json", "[]")), &tagIDs); err != nil {
			return toolError("autonomous_prd_set_tags", err), nil
		}
		body["tag_ids"] = tagIDs
	}
	if _, given := args["primary_tag_id"]; given {
		body["primary_tag_id"] = req.GetString("primary_tag_id", "")
	}
	if len(body) == 0 {
		return toolError("autonomous_prd_set_tags", fmt.Errorf("tag_ids_json or primary_tag_id is required")), nil
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(id)+"/set_tags", body)
	if err != nil {
		return toolError("autonomous_prd_set_tags", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolSessionSetTags() mcpsdk.Tool {
	return mcpsdk.NewTool("session_set_tags",
		mcpsdk.WithDescription("Replace the tags a session carries itself (not the tags it inherits from its Automaton). Pass an empty array to clear. Tag IDs must exist in the Automata tag registry."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Session ID (full or short)")),
		mcpsdk.WithString("tag_ids_json", mcpsdk.Required(), mcpsdk.Description(`JSON array of tag IDs, e.g. ["1a2b3c4d","5e6f7a8b"]`)),
	)
}
func (s *Server) handleSessionSetTags(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	tagIDs := []string{}
	if err := json.Unmarshal([]byte(req.GetString("tag_ids_json", "[]")), &tagIDs); err != nil {
		return toolError("session_set_tags", err), nil
	}
	body, _ := json.Marshal(map[string]any{"id": req.GetString("id", ""), "tag_ids": tagIDs})
	out, err := s.proxyJSON(http.MethodPost, "/api/sessions/tags", body)
	if err != nil {
		return toolError("session_set_tags", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousAttentionList() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_attention_list",
		mcpsdk.WithDescription("List the Automata that need the operator's attention: those whose status is in the attention statuses (blocked, needs_review, paused by default) plus Automata with a task held for operator review. Returns the statuses in force and the items."),
	)
}
func (s *Server) handleAutonomousAttentionList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/attention", nil)
	if err != nil {
		return toolError("autonomous_attention_list", err), nil
	}
	return textOK(string(out)), nil
}
