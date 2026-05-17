// BL302 S4 — MCP Prompts surface for the datawatch daemon.
//
// This file registers 10 focused prompts as MCP slash commands. Each injects
// live resource data via readResourceText() so Claude has full context when
// invoked. The REST surface (/api/mcp/prompts, /api/mcp/prompts/get) exposes
// the same list so the channel bridge can discover and proxy them.
//
// Prompts registered (10 total):
//   1. analyze-session     — session state + channel history
//   2. review-automaton    — automaton config + execution status
//   3. triage-alert        — alert details + system stats
//   4. morning-briefing    — sessions + alerts + recent memory + stats
//   5. research-topic      — recent memory + KG entities
//   6. council-brief       — council run + personas
//   7. session-summary     — session channel history only
//   8. diagnose-system     — stats + alerts + config (no args)
//   9. explore-kg          — KG entities + triples
//  10. plan-sprint         — recent memory + daemon version

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dmz006/datawatch/internal/stats"
)

// promptDescriptor holds a prompt's metadata for REST export.
type promptDescriptor struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Arguments   []promptArgDesc  `json:"arguments,omitempty"`
}

// promptArgDesc is a compact prompt argument descriptor for REST export.
type promptArgDesc struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// promptRegistry is populated when RegisterPrompts() is called.
var globalPromptRegistry []promptDescriptor

// MCPPromptServer builds and serves the 10 datawatch prompts.
// It reads live resource data via the parent Server's REST loopback.
type MCPPromptServer struct {
	srv    *Server
	mcpSrv *server.MCPServer
	stats  *stats.MCPStatsCounters
}

// NewMCPPromptServer creates an MCPPromptServer attached to the given daemon Server.
// stats may be nil (stats recording is skipped).
func NewMCPPromptServer(srv *Server, mcpSrv *server.MCPServer, sc *stats.MCPStatsCounters) *MCPPromptServer {
	return &MCPPromptServer{srv: srv, mcpSrv: mcpSrv, stats: sc}
}

// readResourceText fetches a resource URI via the REST loopback and returns
// the first text content found. On any error (REST unavailable, resource not
// found, etc.) it returns "(data unavailable)" so prompt templates degrade
// gracefully without failing the whole prompt call.
func (p *MCPPromptServer) readResourceText(ctx context.Context, uri string) string {
	raw, err := p.srv.ResourceReadJSON(ctx, uri)
	if err != nil {
		return "(data unavailable)"
	}
	// ResourceReadJSON wraps the result in {"contents":[{"uri","mimeType","text"}],"uri":"..."}.
	// On REST loopback error the top-level envelope may have an "error" key, or the
	// individual text content itself may be an error JSON (e.g. {"error":"...","id":"..."}).
	var envelope struct {
		Error    string `json:"error"`
		Contents []struct {
			Text string `json:"text"`
		} `json:"contents"`
	}
	if json.Unmarshal(raw, &envelope) == nil {
		if envelope.Error != "" {
			return "(data unavailable)"
		}
		if len(envelope.Contents) > 0 {
			text := envelope.Contents[0].Text
			// Check whether the text itself is an error JSON from a handler
			// (e.g. {"error":"REST loopback unavailable...","id":"..."}).
			var textEnv struct {
				Error string `json:"error"`
			}
			if json.Unmarshal([]byte(text), &textEnv) == nil && textEnv.Error != "" {
				return "(data unavailable)"
			}
			return text
		}
	}
	// Fall back to the raw JSON bytes (shouldn't normally reach here).
	return string(raw)
}

// recordPromptCall increments stats if wired.
func (p *MCPPromptServer) recordPromptCall(name string) {
	if p.stats != nil {
		p.stats.RecordPromptCall(name)
	}
}

// RegisterPrompts registers all 10 BL302 S4 prompts on the MCP server
// and populates globalPromptRegistry for REST export.
func (p *MCPPromptServer) RegisterPrompts() {
	globalPromptRegistry = nil // reset on re-register

	type promptEntry struct {
		prompt  mcpsdk.Prompt
		handler server.PromptHandlerFunc
		desc    promptDescriptor
	}

	entries := []promptEntry{
		// 1. analyze-session
		{
			prompt: mcpsdk.NewPrompt("analyze-session",
				mcpsdk.WithPromptDescription("Analyze a datawatch session: summarize activity, note events, provide recommendations."),
				mcpsdk.WithArgument("session_id",
					mcpsdk.ArgumentDescription("Session ID to analyze (optional; omit for active session overview)"),
				),
			),
			handler: p.handleAnalyzeSession,
			desc: promptDescriptor{
				Name:        "analyze-session",
				Description: "Analyze a datawatch session: summarize activity, note events, provide recommendations.",
				Arguments: []promptArgDesc{
					{Name: "session_id", Description: "Session ID to analyze (optional; omit for active session overview)", Required: false},
				},
			},
		},
		// 2. review-automaton
		{
			prompt: mcpsdk.NewPrompt("review-automaton",
				mcpsdk.WithPromptDescription("Review an Automaton (PRD): check its config, current execution status, and suggest improvements."),
				mcpsdk.WithArgument("automaton_id",
					mcpsdk.ArgumentDescription("Automaton ID to review"),
					mcpsdk.RequiredArgument(),
				),
			),
			handler: p.handleReviewAutomaton,
			desc: promptDescriptor{
				Name:        "review-automaton",
				Description: "Review an Automaton (PRD): check its config, current execution status, and suggest improvements.",
				Arguments: []promptArgDesc{
					{Name: "automaton_id", Description: "Automaton ID to review", Required: true},
				},
			},
		},
		// 3. triage-alert
		{
			prompt: mcpsdk.NewPrompt("triage-alert",
				mcpsdk.WithPromptDescription("Triage a datawatch alert: assess severity, identify root cause, recommend action."),
				mcpsdk.WithArgument("alert_id",
					mcpsdk.ArgumentDescription("Alert ID to triage"),
					mcpsdk.RequiredArgument(),
				),
			),
			handler: p.handleTriageAlert,
			desc: promptDescriptor{
				Name:        "triage-alert",
				Description: "Triage a datawatch alert: assess severity, identify root cause, recommend action.",
				Arguments: []promptArgDesc{
					{Name: "alert_id", Description: "Alert ID to triage", Required: true},
				},
			},
		},
		// 4. morning-briefing
		{
			prompt: mcpsdk.NewPrompt("morning-briefing",
				mcpsdk.WithPromptDescription("Daily briefing: active sessions, alerts, recent memory, and daemon stats."),
				mcpsdk.WithArgument("since",
					mcpsdk.ArgumentDescription("ISO date to filter data from (optional, e.g. 2026-05-17)"),
				),
			),
			handler: p.handleMorningBriefing,
			desc: promptDescriptor{
				Name:        "morning-briefing",
				Description: "Daily briefing: active sessions, alerts, recent memory, and daemon stats.",
				Arguments: []promptArgDesc{
					{Name: "since", Description: "ISO date to filter data from (optional, e.g. 2026-05-17)", Required: false},
				},
			},
		},
		// 5. research-topic
		{
			prompt: mcpsdk.NewPrompt("research-topic",
				mcpsdk.WithPromptDescription("Research a topic using datawatch memory and knowledge graph."),
				mcpsdk.WithArgument("topic",
					mcpsdk.ArgumentDescription("Topic to research"),
					mcpsdk.RequiredArgument(),
				),
			),
			handler: p.handleResearchTopic,
			desc: promptDescriptor{
				Name:        "research-topic",
				Description: "Research a topic using datawatch memory and knowledge graph.",
				Arguments: []promptArgDesc{
					{Name: "topic", Description: "Topic to research", Required: true},
				},
			},
		},
		// 6. council-brief
		{
			prompt: mcpsdk.NewPrompt("council-brief",
				mcpsdk.WithPromptDescription("Brief the council: load a council run and registered personas for deliberation."),
				mcpsdk.WithArgument("council_id",
					mcpsdk.ArgumentDescription("Council run ID to brief on"),
					mcpsdk.RequiredArgument(),
				),
			),
			handler: p.handleCouncilBrief,
			desc: promptDescriptor{
				Name:        "council-brief",
				Description: "Brief the council: load a council run and registered personas for deliberation.",
				Arguments: []promptArgDesc{
					{Name: "council_id", Description: "Council run ID to brief on", Required: true},
				},
			},
		},
		// 7. session-summary
		{
			prompt: mcpsdk.NewPrompt("session-summary",
				mcpsdk.WithPromptDescription("Summarize the channel history of a session."),
				mcpsdk.WithArgument("session_id",
					mcpsdk.ArgumentDescription("Session ID to summarize"),
					mcpsdk.RequiredArgument(),
				),
			),
			handler: p.handleSessionSummary,
			desc: promptDescriptor{
				Name:        "session-summary",
				Description: "Summarize the channel history of a session.",
				Arguments: []promptArgDesc{
					{Name: "session_id", Description: "Session ID to summarize", Required: true},
				},
			},
		},
		// 8. diagnose-system
		{
			prompt: mcpsdk.NewPrompt("diagnose-system",
				mcpsdk.WithPromptDescription("Diagnose overall daemon health: inspect stats, alerts, and config for issues."),
			),
			handler: p.handleDiagnoseSystem,
			desc: promptDescriptor{
				Name:        "diagnose-system",
				Description: "Diagnose overall daemon health: inspect stats, alerts, and config for issues.",
			},
		},
		// 9. explore-kg
		{
			prompt: mcpsdk.NewPrompt("explore-kg",
				mcpsdk.WithPromptDescription("Explore the knowledge graph: entity statistics and recent triples."),
				mcpsdk.WithArgument("entity",
					mcpsdk.ArgumentDescription("Specific entity to focus on (optional)"),
				),
			),
			handler: p.handleExploreKG,
			desc: promptDescriptor{
				Name:        "explore-kg",
				Description: "Explore the knowledge graph: entity statistics and recent triples.",
				Arguments: []promptArgDesc{
					{Name: "entity", Description: "Specific entity to focus on (optional)", Required: false},
				},
			},
		},
		// 10. plan-sprint
		{
			prompt: mcpsdk.NewPrompt("plan-sprint",
				mcpsdk.WithPromptDescription("Plan the next sprint using recent memory entries and current daemon version."),
				mcpsdk.WithArgument("context",
					mcpsdk.ArgumentDescription("Additional context or focus area for sprint planning (optional)"),
				),
			),
			handler: p.handlePlanSprint,
			desc: promptDescriptor{
				Name:        "plan-sprint",
				Description: "Plan the next sprint using recent memory entries and current daemon version.",
				Arguments: []promptArgDesc{
					{Name: "context", Description: "Additional context or focus area for sprint planning (optional)", Required: false},
				},
			},
		},
	}

	for _, e := range entries {
		e := e
		p.mcpSrv.AddPrompt(e.prompt, e.handler)
		globalPromptRegistry = append(globalPromptRegistry, e.desc)
	}
}

// ── Prompt handlers ────────────────────────────────────────────────────────

func (p *MCPPromptServer) handleAnalyzeSession(ctx context.Context, req mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	p.recordPromptCall("analyze-session")
	id := req.Params.Arguments["session_id"]
	var sessionData, historyData string
	if id != "" {
		sessionData = p.readResourceText(ctx, "datawatch://sessions/"+id)
		historyData = p.readResourceText(ctx, "datawatch://sessions/"+id+"/history")
	} else {
		sessionData = p.readResourceText(ctx, "datawatch://sessions")
		historyData = "(no specific session selected — showing all sessions)"
	}
	text := fmt.Sprintf(`Analyze this datawatch session and provide a detailed assessment.

Session state:
%s

Channel history:
%s

Please provide:
1. A summary of session activity
2. Notable events or anomalies
3. Current status assessment
4. Recommendations for next steps`, sessionData, historyData)

	return &mcpsdk.GetPromptResult{
		Description: "Analyze datawatch session " + cond(id, id, "(active)"),
		Messages: []mcpsdk.PromptMessage{
			mcpsdk.NewPromptMessage(mcpsdk.RoleUser, mcpsdk.TextContent{Type: "text", Text: text}),
		},
	}, nil
}

func (p *MCPPromptServer) handleReviewAutomaton(ctx context.Context, req mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	p.recordPromptCall("review-automaton")
	id := req.Params.Arguments["automaton_id"]
	automatonData := p.readResourceText(ctx, "datawatch://automata/"+id)
	statusData := p.readResourceText(ctx, "datawatch://automata/"+id+"/status")
	text := fmt.Sprintf(`Review this Automaton (PRD) configuration and execution status.

Automaton configuration:
%s

Execution status:
%s

Please provide:
1. Configuration review (goals, tasks, LLM settings)
2. Current execution status assessment
3. Any blockers or issues identified
4. Recommendations for improvement or next steps`, automatonData, statusData)

	return &mcpsdk.GetPromptResult{
		Description: "Review Automaton " + id,
		Messages: []mcpsdk.PromptMessage{
			mcpsdk.NewPromptMessage(mcpsdk.RoleUser, mcpsdk.TextContent{Type: "text", Text: text}),
		},
	}, nil
}

func (p *MCPPromptServer) handleTriageAlert(ctx context.Context, req mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	p.recordPromptCall("triage-alert")
	id := req.Params.Arguments["alert_id"]
	alertData := p.readResourceText(ctx, "datawatch://alerts/"+id)
	statsData := p.readResourceText(ctx, "datawatch://stats")
	text := fmt.Sprintf(`Triage this datawatch alert and recommend a course of action.

Alert details:
%s

System stats (for context):
%s

Please provide:
1. Severity assessment
2. Root cause analysis
3. Immediate action recommended
4. Prevention recommendations`, alertData, statsData)

	return &mcpsdk.GetPromptResult{
		Description: "Triage alert " + id,
		Messages: []mcpsdk.PromptMessage{
			mcpsdk.NewPromptMessage(mcpsdk.RoleUser, mcpsdk.TextContent{Type: "text", Text: text}),
		},
	}, nil
}

func (p *MCPPromptServer) handleMorningBriefing(ctx context.Context, req mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	p.recordPromptCall("morning-briefing")
	since := req.Params.Arguments["since"]
	sinceNote := ""
	if since != "" {
		sinceNote = "\n\nNote: Filter data from " + since + " onwards where applicable."
	}
	sessionsData := p.readResourceText(ctx, "datawatch://sessions")
	alertsData := p.readResourceText(ctx, "datawatch://alerts")
	memoryData := p.readResourceText(ctx, "datawatch://memory/recent")
	statsData := p.readResourceText(ctx, "datawatch://stats")
	text := fmt.Sprintf(`Good morning! Here is your datawatch briefing.%s

Active sessions:
%s

Active alerts:
%s

Recent memory entries:
%s

Daemon stats:
%s

Please provide:
1. Overview of current session activity
2. Alert summary and priority items
3. Key insights from recent memory
4. Overall system health assessment
5. Recommended focus areas for today`, sinceNote, sessionsData, alertsData, memoryData, statsData)

	return &mcpsdk.GetPromptResult{
		Description: "Morning briefing" + cond(since, " (since "+since+")", ""),
		Messages: []mcpsdk.PromptMessage{
			mcpsdk.NewPromptMessage(mcpsdk.RoleUser, mcpsdk.TextContent{Type: "text", Text: text}),
		},
	}, nil
}

func (p *MCPPromptServer) handleResearchTopic(ctx context.Context, req mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	p.recordPromptCall("research-topic")
	topic := req.Params.Arguments["topic"]
	memoryData := p.readResourceText(ctx, "datawatch://memory/recent")
	kgData := p.readResourceText(ctx, "datawatch://kg/entities")
	text := fmt.Sprintf(`Research the following topic using datawatch memory and knowledge graph data.

Topic: %s

Recent memory entries:
%s

Knowledge graph entities:
%s

Please provide:
1. What is known about this topic from the memory store
2. Relevant entities and relationships in the knowledge graph
3. Key findings and patterns
4. Gaps or areas needing more investigation`, topic, memoryData, kgData)

	return &mcpsdk.GetPromptResult{
		Description: "Research topic: " + topic,
		Messages: []mcpsdk.PromptMessage{
			mcpsdk.NewPromptMessage(mcpsdk.RoleUser, mcpsdk.TextContent{Type: "text", Text: text}),
		},
	}, nil
}

func (p *MCPPromptServer) handleCouncilBrief(ctx context.Context, req mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	p.recordPromptCall("council-brief")
	id := req.Params.Arguments["council_id"]
	councilData := p.readResourceText(ctx, "datawatch://council/"+id)
	personasData := p.readResourceText(ctx, "datawatch://council/personas")
	text := fmt.Sprintf(`Brief the council on this run and prepare for deliberation.

Council run %s:
%s

Registered personas:
%s

Please provide:
1. Summary of the council run topic and current state
2. Key perspectives each persona should bring
3. Important questions to resolve in deliberation
4. Suggested discussion structure`, id, councilData, personasData)

	return &mcpsdk.GetPromptResult{
		Description: "Council brief for run " + id,
		Messages: []mcpsdk.PromptMessage{
			mcpsdk.NewPromptMessage(mcpsdk.RoleUser, mcpsdk.TextContent{Type: "text", Text: text}),
		},
	}, nil
}

func (p *MCPPromptServer) handleSessionSummary(ctx context.Context, req mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	p.recordPromptCall("session-summary")
	id := req.Params.Arguments["session_id"]
	historyData := p.readResourceText(ctx, "datawatch://sessions/"+id+"/history")
	text := fmt.Sprintf(`Summarize the channel history of this datawatch session.

Session ID: %s

Channel history:
%s

Please provide:
1. Concise summary of what was accomplished
2. Key decisions made
3. Outstanding items or TODOs
4. Overall assessment of the session`, id, historyData)

	return &mcpsdk.GetPromptResult{
		Description: "Session summary for " + id,
		Messages: []mcpsdk.PromptMessage{
			mcpsdk.NewPromptMessage(mcpsdk.RoleUser, mcpsdk.TextContent{Type: "text", Text: text}),
		},
	}, nil
}

func (p *MCPPromptServer) handleDiagnoseSystem(ctx context.Context, _ mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	p.recordPromptCall("diagnose-system")
	statsData := p.readResourceText(ctx, "datawatch://stats")
	alertsData := p.readResourceText(ctx, "datawatch://alerts")
	configData := p.readResourceText(ctx, "datawatch://config")
	text := fmt.Sprintf(`Diagnose the overall health of this datawatch daemon installation.

Daemon stats:
%s

Active alerts:
%s

Daemon configuration (sanitized):
%s

Please provide:
1. Overall health assessment (healthy / degraded / critical)
2. Any issues or anomalies detected
3. Configuration review (any misconfigurations or risks)
4. Specific recommendations to improve reliability or performance`, statsData, alertsData, configData)

	return &mcpsdk.GetPromptResult{
		Description: "Diagnose system health",
		Messages: []mcpsdk.PromptMessage{
			mcpsdk.NewPromptMessage(mcpsdk.RoleUser, mcpsdk.TextContent{Type: "text", Text: text}),
		},
	}, nil
}

func (p *MCPPromptServer) handleExploreKG(ctx context.Context, req mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	p.recordPromptCall("explore-kg")
	entity := req.Params.Arguments["entity"]
	entitiesData := p.readResourceText(ctx, "datawatch://kg/entities")
	triplesData := p.readResourceText(ctx, "datawatch://kg/triples")
	entityNote := ""
	if entity != "" {
		entityNote = "\n\nFocus entity: " + entity
	}
	text := fmt.Sprintf(`Explore the datawatch knowledge graph.%s

KG entity statistics:
%s

Recent KG triples:
%s

Please provide:
1. Overview of the knowledge graph structure
2. Most prominent entities and their roles%s
3. Notable relationships and patterns
4. Suggestions for enriching the knowledge graph`, entityNote, entitiesData, triplesData,
		cond(entity, "\n5. Deep dive on entity: "+entity, ""))

	return &mcpsdk.GetPromptResult{
		Description: "Explore knowledge graph" + cond(entity, " (focus: "+entity+")", ""),
		Messages: []mcpsdk.PromptMessage{
			mcpsdk.NewPromptMessage(mcpsdk.RoleUser, mcpsdk.TextContent{Type: "text", Text: text}),
		},
	}, nil
}

func (p *MCPPromptServer) handlePlanSprint(ctx context.Context, req mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	p.recordPromptCall("plan-sprint")
	extraContext := req.Params.Arguments["context"]
	memoryData := p.readResourceText(ctx, "datawatch://memory/recent")
	versionData := p.readResourceText(ctx, "datawatch://version")
	contextNote := ""
	if extraContext != "" {
		contextNote = "\n\nAdditional context: " + extraContext
	}
	text := fmt.Sprintf(`Plan the next development sprint for datawatch.%s

Daemon version info:
%s

Recent memory entries (decisions, learnings, context):
%s

Please provide:
1. Proposed sprint goal
2. Suggested backlog items to prioritize (with rationale)
3. Dependencies or blockers to address first
4. Risk assessment
5. Definition of done for the sprint`, contextNote, versionData, memoryData)

	return &mcpsdk.GetPromptResult{
		Description: "Plan next sprint" + cond(extraContext, " ("+extraContext+")", ""),
		Messages: []mcpsdk.PromptMessage{
			mcpsdk.NewPromptMessage(mcpsdk.RoleUser, mcpsdk.TextContent{Type: "text", Text: text}),
		},
	}, nil
}

// ── REST export methods ────────────────────────────────────────────────────

// PromptsListJSON returns all registered prompts as JSON for GET /api/mcp/prompts.
func PromptsListJSON() []byte {
	out, _ := json.Marshal(map[string]interface{}{
		"prompts": globalPromptRegistry,
	})
	return out
}

// PromptsGetJSON renders a prompt by name with the given arguments.
// Returns the rendered messages as JSON, or an error message JSON on failure.
// stats may be nil.
func (p *MCPPromptServer) PromptsGetJSON(ctx context.Context, name string, args map[string]string) ([]byte, error) {
	req := mcpsdk.GetPromptRequest{
		Params: mcpsdk.GetPromptParams{
			Name:      name,
			Arguments: args,
		},
	}

	var result *mcpsdk.GetPromptResult
	var err error
	switch name {
	case "analyze-session":
		result, err = p.handleAnalyzeSession(ctx, req)
	case "review-automaton":
		result, err = p.handleReviewAutomaton(ctx, req)
	case "triage-alert":
		result, err = p.handleTriageAlert(ctx, req)
	case "morning-briefing":
		result, err = p.handleMorningBriefing(ctx, req)
	case "research-topic":
		result, err = p.handleResearchTopic(ctx, req)
	case "council-brief":
		result, err = p.handleCouncilBrief(ctx, req)
	case "session-summary":
		result, err = p.handleSessionSummary(ctx, req)
	case "diagnose-system":
		result, err = p.handleDiagnoseSystem(ctx, req)
	case "explore-kg":
		result, err = p.handleExploreKG(ctx, req)
	case "plan-sprint":
		result, err = p.handlePlanSprint(ctx, req)
	default:
		return nil, fmt.Errorf("unknown prompt: %s", name)
	}
	if err != nil {
		return nil, err
	}

	// Serialise messages — TextContent must be converted to a JSON-friendly form.
	type msgOut struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	msgs := make([]msgOut, 0, len(result.Messages))
	for _, m := range result.Messages {
		text := ""
		if tc, ok := m.Content.(mcpsdk.TextContent); ok {
			text = tc.Text
		} else {
			raw, _ := json.Marshal(m.Content)
			text = string(raw)
		}
		msgs = append(msgs, msgOut{Role: string(m.Role), Content: text})
	}
	out, err := json.Marshal(map[string]interface{}{
		"name":        name,
		"description": result.Description,
		"messages":    msgs,
	})
	return out, err
}

// ── helper ─────────────────────────────────────────────────────────────────

// cond returns ifTrue when s is non-empty, otherwise ifFalse.
func cond(s, ifTrue, ifFalse string) string {
	if strings.TrimSpace(s) != "" {
		return ifTrue
	}
	return ifFalse
}
