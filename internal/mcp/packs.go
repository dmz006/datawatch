// packs — MCP tools for installable task and Automaton template packs.
// Each tool proxies the matching REST route under /api/packs, so the daemon's
// handlers (which hold the config, trust store and pack folders) stay the one
// place the work happens and the MCP server never keeps a second copy.

package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolPacksList() mcpsdk.Tool {
	return mcpsdk.NewTool("packs_list",
		mcpsdk.WithDescription("Lists the installed task and Automaton template packs with name, version, digest, whether the operator has accepted them, and a summary."),
	)
}

func (s *Server) handlePacksList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/packs", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPacksSettingGet() mcpsdk.Tool {
	return mcpsdk.NewTool("packs_setting_get",
		mcpsdk.WithDescription("Reads the packs setting: {enabled, active[]}. enabled is the global switch; active lists the pack names in use."),
	)
}

func (s *Server) handlePacksSettingGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/packs/config", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPacksSettingSet() mcpsdk.Tool {
	return mcpsdk.NewTool("packs_setting_set",
		mcpsdk.WithDescription("Changes the packs setting. Give enabled, active, or both; a field left out is unchanged. Every active name must be an installed pack."),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Turn the packs feature on or off.")),
		mcpsdk.WithArray("active", mcpsdk.Description("Names of the packs to use. An empty list clears them."), mcpsdk.WithStringItems()),
	)
}

func (s *Server) handlePacksSettingSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	args := req.GetArguments()
	body := map[string]any{}
	if v, ok := args["enabled"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			return mcpsdk.NewToolResultError("enabled must be a boolean"), nil
		}
		body["enabled"] = b
	}
	if _, ok := args["active"]; ok {
		active := req.GetStringSlice("active", nil)
		if active == nil {
			active = []string{}
		}
		body["active"] = active
	}
	if len(body) == 0 {
		return mcpsdk.NewToolResultError("give enabled, active, or both"), nil
	}
	out, err := s.proxyJSON("PUT", "/api/packs/config", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// packsInputsQuery validates and builds the query shared by the inputs tools.
func packsInputsQuery(req mcpsdk.CallToolRequest) (url.Values, string) {
	level := strings.TrimSpace(req.GetString("level", ""))
	key := strings.TrimSpace(req.GetString("key", ""))
	pack := strings.TrimSpace(req.GetString("pack", ""))
	switch level {
	case "global", "project", "automaton":
	default:
		return nil, "level must be global, project or automaton"
	}
	if level != "global" && key == "" {
		return nil, fmt.Sprintf("key is required for the %s level", level)
	}
	if pack == "" {
		return nil, "pack is required"
	}
	q := url.Values{}
	q.Set("level", level)
	q.Set("pack", pack)
	if key != "" {
		q.Set("key", key)
	}
	return q, ""
}

func (s *Server) toolPacksInputsGet() mcpsdk.Tool {
	return mcpsdk.NewTool("packs_inputs_get",
		mcpsdk.WithDescription("Reads the input values a pack has at one level. Secret inputs come back redacted."),
		mcpsdk.WithString("level", mcpsdk.Required(), mcpsdk.Description("global, project or automaton.")),
		mcpsdk.WithString("key", mcpsdk.Description("Project or Automaton key. Required unless level is global.")),
		mcpsdk.WithString("pack", mcpsdk.Required(), mcpsdk.Description("Pack name, or builtin.")),
	)
}

func (s *Server) handlePacksInputsGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q, msg := packsInputsQuery(req)
	if msg != "" {
		return mcpsdk.NewToolResultError(msg), nil
	}
	out, err := s.proxyGet("/api/packs/inputs", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPacksInputsSet() mcpsdk.Tool {
	return mcpsdk.NewTool("packs_inputs_set",
		mcpsdk.WithDescription("Replaces the input values a pack has at one level. Every name must be an input the pack declares."),
		mcpsdk.WithString("level", mcpsdk.Required(), mcpsdk.Description("global, project or automaton.")),
		mcpsdk.WithString("key", mcpsdk.Description("Project or Automaton key. Required unless level is global.")),
		mcpsdk.WithString("pack", mcpsdk.Required(), mcpsdk.Description("Pack name, or builtin.")),
		mcpsdk.WithObject("inputs", mcpsdk.Required(), mcpsdk.Description("Map of input name to string value.")),
	)
}

func (s *Server) handlePacksInputsSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q, msg := packsInputsQuery(req)
	if msg != "" {
		return mcpsdk.NewToolResultError(msg), nil
	}
	raw, ok := req.GetArguments()["inputs"].(map[string]any)
	if !ok {
		return mcpsdk.NewToolResultError("inputs must be an object of name to string value"), nil
	}
	inputs := make(map[string]string, len(raw))
	for k, v := range raw {
		str, isStr := v.(string)
		if !isStr {
			return mcpsdk.NewToolResultError("input " + k + " must be a string"), nil
		}
		inputs[k] = str
	}
	out, err := s.proxyJSON("PUT", "/api/packs/inputs?"+q.Encode(), map[string]any{"inputs": inputs})
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPacksInstall() mcpsdk.Tool {
	return mcpsdk.NewTool("packs_install",
		mcpsdk.WithDescription("Installs a pack from a folder on the daemon host. The pack is not usable until it is accepted."),
		mcpsdk.WithString("source_dir", mcpsdk.Required(), mcpsdk.Description("Folder holding the pack.")),
	)
}

func (s *Server) handlePacksInstall(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	dir := strings.TrimSpace(req.GetString("source_dir", ""))
	if dir == "" {
		return mcpsdk.NewToolResultError("source_dir is required"), nil
	}
	out, err := s.proxyJSON("POST", "/api/packs/install", map[string]any{"source_dir": dir})
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPacksAccept() mcpsdk.Tool {
	return mcpsdk.NewTool("packs_accept",
		mcpsdk.WithDescription("Accepts an installed pack version so it may run. Records the pack digest in the trust store."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Pack name.")),
		mcpsdk.WithString("version", mcpsdk.Required(), mcpsdk.Description("Pack version.")),
	)
}

func (s *Server) handlePacksAccept(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := strings.TrimSpace(req.GetString("name", ""))
	version := strings.TrimSpace(req.GetString("version", ""))
	if name == "" || version == "" {
		return mcpsdk.NewToolResultError("name and version are required"), nil
	}
	out, err := s.proxyJSON("POST", "/api/packs/accept", map[string]any{"name": name, "version": version})
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPacksRemove() mcpsdk.Tool {
	return mcpsdk.NewTool("packs_remove",
		mcpsdk.WithDescription("Removes one installed version of a pack."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Pack name.")),
		mcpsdk.WithString("version", mcpsdk.Required(), mcpsdk.Description("Pack version.")),
	)
}

func (s *Server) handlePacksRemove(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := strings.TrimSpace(req.GetString("name", ""))
	version := strings.TrimSpace(req.GetString("version", ""))
	if name == "" || version == "" {
		return mcpsdk.NewToolResultError("name and version are required"), nil
	}
	if strings.ContainsAny(name+version, `/\`) || name == ".." || version == ".." {
		return mcpsdk.NewToolResultError("unsafe name or version"), nil
	}
	out, err := s.proxyJSON("DELETE", "/api/packs/"+url.PathEscape(name)+"/"+url.PathEscape(version), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
