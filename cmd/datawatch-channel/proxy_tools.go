// proxy_tools.go — dynamic proxy helpers for the datawatch channel bridge.
//
// At startup, discoverTools fetches all daemon MCP tools via GET /api/mcp/tools
// and registers a generic forwarding handler for each one via makeForwarder.
// Tool calls are dispatched to POST /api/mcp/call on the daemon, which executes
// the tool in-process and returns the result as JSON.
//
// BL302 S1 adds discoverResources and discoverResourceTemplates which fetch
// /api/mcp/resources and /api/mcp/resources/templates and register forwarding
// handlers that proxy to /api/mcp/resources/read?uri=<uri>.
//
// The reply tool is the only hardcoded stub — it is outbound-only and not
// reachable via the daemon tool surface.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// daemonTool is a tool descriptor returned by GET /api/mcp/tools.
type daemonTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// asMCPTool converts a daemonTool to an MCP tool using the raw JSON schema.
func (d daemonTool) asMCPTool() mcpsdk.Tool {
	return mcpsdk.NewToolWithRawSchema(d.Name, d.Description, d.InputSchema)
}

// discoverTools fetches the daemon's full MCP tool list via GET /api/mcp/tools.
func (b *bridge) discoverTools() ([]daemonTool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := b.callParent(ctx, http.MethodGet, "/api/mcp/tools", nil)
	if err != nil {
		return nil, fmt.Errorf("GET /api/mcp/tools: %w", err)
	}
	var tools []daemonTool
	if err := json.Unmarshal(out, &tools); err != nil {
		return nil, fmt.Errorf("parse tool list: %w", err)
	}
	return tools, nil
}

// ── Resource discovery + forwarding (BL302 S1) ────────────────────────────

// daemonResource describes a resource returned by GET /api/mcp/resources.
type daemonResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MIMEType    string `json:"mime_type"`
}

// daemonResourceTemplate describes a resource template from GET /api/mcp/resources/templates.
type daemonResourceTemplate struct {
	URITemplate string `json:"uri_template"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MIMEType    string `json:"mime_type"`
}

// asMCPResource converts a daemonResource to an MCP Resource.
func (d daemonResource) asMCPResource() mcpsdk.Resource {
	return mcpsdk.NewResource(d.URI, d.Name,
		mcpsdk.WithResourceDescription(d.Description),
		mcpsdk.WithMIMEType(d.MIMEType),
	)
}

// asMCPTemplate converts a daemonResourceTemplate to an MCP ResourceTemplate.
func (d daemonResourceTemplate) asMCPTemplate() mcpsdk.ResourceTemplate {
	return mcpsdk.NewResourceTemplate(d.URITemplate, d.Name,
		mcpsdk.WithTemplateDescription(d.Description),
		mcpsdk.WithTemplateMIMEType(d.MIMEType),
	)
}

// discoverResources fetches the daemon's resource list via GET /api/mcp/resources.
func (b *bridge) discoverResources() ([]daemonResource, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := b.callParent(ctx, http.MethodGet, "/api/mcp/resources", nil)
	if err != nil {
		return nil, fmt.Errorf("GET /api/mcp/resources: %w", err)
	}
	var envelope struct {
		Resources []daemonResource `json:"resources"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, fmt.Errorf("parse resources: %w", err)
	}
	return envelope.Resources, nil
}

// discoverResourceTemplates fetches the daemon's resource templates via GET /api/mcp/resources/templates.
func (b *bridge) discoverResourceTemplates() ([]daemonResourceTemplate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := b.callParent(ctx, http.MethodGet, "/api/mcp/resources/templates", nil)
	if err != nil {
		return nil, fmt.Errorf("GET /api/mcp/resources/templates: %w", err)
	}
	var envelope struct {
		Templates []daemonResourceTemplate `json:"templates"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, fmt.Errorf("parse resource templates: %w", err)
	}
	return envelope.Templates, nil
}

// makeResourceForwarder returns a ResourceHandlerFunc that proxies to /api/mcp/resources/read?uri=<uri>.
func (b *bridge) makeResourceForwarder(uri string) server.ResourceHandlerFunc {
	return func(ctx context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
		target := "/api/mcp/resources/read?uri=" + uri
		out, err := b.callParent(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, fmt.Errorf("resource read %s: %w", uri, err)
		}
		// Decode the envelope and return TextResourceContents.
		var envelope struct {
			URI      string `json:"uri"`
			Contents []struct {
				URI      string `json:"uri"`
				MIMEType string `json:"mimeType"`
				Text     string `json:"text"`
			} `json:"contents"`
		}
		if err := json.Unmarshal(out, &envelope); err != nil {
			// Fallback: return raw text
			return []mcpsdk.ResourceContents{
				mcpsdk.TextResourceContents{URI: uri, MIMEType: "application/json", Text: string(out)},
			}, nil
		}
		var contents []mcpsdk.ResourceContents
		for _, c := range envelope.Contents {
			contents = append(contents, mcpsdk.TextResourceContents{
				URI:      c.URI,
				MIMEType: c.MIMEType,
				Text:     c.Text,
			})
		}
		if len(contents) == 0 {
			contents = []mcpsdk.ResourceContents{
				mcpsdk.TextResourceContents{URI: uri, MIMEType: "application/json", Text: string(out)},
			}
		}
		return contents, nil
	}
}

// makeTemplateForwarder returns a ResourceTemplateHandlerFunc that proxies to /api/mcp/resources/read?uri=<resolved-uri>.
func (b *bridge) makeTemplateForwarder() server.ResourceTemplateHandlerFunc {
	return func(ctx context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
		// The resolved URI comes from req.Params.URI (the client's actual URI, e.g. datawatch://sessions/abc123)
		resolvedURI := req.Params.URI
		target := "/api/mcp/resources/read?uri=" + resolvedURI
		out, err := b.callParent(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, fmt.Errorf("resource read %s: %w", resolvedURI, err)
		}
		var envelope struct {
			URI      string `json:"uri"`
			Contents []struct {
				URI      string `json:"uri"`
				MIMEType string `json:"mimeType"`
				Text     string `json:"text"`
			} `json:"contents"`
		}
		if err := json.Unmarshal(out, &envelope); err != nil {
			return []mcpsdk.ResourceContents{
				mcpsdk.TextResourceContents{URI: resolvedURI, MIMEType: "application/json", Text: string(out)},
			}, nil
		}
		var contents []mcpsdk.ResourceContents
		for _, c := range envelope.Contents {
			contents = append(contents, mcpsdk.TextResourceContents{
				URI:      c.URI,
				MIMEType: c.MIMEType,
				Text:     c.Text,
			})
		}
		if len(contents) == 0 {
			contents = []mcpsdk.ResourceContents{
				mcpsdk.TextResourceContents{URI: resolvedURI, MIMEType: "application/json", Text: string(out)},
			}
		}
		return contents, nil
	}
}

// ── Prompt discovery + forwarding (BL302 S4) ──────────────────────────────

// daemonPrompt describes a prompt returned by GET /api/mcp/prompts.
type daemonPrompt struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Arguments   []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Required    bool   `json:"required"`
	} `json:"arguments,omitempty"`
}

// asMCPPrompt converts a daemonPrompt to an MCP Prompt.
func (d daemonPrompt) asMCPPrompt() mcpsdk.Prompt {
	opts := []mcpsdk.PromptOption{
		mcpsdk.WithPromptDescription(d.Description),
	}
	for _, a := range d.Arguments {
		var argOpts []mcpsdk.ArgumentOption
		argOpts = append(argOpts, mcpsdk.ArgumentDescription(a.Description))
		if a.Required {
			argOpts = append(argOpts, mcpsdk.RequiredArgument())
		}
		opts = append(opts, mcpsdk.WithArgument(a.Name, argOpts...))
	}
	return mcpsdk.NewPrompt(d.Name, opts...)
}

// discoverPrompts fetches the daemon's prompt list via GET /api/mcp/prompts.
func (b *bridge) discoverPrompts() ([]daemonPrompt, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := b.callParent(ctx, http.MethodGet, "/api/mcp/prompts", nil)
	if err != nil {
		return nil, fmt.Errorf("GET /api/mcp/prompts: %w", err)
	}
	var envelope struct {
		Prompts []daemonPrompt `json:"prompts"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, fmt.Errorf("parse prompts: %w", err)
	}
	return envelope.Prompts, nil
}

// makePromptForwarder returns a PromptHandlerFunc that proxies to POST /api/mcp/prompts/get.
func (b *bridge) makePromptForwarder(name string) server.PromptHandlerFunc {
	return func(ctx context.Context, req mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
		args := req.Params.Arguments
		if args == nil {
			args = map[string]string{}
		}
		payload := map[string]any{
			"name":      name,
			"arguments": args,
		}
		out, err := b.callParent(ctx, http.MethodPost, "/api/mcp/prompts/get", payload)
		if err != nil {
			return nil, fmt.Errorf("prompt %s: %w", name, err)
		}
		// Decode the response and reconstruct a GetPromptResult.
		var env struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Messages    []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(out, &env); err != nil {
			// Return raw text as a single user message.
			return &mcpsdk.GetPromptResult{
				Messages: []mcpsdk.PromptMessage{
					mcpsdk.NewPromptMessage(mcpsdk.RoleUser, mcpsdk.TextContent{Type: "text", Text: string(out)}),
				},
			}, nil
		}
		if env.Error != "" {
			return nil, fmt.Errorf("prompt %s: %s", name, env.Error)
		}
		msgs := make([]mcpsdk.PromptMessage, 0, len(env.Messages))
		for _, m := range env.Messages {
			role := mcpsdk.RoleUser
			if m.Role == "assistant" {
				role = mcpsdk.RoleAssistant
			}
			msgs = append(msgs, mcpsdk.NewPromptMessage(role, mcpsdk.TextContent{Type: "text", Text: m.Content}))
		}
		return &mcpsdk.GetPromptResult{
			Description: env.Description,
			Messages:    msgs,
		}, nil
	}
}

// makeForwarder returns a ToolHandlerFunc that forwards the call to POST /api/mcp/call.
func (b *bridge) makeForwarder(toolName string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		if args == nil {
			args = map[string]any{}
		}
		payload := map[string]any{
			"tool": toolName,
			"args": args,
		}
		out, err := b.callParent(ctx, http.MethodPost, "/api/mcp/call", payload)
		if err != nil {
			return mcpsdk.NewToolResultError(fmt.Sprintf("%s: %v", toolName, err)), nil
		}
		// Decode and relay the daemon's result.
		var result struct {
			Content []json.RawMessage `json:"content"`
			IsError bool              `json:"isError"`
		}
		if err := json.Unmarshal(out, &result); err != nil {
			return mcpsdk.NewToolResultText(string(out)), nil
		}
		var content []mcpsdk.Content
		for _, raw := range result.Content {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				continue
			}
			if typ, _ := m["type"].(string); typ == "text" {
				text, _ := m["text"].(string)
				content = append(content, mcpsdk.TextContent{Type: "text", Text: text})
			}
		}
		if len(content) == 0 {
			content = []mcpsdk.Content{mcpsdk.TextContent{Type: "text", Text: string(out)}}
		}
		return &mcpsdk.CallToolResult{Content: content, IsError: result.IsError}, nil
	}
}
