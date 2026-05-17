// BL302 S1/S2 — MCP Resource surface for the datawatch daemon.
//
// This file registers static resources (datawatch://) and resource templates
// on the daemon's MCP server. The REST surface (/api/mcp/resources*) is
// backed by the same list so the channel bridge can discover and proxy them.
//
// Static resources registered here (S1 + S2):
//   - datawatch://version       — daemon version + build info
//   - datawatch://config        — sanitized daemon config (no secrets)
//   - datawatch://channel/info  — channel bridge status
//   - datawatch://docs          — list of curated howto docs
//   - datawatch://sessions      — all active/recent sessions
//   - datawatch://stats         — current stats snapshot
//   - datawatch://stats/mcp     — MCPStats block
//   - datawatch://alerts        — active alerts
//   - datawatch://memory/recent — last 20 memory entries
//   - datawatch://automata      — all automata (PRDs)
//   - datawatch://council/personas — registered council personas
//   - datawatch://kg/entities   — KG entity overview
//   - datawatch://kg/triples    — recent KG triples
//
// Resource templates registered here (8 total):
//   - datawatch://docs/{path}
//   - datawatch://sessions/{id}
//   - datawatch://sessions/{id}/history
//   - datawatch://automata/{id}
//   - datawatch://automata/{id}/status
//   - datawatch://memory/{id}
//   - datawatch://alerts/{id}
//   - datawatch://council/{id}

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ResourceDescriptor is a compact description of an MCP resource for the REST surface.
type ResourceDescriptor struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MIMEType    string `json:"mime_type"`
}

// ResourceTemplateDescriptor is a compact description of an MCP resource template.
type ResourceTemplateDescriptor struct {
	URITemplate string `json:"uri_template"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MIMEType    string `json:"mime_type"`
}

// resourceRegistry holds the flat list of registered resources and templates for REST export.
// It is populated when RegisterResources() is called.
type resourceRegistry struct {
	resources []ResourceDescriptor
	templates []ResourceTemplateDescriptor
}

var globalResourceRegistry resourceRegistry

// RegisterResources registers all BL302 S1 static resources and templates on srv.
// It also populates globalResourceRegistry for REST export.
// Should be called after mcp.New() before serving.
func (s *Server) RegisterResources() {
	if s.srv == nil {
		return
	}

	// -- Static resources -----------------------------------------------

	staticResources := []struct {
		uri         string
		name        string
		description string
		mimeType    string
		handler     server.ResourceHandlerFunc
	}{
		{
			uri:         "datawatch://version",
			name:        "Daemon Version",
			description: "Current daemon version, build time, and git SHA.",
			mimeType:    "application/json",
			handler:     s.handleVersionResource,
		},
		{
			uri:         "datawatch://config",
			name:        "Daemon Config (sanitized)",
			description: "Current daemon configuration with secrets redacted.",
			mimeType:    "application/json",
			handler:     s.handleConfigResource,
		},
		{
			uri:         "datawatch://channel/info",
			name:        "Channel Bridge Info",
			description: "Channel bridge kind, port, and version.",
			mimeType:    "application/json",
			handler:     s.handleChannelInfoResource,
		},
		{
			uri:         "datawatch://docs",
			name:        "Howto Docs List",
			description: "List of all curated howto documents with path and title.",
			mimeType:    "application/json",
			handler:     s.handleDocsListResource,
		},
		// BL302 S2 — live resources backed by daemon state.
		{
			uri:         "datawatch://sessions",
			name:        "Active Sessions",
			description: "All active and recent sessions on this host.",
			mimeType:    "application/json",
			handler:     s.handleSessionsResource,
		},
		{
			uri:         "datawatch://stats",
			name:        "Daemon Stats",
			description: "Current daemon stats snapshot (sessions, memory, backends).",
			mimeType:    "application/json",
			handler:     s.handleStatsResource,
		},
		{
			uri:         "datawatch://stats/mcp",
			name:        "MCP Stats",
			description: "MCP-specific stats: resource reads, prompt calls, sampling requests.",
			mimeType:    "application/json",
			handler:     s.handleStatsMCPResource,
		},
		{
			uri:         "datawatch://alerts",
			name:        "Active Alerts",
			description: "All active and recent system alerts.",
			mimeType:    "application/json",
			handler:     s.handleAlertsResource,
		},
		{
			uri:         "datawatch://memory/recent",
			name:        "Recent Memory Entries",
			description: "Last 20 memory entries across all projects.",
			mimeType:    "application/json",
			handler:     s.handleMemoryRecentResource,
		},
		{
			uri:         "datawatch://automata",
			name:        "Automata (PRDs)",
			description: "All automata (autonomous PRDs) registered on this host.",
			mimeType:    "application/json",
			handler:     s.handleAutomataResource,
		},
		{
			uri:         "datawatch://council/personas",
			name:        "Council Personas",
			description: "Registered council personas for deliberation.",
			mimeType:    "application/json",
			handler:     s.handleCouncilPersonasResource,
		},
		{
			uri:         "datawatch://kg/entities",
			name:        "KG Entities Overview",
			description: "Knowledge graph entity statistics and overview (up to 100).",
			mimeType:    "application/json",
			handler:     s.handleKGEntitiesResource,
		},
		{
			uri:         "datawatch://kg/triples",
			name:        "Recent KG Triples",
			description: "Recent knowledge graph triples (up to 50).",
			mimeType:    "application/json",
			handler:     s.handleKGTriplesResource,
		},
	}

	for _, r := range staticResources {
		resource := mcpsdk.NewResource(r.uri, r.name,
			mcpsdk.WithResourceDescription(r.description),
			mcpsdk.WithMIMEType(r.mimeType),
		)
		s.srv.AddResource(resource, r.handler)
		globalResourceRegistry.resources = append(globalResourceRegistry.resources, ResourceDescriptor{
			URI:         r.uri,
			Name:        r.name,
			Description: r.description,
			MIMEType:    r.mimeType,
		})
	}

	// -- Resource templates ---------------------------------------------

	templates := []struct {
		uriTemplate string
		name        string
		description string
		mimeType    string
		handler     server.ResourceTemplateHandlerFunc
	}{
		{
			uriTemplate: "datawatch://docs/{path}",
			name:        "Howto Doc by Path",
			description: "Read a specific howto document by its path slug.",
			mimeType:    "text/markdown",
			handler:     s.handleDocsByPathTemplate,
		},
		{
			uriTemplate: "datawatch://sessions/{id}",
			name:        "Session by ID",
			description: "Single session state by session ID.",
			mimeType:    "application/json",
			handler:     s.handleSessionByIDTemplate,
		},
		{
			uriTemplate: "datawatch://sessions/{id}/history",
			name:        "Session Channel History",
			description: "Channel message history for a session.",
			mimeType:    "application/json",
			handler:     s.handleSessionHistoryTemplate,
		},
		{
			uriTemplate: "datawatch://automata/{id}",
			name:        "Automaton by ID",
			description: "Single automaton configuration by ID.",
			mimeType:    "application/json",
			handler:     s.handleAutomatonByIDTemplate,
		},
		{
			uriTemplate: "datawatch://automata/{id}/status",
			name:        "Automaton Execution Status",
			description: "Execution status for a specific automaton.",
			mimeType:    "application/json",
			handler:     s.handleAutomatonStatusTemplate,
		},
		{
			uriTemplate: "datawatch://memory/{id}",
			name:        "Memory Entry by ID",
			description: "Single memory entry by numeric ID.",
			mimeType:    "application/json",
			handler:     s.handleMemoryByIDTemplate,
		},
		{
			uriTemplate: "datawatch://alerts/{id}",
			name:        "Alert by ID",
			description: "Single alert by ID.",
			mimeType:    "application/json",
			handler:     s.handleAlertByIDTemplate,
		},
		{
			uriTemplate: "datawatch://council/{id}",
			name:        "Council Run by ID",
			description: "Council run state by ID.",
			mimeType:    "application/json",
			handler:     s.handleCouncilByIDTemplate,
		},
	}

	for _, t := range templates {
		tmpl := mcpsdk.NewResourceTemplate(t.uriTemplate, t.name,
			mcpsdk.WithTemplateDescription(t.description),
			mcpsdk.WithTemplateMIMEType(t.mimeType),
		)
		s.srv.AddResourceTemplate(tmpl, t.handler)
		globalResourceRegistry.templates = append(globalResourceRegistry.templates, ResourceTemplateDescriptor{
			URITemplate: t.uriTemplate,
			Name:        t.name,
			Description: t.description,
			MIMEType:    t.mimeType,
		})
	}
}

// ── Static resource handlers ───────────────────────────────────────────────

func (s *Server) handleVersionResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	data := map[string]interface{}{
		"version": s.version,
	}
	if s.latestVersion != nil {
		if latest, err := s.latestVersion(); err == nil {
			data["latest_version"] = latest
		}
	}
	out, _ := json.Marshal(data)
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
	}, nil
}

func (s *Server) handleConfigResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	raw, err := s.proxyGet("/api/config", nil)
	if err != nil {
		// Return a minimal JSON with the error rather than failing the read
		out, _ := json.Marshal(map[string]interface{}{"error": err.Error()})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	// Config is already sanitized (secrets masked) by the REST endpoint.
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleChannelInfoResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	raw, err := s.proxyGet("/api/channel/info", nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"error": err.Error()})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleDocsListResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	raw, err := s.proxyGet("/api/docs/list-howtos", nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"error": err.Error()})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

// ── Template resource handlers ─────────────────────────────────────────────

// extractTemplateParam extracts a named path segment from a datawatch:// URI.
// e.g. extractTemplateParam("datawatch://docs/mcp-resources.md", "datawatch://docs/", "") → "mcp-resources.md"
// For multi-segment templates like "datawatch://sessions/{id}/history", we strip
// the base prefix and the trailing suffix.
func extractTemplateParam(uri, prefix, suffix string) string {
	id := strings.TrimPrefix(uri, prefix)
	if suffix != "" {
		id = strings.TrimSuffix(id, suffix)
	}
	return id
}

func (s *Server) handleDocsByPathTemplate(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	path := extractTemplateParam(req.Params.URI, "datawatch://docs/", "")
	q := url.Values{}
	q.Set("path", path)
	q.Set("anchor", "")
	raw, err := s.proxyGet("/api/docs/read", q)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"error": err.Error(), "path": path})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "text/markdown", Text: string(out)},
		}, nil
	}
	// docs_read returns JSON with a "body" field; extract markdown body if possible
	var doc map[string]interface{}
	if json.Unmarshal(raw, &doc) == nil {
		if body, ok := doc["body"].(string); ok && body != "" {
			return []mcpsdk.ResourceContents{
				mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "text/markdown", Text: body},
			}, nil
		}
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "text/markdown", Text: string(raw)},
	}, nil
}

func (s *Server) handleSessionByIDTemplate(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	id := extractTemplateParam(req.Params.URI, "datawatch://sessions/", "")
	raw, err := s.proxyGet("/api/sessions/"+url.PathEscape(id), nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"error": err.Error(), "id": id})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleSessionHistoryTemplate(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	// URI: datawatch://sessions/{id}/history
	id := extractTemplateParam(req.Params.URI, "datawatch://sessions/", "/history")
	raw, err := s.proxyGet(fmt.Sprintf("/api/sessions/%s/output", url.PathEscape(id)), nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"error": err.Error(), "id": id})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleAutomatonByIDTemplate(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	id := extractTemplateParam(req.Params.URI, "datawatch://automata/", "")
	raw, err := s.proxyGet("/api/autonomous/prds/"+url.PathEscape(id), nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"error": err.Error(), "id": id})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleAutomatonStatusTemplate(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	// URI: datawatch://automata/{id}/status
	id := extractTemplateParam(req.Params.URI, "datawatch://automata/", "/status")
	raw, err := s.proxyGet("/api/autonomous/prds/"+url.PathEscape(id), nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"error": err.Error(), "id": id})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	// Extract status fields only
	var full map[string]interface{}
	if json.Unmarshal(raw, &full) == nil {
		status := map[string]interface{}{"id": id}
		for _, k := range []string{"state", "status", "progress", "current_story", "error"} {
			if v, ok := full[k]; ok {
				status[k] = v
			}
		}
		out, _ := json.Marshal(status)
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleMemoryByIDTemplate(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	id := extractTemplateParam(req.Params.URI, "datawatch://memory/", "")
	raw, err := s.proxyGet("/api/memory/"+url.PathEscape(id), nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"error": err.Error(), "id": id})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleAlertByIDTemplate(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	id := extractTemplateParam(req.Params.URI, "datawatch://alerts/", "")
	raw, err := s.proxyGet("/api/alerts/"+url.PathEscape(id), nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"error": err.Error(), "id": id})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleCouncilByIDTemplate(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	id := extractTemplateParam(req.Params.URI, "datawatch://council/", "")
	raw, err := s.proxyGet("/api/council/runs/"+url.PathEscape(id), nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"error": err.Error(), "id": id})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

// ── BL302 S2: Live resource handlers ──────────────────────────────────────
// These handlers call the REST loopback to get live daemon state.
// When the underlying subsystem is unavailable (memory not wired, KG not
// wired, etc.) the handler returns a graceful empty JSON array rather than
// an error, so MCP clients always get well-formed JSON.

func (s *Server) handleSessionsResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	raw, err := s.proxyGet("/api/sessions", nil)
	if err != nil {
		// Graceful fallback: return empty sessions array.
		out, _ := json.Marshal(map[string]interface{}{"sessions": []interface{}{}})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	// /api/sessions returns a JSON array directly; wrap in object for clarity.
	var arr []interface{}
	if json.Unmarshal(raw, &arr) == nil {
		out, _ := json.Marshal(map[string]interface{}{"sessions": arr})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleStatsResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	raw, err := s.proxyGet("/api/stats", nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"error": err.Error()})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleStatsMCPResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	raw, err := s.proxyGet("/api/stats", nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"error": err.Error()})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	// Extract mcp_stats sub-block if present; fall back to full stats.
	var full map[string]interface{}
	if json.Unmarshal(raw, &full) == nil {
		if mcpStats, ok := full["mcp_stats"]; ok {
			out, _ := json.Marshal(mcpStats)
			return []mcpsdk.ResourceContents{
				mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
			}, nil
		}
		// mcp_stats not present; return empty object.
		out, _ := json.Marshal(map[string]interface{}{})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleAlertsResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	raw, err := s.proxyGet("/api/alerts", nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"alerts": []interface{}{}})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	// /api/alerts may return an array or object; normalize to object.
	var arr []interface{}
	if json.Unmarshal(raw, &arr) == nil {
		out, _ := json.Marshal(map[string]interface{}{"alerts": arr})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleMemoryRecentResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	q := url.Values{}
	q.Set("n", "20")
	raw, err := s.proxyGet("/api/memory/list", q)
	if err != nil {
		// Memory not available — return graceful empty response.
		out, _ := json.Marshal(map[string]interface{}{"entries": []interface{}{}})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	// memory/list returns an array; wrap for consistency.
	var arr []interface{}
	if json.Unmarshal(raw, &arr) == nil {
		out, _ := json.Marshal(map[string]interface{}{"entries": arr})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleAutomataResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	raw, err := s.proxyGet("/api/autonomous/prds", nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"automata": []interface{}{}})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	// Normalize to object.
	var arr []interface{}
	if json.Unmarshal(raw, &arr) == nil {
		out, _ := json.Marshal(map[string]interface{}{"automata": arr})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	var obj map[string]interface{}
	if json.Unmarshal(raw, &obj) == nil {
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleCouncilPersonasResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	raw, err := s.proxyGet("/api/council/personas", nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"personas": []interface{}{}})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleKGEntitiesResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	// KG has no "list all entities" endpoint; use stats to show overview.
	raw, err := s.proxyGet("/api/memory/kg/stats", nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"entities": []interface{}{}, "available": false})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	// Wrap in an entities context for clarity.
	var statsObj map[string]interface{}
	if json.Unmarshal(raw, &statsObj) == nil {
		out, _ := json.Marshal(map[string]interface{}{
			"kg_stats": statsObj,
			"note":     "Use datawatch://council/{id} or memory_recall for entity-specific queries",
		})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

func (s *Server) handleKGTriplesResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	// KG WAL provides recent triples; fall back to KG stats when WAL unavailable.
	raw, err := s.proxyGet("/api/memory/wal", nil)
	if err != nil {
		out, _ := json.Marshal(map[string]interface{}{"triples": []interface{}{}, "available": false})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	// WAL may return array or object; normalize.
	var arr []interface{}
	if json.Unmarshal(raw, &arr) == nil {
		// Cap at 50 entries.
		if len(arr) > 50 {
			arr = arr[len(arr)-50:]
		}
		out, _ := json.Marshal(map[string]interface{}{"triples": arr})
		return []mcpsdk.ResourceContents{
			mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(out)},
		}, nil
	}
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
	}, nil
}

// ── REST export methods ────────────────────────────────────────────────────
// These methods satisfy the mcpBridgeAPI extension so the REST server can
// expose /api/mcp/resources, /api/mcp/resources/read, /api/mcp/resources/templates.

// ResourcesJSON returns all registered resources as JSON.
func (s *Server) ResourcesJSON() ([]byte, error) {
	result := map[string]interface{}{
		"resources": globalResourceRegistry.resources,
	}
	return json.Marshal(result)
}

// ResourceReadJSON reads a resource by URI and returns the result as JSON.
func (s *Server) ResourceReadJSON(ctx context.Context, uri string) ([]byte, error) {
	req := mcpsdk.ReadResourceRequest{
		Params: mcpsdk.ReadResourceParams{URI: uri},
	}

	// Dispatch to the appropriate handler based on URI.
	contents, err := s.dispatchResourceRead(ctx, req)
	if err != nil {
		return json.Marshal(map[string]interface{}{"error": err.Error(), "uri": uri})
	}
	result := map[string]interface{}{
		"contents": resourceContentsToMaps(contents),
		"uri":      uri,
	}
	return json.Marshal(result)
}

// ResourceTemplatesJSON returns all registered resource templates as JSON.
func (s *Server) ResourceTemplatesJSON() ([]byte, error) {
	result := map[string]interface{}{
		"templates": globalResourceRegistry.templates,
	}
	return json.Marshal(result)
}

// dispatchResourceRead dispatches a ReadResourceRequest to the matching handler.
// We implement our own dispatch here (rather than going through the MCP server's
// internal routing) because the MCPServer doesn't expose a public ReadResource method.
func (s *Server) dispatchResourceRead(ctx context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	uri := req.Params.URI

	// Check exact-match static resources first.
	switch uri {
	case "datawatch://version":
		return s.handleVersionResource(ctx, req)
	case "datawatch://config":
		return s.handleConfigResource(ctx, req)
	case "datawatch://channel/info":
		return s.handleChannelInfoResource(ctx, req)
	case "datawatch://docs":
		return s.handleDocsListResource(ctx, req)
	// BL302 S2 — live resources.
	case "datawatch://sessions":
		return s.handleSessionsResource(ctx, req)
	case "datawatch://stats":
		return s.handleStatsResource(ctx, req)
	case "datawatch://stats/mcp":
		return s.handleStatsMCPResource(ctx, req)
	case "datawatch://alerts":
		return s.handleAlertsResource(ctx, req)
	case "datawatch://memory/recent":
		return s.handleMemoryRecentResource(ctx, req)
	case "datawatch://automata":
		return s.handleAutomataResource(ctx, req)
	case "datawatch://council/personas":
		return s.handleCouncilPersonasResource(ctx, req)
	case "datawatch://kg/entities":
		return s.handleKGEntitiesResource(ctx, req)
	case "datawatch://kg/triples":
		return s.handleKGTriplesResource(ctx, req)
	}

	// Template dispatch by prefix.
	switch {
	case strings.HasPrefix(uri, "datawatch://docs/"):
		return s.handleDocsByPathTemplate(ctx, req)
	case strings.HasSuffix(uri, "/history") && strings.HasPrefix(uri, "datawatch://sessions/"):
		return s.handleSessionHistoryTemplate(ctx, req)
	case strings.HasPrefix(uri, "datawatch://sessions/"):
		return s.handleSessionByIDTemplate(ctx, req)
	case strings.HasSuffix(uri, "/status") && strings.HasPrefix(uri, "datawatch://automata/"):
		return s.handleAutomatonStatusTemplate(ctx, req)
	case strings.HasPrefix(uri, "datawatch://automata/"):
		return s.handleAutomatonByIDTemplate(ctx, req)
	case strings.HasPrefix(uri, "datawatch://memory/"):
		return s.handleMemoryByIDTemplate(ctx, req)
	case strings.HasPrefix(uri, "datawatch://alerts/"):
		return s.handleAlertByIDTemplate(ctx, req)
	case strings.HasPrefix(uri, "datawatch://council/"):
		return s.handleCouncilByIDTemplate(ctx, req)
	}

	return nil, fmt.Errorf("unknown resource URI: %s", uri)
}

// resourceContentsToMaps converts resource contents to JSON-friendly maps.
func resourceContentsToMaps(contents []mcpsdk.ResourceContents) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(contents))
	for _, c := range contents {
		if tc, ok := c.(mcpsdk.TextResourceContents); ok {
			out = append(out, map[string]interface{}{
				"uri":      tc.URI,
				"mimeType": tc.MIMEType,
				"text":     tc.Text,
			})
		}
	}
	return out
}
