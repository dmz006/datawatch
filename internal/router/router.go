package router

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/agents"
	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/messaging"
	"github.com/dmz006/datawatch/internal/profile"
	"github.com/dmz006/datawatch/internal/proxy"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/stats"
	"github.com/dmz006/datawatch/internal/transcribe"
	"github.com/dmz006/datawatch/internal/wizard"
)

// Router dispatches incoming messages to the session manager
// and formats responses back to the messaging backend.
type Router struct {
	hostname    string
	groupID     string
	backend     messaging.Backend
	manager     *session.Manager
	tailLines   int
	wizardMgr   *wizard.Manager
	schedStore  *session.ScheduleStore
	alertStore  *alerts.Store
	cmdLib      *session.CmdLibrary
	// F10 sprint 2: read-only profile access over chat channels.
	projectStore *profile.ProjectStore
	clusterStore *profile.ClusterStore
	// F10 sprint 3: agent manager for "agent …" commands.
	agentMgr *agents.Manager
	version     string
	checkUpdate func() string // optional func that returns latest version string
	restartFn   func()        // optional func to restart the daemon
	statsFn     func() string // optional func returning system stats summary
	configureFn func(key, value string) error // optional func to set a config value
	chanTracker  *stats.ChannelCounters      // per-channel message counters
	transcriber  transcribe.Transcriber      // optional voice-to-text transcriber
	remote       *proxy.RemoteDispatcher     // optional remote server dispatcher

	// Memory system — optional, nil when memory is disabled
	memoryRetriever MemoryRetriever
	defaultProject  string // default project dir for memory commands
	// Knowledge graph — optional, nil when memory is disabled
	knowledgeGraph KnowledgeGraphAPI
	// Pipeline executor — optional, nil when pipelines not configured
	pipelineExec PipelineExecutor
}

// NewRouter creates a new Router.
func NewRouter(hostname, groupID string, backend messaging.Backend, manager *session.Manager, tailLines int, wm *wizard.Manager) *Router {
	return &Router{
		hostname:  hostname,
		groupID:   groupID,
		backend:   backend,
		manager:   manager,
		tailLines: tailLines,
		wizardMgr: wm,
	}
}

// SetScheduleStore wires a schedule store into the router for the schedule command.
func (r *Router) SetScheduleStore(s *session.ScheduleStore) { r.schedStore = s }

// SetAlertStore wires an alert store into the router for the alerts command and SendAlert.
func (r *Router) SetAlertStore(s *alerts.Store) { r.alertStore = s }

// SetCmdLibrary wires the saved command library into the router for alert quick-reply hints.
func (r *Router) SetCmdLibrary(l *session.CmdLibrary) { r.cmdLib = l }

// SetProjectStore wires the Project Profile store for "profile project …" commands.
func (r *Router) SetProjectStore(s *profile.ProjectStore) { r.projectStore = s }

// SetClusterStore wires the Cluster Profile store for "profile cluster …" commands.
func (r *Router) SetClusterStore(s *profile.ClusterStore) { r.clusterStore = s }

// SetVersion sets the version string reported by the version command.
func (r *Router) SetVersion(v string) { r.version = v }

// SetUpdateChecker sets an optional function that returns the latest available version.
func (r *Router) SetUpdateChecker(fn func() string) { r.checkUpdate = fn }

// SetRestartFunc sets an optional function that restarts the daemon.
func (r *Router) SetRestartFunc(fn func()) { r.restartFn = fn }
func (r *Router) SetStatsFunc(fn func() string)                     { r.statsFn = fn }
func (r *Router) SetConfigureFunc(fn func(key, value string) error) { r.configureFn = fn }

// SetChannelTracker sets the per-channel stats counters for this router.
func (r *Router) SetChannelTracker(ct *stats.ChannelCounters) { r.chanTracker = ct }

// SetTranscriber sets an optional voice-to-text transcriber for audio attachments.
func (r *Router) SetTranscriber(t transcribe.Transcriber) { r.transcriber = t }

// SetRemoteDispatcher sets the remote server dispatcher for proxy mode routing.
func (r *Router) SetRemoteDispatcher(d *proxy.RemoteDispatcher) { r.remote = d }

// MemoryRetriever is the interface for memory operations from the router.
// Decoupled from internal/memory to avoid circular imports.
type MemoryRetriever interface {
	Remember(projectDir, text string) (int64, error)
	Recall(projectDir, query string) ([]Memory, error)
	RecallAll(query string) ([]Memory, error)
	Store() MemoryStore
	Reindex() (int, error)
}

// MemoryStore is the subset of store operations needed by the router.
type MemoryStore interface {
	ListRecent(projectDir string, n int) ([]Memory, error)
	ListByRole(projectDir, role string, n int) ([]Memory, error)
	Delete(id int64) error
	Count(projectDir string) (int, error)
	FindTunnels() (map[string][]string, error)
	Stats() MemoryStats
	Export(w io.Writer) error
}

// MemoryStats holds memory store statistics.
type MemoryStats struct {
	TotalCount     int
	ManualCount    int
	SessionCount   int
	LearningCount  int
	ChunkCount     int
	DBSizeBytes    int64
	Encrypted      bool
	KeyFingerprint string
}

// Memory mirrors memory.Memory for the router interface.
type Memory struct {
	ID         int64   `json:"id"`
	SessionID  string  `json:"session_id,omitempty"`
	ProjectDir string  `json:"project_dir"`
	Content    string  `json:"content"`
	Summary    string  `json:"summary,omitempty"`
	Role       string  `json:"role"`
	Wing       string  `json:"wing,omitempty"`
	Room       string  `json:"room,omitempty"`
	Hall       string  `json:"hall,omitempty"`
	CreatedAt  interface{} `json:"created_at"`
	Similarity float64 `json:"similarity,omitempty"`
}

// KnowledgeGraphAPI is the interface for KG operations from the router.
type KnowledgeGraphAPI interface {
	AddTriple(subject, predicate, object, validFrom, source string) (int64, error)
	Invalidate(subject, predicate, object, ended string) error
	QueryEntity(name, asOf string) ([]KGTriple, error)
	Timeline(name string) ([]KGTriple, error)
	Stats() KGStats
}

// KGTriple mirrors memory.KGTriple for the router.
type KGTriple struct {
	ID        int64  `json:"id"`
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
	ValidFrom string `json:"valid_from,omitempty"`
	ValidTo   string `json:"valid_to,omitempty"`
}

// KGStats mirrors memory.KGStats for the router.
type KGStats struct {
	EntityCount  int `json:"entity_count"`
	TripleCount  int `json:"triple_count"`
	ActiveCount  int `json:"active_count"`
	ExpiredCount int `json:"expired_count"`
}

// SetMemoryRetriever wires the memory system into the router.
func (r *Router) SetMemoryRetriever(mr MemoryRetriever, defaultProject string) {
	r.memoryRetriever = mr
	r.defaultProject = defaultProject
}

// PipelineExecutor is the interface for pipeline operations from the router.
type PipelineExecutor interface {
	StartPipeline(name, projectDir string, taskSpecs []string, maxParallel int) (string, error)
	GetStatus(id string) string
	Cancel(id string) error
	ListAll() string
}

// SetPipelineExecutor wires the pipeline system into the router.
func (r *Router) SetPipelineExecutor(pe PipelineExecutor) {
	r.pipelineExec = pe
}

// SetKnowledgeGraph wires the knowledge graph into the router.
func (r *Router) SetKnowledgeGraph(kg KnowledgeGraphAPI) {
	r.knowledgeGraph = kg
}

func (r *Router) handleCopy(cmd Command) {
	// If no session ID given, find the most recently updated session
	sessionID := cmd.SessionID
	if sessionID == "" {
		sessions := r.manager.ListSessions()
		var latest *session.Session
		for _, s := range sessions {
			if latest == nil || s.UpdatedAt.After(latest.UpdatedAt) {
				latest = s
			}
		}
		if latest == nil {
			r.send(fmt.Sprintf("[%s] No sessions found.", r.hostname))
			return
		}
		sessionID = latest.FullID
	}

	resp := r.manager.GetLastResponse(sessionID)
	if resp == "" {
		r.send(fmt.Sprintf("[%s] No response captured for session %s.", r.hostname, sessionID))
		return
	}
	// Truncate for messaging channels (keep first 3000 chars)
	if len(resp) > 3000 {
		resp = resp[:3000] + "\n…(truncated)"
	}
	header := fmt.Sprintf("[%s] Last response [%s]:", r.hostname, sessionID)
	// Use rich text if the backend supports it
	if rs, ok := r.backend.(messaging.RichSender); ok {
		mdText := fmt.Sprintf("**%s**\n```\n%s\n```", header, resp)
		go rs.SendMarkdown(r.groupID, mdText) //nolint:errcheck
		return
	}
	r.send(fmt.Sprintf("%s\n%s", header, resp))
}

func (r *Router) handlePrompt(cmd Command) {
	sessionID := cmd.SessionID
	if sessionID == "" {
		sessions := r.manager.ListSessions()
		var latest *session.Session
		for _, s := range sessions {
			if latest == nil || s.UpdatedAt.After(latest.UpdatedAt) {
				latest = s
			}
		}
		if latest == nil {
			r.send(fmt.Sprintf("[%s] No sessions found.", r.hostname))
			return
		}
		sessionID = latest.FullID
	}
	sess, ok := r.manager.GetSession(sessionID)
	if !ok {
		r.send(fmt.Sprintf("[%s] Session %s not found.", r.hostname, sessionID))
		return
	}
	if sess.LastInput == "" {
		r.send(fmt.Sprintf("[%s] No prompt captured for session %s.", r.hostname, sessionID))
		return
	}
	r.send(fmt.Sprintf("[%s] Last prompt [%s]: %s", r.hostname, sessionID, sess.LastInput))
}

func (r *Router) handleRemember(cmd Command) {
	if r.memoryRetriever == nil {
		r.send(fmt.Sprintf("[%s] Memory system not enabled. Set memory.enabled=true in config.", r.hostname))
		return
	}
	if cmd.Text == "" {
		r.send(fmt.Sprintf("[%s] Usage: remember: <text to remember>", r.hostname))
		return
	}
	id, err := r.memoryRetriever.Remember(r.defaultProject, cmd.Text)
	if err != nil {
		r.send(fmt.Sprintf("[%s] Failed to save memory: %v", r.hostname, err))
		return
	}
	r.send(fmt.Sprintf("[%s] Saved memory #%d", r.hostname, id))
}

func (r *Router) handleRecall(cmd Command) {
	if r.memoryRetriever == nil {
		r.send(fmt.Sprintf("[%s] Memory system not enabled.", r.hostname))
		return
	}
	if cmd.Text == "" {
		r.send(fmt.Sprintf("[%s] Usage: recall: <search query>", r.hostname))
		return
	}
	results, err := r.memoryRetriever.RecallAll(cmd.Text)
	if err != nil {
		r.send(fmt.Sprintf("[%s] Recall failed: %v", r.hostname, err))
		return
	}
	r.send(fmt.Sprintf("[%s] Recall results:\n%s", r.hostname, formatMemories(results)))
}

func (r *Router) handleMemories(cmd Command) {
	if r.memoryRetriever == nil {
		r.send(fmt.Sprintf("[%s] Memory system not enabled.", r.hostname))
		return
	}
	n := cmd.TailN
	if n <= 0 {
		n = 10
	}
	memories, err := r.memoryRetriever.Store().ListRecent(r.defaultProject, n)
	if err != nil {
		r.send(fmt.Sprintf("[%s] Failed to list memories: %v", r.hostname, err))
		return
	}
	count, _ := r.memoryRetriever.Store().Count(r.defaultProject)
	r.send(fmt.Sprintf("[%s] Memories (%d total, showing %d):\n%s", r.hostname, count, len(memories), formatMemories(memories)))
}

func (r *Router) handleForget(cmd Command) {
	if r.memoryRetriever == nil {
		r.send(fmt.Sprintf("[%s] Memory system not enabled.", r.hostname))
		return
	}
	var id int64
	if _, err := fmt.Sscanf(cmd.Text, "%d", &id); err != nil || id <= 0 {
		r.send(fmt.Sprintf("[%s] Usage: forget <memory-id> (e.g. forget 42)", r.hostname))
		return
	}
	if err := r.memoryRetriever.Store().Delete(id); err != nil {
		r.send(fmt.Sprintf("[%s] Failed to delete memory #%d: %v", r.hostname, id, err))
		return
	}
	r.send(fmt.Sprintf("[%s] Deleted memory #%d", r.hostname, id))
}

func (r *Router) handleLearnings(cmd Command) {
	if r.memoryRetriever == nil {
		r.send(fmt.Sprintf("[%s] Memory system not enabled.", r.hostname))
		return
	}
	lower := strings.ToLower(cmd.Text)
	if strings.HasPrefix(lower, "search:") || strings.HasPrefix(lower, "search ") {
		query := strings.TrimSpace(cmd.Text[7:])
		if query == "" {
			r.send(fmt.Sprintf("[%s] Usage: learnings search: <query>", r.hostname))
			return
		}
		results, err := r.memoryRetriever.RecallAll(query)
		if err != nil {
			r.send(fmt.Sprintf("[%s] Learning search failed: %v", r.hostname, err))
			return
		}
		// Filter to learnings only
		var learnings []Memory
		for _, m := range results {
			if m.Role == "learning" {
				learnings = append(learnings, m)
			}
		}
		r.send(fmt.Sprintf("[%s] Learnings matching %q:\n%s", r.hostname, query, formatMemories(learnings)))
		return
	}

	learnings, err := r.memoryRetriever.Store().ListByRole(r.defaultProject, "learning", 20)
	if err != nil {
		r.send(fmt.Sprintf("[%s] Failed to list learnings: %v", r.hostname, err))
		return
	}
	r.send(fmt.Sprintf("[%s] Task learnings:\n%s", r.hostname, formatMemories(learnings)))
}

func (r *Router) handleResearch(cmd Command) {
	if r.memoryRetriever == nil {
		r.send(fmt.Sprintf("[%s] Memory not enabled.", r.hostname))
		return
	}
	if cmd.Text == "" {
		r.send(fmt.Sprintf("[%s] Usage: research: <query>", r.hostname))
		return
	}

	var sections []string

	// Search memories
	results, err := r.memoryRetriever.RecallAll(cmd.Text)
	if err == nil && len(results) > 0 {
		var lines []string
		for _, m := range results {
			content := m.Content
			if len(content) > 150 { content = content[:147] + "..." }
			content = strings.ReplaceAll(content, "\n", " ")
			lines = append(lines, fmt.Sprintf("  [%.0f%%] %s: %s", m.Similarity*100, m.Role, content))
		}
		sections = append(sections, "Memories:\n"+strings.Join(lines, "\n"))
	}

	// Search KG
	if r.knowledgeGraph != nil {
		triples, err := r.knowledgeGraph.QueryEntity(cmd.Text, "")
		if err == nil && len(triples) > 0 {
			var lines []string
			for _, t := range triples {
				lines = append(lines, fmt.Sprintf("  %s %s %s", t.Subject, t.Predicate, t.Object))
			}
			sections = append(sections, "Knowledge Graph:\n"+strings.Join(lines, "\n"))
		}
	}

	// Search session outputs
	sessions := r.manager.ListSessions()
	queryLower := strings.ToLower(cmd.Text)
	var hits []string
	for _, sess := range sessions {
		if sess.LastResponse != "" && strings.Contains(strings.ToLower(sess.LastResponse), queryLower) {
			snippet := sess.LastResponse
			if len(snippet) > 150 { snippet = snippet[:147] + "..." }
			hits = append(hits, fmt.Sprintf("  [%s] %s: %s", sess.ID, sess.Task, snippet))
		}
	}
	if len(hits) > 0 {
		sections = append(sections, "Sessions:\n"+strings.Join(hits, "\n"))
	}

	if len(sections) == 0 {
		r.send(fmt.Sprintf("[%s] No results for: %q", r.hostname, cmd.Text))
		return
	}
	r.send(fmt.Sprintf("[%s] Research: %s\n%s", r.hostname, cmd.Text, strings.Join(sections, "\n\n")))
}

func (r *Router) handlePipeline(cmd Command) {
	if r.pipelineExec == nil {
		r.send(fmt.Sprintf("[%s] Pipeline system not available.", r.hostname))
		return
	}
	text := cmd.Text
	lower := strings.ToLower(text)

	switch {
	case strings.HasPrefix(lower, "status"):
		id := strings.TrimSpace(text[6:])
		if id == "" {
			r.send(fmt.Sprintf("[%s] %s", r.hostname, r.pipelineExec.ListAll()))
		} else {
			r.send(fmt.Sprintf("[%s] %s", r.hostname, r.pipelineExec.GetStatus(id)))
		}

	case strings.HasPrefix(lower, "cancel "):
		id := strings.TrimSpace(text[7:])
		if err := r.pipelineExec.Cancel(id); err != nil {
			r.send(fmt.Sprintf("[%s] Cancel error: %v", r.hostname, err))
		} else {
			r.send(fmt.Sprintf("[%s] Pipeline %s cancelled.", r.hostname, id))
		}

	default:
		// Parse as pipeline spec: "task1 -> task2 -> task3"
		if text == "" {
			r.send(fmt.Sprintf("[%s] Usage: pipeline: task1 -> task2 -> task3", r.hostname))
			return
		}
		id, err := r.pipelineExec.StartPipeline(text, r.defaultProject, nil, 3)
		if err != nil {
			r.send(fmt.Sprintf("[%s] Pipeline error: %v", r.hostname, err))
			return
		}
		r.send(fmt.Sprintf("[%s] Pipeline started: %s", r.hostname, id))
	}
}

func (r *Router) handleTunnels() {
	if r.memoryRetriever == nil {
		r.send(fmt.Sprintf("[%s] Memory not enabled.", r.hostname))
		return
	}
	tunnels, err := r.memoryRetriever.Store().FindTunnels()
	if err != nil {
		r.send(fmt.Sprintf("[%s] Tunnels error: %v", r.hostname, err))
		return
	}
	if len(tunnels) == 0 {
		r.send(fmt.Sprintf("[%s] No cross-project tunnels found (rooms shared across wings).", r.hostname))
		return
	}
	var b strings.Builder
	for room, wings := range tunnels {
		fmt.Fprintf(&b, "  %s → %s\n", room, strings.Join(wings, ", "))
	}
	r.send(fmt.Sprintf("[%s] Cross-project tunnels (shared rooms):\n%s", r.hostname, b.String()))
}

func (r *Router) handleMemReindex() {
	if r.memoryRetriever == nil {
		r.send(fmt.Sprintf("[%s] Memory system not enabled.", r.hostname))
		return
	}
	r.send(fmt.Sprintf("[%s] Starting memory reindex…", r.hostname))
	go func() {
		count, err := r.memoryRetriever.Reindex()
		if err != nil {
			r.send(fmt.Sprintf("[%s] Reindex error: %v", r.hostname, err))
			return
		}
		r.send(fmt.Sprintf("[%s] Reindex complete: %d memories re-embedded.", r.hostname, count))
	}()
}

func (r *Router) handleMemoryStats() {
	if r.memoryRetriever == nil {
		r.send(fmt.Sprintf("[%s] Memory system not enabled.", r.hostname))
		return
	}
	ms := r.memoryRetriever.Store().Stats()
	r.send(fmt.Sprintf("[%s] Memory Stats:\n  Total: %d memories (%d manual, %d session, %d learning, %d chunk)\n  DB: %.1f MB\n  Encrypted: %v",
		r.hostname, ms.TotalCount, ms.ManualCount, ms.SessionCount, ms.LearningCount, ms.ChunkCount,
		float64(ms.DBSizeBytes)/1024/1024, ms.Encrypted))
}

func (r *Router) handleMemoryExport() {
	if r.memoryRetriever == nil {
		r.send(fmt.Sprintf("[%s] Memory system not enabled.", r.hostname))
		return
	}
	var buf strings.Builder
	if err := r.memoryRetriever.Store().Export(&buf); err != nil {
		r.send(fmt.Sprintf("[%s] Export error: %v", r.hostname, err))
		return
	}
	// Truncate for messaging channels (export can be large)
	output := buf.String()
	if len(output) > 3000 {
		output = output[:3000] + "\n...(truncated, use API for full export)"
	}
	r.send(fmt.Sprintf("[%s] Memory Export:\n%s", r.hostname, output))
}

func (r *Router) handleKG(cmd Command) {
	if r.knowledgeGraph == nil {
		r.send(fmt.Sprintf("[%s] Knowledge graph not enabled.", r.hostname))
		return
	}
	text := cmd.Text
	lower := strings.ToLower(text)

	switch {
	case strings.HasPrefix(lower, "query "):
		entity := strings.TrimSpace(text[6:])
		triples, err := r.knowledgeGraph.QueryEntity(entity, "")
		if err != nil {
			r.send(fmt.Sprintf("[%s] KG query error: %v", r.hostname, err))
			return
		}
		r.send(fmt.Sprintf("[%s] KG: %s\n%s", r.hostname, entity, formatKGTriples(triples)))

	case strings.HasPrefix(lower, "add "):
		parts := strings.Fields(text[4:])
		if len(parts) < 3 {
			r.send(fmt.Sprintf("[%s] Usage: kg add <subject> <predicate> <object>", r.hostname))
			return
		}
		id, err := r.knowledgeGraph.AddTriple(parts[0], parts[1], strings.Join(parts[2:], " "), "", "manual")
		if err != nil {
			r.send(fmt.Sprintf("[%s] KG add error: %v", r.hostname, err))
			return
		}
		r.send(fmt.Sprintf("[%s] Added triple #%d: %s %s %s", r.hostname, id, parts[0], parts[1], strings.Join(parts[2:], " ")))

	case strings.HasPrefix(lower, "timeline "):
		entity := strings.TrimSpace(text[9:])
		triples, err := r.knowledgeGraph.Timeline(entity)
		if err != nil {
			r.send(fmt.Sprintf("[%s] KG timeline error: %v", r.hostname, err))
			return
		}
		r.send(fmt.Sprintf("[%s] Timeline: %s\n%s", r.hostname, entity, formatKGTriples(triples)))

	case strings.HasPrefix(lower, "invalidate "):
		parts := strings.Fields(text[11:])
		if len(parts) < 3 {
			r.send(fmt.Sprintf("[%s] Usage: kg invalidate <subject> <predicate> <object>", r.hostname))
			return
		}
		if err := r.knowledgeGraph.Invalidate(parts[0], parts[1], strings.Join(parts[2:], " "), ""); err != nil {
			r.send(fmt.Sprintf("[%s] KG invalidate error: %v", r.hostname, err))
			return
		}
		r.send(fmt.Sprintf("[%s] Invalidated: %s %s %s", r.hostname, parts[0], parts[1], strings.Join(parts[2:], " ")))

	case lower == "stats":
		stats := r.knowledgeGraph.Stats()
		r.send(fmt.Sprintf("[%s] KG Stats: %d entities, %d triples (%d active, %d expired)",
			r.hostname, stats.EntityCount, stats.TripleCount, stats.ActiveCount, stats.ExpiredCount))

	default:
		r.send(fmt.Sprintf("[%s] Usage: kg query|add|invalidate|timeline|stats <args>", r.hostname))
	}
}

func formatKGTriples(triples []KGTriple) string {
	if len(triples) == 0 {
		return "  (none)"
	}
	var b strings.Builder
	for _, t := range triples {
		validity := ""
		if t.ValidFrom != "" {
			validity = fmt.Sprintf(" (from %s", t.ValidFrom)
			if t.ValidTo != "" {
				validity += fmt.Sprintf(" to %s", t.ValidTo)
			}
			validity += ")"
		}
		fmt.Fprintf(&b, "  #%d %s %s %s%s\n", t.ID, t.Subject, t.Predicate, t.Object, validity)
	}
	return b.String()
}

func formatMemories(memories []Memory) string {
	if len(memories) == 0 {
		return "  (none)"
	}
	var b strings.Builder
	for _, m := range memories {
		content := m.Content
		if len(content) > 150 {
			content = content[:147] + "..."
		}
		content = strings.ReplaceAll(content, "\n", " ")
		if m.Similarity > 0 {
			fmt.Fprintf(&b, "  #%d [%.0f%%] %s: %s\n", m.ID, m.Similarity*100, m.Role, content)
		} else {
			fmt.Fprintf(&b, "  #%d %s: %s\n", m.ID, m.Role, content)
		}
	}
	return b.String()
}

func (r *Router) handleConfigure(cmd Command) {
	if r.configureFn == nil {
		r.send(fmt.Sprintf("[%s] Configuration not available.", r.hostname))
		return
	}
	text := strings.TrimSpace(cmd.Text)
	if text == "" || text == "help" {
		r.send(fmt.Sprintf("[%s] Usage: configure <key>=<value>\nExample: configure session.console_cols=120\n\nCommon keys:\n  session.llm_backend, session.max_sessions, session.console_cols, session.console_rows\n  ollama.host, ollama.model, ollama.enabled\n  detection.prompt_debounce, detection.notify_cooldown\n  server.host, server.port", r.hostname))
		return
	}
	if text == "list" {
		r.send(fmt.Sprintf("[%s] Configurable keys (use configure <key>=<value>):\n  session.llm_backend, session.max_sessions, session.input_idle_timeout\n  session.console_cols, session.console_rows, session.auto_git_commit\n  ollama.enabled, ollama.host, ollama.model\n  opencode.enabled, opencode.binary\n  detection.prompt_debounce, detection.notify_cooldown\n  server.host, server.port, server.tls\n  mcp.sse_host, mcp.sse_port", r.hostname))
		return
	}
	eqIdx := strings.Index(text, "=")
	if eqIdx < 1 {
		r.send(fmt.Sprintf("[%s] Invalid format. Use: configure <key>=<value>", r.hostname))
		return
	}
	key := strings.TrimSpace(text[:eqIdx])
	value := strings.TrimSpace(text[eqIdx+1:])
	if err := r.configureFn(key, value); err != nil {
		r.send(fmt.Sprintf("[%s] Configure failed: %v", r.hostname, err))
	} else {
		r.send(fmt.Sprintf("[%s] Set %s = %s. Restart may be required.", r.hostname, key, value))
	}
}

func (r *Router) handleStats() {
	if r.statsFn == nil {
		r.send(fmt.Sprintf("[%s] Stats not available.", r.hostname))
		return
	}
	r.send(fmt.Sprintf("[%s] System Stats:\n%s", r.hostname, r.statsFn()))
}

// Run starts the router, subscribing to Signal messages and dispatching them.
// Blocks until ctx is cancelled.
func (r *Router) Run(ctx context.Context) error {
	fmt.Printf("[%s] Router (%s) listening on group: %q\n", r.hostname, r.backend.Name(), r.groupID)
	// Only set default callbacks if none have been wired up yet.
	// When the HTTP server is enabled, main.go sets combined callbacks before
	// calling Run, so we skip re-setting them here.
	if r.manager.StateChangeHandler() == nil {
		r.manager.SetStateChangeHandler(r.HandleStateChange)
	}
	if r.manager.NeedsInputHandler() == nil {
		r.manager.SetNeedsInputHandler(r.HandleNeedsInput)
	}

	// Subscribe to messages
	return r.backend.Subscribe(ctx, r.handleMessage)
}

// handleMessage processes an incoming message.
func (r *Router) handleMessage(msg messaging.Message) {
	// Only process messages from our configured group
	if msg.GroupID != r.groupID {
		// Log mismatches for debugging (use -v flag to see)
		if msg.GroupID != "" {
			fmt.Printf("[%s] [debug] Message from group %q (expected %q) — ignoring\n",
				r.hostname, msg.GroupID, r.groupID)
		}
		return
	}

	// Transcribe audio attachments into text before processing
	if r.transcriber != nil && len(msg.Attachments) > 0 {
		for _, att := range msg.Attachments {
			if !att.IsAudio() || att.FilePath == "" {
				continue
			}
			fmt.Printf("[%s] [%s] Voice message received, transcribing…\n", r.hostname, msg.Backend)
			text, err := r.transcriber.Transcribe(context.Background(), att.FilePath)
			// Clean up temp file after transcription
			os.Remove(att.FilePath)
			if err != nil {
				fmt.Printf("[%s] [%s] Transcription failed: %v\n", r.hostname, msg.Backend, err)
				r.send(fmt.Sprintf("[%s] Voice transcription failed: %v", r.hostname, err))
				return
			}
			if text == "" {
				r.send(fmt.Sprintf("[%s] Voice message was empty (no speech detected).", r.hostname))
				return
			}
			r.send(fmt.Sprintf("[%s] Voice: %s", r.hostname, text))
			msg.Text = text
			break // only transcribe the first audio attachment
		}
	}

	fmt.Printf("[%s] [%s] Received: %q\n", r.hostname, msg.Backend, truncate(msg.Text, 80))
	if r.chanTracker != nil {
		r.chanTracker.RecordRecv(len(msg.Text))
	}

	// Check if an active wizard is waiting for a response in this group
	if r.wizardMgr != nil && r.wizardMgr.HandleMessage(msg.GroupID, msg.Text) {
		return
	}

	cmd := Parse(msg.Text)

	switch cmd.Type {
	case CmdNew:
		r.handleNew(cmd)
	case CmdList:
		r.handleList(cmd.Text)
	case CmdStatus:
		r.handleStatus(cmd)
	case CmdSend:
		r.handleSend(cmd)
	case CmdKill:
		r.handleKill(cmd)
	case CmdTail:
		r.handleTail(cmd)
	case CmdAttach:
		r.handleAttach(cmd)
	case CmdSetup:
		r.handleSetup(cmd, msg.GroupID)
	case CmdVersion:
		r.handleVersion()
	case CmdRestart:
		r.handleRestart()
	case CmdUpdateCheck:
		r.handleUpdateCheck()
	case CmdSchedule:
		r.handleSchedule(cmd)
	case CmdAlerts:
		r.handleAlerts(cmd)
	case CmdStats:
		r.handleStats()
	case CmdConfigure:
		r.handleConfigure(cmd)
	case CmdCopy:
		r.handleCopy(cmd)
	case CmdPrompt:
		r.handlePrompt(cmd)
	case CmdRemember:
		r.handleRemember(cmd)
	case CmdRecall:
		r.handleRecall(cmd)
	case CmdMemories:
		switch cmd.Text {
		case "tunnels", "__tunnels__":
			r.handleTunnels()
		case "reindex":
			r.handleMemReindex()
		case "stats":
			r.handleMemoryStats()
		case "export":
			r.handleMemoryExport()
		default:
			r.handleMemories(cmd)
		}
	case CmdForget:
		r.handleForget(cmd)
	case CmdLearnings:
		r.handleLearnings(cmd)
	case CmdResearch:
		r.handleResearch(cmd)
	case CmdPipeline:
		r.handlePipeline(cmd)
	case CmdKG:
		r.handleKG(cmd)
	case CmdMemReindex:
		r.handleMemReindex()
	case CmdProfile:
		r.handleProfile(cmd)
	case CmdAgent:
		r.handleAgent(cmd)
	case CmdBind:
		r.handleBind(cmd)
	case CmdSession:
		r.handleSessionCmd(cmd)
	case CmdHelp:
		r.send(HelpText(r.hostname))
	default:
		// If exactly one session on this host is waiting for input,
		// treat any unrecognised message as the reply.
		r.handleImplicitSend(msg.Text)
	}
}

func (r *Router) handleSetup(cmd Command, groupID string) {
	if r.wizardMgr == nil {
		r.send(fmt.Sprintf("[%s] Setup wizards are not available in this context.", r.hostname))
		return
	}
	service := strings.TrimSpace(cmd.Text)
	if service == "" {
		r.send(fmt.Sprintf("[%s] Usage: setup <service>\nAvailable: signal, telegram, discord, slack, matrix, twilio, ntfy, email, webhook, github, web, server, llm <backend>, session, mcp", r.hostname))
		return
	}
	if err := r.wizardMgr.StartWizard(groupID, service, r.send); err != nil {
		r.send(fmt.Sprintf("[%s] %v", r.hostname, err))
	}
}

func (r *Router) handleAlerts(cmd Command) {
	if r.alertStore == nil {
		r.send(fmt.Sprintf("[%s] Alert store not available.", r.hostname))
		return
	}
	n := cmd.TailN
	if n <= 0 {
		n = 5
	}
	all := r.alertStore.List()
	if len(all) == 0 {
		r.send(fmt.Sprintf("[%s] No alerts.", r.hostname))
		return
	}
	if n > len(all) {
		n = len(all)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] Last %d alert(s):\n", r.hostname, n))
	for i, a := range all[:n] {
		sessLabel := ""
		if a.SessionID != "" {
			parts := strings.Split(a.SessionID, "-")
			sessLabel = fmt.Sprintf("[%s] ", parts[len(parts)-1])
		}
		sb.WriteString(fmt.Sprintf("  %s%s %s — %s\n",
			sessLabel, a.CreatedAt.Format("15:04:05"), strings.ToUpper(string(a.Level)), a.Title))
		if a.Body != "" {
			sb.WriteString(fmt.Sprintf("    %s\n", truncate(a.Body, 100)))
		}
		if i < n-1 {
			sb.WriteString("  ────\n")
		}
	}
	r.send(sb.String())
}

// SendAlert formats an alert and broadcasts it to this router's backend group.
// Called by main.go's alert listener for each active messaging backend.
func (r *Router) SendAlert(a *alerts.Alert) {
	body := ""
	if a.Body != "" {
		body = "\n" + truncate(a.Body, 200)
	}
	// Append quick-reply hints when a session is waiting for input and saved commands exist.
	quickHints := ""
	if a.SessionID != "" && r.cmdLib != nil {
		sess, ok := r.manager.GetSession(a.SessionID)
		if ok && sess.State == session.StateWaitingInput {
			cmds := r.cmdLib.List()
			if len(cmds) > 0 {
				names := make([]string, 0, len(cmds))
				for _, c := range cmds {
					names = append(names, c.Name)
				}
				shortID := sess.ID
				quickHints = fmt.Sprintf("\nReply: send %s: !<cmd>  options: %s",
					shortID, strings.Join(names, " | "))
			}
		}
	}
	r.send(fmt.Sprintf("[%s] ALERT [%s] %s%s%s",
		r.hostname, strings.ToUpper(string(a.Level)), a.Title, body, quickHints))
}

func (r *Router) handleVersion() {
	r.send(r.aboutText())
}

// aboutText returns ASCII art logo with version and host info.
func (r *Router) aboutText() string {
	v := r.version
	if v == "" {
		v = "unknown"
	}
	sessions := r.manager.ListSessions()
	active := 0
	for _, s := range sessions {
		if s.State == session.StateRunning || s.State == session.StateWaitingInput {
			active++
		}
	}
	return fmt.Sprintf(`
    ╔═══════════════════════════════════╗
    ║         ░▒▓ DATAWATCH ▓▒░        ║
    ║      ┌──────────────────┐        ║
    ║      │   ◉  ◎  ◉  ◎    │        ║
    ║      │  ╔══╗  ╔══╗     │        ║
    ║      │  ║◉◉║──║◎◎║     │        ║
    ║      │  ╚══╝  ╚══╝     │        ║
    ║      │    ◎  ◉  ◎  ◉   │        ║
    ║      └──────────────────┘        ║
    ║   AI Session Monitor & Bridge    ║
    ╠═══════════════════════════════════╣
    ║  Version:  v%-22s ║
    ║  Host:     %-22s  ║
    ║  Sessions: %d active / %-10d ║
    ║  Backend:  %-22s  ║
    ╚═══════════════════════════════════╝`, v, r.hostname, active, len(sessions), r.manager.ActiveBackend())
}

func (r *Router) handleRestart() {
	if r.restartFn == nil {
		r.send(fmt.Sprintf("[%s] restart not available.", r.hostname))
		return
	}
	r.send(fmt.Sprintf("[%s] Restarting daemon…", r.hostname))
	go func() {
		time.Sleep(500 * time.Millisecond)
		r.restartFn()
	}()
}

func (r *Router) handleUpdateCheck() {
	if r.checkUpdate == nil {
		v := r.version
		if v == "" {
			v = "unknown"
		}
		r.send(fmt.Sprintf("[%s] datawatch v%s (update check not available)", r.hostname, v))
		return
	}
	latest := r.checkUpdate()
	current := r.version
	if current == "" {
		current = "unknown"
	}
	switch {
	case latest == "" || !isNewerSemver(latest, current):
		r.send(fmt.Sprintf("[%s] datawatch v%s — up to date", r.hostname, current))
	default:
		r.send(fmt.Sprintf("[%s] datawatch v%s — update available: v%s\nRun `datawatch update` on the host to upgrade.", r.hostname, current, latest))
	}
}

// isNewerSemver reports whether `latest` is strictly newer than `current`
// using numeric semver part comparison. Returns false on parse errors so we
// never falsely advertise an update.
func isNewerSemver(latest, current string) bool {
	parse := func(s string) []int {
		s = strings.TrimPrefix(strings.TrimSpace(s), "v")
		if i := strings.IndexAny(s, "-+"); i >= 0 {
			s = s[:i]
		}
		parts := strings.Split(s, ".")
		out := make([]int, len(parts))
		for i, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil {
				return nil
			}
			out[i] = n
		}
		return out
	}
	a, b := parse(latest), parse(current)
	if a == nil || b == nil {
		return false
	}
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func (r *Router) handleSchedule(cmd Command) {
	if r.schedStore == nil {
		r.send(fmt.Sprintf("[%s] Scheduling is not available (no schedule store).", r.hostname))
		return
	}
	if cmd.SessionID == "" || cmd.Text == "" {
		r.send(fmt.Sprintf("[%s] Usage: schedule <id>: <when> <command>\n  when: now | HH:MM | cancel <schedID>", r.hostname))
		return
	}

	// Split Text into "when" and "command"
	parts := strings.SplitN(strings.TrimSpace(cmd.Text), " ", 2)
	when := strings.ToLower(strings.TrimSpace(parts[0]))
	command := ""
	if len(parts) >= 2 {
		command = strings.TrimSpace(parts[1])
	}

	// Handle cancel
	if when == "cancel" {
		if command == "" {
			r.send(fmt.Sprintf("[%s] Usage: schedule <id>: cancel <schedID>", r.hostname))
			return
		}
		if err := r.schedStore.Cancel(command); err != nil {
			r.send(fmt.Sprintf("[%s] %v", r.hostname, err))
		} else {
			r.send(fmt.Sprintf("[%s] Scheduled command %s cancelled.", r.hostname, command))
		}
		return
	}

	if command == "" {
		r.send(fmt.Sprintf("[%s] Usage: schedule <id>: <when> <command>", r.hostname))
		return
	}

	// Handle "list" to show pending schedules
	if when == "list" {
		pending := r.schedStore.List(session.SchedPending)
		if len(pending) == 0 {
			r.send(fmt.Sprintf("[%s] No pending scheduled items.", r.hostname))
			return
		}
		lines := []string{fmt.Sprintf("[%s] Pending schedules:", r.hostname)}
		for _, sc := range pending {
			when2 := "on input"
			if !sc.RunAt.IsZero() {
				when2 = sc.RunAt.Format("2006-01-02 15:04")
			}
			label := sc.SessionID
			if sc.Type == session.SchedTypeNewSession && sc.DeferredSession != nil {
				label = "NEW: " + sc.DeferredSession.Name
			}
			lines = append(lines, fmt.Sprintf("  [%s] %s @ %s: %s", sc.ID, label, when2, sc.Command))
		}
		r.send(strings.Join(lines, "\n"))
		return
	}

	var runAt time.Time
	if when != "now" {
		// Use natural language time parser (supports "in 30m", "at 14:00", "tomorrow at 9am", etc.)
		var err error
		runAt, err = session.ParseScheduleTime(when+" "+command, time.Now())
		if err != nil {
			// Fallback: try just the "when" part
			runAt, err = session.ParseScheduleTime(when, time.Now())
			if err != nil {
				r.send(fmt.Sprintf("[%s] Invalid time %q — try: now, in 30m, at 14:00, tomorrow at 9am", r.hostname, when))
				return
			}
		}
	}

	sc, err := r.schedStore.Add(cmd.SessionID, command, runAt, "")
	if err != nil {
		r.send(fmt.Sprintf("[%s] Failed to schedule: %v", r.hostname, err))
		return
	}

	when2 := "on next input prompt"
	if !sc.RunAt.IsZero() {
		when2 = sc.RunAt.Format("2006-01-02 15:04")
	}
	r.send(fmt.Sprintf("[%s] Scheduled [%s] for session %s at %s:\n  %s", r.hostname, sc.ID, cmd.SessionID, when2, command))
}

func (r *Router) handleNew(cmd Command) {
	if cmd.Text == "" {
		r.send(fmt.Sprintf("[%s] Usage: new: <task description>\n  new: @server: <task> — start on remote server", r.hostname))
		return
	}

	// Route to remote server if @server specified
	if cmd.Server != "" && r.remote != nil {
		responses, err := r.remote.ForwardCommand(cmd.Server, "new: "+cmd.Text)
		if err != nil {
			r.send(fmt.Sprintf("[%s] Remote %s: %v", r.hostname, cmd.Server, err))
			return
		}
		for _, resp := range responses {
			r.send(resp)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sess, err := r.manager.Start(ctx, cmd.Text, r.groupID, cmd.ProjectDir)
	if err != nil {
		r.send(fmt.Sprintf("[%s] Failed to start session: %v", r.hostname, err))
		return
	}
	r.send(fmt.Sprintf("[%s][%s] Started session for: %s\nTmux: %s\nAttach: tmux attach -t %s",
		r.hostname, sess.ID, cmd.Text, sess.TmuxSession, sess.TmuxSession))
}

func (r *Router) handleList(filter string) {
	sessions := r.manager.ListSessions()
	doneStates := map[session.State]bool{
		session.StateComplete: true,
		session.StateFailed:   true,
		session.StateKilled:   true,
	}

	filterFn := func(s *session.Session) bool {
		switch strings.TrimPrefix(filter, "--") {
		case "active":
			return !doneStates[s.State]
		case "inactive":
			return doneStates[s.State]
		}
		return true
	}

	var mine []*session.Session
	for _, s := range sessions {
		if s.Hostname == r.hostname && filterFn(s) {
			mine = append(mine, s)
		}
	}

	var sb strings.Builder

	// Local sessions
	if len(mine) > 0 {
		sb.WriteString(fmt.Sprintf("[%s] Sessions (%d):\n", r.hostname, len(mine)))
		for i, s := range mine {
			name := s.Name
			if name == "" { name = truncate(s.Task, 40) }
			if name == "" { name = "(no task)" }
			sb.WriteString(fmt.Sprintf("  [%s] %s | %s | %s | %s",
				s.ID, s.State, s.LLMBackend, s.UpdatedAt.Format("15:04"), name))
			if s.State == session.StateWaitingInput {
				sb.WriteString(" ⚠ INPUT")
			}
			sb.WriteByte('\n')
			if i < len(mine)-1 {
				sb.WriteString("  ────\n")
			}
		}
	}

	// Remote sessions (proxy mode)
	if r.remote != nil && r.remote.HasServers() {
		remoteSessions := r.remote.ListAllSessions()
		for serverName, remoteSess := range remoteSessions {
			var filtered []*session.Session
			for _, s := range remoteSess {
				if filterFn(s) {
					filtered = append(filtered, s)
				}
			}
			if len(filtered) == 0 {
				sb.WriteString(fmt.Sprintf("[%s] Sessions (0): idle\n", serverName))
				continue
			}
			sb.WriteString(fmt.Sprintf("[%s] Sessions (%d):\n", serverName, len(filtered)))
			for i, s := range filtered {
				name := s.Name
				if name == "" { name = truncate(s.Task, 40) }
				if name == "" { name = "(no task)" }
				sb.WriteString(fmt.Sprintf("  [%s] %s | %s | %s | %s",
					s.ID, s.State, s.LLMBackend, s.UpdatedAt.Format("15:04"), name))
				if s.State == session.StateWaitingInput {
					sb.WriteString(" ⚠ INPUT")
				}
				sb.WriteByte('\n')
				if i < len(filtered)-1 {
					sb.WriteString("  ────\n")
				}
			}
		}
	}

	if sb.Len() == 0 {
		label := "sessions"
		if filter != "" { label = filter + " sessions" }
		r.send(fmt.Sprintf("[%s] No %s.", r.hostname, label))
		return
	}
	r.send(sb.String())
}

func (r *Router) handleStatus(cmd Command) {
	if cmd.SessionID == "" {
		r.send(fmt.Sprintf("[%s] Usage: status <id>", r.hostname))
		return
	}

	sess, ok := r.manager.GetSession(cmd.SessionID)
	if !ok {
		// Try remote servers
		if r.tryRemoteCommand(cmd.SessionID, "status "+cmd.SessionID) {
			return
		}
		r.send(fmt.Sprintf("[%s] Session %s not found.", r.hostname, cmd.SessionID))
		return
	}

	out, err := r.manager.TailOutput(sess.FullID, r.tailLines)
	if err != nil {
		r.send(fmt.Sprintf("[%s][%s] Error reading output: %v", r.hostname, sess.ID, err))
		return
	}

	r.send(fmt.Sprintf("[%s][%s] State: %s\nTask: %s\n---\n%s",
		r.hostname, sess.ID, sess.State, sess.Task, out))
}

func (r *Router) handleSend(cmd Command) {
	if cmd.SessionID == "" || cmd.Text == "" {
		r.send(fmt.Sprintf("[%s] Usage: send <id>: <message>", r.hostname))
		return
	}

	sess, ok := r.manager.GetSession(cmd.SessionID)
	if !ok {
		// Try remote servers
		if r.tryRemoteCommand(cmd.SessionID, "send "+cmd.SessionID+": "+cmd.Text) {
			return
		}
		r.send(fmt.Sprintf("[%s] Session %s not found.", r.hostname, cmd.SessionID))
		return
	}

	// Expand saved commands: !name or /name looks up the command library
	text := cmd.Text
	text = r.expandSavedCommand(text)

	if err := r.manager.SendInput(sess.FullID, text, r.backend.Name()); err != nil {
		r.send(fmt.Sprintf("[%s][%s] Failed to send input: %v", r.hostname, sess.ID, err))
		return
	}
	r.send(fmt.Sprintf("[%s][%s] Input sent.", r.hostname, sess.ID))
}

// expandSavedCommand checks if text starts with ! or / and expands it
// from the saved command library. Returns original text if no match.
// Special handling: \n → empty string (SendInput appends Enter),
// \x03 and other control chars are preserved.
func (r *Router) expandSavedCommand(text string) string {
	if r.cmdLib == nil {
		return text
	}
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 2 {
		return text
	}
	prefix := trimmed[0]
	if prefix != '!' && prefix != '/' {
		return text
	}
	name := strings.ToLower(trimmed[1:])
	for _, c := range r.cmdLib.List() {
		if strings.ToLower(c.Name) == name {
			cmd := c.Command
			// Normalize: \n means "just press Enter" → empty string
			// (SendInput always appends Enter, so empty = Enter only)
			if cmd == "\n" || cmd == "\\n" || cmd == "" {
				return ""
			}
			return cmd
		}
	}
	// No match — return original text without prefix
	return text
}

// handleBind wires a session to a parent-spawned worker agent
// (F10 sprint 3.6). Usage: "bind <session-id> <agent-id>" or
// "bind <session-id> -" / "bind <session-id>" to unbind.
func (r *Router) handleBind(cmd Command) {
	if cmd.SessionID == "" {
		r.send(fmt.Sprintf("[%s] Usage: bind <session-id> <agent-id>  (use - to unbind)", r.hostname))
		return
	}
	sess, ok := r.manager.GetSession(cmd.SessionID)
	if !ok {
		r.send(fmt.Sprintf("[%s] Session %s not found.", r.hostname, cmd.SessionID))
		return
	}
	if cmd.BindAgentID != "" && r.agentMgr != nil {
		if a := r.agentMgr.Get(cmd.BindAgentID); a == nil {
			r.send(fmt.Sprintf("[%s] Agent %s not found.", r.hostname, cmd.BindAgentID))
			return
		}
	}
	if err := r.manager.SetAgentBinding(sess.FullID, cmd.BindAgentID); err != nil {
		r.send(fmt.Sprintf("[%s] bind failed: %v", r.hostname, err))
		return
	}
	if cmd.BindAgentID == "" {
		r.send(fmt.Sprintf("[%s][%s] Session unbound from agent.", r.hostname, sess.ID))
		return
	}
	if r.agentMgr != nil {
		_ = r.agentMgr.MarkSessionBound(cmd.BindAgentID, sess.FullID)
	}
	r.send(fmt.Sprintf("[%s][%s] Session bound to agent %s.", r.hostname, sess.ID, cmd.BindAgentID))
}

func (r *Router) handleKill(cmd Command) {
	if cmd.SessionID == "" {
		r.send(fmt.Sprintf("[%s] Usage: kill <id>", r.hostname))
		return
	}

	sess, ok := r.manager.GetSession(cmd.SessionID)
	if !ok {
		if r.tryRemoteCommand(cmd.SessionID, "kill "+cmd.SessionID) {
			return
		}
		r.send(fmt.Sprintf("[%s] Session %s not found.", r.hostname, cmd.SessionID))
		return
	}

	if err := r.manager.Kill(sess.FullID); err != nil {
		r.send(fmt.Sprintf("[%s][%s] Failed to kill: %v", r.hostname, sess.ID, err))
		return
	}
	r.send(fmt.Sprintf("[%s][%s] Session killed.", r.hostname, sess.ID))
}

func (r *Router) handleTail(cmd Command) {
	if cmd.SessionID == "" {
		r.send(fmt.Sprintf("[%s] Usage: tail <id> [n]", r.hostname))
		return
	}

	n := cmd.TailN
	if n <= 0 {
		n = r.tailLines
	}

	sess, ok := r.manager.GetSession(cmd.SessionID)
	if !ok {
		tailCmd := fmt.Sprintf("tail %s %d", cmd.SessionID, n)
		if r.tryRemoteCommand(cmd.SessionID, tailCmd) {
			return
		}
		r.send(fmt.Sprintf("[%s] Session %s not found.", r.hostname, cmd.SessionID))
		return
	}

	out, err := r.manager.TailOutput(sess.FullID, n)
	if err != nil {
		r.send(fmt.Sprintf("[%s][%s] Error reading output: %v", r.hostname, sess.ID, err))
		return
	}
	r.send(fmt.Sprintf("[%s][%s] Last %d lines:\n%s", r.hostname, sess.ID, n, out))
}

func (r *Router) handleAttach(cmd Command) {
	if cmd.SessionID == "" {
		r.send(fmt.Sprintf("[%s] Usage: attach <id>", r.hostname))
		return
	}

	sess, ok := r.manager.GetSession(cmd.SessionID)
	if !ok {
		r.send(fmt.Sprintf("[%s] Session %s not found.", r.hostname, cmd.SessionID))
		return
	}

	r.send(fmt.Sprintf("[%s][%s] Run on %s:\n  tmux attach -t %s",
		r.hostname, sess.ID, sess.Hostname, sess.TmuxSession))
}

// handleImplicitSend routes an unrecognised message to the single waiting session, if any.
func (r *Router) handleImplicitSend(text string) {
	var waiting []*session.Session
	for _, s := range r.manager.ListSessions() {
		if s.State == session.StateWaitingInput && s.Hostname == r.hostname {
			waiting = append(waiting, s)
		}
	}

	switch len(waiting) {
	case 0:
		// Nothing to do — message is noise
	case 1:
		expanded := r.expandSavedCommand(text)
		if err := r.manager.SendInput(waiting[0].FullID, expanded, r.backend.Name()); err != nil {
			r.send(fmt.Sprintf("[%s][%s] Failed to send input: %v", r.hostname, waiting[0].ID, err))
		} else {
			r.send(fmt.Sprintf("[%s][%s] Input sent.", r.hostname, waiting[0].ID))
		}
	default:
		r.send(fmt.Sprintf("[%s] Multiple sessions waiting for input. Use: send <id>: <message>", r.hostname))
	}
}

// HandleStateChange is called by the session manager when session state changes.
// It is exported so that main.go can compose it with other callbacks (e.g. WS broadcast).
func (r *Router) HandleStateChange(sess *session.Session, oldState session.State) {
	if sess.Hostname != r.hostname {
		return
	}
	label := sess.ID
	if sess.Name != "" {
		label = sess.ID + " " + sess.Name
	}
	r.send(fmt.Sprintf("[%s][%s] State: %s → %s", r.hostname, label, oldState, sess.State))
}

// HandleNeedsInput is called when a session is waiting for user input.
// It is exported so that main.go can compose it with other callbacks (e.g. WS broadcast).
func (r *Router) HandleNeedsInput(sess *session.Session, prompt string) {
	if sess.Hostname != r.hostname {
		return
	}
	label := sess.ID
	if sess.Name != "" {
		label = sess.ID + " " + sess.Name
	}
	r.send(fmt.Sprintf("[%s][%s] Needs input:\n%s\n\nReply with: send %s: <your response>",
		r.hostname, label, prompt, sess.ID))
}

// send delivers a message to the messaging backend group asynchronously.
// Runs in a goroutine so the message handler is never blocked by a slow send.
func (r *Router) send(text string) {
	if r.chanTracker != nil {
		r.chanTracker.RecordSent(len(text))
	}
	go func() {
		if err := r.backend.Send(r.groupID, text); err != nil {
			fmt.Printf("ERROR sending to %s: %v\n", r.backend.Name(), err)
			if r.chanTracker != nil {
				r.chanTracker.RecordError()
			}
		}
	}()
}

// HandleTestMessage simulates an incoming message and captures all responses.
// Used by the API test endpoint for comm channel testing.
func (r *Router) HandleTestMessage(text string) []string {
	var responses []string
	var mu sync.Mutex

	// Create a capture backend that records sends
	origBackend := r.backend
	r.backend = &captureBackend{
		name:    "test",
		capture: func(msg string) { mu.Lock(); responses = append(responses, msg); mu.Unlock() },
	}

	// Simulate the message
	r.handleMessage(messaging.Message{
		ID:      "test-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		GroupID: r.groupID,
		Sender:  "test-api",
		Text:    text,
		Backend: "test",
	})

	// Wait briefly for async sends
	time.Sleep(200 * time.Millisecond)

	// Restore original backend
	r.backend = origBackend

	mu.Lock()
	defer mu.Unlock()
	return responses
}

// captureBackend is a messaging.Backend that captures sent messages for testing.
type captureBackend struct {
	name    string
	capture func(string)
}

func (b *captureBackend) Name() string                                              { return b.name }
func (b *captureBackend) Send(_, message string) error                              { b.capture(message); return nil }
func (b *captureBackend) Subscribe(_ context.Context, _ func(messaging.Message)) error { return nil }
func (b *captureBackend) Link(_ string, _ func(string)) error                       { return nil }
func (b *captureBackend) SelfID() string                                            { return "test" }
func (b *captureBackend) Close() error                                              { return nil }

// tryRemoteCommand checks if a session exists on a remote server and forwards
// the command. Returns true if the command was forwarded (caller should return).
func (r *Router) tryRemoteCommand(sessionID, command string) bool {
	if r.remote == nil || !r.remote.HasServers() {
		return false
	}
	serverName := r.remote.FindSession(sessionID)
	if serverName == "" {
		return false
	}
	responses, err := r.remote.ForwardCommand(serverName, command)
	if err != nil {
		r.send(fmt.Sprintf("[%s] Remote %s: %v", r.hostname, serverName, err))
		return true
	}
	for _, resp := range responses {
		r.send(resp)
	}
	return true
}

// SendDirect sends a message to the backend group without command parsing.
// Used for bundled alert messages.
func (r *Router) SendDirect(text string) {
	r.send(text)
}

// SendThreaded sends a message in a thread for the given session.
// If the backend supports threading, creates/reuses a thread per session.
// If the backend supports rich formatting, sends markdown instead of plain text.
// Falls back to SendDirect if neither is supported.
func (r *Router) SendThreaded(text, sessionFullID string, getThreadID func(string, string) string, setThreadID func(string, string, string)) {
	// If backend supports markdown but not threading, send rich text directly
	if _, ok := r.backend.(messaging.RichSender); ok {
		if _, isThreaded := r.backend.(messaging.ThreadedSender); !isThreaded {
			mdText := formatMarkdown(text)
			go r.backend.(messaging.RichSender).SendMarkdown(r.groupID, mdText) //nolint:errcheck
			return
		}
	}
	ts, ok := r.backend.(messaging.ThreadedSender)
	if !ok {
		r.send(text)
		return
	}
	backendName := r.backend.Name()
	threadID := ""
	if getThreadID != nil {
		threadID = getThreadID(sessionFullID, backendName)
	}
	if r.chanTracker != nil {
		r.chanTracker.RecordSent(len(text))
	}
	go func() {
		newThreadID, err := ts.SendThreaded(r.groupID, text, threadID)
		if err != nil {
			fmt.Printf("ERROR sending threaded to %s: %v\n", backendName, err)
			if r.chanTracker != nil {
				r.chanTracker.RecordError()
			}
			return
		}
		// Store thread ID for future replies
		if newThreadID != "" && threadID == "" && setThreadID != nil {
			setThreadID(sessionFullID, backendName, newThreadID)
		}
	}()
}

// SendWithButtons sends a message with action buttons if the backend supports it.
// Falls back to SendThreaded if buttons aren't supported.
func (r *Router) SendWithButtons(text, sessionFullID string, buttons []messaging.Button, getThreadID func(string, string) string, setThreadID func(string, string, string)) {
	bs, ok := r.backend.(messaging.ButtonSender)
	if !ok {
		// Fallback: append button hints as text
		r.SendThreaded(text, sessionFullID, getThreadID, setThreadID)
		return
	}
	backendName := r.backend.Name()
	threadID := ""
	if getThreadID != nil {
		threadID = getThreadID(sessionFullID, backendName)
	}
	if r.chanTracker != nil {
		r.chanTracker.RecordSent(len(text))
	}
	go func() {
		mdText := formatMarkdown(text)
		newThreadID, err := bs.SendWithButtons(r.groupID, mdText, buttons, threadID)
		if err != nil {
			fmt.Printf("ERROR sending buttons to %s: %v\n", backendName, err)
			return
		}
		if newThreadID != "" && threadID == "" && setThreadID != nil {
			setThreadID(sessionFullID, backendName, newThreadID)
		}
	}()
}

// SendFileInThread uploads a file in a thread if the backend supports it.
func (r *Router) SendFileInThread(filename, content, sessionFullID string, getThreadID func(string, string) string) {
	fs, ok := r.backend.(messaging.FileSender)
	if !ok {
		return // silently skip if not supported
	}
	backendName := r.backend.Name()
	threadID := ""
	if getThreadID != nil {
		threadID = getThreadID(sessionFullID, backendName)
	}
	go func() {
		if err := fs.SendFile(r.groupID, filename, content, threadID); err != nil {
			fmt.Printf("ERROR uploading file to %s: %v\n", backendName, err)
		}
	}()
}

// formatMarkdown converts a plain text alert message to markdown.
// Format: **header** on first line, task in italics, context in code block.
func formatMarkdown(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return text
	}
	// First line is the header: [hostname] name [id]: event
	result := "**" + lines[0] + "**"
	if len(lines) > 1 {
		result += "\n_" + lines[1] + "_" // task in italics
	}
	// If there's a --- separator followed by context, wrap in code block
	inContext := false
	var contextLines []string
	for i := 2; i < len(lines); i++ {
		if lines[i] == "---" {
			inContext = true
			continue
		}
		if inContext {
			contextLines = append(contextLines, lines[i])
		} else {
			result += "\n" + lines[i]
		}
	}
	if len(contextLines) > 0 {
		result += "\n```\n" + strings.Join(contextLines, "\n") + "\n```"
	}
	return result
}

// truncate shortens s to at most n characters, appending "..." if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
