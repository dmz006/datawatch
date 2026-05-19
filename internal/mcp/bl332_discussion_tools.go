// BL332 T42c — MCP tools for Discussion Scopes.
//
// 4 tools forward to the REST surface already implemented in T42a/T42b:
//
//	memory_discussion_write      POST /api/memory/discussion/{id}
//	memory_discussion_recall     GET  /api/memory/discussion/{id}
//	memory_discussion_wal        GET  /api/memory/discussion/{id}/wal
//	memory_discussion_participants  GET/PUT /api/memory/discussion/{id}/participants

package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── memory_discussion_write ───────────────────────────────────────────────────

func (s *Server) toolDiscussionWrite() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_discussion_write",
		mcpsdk.WithDescription("Write an entry to a shared discussion scope (BL332). "+
			"Entries are stored in the memory backend under the discussion/<id> role and "+
			"appended to the WAL for federated sync to participant peers."),
		mcpsdk.WithString("discussion_id", mcpsdk.Required(), mcpsdk.Description("Discussion scope ID (arbitrary slug, e.g. 'sprint-42' or 'incident-2026')")),
		mcpsdk.WithString("content", mcpsdk.Required(), mcpsdk.Description("Entry content to write")),
		mcpsdk.WithString("summary", mcpsdk.Description("Optional short summary for search indexing")),
		mcpsdk.WithString("role", mcpsdk.Description("Optional role override (default: 'discussion')")),
	)
}

func (s *Server) handleDiscussionWrite(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("discussion_id", "")
	if id == "" {
		return textOK("Error: discussion_id is required"), nil
	}
	body := map[string]any{
		"content": req.GetString("content", ""),
	}
	if v := req.GetString("summary", ""); v != "" {
		body["summary"] = v
	}
	if v := req.GetString("role", ""); v != "" {
		body["role"] = v
	}
	out, err := s.proxyJSON(http.MethodPost, fmt.Sprintf("/api/memory/discussion/%s", url.PathEscape(id)), body)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── memory_discussion_recall ──────────────────────────────────────────────────

func (s *Server) toolDiscussionRecall() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_discussion_recall",
		mcpsdk.WithDescription("Recall entries from a shared discussion scope (BL332). "+
			"Returns entries stored under the discussion/<id> role, optionally filtered by a semantic query."),
		mcpsdk.WithString("discussion_id", mcpsdk.Required(), mcpsdk.Description("Discussion scope ID")),
		mcpsdk.WithString("q", mcpsdk.Description("Semantic search query (empty = list all recent entries)")),
		mcpsdk.WithString("top_k", mcpsdk.Description("Maximum results to return (default 10)")),
	)
}

func (s *Server) handleDiscussionRecall(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("discussion_id", "")
	if id == "" {
		return textOK("Error: discussion_id is required"), nil
	}
	q := url.Values{}
	if v := req.GetString("q", ""); v != "" {
		q.Set("q", v)
	}
	if v := req.GetString("top_k", ""); v != "" {
		q.Set("top_k", v)
	}
	path := fmt.Sprintf("/api/memory/discussion/%s", url.PathEscape(id))
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	out, err := s.proxyJSON(http.MethodGet, path, nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── memory_discussion_wal ─────────────────────────────────────────────────────

func (s *Server) toolDiscussionWAL() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_discussion_wal",
		mcpsdk.WithDescription("Read the write-ahead log (WAL) for a discussion scope (BL332). "+
			"Returns the last N entries including origin peer, sequence number, and timestamp."),
		mcpsdk.WithString("discussion_id", mcpsdk.Required(), mcpsdk.Description("Discussion scope ID")),
		mcpsdk.WithString("n", mcpsdk.Description("Number of WAL entries to return (default 20)")),
	)
}

func (s *Server) handleDiscussionWAL(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("discussion_id", "")
	if id == "" {
		return textOK("Error: discussion_id is required"), nil
	}
	q := url.Values{}
	if v := req.GetString("n", ""); v != "" {
		q.Set("n", v)
	}
	path := fmt.Sprintf("/api/memory/discussion/%s/wal", url.PathEscape(id))
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	out, err := s.proxyJSON(http.MethodGet, path, nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── memory_discussion_participants ────────────────────────────────────────────

func (s *Server) toolDiscussionParticipants() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_discussion_participants",
		mcpsdk.WithDescription("Get or set the participant peer list for a discussion scope (BL332). "+
			"When 'peers' is omitted, returns the current participant list (GET). "+
			"When 'peers' is provided, replaces the participant list (PUT) — writes will then be synced to those peers."),
		mcpsdk.WithString("discussion_id", mcpsdk.Required(), mcpsdk.Description("Discussion scope ID")),
		mcpsdk.WithString("peers", mcpsdk.Description("Comma-separated peer hostnames to set as participants. Omit to read current list.")),
	)
}

func (s *Server) handleDiscussionParticipants(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("discussion_id", "")
	if id == "" {
		return textOK("Error: discussion_id is required"), nil
	}
	path := fmt.Sprintf("/api/memory/discussion/%s/participants", url.PathEscape(id))
	peersRaw := req.GetString("peers", "")
	if peersRaw != "" {
		// Split comma-separated peers into a slice.
		peers := splitCSV(peersRaw)
		body := map[string]any{"peers": peers}
		out, err := s.proxyJSON(http.MethodPut, path, body)
		if err != nil {
			return textOK("Error: " + err.Error()), nil
		}
		return textOK(string(out)), nil
	}
	out, err := s.proxyJSON(http.MethodGet, path, nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

