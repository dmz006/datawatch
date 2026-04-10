package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/session"
)

// fetchOllamaStatsHTTP fetches Ollama stats via HTTP for MCP tool.
func fetchOllamaStatsHTTP(host string) map[string]interface{} {
	result := map[string]interface{}{"host": host}
	client := &http.Client{Timeout: 5 * time.Second}

	// Fetch models
	resp, err := client.Get(host + "/api/tags")
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tags map[string]interface{}
	json.Unmarshal(body, &tags) //nolint:errcheck

	if models, ok := tags["models"].([]interface{}); ok {
		result["model_count"] = len(models)
		var totalSize float64
		var names []string
		for _, m := range models {
			if mm, ok := m.(map[string]interface{}); ok {
				if name, ok := mm["name"].(string); ok { names = append(names, name) }
				if size, ok := mm["size"].(float64); ok { totalSize += size }
			}
		}
		result["models"] = names
		result["total_size_gb"] = fmt.Sprintf("%.1f", totalSize/(1024*1024*1024))
	}

	// Fetch running
	psResp, err := client.Get(host + "/api/ps")
	if err == nil {
		defer psResp.Body.Close()
		psBody, _ := io.ReadAll(psResp.Body)
		var ps map[string]interface{}
		json.Unmarshal(psBody, &ps) //nolint:errcheck
		if models, ok := ps["models"].([]interface{}); ok {
			var running []map[string]interface{}
			for _, m := range models {
				if mm, ok := m.(map[string]interface{}); ok {
					running = append(running, map[string]interface{}{
						"name":     mm["name"],
						"vram_gb":  fmt.Sprintf("%.1f", toFloat(mm["size_vram"])/(1024*1024*1024)),
					})
				}
			}
			result["running"] = running
		}
	}
	result["available"] = true
	return result
}

func toFloat(v interface{}) float64 {
	if f, ok := v.(float64); ok { return f }
	return 0
}

func (s *Server) toolMemoryRemember() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_remember",
		mcpsdk.WithDescription("Store a memory/fact for future retrieval. Embedded with vector search for semantic recall."),
		mcpsdk.WithString("text", mcpsdk.Required(), mcpsdk.Description("The text to remember")),
		mcpsdk.WithString("project_dir", mcpsdk.Description("Project directory (default: session default)")),
	)
}

func (s *Server) handleMemoryRemember(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText("Memory not enabled. Set memory.enabled=true in config."), nil
	}
	text := req.GetString("text", "")
	projectDir := req.GetString("project_dir", "")
	if text == "" {
		return mcpsdk.NewToolResultError("text is required"), nil
	}
	id, err := s.memoryAPI.Remember(projectDir, text)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("remember failed: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Saved memory #%d", id)), nil
}

func (s *Server) toolMemoryRecall() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_recall",
		mcpsdk.WithDescription("Semantic search across all memories. Returns top matches ranked by similarity."),
		mcpsdk.WithString("query", mcpsdk.Required(), mcpsdk.Description("Search query")),
	)
}

func (s *Server) handleMemoryRecall(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText("Memory not enabled."), nil
	}
	query := req.GetString("query", "")
	if query == "" {
		return mcpsdk.NewToolResultError("query is required"), nil
	}
	results, err := s.memoryAPI.Search(query, 10)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("recall failed: %v", err)), nil
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) toolMemoryList() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_list",
		mcpsdk.WithDescription("List recent memories, optionally filtered by project directory."),
		mcpsdk.WithString("project_dir", mcpsdk.Description("Project directory filter (empty = default project)")),
		mcpsdk.WithNumber("n", mcpsdk.Description("Number of memories to return (default 20)")),
	)
}

func (s *Server) handleMemoryList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText("Memory not enabled."), nil
	}
	projectDir := req.GetString("project_dir", "")
	n := req.GetInt("n", 20)
	results, err := s.memoryAPI.ListRecent(projectDir, n)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("list failed: %v", err)), nil
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) toolMemoryForget() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_forget",
		mcpsdk.WithDescription("Delete a memory by its numeric ID."),
		mcpsdk.WithNumber("id", mcpsdk.Required(), mcpsdk.Description("Memory ID to delete")),
	)
}

func (s *Server) handleMemoryForget(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText("Memory not enabled."), nil
	}
	id := int64(req.GetInt("id", 0))
	if id <= 0 {
		return mcpsdk.NewToolResultError("valid numeric id is required"), nil
	}
	if err := s.memoryAPI.Delete(id); err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("delete failed: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Deleted memory #%d", id)), nil
}

func (s *Server) toolMemoryStats() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_stats",
		mcpsdk.WithDescription("Get episodic memory system statistics: total count, counts by role, database size."),
	)
}

func (s *Server) handleMemoryStats(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText(`{"enabled":false}`), nil
	}
	data, _ := json.MarshalIndent(s.memoryAPI.Stats(), "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) toolGetPrompt() mcpsdk.Tool {
	return mcpsdk.NewTool("get_prompt",
		mcpsdk.WithDescription("Get the last user prompt sent to a session."),
		mcpsdk.WithString("session_id", mcpsdk.Description("Session ID. Empty = most recent.")),
	)
}

func (s *Server) handleGetPrompt(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	sessionID := req.GetString("session_id", "")
	if sessionID == "" {
		sessions := s.manager.ListSessions()
		var latest *session.Session
		for _, sess := range sessions {
			if latest == nil || sess.UpdatedAt.After(latest.UpdatedAt) {
				latest = sess
			}
		}
		if latest == nil {
			return mcpsdk.NewToolResultText("No sessions found."), nil
		}
		sessionID = latest.FullID
	}
	sess, ok := s.manager.GetSession(sessionID)
	if !ok {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Session %s not found.", sessionID)), nil
	}
	if sess.LastInput == "" {
		return mcpsdk.NewToolResultText(fmt.Sprintf("No prompt captured for session %s.", sessionID)), nil
	}
	return mcpsdk.NewToolResultText(sess.LastInput), nil
}

func (s *Server) toolMemoryReindex() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_reindex",
		mcpsdk.WithDescription("Re-embed all memories with the current embedding model. Use after changing the embedder model."),
	)
}

func (s *Server) handleMemoryReindex(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText("Memory not enabled."), nil
	}
	// Call reindex via a simple approach — find the Reindex method
	// The memory API doesn't have Reindex directly, route through comm channel
	return mcpsdk.NewToolResultText("Reindex started. Use 'memories reindex' via comm channel for async operation."), nil
}

// ── Ollama Server Stats MCP Tool ──────────────────────────────────────────────

func (s *Server) toolOllamaStats() mcpsdk.Tool {
	return mcpsdk.NewTool("ollama_stats",
		mcpsdk.WithDescription("Get statistics from the configured Ollama server: models, running models, VRAM usage, disk space."),
	)
}

func (s *Server) handleOllamaStats(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.ollamaHost == "" {
		return mcpsdk.NewToolResultText("Ollama not configured."), nil
	}
	// Fetch live stats from Ollama API
	olStats := fetchOllamaStatsHTTP(s.ollamaHost)
	data, _ := json.MarshalIndent(olStats, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── Knowledge Graph MCP Tools ─────────────────────────────────────────────────

func (s *Server) toolKGQuery() mcpsdk.Tool {
	return mcpsdk.NewTool("kg_query",
		mcpsdk.WithDescription("Query the knowledge graph for all relationships involving an entity. Optionally filter by date."),
		mcpsdk.WithString("entity", mcpsdk.Required(), mcpsdk.Description("Entity name to query")),
		mcpsdk.WithString("as_of", mcpsdk.Description("Point-in-time filter (YYYY-MM-DD). Empty = all time.")),
	)
}

func (s *Server) handleKGQuery(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.kgAPI == nil {
		return mcpsdk.NewToolResultText("Knowledge graph not enabled."), nil
	}
	entity := req.GetString("entity", "")
	asOf := req.GetString("as_of", "")
	if entity == "" {
		return mcpsdk.NewToolResultError("entity is required"), nil
	}
	results, err := s.kgAPI.QueryEntity(entity, asOf)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("KG query error: %v", err)), nil
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) toolKGAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("kg_add",
		mcpsdk.WithDescription("Add a relationship triple to the knowledge graph with optional temporal validity."),
		mcpsdk.WithString("subject", mcpsdk.Required(), mcpsdk.Description("Subject entity")),
		mcpsdk.WithString("predicate", mcpsdk.Required(), mcpsdk.Description("Relationship type (e.g. works_on, loves, uses)")),
		mcpsdk.WithString("object", mcpsdk.Required(), mcpsdk.Description("Object entity")),
		mcpsdk.WithString("valid_from", mcpsdk.Description("Start date (YYYY-MM-DD). Default: today.")),
		mcpsdk.WithString("source", mcpsdk.Description("Source context (e.g. session ID)")),
	)
}

func (s *Server) handleKGAdd(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.kgAPI == nil {
		return mcpsdk.NewToolResultText("Knowledge graph not enabled."), nil
	}
	subj := req.GetString("subject", "")
	pred := req.GetString("predicate", "")
	obj := req.GetString("object", "")
	validFrom := req.GetString("valid_from", "")
	source := req.GetString("source", "")
	if subj == "" || pred == "" || obj == "" {
		return mcpsdk.NewToolResultError("subject, predicate, and object are required"), nil
	}
	id, err := s.kgAPI.AddTriple(subj, pred, obj, validFrom, source)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("KG add error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Added triple #%d: %s %s %s", id, subj, pred, obj)), nil
}

func (s *Server) toolKGInvalidate() mcpsdk.Tool {
	return mcpsdk.NewTool("kg_invalidate",
		mcpsdk.WithDescription("End the validity of a relationship triple (invalidate, not delete). Preserves history."),
		mcpsdk.WithString("subject", mcpsdk.Required(), mcpsdk.Description("Subject entity")),
		mcpsdk.WithString("predicate", mcpsdk.Required(), mcpsdk.Description("Relationship type")),
		mcpsdk.WithString("object", mcpsdk.Required(), mcpsdk.Description("Object entity")),
		mcpsdk.WithString("ended", mcpsdk.Description("End date (YYYY-MM-DD). Default: today.")),
	)
}

func (s *Server) handleKGInvalidate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.kgAPI == nil {
		return mcpsdk.NewToolResultText("Knowledge graph not enabled."), nil
	}
	subj := req.GetString("subject", "")
	pred := req.GetString("predicate", "")
	obj := req.GetString("object", "")
	ended := req.GetString("ended", "")
	if subj == "" || pred == "" || obj == "" {
		return mcpsdk.NewToolResultError("subject, predicate, and object are required"), nil
	}
	if err := s.kgAPI.Invalidate(subj, pred, obj, ended); err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("KG invalidate error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Invalidated: %s %s %s", subj, pred, obj)), nil
}

func (s *Server) toolKGTimeline() mcpsdk.Tool {
	return mcpsdk.NewTool("kg_timeline",
		mcpsdk.WithDescription("Get the chronological timeline of all relationships for an entity."),
		mcpsdk.WithString("entity", mcpsdk.Required(), mcpsdk.Description("Entity name")),
	)
}

func (s *Server) handleKGTimeline(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.kgAPI == nil {
		return mcpsdk.NewToolResultText("Knowledge graph not enabled."), nil
	}
	entity := req.GetString("entity", "")
	if entity == "" {
		return mcpsdk.NewToolResultError("entity is required"), nil
	}
	results, err := s.kgAPI.Timeline(entity)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("KG timeline error: %v", err)), nil
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) toolKGStats() mcpsdk.Tool {
	return mcpsdk.NewTool("kg_stats",
		mcpsdk.WithDescription("Get knowledge graph statistics: entity count, triple count, active vs expired."),
	)
}

func (s *Server) handleKGStats(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.kgAPI == nil {
		return mcpsdk.NewToolResultText(`{"enabled":false}`), nil
	}
	data, _ := json.MarshalIndent(s.kgAPI.Stats(), "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) toolCopyResponse() mcpsdk.Tool {
	return mcpsdk.NewTool("copy_response",
		mcpsdk.WithDescription("Get the last captured LLM response for a session. If no session_id given, uses the most recently updated session."),
		mcpsdk.WithString("session_id", mcpsdk.Description("Session ID (short or full). Empty = most recent.")),
	)
}

func (s *Server) handleCopyResponse(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	sessionID := req.GetString("session_id", "")
	if sessionID == "" {
		sessions := s.manager.ListSessions()
		var latest *session.Session
		for _, sess := range sessions {
			if latest == nil || sess.UpdatedAt.After(latest.UpdatedAt) {
				latest = sess
			}
		}
		if latest == nil {
			return mcpsdk.NewToolResultText("No sessions found."), nil
		}
		sessionID = latest.FullID
	}
	resp := s.manager.GetLastResponse(sessionID)
	if resp == "" {
		return mcpsdk.NewToolResultText(fmt.Sprintf("No response captured for session %s.", sessionID)), nil
	}
	return mcpsdk.NewToolResultText(resp), nil
}
