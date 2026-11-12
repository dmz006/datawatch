package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// The requirements surface tools proxy the REST routes:
//
//	automation_requirements_report        GET /api/automation/requirements-report
//	automation_model_pull_consent_get    GET /api/automation/model-consent
//	automation_model_pull_consent_set    PUT /api/automation/model-consent
//	compute_node_backend_versions        GET /api/compute/nodes/{name}/versions
//
// The report text is rendered by the autonomous package, the same renderer
// the REST route, the CLI and the comm verb use. The consent set tool sends
// the complete policy (same shape and rejection rules as the REST PUT).

// RequirementsSurfacesToolNames lists the requirements surface MCP tools,
// for the parity test.
func RequirementsSurfacesToolNames() []string {
	return []string{
		"automation_requirements_report",
		"automation_model_pull_consent_get",
		"automation_model_pull_consent_set",
		"compute_node_backend_versions",
	}
}

// ── automation_requirements_report ──────────────────────────────────────

func (s *Server) toolAutomationRequirementsReport() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_requirements_report",
		mcpsdk.WithDescription("Read the automation requirements report: the Ollama minimum per capability (with operator overrides), each known node's reported version against it, the configured execution/planning/verification backends, and whether at least one of the two optional providers (Claude, local model pool) is configured. Read-only; same data as GET /api/automation/requirements-report. format is text (the default) or json."),
		mcpsdk.WithString("format", mcpsdk.Description("Optional — text (default) or json")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutomationRequirementsReport(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	format := "text"
	if v, ok := req.GetArguments()["format"].(string); ok && strings.TrimSpace(v) != "" {
		format = strings.ToLower(strings.TrimSpace(v))
	}
	if format != "text" && format != "json" {
		return toolError("automation_requirements_report", fmt.Errorf("format must be text or json, got %q", format)), nil
	}
	out, err := s.proxyGet("/api/automation/requirements-report", url.Values{"format": {"json"}})
	if err != nil {
		return toolError("automation_requirements_report", err), nil
	}
	if format == "json" {
		return textOK(string(out)), nil
	}
	var rep autonomous.RequirementsReport
	if err := json.Unmarshal(out, &rep); err != nil {
		return toolError("automation_requirements_report", fmt.Errorf("decode report: %w", err)), nil
	}
	return textOK(rep.Text()), nil
}

// ── automation_model_pull_consent_get ───────────────────────────────────

func (s *Server) toolModelPullConsentGet() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_model_pull_consent_get",
		mcpsdk.WithDescription("Read the model-pull consent policy: the model list (name, allowed, note), the publisher allow-list, and the decision rule the gate implements in words. Read-only; same data as GET /api/automation/model-consent."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleModelPullConsentGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/automation/model-consent", nil)
	if err != nil {
		return toolError("automation_model_pull_consent_get", err), nil
	}
	var view struct {
		Models       []config.ModelConsentEntry `json:"models"`
		Publishers   []string                   `json:"publishers"`
		DecisionRule string                     `json:"decision_rule"`
	}
	if err := json.Unmarshal(out, &view); err != nil {
		return toolError("automation_model_pull_consent_get", fmt.Errorf("decode consent: %w", err)), nil
	}
	var sb strings.Builder
	sb.WriteString("Model pull consent\n")
	sb.WriteString("  rule: " + view.DecisionRule + "\n")
	if len(view.Models) == 0 {
		sb.WriteString("  models: (none listed)\n")
	} else {
		sb.WriteString("  models:\n")
		for _, m := range view.Models {
			allowed := "allowed"
			if !m.Allowed {
				allowed = "refused"
			}
			line := "    " + m.Name + " " + allowed
			if m.Note != "" {
				line += " — " + m.Note
			}
			sb.WriteString(line + "\n")
		}
	}
	if len(view.Publishers) == 0 {
		sb.WriteString("  publishers: (none allow-listed)\n")
	} else {
		sb.WriteString("  publishers: " + strings.Join(view.Publishers, ", ") + "\n")
	}
	return textOK(sb.String()), nil
}

// ── automation_model_pull_consent_set ───────────────────────────────────

const consentSetModelsDesc = `JSON array — the complete model list to store (it replaces the current list). Each item is a model name string (stored as allowed: true) or an object {"name": "...", "allowed": true|false, "note": "optional note"}. Example: ["qwen3:8b", {"name":"llama3.1:70b","allowed":false,"note":"too big for the local pool"}]`

const consentSetPublishersDesc = `JSON array of publisher names — the complete publisher allow-list to store (it replaces the current list). A model not listed individually is pullable when its publisher is on this list. Example: ["qwen", "mistral"]`

func (s *Server) toolModelPullConsentSet() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_model_pull_consent_set",
		mcpsdk.WithDescription("Set the model-pull consent policy (replaces the current one). models is the complete model list; publishers is the complete publisher allow-list. A model pull is allowed when the model is listed with allowed true, or when the model is unlisted and its publisher is allow-listed; every other pull is refused. Invalid entries are rejected with 400 (e.g. a duplicated model name). Same shape and rejection rules as PUT /api/automation/model-consent. Audited as model_pull_consent_changed."),
		mcpsdk.WithString("models", mcpsdk.Description(consentSetModelsDesc)),
		mcpsdk.WithString("publishers", mcpsdk.Description(consentSetPublishersDesc)),
	)
}

func (s *Server) handleModelPullConsentSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	args := req.GetArguments()
	modelsRaw, _ := args["models"].(string)
	publishersRaw, _ := args["publishers"].(string)

	var models []config.ModelConsentEntry
	if strings.TrimSpace(modelsRaw) != "" {
		var parsed []any
		if err := json.Unmarshal([]byte(modelsRaw), &parsed); err != nil {
			return toolError("automation_model_pull_consent_set", fmt.Errorf("models must be a JSON array: %v", err)), nil
		}
		for _, item := range parsed {
			switch v := item.(type) {
			case string:
				name := strings.TrimSpace(v)
				if name == "" {
					return toolError("automation_model_pull_consent_set", fmt.Errorf("empty model name in models list")), nil
				}
				models = append(models, config.ModelConsentEntry{Name: name, Allowed: true})
			case map[string]any:
				entry, ok := modelConsentEntryFromJSON(v)
				if !ok {
					return toolError("automation_model_pull_consent_set", fmt.Errorf("model objects need a non-empty \"name\"")), nil
				}
				models = append(models, entry)
			default:
				return toolError("automation_model_pull_consent_set", fmt.Errorf("models items are name strings or {name, allowed, note} objects")), nil
			}
		}
	}
	var publishers []string
	if strings.TrimSpace(publishersRaw) != "" {
		if err := json.Unmarshal([]byte(publishersRaw), &publishers); err != nil {
			return toolError("automation_model_pull_consent_set", fmt.Errorf("publishers must be a JSON array of names: %v", err)), nil
		}
	}

	body, _ := json.Marshal(config.ModelPullConsent{Models: models, Publishers: publishers})
	out, err := s.proxyJSON("PUT", "/api/automation/model-consent", []byte(body))
	if err != nil {
		return toolError("automation_model_pull_consent_set", err), nil
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out, &resp); err == nil && resp.Status == "ok" {
		return textOK(fmt.Sprintf("model pull consent set: %d model(s), %d publisher(s)", len(models), len(publishers))), nil
	}
	return textOK(string(out)), nil
}

func modelConsentEntryFromJSON(v map[string]any) (config.ModelConsentEntry, bool) {
	name, _ := v["name"].(string)
	if strings.TrimSpace(name) == "" {
		return config.ModelConsentEntry{}, false
	}
	entry := config.ModelConsentEntry{Name: strings.TrimSpace(name)}
	if allowed, ok := v["allowed"].(bool); ok {
		entry.Allowed = allowed
	}
	if note, ok := v["note"].(string); ok {
		entry.Note = note
	}
	return entry, true
}

// ── compute_node_backend_versions ───────────────────────────────────────

func (s *Server) toolComputeNodeBackendVersions() mcpsdk.Tool {
	return mcpsdk.NewTool("compute_node_backend_versions",
		mcpsdk.WithDescription("Read a compute node's identity and reported backend version (with when it was recorded), and per-capability Ollama minimums applied to that version: each line says whether the version meets the minimum, fails below it, or is unknown (which fails closed). Ollama minimums apply to Ollama nodes and to nodes whose version was probed against Ollama. Requires a known node name (404 otherwise). Same data as GET /api/compute/nodes/{name}/versions."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Compute node name (as listed by compute_nodes)")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleComputeNodeBackendVersions(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name, _ := req.GetArguments()["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return toolError("compute_node_backend_versions", fmt.Errorf("name is required (a known compute node name)")), nil
	}
	path := "/api/compute/nodes/" + url.PathEscape(name) + "/versions"
	out, err := s.proxyGet(path, nil)
	if err != nil {
		return toolError("compute_node_backend_versions", err), nil
	}
	var view struct {
		Name            string `json:"name"`
		Kind            string `json:"kind"`
		Address         string `json:"address"`
		Backend         string `json:"backend"`
		BackendVersion  string `json:"backend_version"`
		VersionProbedAt string `json:"version_probed_at"`
		Capabilities    []struct {
			Capability string `json:"capability"`
			Minimum    string `json:"minimum"`
			OK         *bool  `json:"ok"`
			Unknown    bool   `json:"unknown"`
			Reason     string `json:"reason"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(out, &view); err != nil {
		return toolError("compute_node_backend_versions", fmt.Errorf("decode node versions: %w", err)), nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Node %s (kind %s, %s)\n", view.Name, view.Kind, view.Address)
	if view.Backend != "" {
		fmt.Fprintf(&sb, "  backend: %s %s", view.Backend, view.BackendVersion)
		if view.VersionProbedAt != "" {
			sb.WriteString(" (recorded " + view.VersionProbedAt + ")")
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("  backend: no version recorded\n")
	}
	if len(view.Capabilities) == 0 {
		sb.WriteString("  no capability minimums apply to this node (not an Ollama backend, or no version recorded)\n")
		return textOK(sb.String()), nil
	}
	for _, c := range view.Capabilities {
		if c.Unknown {
			fmt.Fprintf(&sb, "  %s: version unknown (requirement %s) — unknown versions fail closed%s\n", c.Capability, c.Minimum, reasonTail(c.Reason))
			continue
		}
		state := "fails the minimum"
		if c.OK != nil && *c.OK {
			state = "meets the minimum"
		}
		fmt.Fprintf(&sb, "  %s: requirement %s — %s%s\n", c.Capability, c.Minimum, state, reasonTail(c.Reason))
	}
	return textOK(sb.String()), nil
}

func reasonTail(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return ""
	}
	return " — " + strings.TrimSpace(reason)
}
