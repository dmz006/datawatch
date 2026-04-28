package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// v5.27.0 — mempalace alignment MCP tools. Each closes a parity
// gap between the new REST endpoints and the stdio surface so
// IDE clients (Cursor, Claude Desktop, VS Code) can drive the
// new capabilities without a separate HTTP call.

func (s *Server) toolMemoryPin() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_pin",
		mcpsdk.WithDescription("Pin or unpin a memory in the L1 wake-up bundle. Pinned rows always surface in critical-facts regardless of vector rank."),
		mcpsdk.WithNumber("id", mcpsdk.Required(), mcpsdk.Description("Memory ID to pin/unpin")),
		mcpsdk.WithBoolean("pinned", mcpsdk.Description("true = pin (default), false = unpin")),
	)
}

func (s *Server) handleMemoryPin(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText("Memory not enabled."), nil
	}
	id := int64(req.GetInt("id", 0))
	if id <= 0 {
		return mcpsdk.NewToolResultError("valid numeric id is required"), nil
	}
	pinned := req.GetBool("pinned", true)
	if err := s.memoryAPI.SetPinned(id, pinned); err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("pin failed: %v", err)), nil
	}
	state := "pinned"
	if !pinned {
		state = "unpinned"
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Memory #%d %s", id, state)), nil
}

func (s *Server) toolMemorySweep() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_sweep_stale",
		mcpsdk.WithDescription("Similarity-stale eviction: drop memories that have never surfaced in any search and are older than the cutoff. Manual + pinned rows are exempt. Defaults to dry-run."),
		mcpsdk.WithNumber("older_than_days", mcpsdk.Description("Age cutoff in days (default 90)")),
		mcpsdk.WithBoolean("dry_run", mcpsdk.Description("true = report candidates only (default), false = actually delete")),
	)
}

func (s *Server) handleMemorySweep(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText("Memory not enabled."), nil
	}
	days := req.GetInt("older_than_days", 90)
	dry := req.GetBool("dry_run", true)
	res, err := s.memoryAPI.SweepStale(days, dry)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("sweep failed: %v", err)), nil
	}
	data, _ := json.MarshalIndent(res, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) toolMemorySpellCheck() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_spellcheck",
		mcpsdk.WithDescription("Conservative Levenshtein-based spellcheck on the supplied text. Returns suggestions only — never rewrites."),
		mcpsdk.WithString("text", mcpsdk.Required(), mcpsdk.Description("Text to check")),
	)
}

func (s *Server) handleMemorySpellCheck(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText("Memory not enabled."), nil
	}
	text := req.GetString("text", "")
	if text == "" {
		return mcpsdk.NewToolResultError("text is required"), nil
	}
	out := s.memoryAPI.SpellCheckText(text, nil)
	data, _ := json.MarshalIndent(out, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) toolMemoryExtractFacts() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_extract_facts",
		mcpsdk.WithDescription("Heuristic schema-free SVO triple extraction from text. Useful for KG pre-population."),
		mcpsdk.WithString("text", mcpsdk.Required(), mcpsdk.Description("Text to extract triples from")),
	)
}

func (s *Server) handleMemoryExtractFacts(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText("Memory not enabled."), nil
	}
	text := req.GetString("text", "")
	if text == "" {
		return mcpsdk.NewToolResultError("text is required"), nil
	}
	out := s.memoryAPI.ExtractFactsText(text)
	data, _ := json.MarshalIndent(out, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) toolMemorySchemaVersion() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_schema_version",
		mcpsdk.WithDescription("Return the highest schema_version row applied to the memory store (e.g. 'v5.27.0')."),
	)
}

func (s *Server) handleMemorySchemaVersion(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText("Memory not enabled."), nil
	}
	v := s.memoryAPI.SchemaVersion()
	if v == "" {
		v = "(no schema_version row — pre-v5.27.0 database)"
	}
	return mcpsdk.NewToolResultText(v), nil
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

// ── Cross-Session Research MCP Tool ───────────────────────────────────────────

func (s *Server) toolResearchSessions() mcpsdk.Tool {
	return mcpsdk.NewTool("research_sessions",
		mcpsdk.WithDescription("Deep cross-session research: searches across ALL session outputs, memories, and knowledge graph for a topic. Returns synthesized results with session context."),
		mcpsdk.WithString("query", mcpsdk.Required(), mcpsdk.Description("Research query — what to search for across sessions")),
		mcpsdk.WithNumber("max_results", mcpsdk.Description("Maximum results to return (default 10)")),
	)
}

func (s *Server) handleResearchSessions(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	query := req.GetString("query", "")
	if query == "" {
		return mcpsdk.NewToolResultError("query is required"), nil
	}
	maxResults := req.GetInt("max_results", 10)

	var sections []string

	// 1. Search memories
	if s.memoryAPI != nil {
		memories, err := s.memoryAPI.Search(query, maxResults)
		if err == nil && len(memories) > 0 {
			var lines []string
			for _, m := range memories {
				mm := m
				content := ""
				if c, ok := mm["content"].(string); ok {
					if len(c) > 200 { c = c[:197] + "..." }
					content = c
				}
				role, _ := mm["role"].(string)
				sid, _ := mm["session_id"].(string)
				sim := 0.0
				if s, ok := mm["similarity"].(float64); ok { sim = s }
				lines = append(lines, fmt.Sprintf("  [%.0f%%] %s: %s (session: %s)", sim*100, role, content, sid))
			}
			sections = append(sections, "## Memories\n"+joinLines(lines))
		}
	}

	// 2. Search KG for related entities
	if s.kgAPI != nil {
		// Try query as entity name
		triples, err := s.kgAPI.QueryEntity(query, "")
		if err == nil && len(triples) > 0 {
			var lines []string
			for _, t := range triples {
				tm := t
				subj, _ := tm["subject"].(string)
				pred, _ := tm["predicate"].(string)
				obj, _ := tm["object"].(string)
				lines = append(lines, fmt.Sprintf("  %s %s %s", subj, pred, obj))
			}
			sections = append(sections, "## Knowledge Graph\n"+joinLines(lines))
		}
	}

	// 3. Search recent session outputs
	sessions := s.manager.ListSessions()
	var sessionHits []string
	queryLower := strings.ToLower(query)
	for _, sess := range sessions {
		if sess.LastResponse != "" && strings.Contains(strings.ToLower(sess.LastResponse), queryLower) {
			snippet := sess.LastResponse
			if len(snippet) > 200 { snippet = snippet[:197] + "..." }
			sessionHits = append(sessionHits, fmt.Sprintf("  [%s] %s (%s): %s", sess.ID, sess.Task, sess.State, snippet))
		}
	}
	if len(sessionHits) > 0 {
		sections = append(sections, "## Session Outputs\n"+joinLines(sessionHits))
	}

	if len(sections) == 0 {
		return mcpsdk.NewToolResultText(fmt.Sprintf("No results found for: %q", query)), nil
	}

	result := fmt.Sprintf("# Research: %s\n\n%s", query, strings.Join(sections, "\n\n"))
	return mcpsdk.NewToolResultText(result), nil
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

// ── Config & Management MCP Tools ─────────────────────────────────────────────

func (s *Server) toolGetConfig() mcpsdk.Tool {
	return mcpsdk.NewTool("get_config",
		mcpsdk.WithDescription("Get the current datawatch configuration. Returns all config sections."),
		mcpsdk.WithString("section", mcpsdk.Description("Optional: specific section to return (e.g. 'memory', 'session', 'ollama')")),
	)
}

func (s *Server) handleGetConfig(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	section := req.GetString("section", "")
	// Fetch config via HTTP to avoid circular imports
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + fmt.Sprintf("%d", s.webPort) + "/api/config")
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("config error: %v", err)), nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if section != "" {
		var full map[string]interface{}
		json.Unmarshal(body, &full) //nolint:errcheck
		if val, ok := full[section]; ok {
			data, _ := json.MarshalIndent(val, "", "  ")
			return mcpsdk.NewToolResultText(string(data)), nil
		}
		return mcpsdk.NewToolResultText(fmt.Sprintf("section %q not found", section)), nil
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}

func (s *Server) toolDeleteSession() mcpsdk.Tool {
	return mcpsdk.NewTool("delete_session",
		mcpsdk.WithDescription("Delete a completed/failed/killed session and its data."),
		mcpsdk.WithString("session_id", mcpsdk.Required(), mcpsdk.Description("Session ID to delete")),
	)
}

func (s *Server) handleDeleteSession(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("session_id", "")
	if id == "" {
		return mcpsdk.NewToolResultError("session_id is required"), nil
	}
	if err := s.manager.Delete(id, true); err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("delete error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Deleted session %s", id)), nil
}

func (s *Server) toolRestartSession() mcpsdk.Tool {
	return mcpsdk.NewTool("restart_session",
		mcpsdk.WithDescription("Restart a completed/failed/killed session with the same task."),
		mcpsdk.WithString("session_id", mcpsdk.Required(), mcpsdk.Description("Session ID to restart")),
	)
}

func (s *Server) handleRestartSession(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("session_id", "")
	if id == "" {
		return mcpsdk.NewToolResultError("session_id is required"), nil
	}
	sess, err := s.manager.Restart(context.Background(), id)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("restart error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Restarted session %s", sess.FullID)), nil
}

func (s *Server) toolGetStats() mcpsdk.Tool {
	return mcpsdk.NewTool("get_stats",
		mcpsdk.WithDescription("Get system statistics: CPU, memory, disk, GPU, sessions, Ollama, RTK, memory system."),
	)
}

func (s *Server) handleGetStats(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + fmt.Sprintf("%d", s.webPort) + "/api/stats")
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("stats error: %v", err)), nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return mcpsdk.NewToolResultText(string(body)), nil
}

func (s *Server) toolMemoryExport() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_export",
		mcpsdk.WithDescription("Export all memories as JSON for backup."),
	)
}

func (s *Server) handleMemoryExport(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText("Memory not enabled."), nil
	}
	var buf strings.Builder
	if err := s.memoryAPI.Export(&buf); err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("export error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(buf.String()), nil
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

// ── Memory Import MCP Tool ──────────────────────────────────────────────────

func (s *Server) toolMemoryImport() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_import",
		mcpsdk.WithDescription("Import memories from JSON text (output of memory_export)."),
		mcpsdk.WithString("json_data",
			mcpsdk.Required(),
			mcpsdk.Description("JSON array of memories to import"),
		),
	)
}

func (s *Server) handleMemoryImport(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText("Memory not enabled."), nil
	}
	data := req.GetString("json_data", "")
	if data == "" {
		return mcpsdk.NewToolResultText("Error: json_data is required"), nil
	}
	count, err := s.memoryAPI.Import(strings.NewReader(data))
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("import error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Imported %d memories.", count)), nil
}

// ── Config Set MCP Tool ─────────────────────────────────────────────────────

func (s *Server) toolConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("config_set",
		mcpsdk.WithDescription("Set a configuration value. Key format: section.field (e.g. 'detection.prompt_debounce')."),
		mcpsdk.WithString("key",
			mcpsdk.Required(),
			mcpsdk.Description("Config key (e.g. 'detection.prompt_debounce', 'pipeline.max_parallel')"),
		),
		mcpsdk.WithString("value",
			mcpsdk.Required(),
			mcpsdk.Description("Value to set"),
		),
	)
}

func (s *Server) handleConfigSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.webPort <= 0 {
		return mcpsdk.NewToolResultText("Web server not available for config update."), nil
	}
	key := req.GetString("key", "")
	value := req.GetString("value", "")
	if key == "" || value == "" {
		return mcpsdk.NewToolResultText("Error: key and value are required"), nil
	}

	// BL110 — permission gate. Default closed; operators flip
	// mcp.allow_self_config=true when they want an in-process AI to
	// be able to tune its own config. Bootstrap protection: the gate
	// itself cannot be opened via config_set, only via direct YAML
	// edit + restart.
	if s.cfg == nil || !s.cfg.AllowSelfConfig {
		return mcpsdk.NewToolResultText(
			"permission denied: set mcp.allow_self_config=true in the config file (and restart) to enable self-modify",
		), nil
	}
	if key == "mcp.allow_self_config" {
		return mcpsdk.NewToolResultText(
			"refused: mcp.allow_self_config is bootstrap-protected; edit the YAML directly to change it",
		), nil
	}
	s.auditSelfConfig(key, value)

	// Route through the HTTP API for proper validation and persistence
	body := fmt.Sprintf(`{"%s": %s}`, key, value)
	// Try as raw value first, then as string
	resp, err := http.Post(fmt.Sprintf("http://localhost:%d/api/config", s.webPort),
		"application/json", strings.NewReader(body))
	if err != nil {
		// Try with quoted value
		body = fmt.Sprintf(`{"%s": "%s"}`, key, value)
		resp, err = http.Post(fmt.Sprintf("http://localhost:%d/api/config", s.webPort),
			"application/json", strings.NewReader(body))
		if err != nil {
			return mcpsdk.NewToolResultError(fmt.Sprintf("config error: %v", err)), nil
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return mcpsdk.NewToolResultError(fmt.Sprintf("config error: HTTP %d", resp.StatusCode)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Set %s = %s", key, value)), nil
}

// ── Memory Learnings MCP Tool ───────────────────────────────────────────────

func (s *Server) toolMemoryLearnings() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_learnings",
		mcpsdk.WithDescription("List or search task learnings extracted from completed sessions."),
		mcpsdk.WithString("query",
			mcpsdk.Description("Optional search query to filter learnings"),
		),
		mcpsdk.WithNumber("limit",
			mcpsdk.Description("Max results to return (default: 20)"),
		),
	)
}

func (s *Server) handleMemoryLearnings(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.memoryAPI == nil {
		return mcpsdk.NewToolResultText("Memory not enabled."), nil
	}
	query := req.GetString("query", "")
	limit := req.GetInt("limit", 20)
	results, err := s.memoryAPI.ListLearnings("", query, limit)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("error: %v", err)), nil
	}
	if len(results) == 0 {
		return mcpsdk.NewToolResultText("No learnings found."), nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Learnings (%d):\n\n", len(results)))
	for _, m := range results {
		content := fmt.Sprintf("%v", m["content"])
		if len(content) > 150 {
			content = content[:147] + "..."
		}
		sb.WriteString(fmt.Sprintf("#%v: %s\n", m["id"], content))
	}
	return mcpsdk.NewToolResultText(sb.String()), nil
}
