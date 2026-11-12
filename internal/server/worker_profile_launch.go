package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/claudecode"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// workerProfileLaunchFn returns a per-session launch function that applies
// the worker profile to this one claude-code launch, or nil when no profile
// was sent or the effective backend is not claude-code. The shared backend
// instance is never mutated, so concurrent sessions each carry their own
// profile. When the profile cannot be applied the launch starts with no
// profile (see claudecode.LaunchWithProfileFallback) and the reason is written
// to fallbackReason, which the caller owns; it may be nil.
func (s *Server) workerProfileLaunchFn(p *workerprofile.WorkerProfile, backendName string, fallbackReason *string) session.LaunchFunc {
	if p == nil || p.Name == "" {
		return nil
	}
	if backendName == "" && s.cfg != nil {
		backendName = s.cfg.Session.LLMBackend
	}
	b, err := llm.Get(backendName)
	if err != nil {
		return nil
	}
	cc, ok := b.(*claudecode.Backend)
	if !ok {
		return nil
	}
	profile := *p
	return func(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
		w := claudecode.WorkerProfileLaunch{
			Profile:    profile,
			MCPServers: sessionMCPServers(projectDir),
			Starting:   autonomous.StartingContextInput{Role: "worker", TaskSpec: task},
		}
		reason, err := cc.LaunchWithProfileFallback(ctx, w, task, tmuxSession, projectDir, logFile)
		if reason != "" && fallbackReason != nil {
			*fallbackReason = reason
		}
		return err
	}
}

// sessionMCPServers reads the MCP servers the session already has in its
// project .mcp.json (the per-session datawatch bridge plus configured extras)
// — the set a worker profile's MCP selection picks from. Best effort: an
// unreadable file yields an empty set.
func sessionMCPServers(projectDir string) map[string]channel.MCPServerSpec {
	raw, err := os.ReadFile(filepath.Join(projectDir, ".mcp.json"))
	if err != nil {
		return nil
	}
	var cfg channel.MCPProjectConfig
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	return cfg.MCPServers
}
