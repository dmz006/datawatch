// internal/mcp/dashboard.go — #57/#58 MCP tools for dashboard card layout.
//
//   dashboard_config_get     — get raw layout JSON
//   dashboard_cards_list     — list cards array
//   dashboard_card_update    — set cs/rs for a card (upserts if not present)
//   dashboard_card_add       — append a card
//   dashboard_card_delete    — remove a card

package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── Tool descriptors ────────────────────────────────────────────────────

func (s *Server) toolDashboardConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("dashboard_config_get",
		mcpsdk.WithDescription("#57 — get the raw dashboard layout JSON (cards + spans)."),
	)
}

func (s *Server) toolDashboardCardsList() mcpsdk.Tool {
	return mcpsdk.NewTool("dashboard_cards_list",
		mcpsdk.WithDescription("#57 — list all dashboard cards with their column and row spans."),
	)
}

func (s *Server) toolDashboardCardUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("dashboard_card_update",
		mcpsdk.WithDescription("#57 — set column span (cs) and optional row span (rs) for a dashboard card. Appends the card if it does not exist (upsert)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("card type id (tree, orbital, events, sparklines, gantt, heatmap, guardrails, ekg, smoke)")),
		mcpsdk.WithNumber("cs", mcpsdk.Required(), mcpsdk.Description("column span 1-12")),
		mcpsdk.WithNumber("rs", mcpsdk.Description("row span (optional)")),
	)
}

func (s *Server) toolDashboardCardAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("dashboard_card_add",
		mcpsdk.WithDescription("#57 — append a new card to the dashboard layout."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("card type id")),
		mcpsdk.WithNumber("cs", mcpsdk.Required(), mcpsdk.Description("column span 1-12")),
		mcpsdk.WithNumber("rs", mcpsdk.Description("row span (optional)")),
	)
}

func (s *Server) toolDashboardCardDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("dashboard_card_delete",
		mcpsdk.WithDescription("#57 — remove a card from the dashboard layout."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("card type id")),
	)
}

// ── Handlers ─────────────────────────────────────────────────────────────

func (s *Server) handleDashboardConfigGetMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body, err := s.proxyGet("/api/dashboard/layout", nil)
	if err != nil {
		return nil, err
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}

func (s *Server) handleDashboardCardsListMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body, err := s.proxyGet("/api/dashboard/cards", nil)
	if err != nil {
		return nil, err
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}

func (s *Server) handleDashboardCardUpdateMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	cs := req.GetInt("cs", 0)
	payload := map[string]any{"id": id, "cs": cs}
	if rs := req.GetInt("rs", 0); rs > 0 {
		payload["rs"] = rs
	}
	body, err := s.proxyJSON("PUT", fmt.Sprintf("/api/dashboard/cards/%s", id), payload)
	if err != nil {
		return nil, err
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}

func (s *Server) handleDashboardCardAddMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	cs := req.GetInt("cs", 0)
	payload := map[string]any{"id": id, "cs": cs}
	if rs := req.GetInt("rs", 0); rs > 0 {
		payload["rs"] = rs
	}
	body, err := s.proxyJSON("POST", "/api/dashboard/cards", payload)
	if err != nil {
		return nil, err
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}

func (s *Server) handleDashboardCardDeleteMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, err := s.proxyJSON("DELETE", fmt.Sprintf("/api/dashboard/cards/%s", id), nil)
	if err != nil {
		return nil, err
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}
