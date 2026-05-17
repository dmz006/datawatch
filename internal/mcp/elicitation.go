// Package mcp — BL302 S3: daemon-initiated elicitation infrastructure.
//
// ElicitationDispatcher wraps the MCP SDK's RequestElicitation call.
// Three named schemas are pre-registered at startup:
//
//   - "approval"   — {action: approve|reject|modify, note?: string}
//   - "text_input" — {text: string}
//   - "choice"     — {selected: string} (enum populated at request time)
//
// When no MCP client is connected the dispatcher returns
// server.ErrElicitationNotSupported; callers degrade gracefully.
package mcp

import (
	"context"
	"errors"
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dmz006/datawatch/internal/stats"
)

// ErrElicitationNotSupported is returned when no MCP client with elicitation
// capability is currently connected.
var ErrElicitationNotSupported = server.ErrElicitationNotSupported

// ElicitationRequest is the datawatch-layer request type.
type ElicitationRequest struct {
	// Schema is one of the registered schema names: "approval", "text_input", "choice".
	Schema string `json:"schema"`
	// Message is the human-readable prompt shown to the user.
	Message string `json:"message"`
	// Options is used by the "choice" schema to populate the enum.
	Options []string `json:"options,omitempty"`
}

// ElicitationResult holds the result returned by the user.
type ElicitationResult struct {
	// Action is the user's response: "accept", "decline", or "cancel".
	Action string `json:"action"`
	// Content holds the schema-specific content if Action=="accept".
	Content map[string]any `json:"content,omitempty"`
}

// elicitationSchema is a registered JSON Schema definition.
type elicitationSchema struct {
	// schema is the JSON Schema map passed to the MCP SDK.
	schema map[string]any
}

// ElicitationDispatcher dispatches daemon-initiated elicitation requests.
type ElicitationDispatcher struct {
	srv     *server.MCPServer
	stats   *stats.MCPStatsCounters
	schemas map[string]elicitationSchema
}

// NewElicitationDispatcher creates an ElicitationDispatcher and registers
// the three built-in schemas.
func NewElicitationDispatcher(srv *server.MCPServer, sc *stats.MCPStatsCounters) *ElicitationDispatcher {
	d := &ElicitationDispatcher{
		srv:     srv,
		stats:   sc,
		schemas: make(map[string]elicitationSchema),
	}
	d.registerBuiltinSchemas()
	return d
}

// RegisterSchema registers a named schema.  Schema must be a valid JSON Schema object.
func (d *ElicitationDispatcher) RegisterSchema(name string, schema map[string]any) {
	d.schemas[name] = elicitationSchema{schema: schema}
}

// Elicit sends a daemon-initiated elicitation request to the connected MCP client.
// Returns ErrElicitationNotSupported when no client with elicitation capability
// is connected.
func (d *ElicitationDispatcher) Elicit(ctx context.Context, req ElicitationRequest) (*ElicitationResult, error) {
	if d.srv == nil {
		return nil, ErrElicitationNotSupported
	}

	s, ok := d.schemas[req.Schema]
	if !ok {
		return nil, fmt.Errorf("unknown elicitation schema %q; registered: approval, text_input, choice", req.Schema)
	}

	// For "choice" schema, inject the options enum at request time.
	schema := s.schema
	if req.Schema == "choice" && len(req.Options) > 0 {
		schema = buildChoiceSchema(req.Options)
	}

	if d.stats != nil {
		d.stats.RecordElicitationReq()
	}

	sdkReq := mcpsdk.ElicitationRequest{
		Request: mcpsdk.Request{Method: string(mcpsdk.MethodElicitationCreate)},
		Params: mcpsdk.ElicitationParams{
			Mode:            mcpsdk.ElicitationModeForm,
			Message:         req.Message,
			RequestedSchema: schema,
		},
	}

	raw, err := d.srv.RequestElicitation(ctx, sdkReq)
	if err != nil {
		if errors.Is(err, server.ErrElicitationNotSupported) || errors.Is(err, server.ErrNoActiveSession) {
			return nil, ErrElicitationNotSupported
		}
		return nil, fmt.Errorf("elicitation request failed: %w", err)
	}

	res := &ElicitationResult{
		Action: string(raw.Action),
	}
	if raw.Content != nil {
		if m, ok := raw.Content.(map[string]any); ok {
			res.Content = m
		}
	}
	return res, nil
}

// Schemas returns a copy of the schema registry for inspection.
func (d *ElicitationDispatcher) Schemas() map[string]map[string]any {
	out := make(map[string]map[string]any, len(d.schemas))
	for k, v := range d.schemas {
		out[k] = v.schema
	}
	return out
}

// registerBuiltinSchemas registers the three standard elicitation schemas.
func (d *ElicitationDispatcher) registerBuiltinSchemas() {
	// approval — structured approval gate.
	d.RegisterSchema("approval", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"approve", "reject", "modify"},
			},
			"note": map[string]any{"type": "string"},
		},
		"required": []string{"action"},
	})

	// text_input — free-form text.
	d.RegisterSchema("text_input", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
		"required": []string{"text"},
	})

	// choice — enum select; enum values injected at request time.
	d.RegisterSchema("choice", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"selected": map[string]any{"type": "string"},
		},
		"required": []string{"selected"},
	})
}

// buildChoiceSchema returns a choice schema with the given options as the enum.
func buildChoiceSchema(options []string) map[string]any {
	enum := make([]any, len(options))
	for i, o := range options {
		enum[i] = o
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"selected": map[string]any{
				"type": "string",
				"enum": enum,
			},
		},
		"required": []string{"selected"},
	}
}
