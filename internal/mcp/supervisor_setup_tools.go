package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// Supervisor set-up tools. Each proxies the daemon's /api/supervisor-setup
// routes, so the daemon's validation stays the single source of truth.

func (s *Server) toolSupervisorSetup() mcpsdk.Tool {
	return mcpsdk.NewTool("supervisor_setup",
		mcpsdk.WithDescription(`Idempotent set-up of a named project's supervisor: launch role supervisor, the project directory, an explicit model (Sonnet or Haiku only; Opus and an empty model are refused), a generated handover and one recurring spawn schedule per schedule name. Calling it again adds nothing — existing schedules are reported with created=false. project and model are required. schedules is optional: comma-separated name[=cron] pairs (default one "watch" schedule every twenty minutes), e.g. "watch=*/10 * * * *;full=0 * * * *" — separate pairs with ";". The same request as POST /api/supervisor-setup.`),
		mcpsdk.WithString("project", mcpsdk.Description("Required — the project name.")),
		mcpsdk.WithString("model", mcpsdk.Description("Required — an explicit Sonnet or Haiku model name.")),
		mcpsdk.WithString("schedules", mcpsdk.Description(`Optional — ";"-separated name[=cron] pairs.`)),
	)
}

func (s *Server) toolSupervisorState() mcpsdk.Tool {
	return mcpsdk.NewTool("supervisor_state",
		mcpsdk.WithDescription(`Read or edit a supervisor's pinned state record. action is one of get, set, change, remove. get needs project (role defaults to supervisor). set needs project, id, section (decision, work, checked or correction), who and why, plus optional text, owner, next_step; writing identical content records no edit. change needs project, id, who, why and any of text, owner, next_step it should alter. remove needs project, id, who, why (removing an absent entry is a no-op). The same requests as /api/supervisor-setup/state.`),
		mcpsdk.WithString("action", mcpsdk.Description("Required — get, set, change or remove.")),
		mcpsdk.WithString("project", mcpsdk.Description("Required — the project name.")),
		mcpsdk.WithString("role", mcpsdk.Description("Optional — the record role (default supervisor).")),
		mcpsdk.WithString("id", mcpsdk.Description("Entry id (set, change, remove).")),
		mcpsdk.WithString("section", mcpsdk.Description("Entry section for set: decision, work, checked or correction.")),
		mcpsdk.WithString("text", mcpsdk.Description("Entry text.")),
		mcpsdk.WithString("owner", mcpsdk.Description("Entry owner.")),
		mcpsdk.WithString("next_step", mcpsdk.Description("Entry next step.")),
		mcpsdk.WithString("who", mcpsdk.Description("Who is making the edit (set, change, remove).")),
		mcpsdk.WithString("why", mcpsdk.Description("Why the edit is made (set, change, remove).")),
	)
}

func (s *Server) toolSupervisorCorrection() mcpsdk.Tool {
	return mcpsdk.NewTool("supervisor_correction",
		mcpsdk.WithDescription(`Record, list or promote the corrections the operator gave a supervisor. action is one of list, record, promote. list needs project. record needs project, id, text, class (state, standing_rule or product_defect), who and why, plus optional evidence and workaround. promote commits a standing_rule correction to the project rules pack and needs project, id, who and why; other classes are refused. The same requests as /api/supervisor-setup/corrections.`),
		mcpsdk.WithString("action", mcpsdk.Description("Required — list, record or promote.")),
		mcpsdk.WithString("project", mcpsdk.Description("Required — the project name.")),
		mcpsdk.WithString("role", mcpsdk.Description("Optional — the record role (default supervisor).")),
		mcpsdk.WithString("id", mcpsdk.Description("Correction id (record, promote).")),
		mcpsdk.WithString("text", mcpsdk.Description("Correction text (record).")),
		mcpsdk.WithString("class", mcpsdk.Description("state, standing_rule or product_defect (record).")),
		mcpsdk.WithString("evidence", mcpsdk.Description("Optional evidence (record).")),
		mcpsdk.WithString("workaround", mcpsdk.Description("Optional workaround (record).")),
		mcpsdk.WithString("who", mcpsdk.Description("Who is acting (record, promote).")),
		mcpsdk.WithString("why", mcpsdk.Description("Why (record, promote).")),
	)
}

// SupervisorSetupToolNames lists the supervisor set-up tools, for the parity test.
func SupervisorSetupToolNames() []string {
	return []string{"supervisor_setup", "supervisor_state", "supervisor_correction"}
}

func (s *Server) handleSupervisorSetup(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"project": strings.TrimSpace(req.GetString("project", "")),
		"model":   strings.TrimSpace(req.GetString("model", "")),
	}
	if raw := strings.TrimSpace(req.GetString("schedules", "")); raw != "" {
		list := []map[string]string{}
		for _, pair := range strings.Split(raw, ";") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			name, cron, _ := strings.Cut(pair, "=")
			list = append(list, map[string]string{"name": strings.TrimSpace(name), "cron": strings.TrimSpace(cron)})
		}
		body["schedules"] = list
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/supervisor-setup", body)
	if err != nil {
		return toolError("supervisor_setup", err), nil
	}
	return textOK(string(out)), nil
}

func supervisorArgs(req mcpsdk.CallToolRequest, keys ...string) map[string]any {
	body := map[string]any{}
	args := req.GetArguments()
	for _, k := range keys {
		if _, ok := args[k]; !ok {
			continue
		}
		body[k] = req.GetString(k, "")
	}
	return body
}

func (s *Server) handleSupervisorState(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	action := strings.TrimSpace(req.GetString("action", ""))
	const path = "/api/supervisor-setup/state"
	var out []byte
	var err error
	switch action {
	case "get":
		q := url.Values{}
		q.Set("project", strings.TrimSpace(req.GetString("project", "")))
		if r := strings.TrimSpace(req.GetString("role", "")); r != "" {
			q.Set("role", r)
		}
		out, err = s.proxyGet(path, q)
	case "set":
		out, err = s.proxyJSON(http.MethodPost, path, supervisorArgs(req, "project", "role", "id", "section", "text", "owner", "next_step", "who", "why"))
	case "change":
		out, err = s.proxyJSON(http.MethodPatch, path, supervisorArgs(req, "project", "role", "id", "text", "owner", "next_step", "who", "why"))
	case "remove":
		out, err = s.proxyJSON(http.MethodDelete, path, supervisorArgs(req, "project", "role", "id", "who", "why"))
	default:
		return toolErrorf("supervisor_state", "action must be get, set, change or remove, got %q", action), nil
	}
	if err != nil {
		return toolError("supervisor_state", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleSupervisorCorrection(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	action := strings.TrimSpace(req.GetString("action", ""))
	const path = "/api/supervisor-setup/corrections"
	var out []byte
	var err error
	switch action {
	case "list":
		q := url.Values{}
		q.Set("project", strings.TrimSpace(req.GetString("project", "")))
		if r := strings.TrimSpace(req.GetString("role", "")); r != "" {
			q.Set("role", r)
		}
		out, err = s.proxyGet(path, q)
	case "record":
		out, err = s.proxyJSON(http.MethodPost, path, supervisorArgs(req, "project", "role", "id", "text", "class", "evidence", "workaround", "who", "why"))
	case "promote":
		out, err = s.proxyJSON(http.MethodPost, path+"/promote", supervisorArgs(req, "project", "role", "id", "who", "why"))
	default:
		return toolErrorf("supervisor_correction", "action must be list, record or promote, got %q", action), nil
	}
	if err != nil {
		return toolError("supervisor_correction", err), nil
	}
	return textOK(string(out)), nil
}
