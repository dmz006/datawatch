package mcp

import (
	"context"
	"encoding/json"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/platformcontext"
	"github.com/dmz006/datawatch/internal/session"
)

// PlatformContextToolNames lists the platform context tools, for the parity test.
func PlatformContextToolNames() []string { return []string{"platform_context_status"} }

func (s *Server) toolPlatformContextStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("platform_context_status",
		mcpsdk.WithDescription("Report the platform context state for the calling session: enabled (effective for this session), project_override (the project's own value, null when none), text_version (version of the platform context text) and delivery (how this session's backend receives it: rules_file, first_prompt or rules_file_and_first_prompt). Read-only."),
		mcpsdk.WithString("hint",
			mcpsdk.Description("Optional session id (short or full) to report for instead of the calling session.")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handlePlatformContextStatus(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const name = "platform_context_status"
	if s.manager == nil {
		return toolErrorf(name, "session manager unavailable"), nil
	}
	var sess *session.Session
	if hint := req.GetString("hint", ""); hint != "" {
		cand, err := s.resolveSession(hint)
		if err != nil {
			return toolError(name, err), nil
		}
		if cand == nil {
			return toolErrorf(name, "session %q not found", hint), nil
		}
		sess = cand
	} else {
		if callerID, ok := s.scopedCallerSessionID(ctx); ok {
			sess, _ = s.resolveSession(callerID)
		}
		if sess == nil {
			for _, cand := range s.manager.ListSessions() {
				if cand.ChannelReady && (cand.State == session.StateRunning || cand.State == session.StateWaitingInput) {
					sess = cand
					break
				}
			}
		}
	}
	if sess == nil {
		return toolErrorf(name, "no session resolved — pass hint=<session id>, or call from inside a session over its scoped channel."), nil
	}
	st := s.manager.PlatformContextStatusFor(sess)
	out, err := json.MarshalIndent(map[string]any{
		"session":          sess.FullID,
		"enabled":          st.Enabled,
		"project_override": st.ProjectOverride,
		"text_version":     platformcontext.PlatformContextVersion,
		"delivery":         st.Delivery,
	}, "", "  ")
	if err != nil {
		return toolError(name, err), nil
	}
	return textOK(string(out)), nil
}
