// release_push_tools.go — the release_push_status MCP tool.
//
// release_push_status answers, for one project, how a finished release is
// published: whether the daemon pushes and tags it (autonomous.push_and_tag),
// the release path (autonomous.release_path), the CI window the daemon waits
// for the release checks (git.release_ci_window_minutes), and the last release
// recorded for the project. The settings are read from the daemon's config
// reply (GET /api/config); the last release is read in-process through
// autonomous.ReadLastRelease. The tool only reads.
package mcp

import (
	"context"
	"encoding/json"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// releaseCIWindowDefaultMinutes is the CI window used when
// git.release_ci_window_minutes is unset (matches the config default).
const releaseCIWindowDefaultMinutes = 60

// releasePushView is the answer of release_push_status.
type releasePushView struct {
	Project         string              `json:"project"`
	PushAndTag      bool                `json:"push_and_tag"`
	ReleasePath     string              `json:"release_path"`
	CIWindowMinutes int                 `json:"ci_window_minutes"`
	LastRelease     *releasePushRelease `json:"last_release"`
}

// releasePushRelease is the last release of the project, as the release
// record holds it.
type releasePushRelease struct {
	Tag         string `json:"tag"`
	Commit      string `json:"commit"`
	ReleasePath string `json:"release_path"`
	PushOutcome string `json:"push_outcome"`
	CIOutcome   string `json:"ci_outcome"`
}

// releasePushConfigReply is the part of GET /api/config that release_push_status
// reads.
type releasePushConfigReply struct {
	Autonomous struct {
		PushAndTag  bool   `json:"push_and_tag"`
		ReleasePath string `json:"release_path"`
	} `json:"autonomous"`
	Git struct {
		ReleaseCIWindowMinutes int `json:"release_ci_window_minutes"`
	} `json:"git"`
}

func (s *Server) toolReleasePushStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("release_push_status",
		mcpsdk.WithDescription("Read how a project's finished release is published. Answers push_and_tag (whether the daemon pushes and tags the release on its own; default off), release_path (direct_push or pull_request; default direct_push), ci_window_minutes (how long the daemon waits for the release checks; default 60), and last_release (the newest release recorded for the project: tag, commit, release_path, push_outcome, ci_outcome; null when none). Settings not set read their defaults. This is about an Automaton's release, never a PRD. Read-only."),
		mcpsdk.WithString("project", mcpsdk.Required(), mcpsdk.Description("The project name")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleReleasePushStatus(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "release_push_status"
	project, _ := req.GetArguments()["project"].(string)
	project = strings.TrimSpace(project)
	if project == "" {
		return toolErrorf(op, "project is required"), nil
	}
	body, err := s.proxyGet("/api/config", nil)
	if err != nil {
		return toolError(op, err), nil
	}
	view, err := releasePushViewFromConfig(body, project)
	if err != nil {
		return toolError(op, err), nil
	}
	b, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(b)), nil
}

// releasePushViewFromConfig builds the answer from the config reply, with the
// defaults for every setting the reply leaves unset, and the project's last
// release from the release record store.
func releasePushViewFromConfig(body []byte, project string) (releasePushView, error) {
	var cfg releasePushConfigReply
	if err := json.Unmarshal(body, &cfg); err != nil {
		return releasePushView{}, err
	}
	view := releasePushView{
		Project:         project,
		PushAndTag:      cfg.Autonomous.PushAndTag,
		ReleasePath:     cfg.Autonomous.ReleasePath,
		CIWindowMinutes: cfg.Git.ReleaseCIWindowMinutes,
	}
	if view.ReleasePath == "" {
		view.ReleasePath = config.ReleasePathDirectPush
	}
	if view.CIWindowMinutes <= 0 {
		view.CIWindowMinutes = releaseCIWindowDefaultMinutes
	}
	if rec := autonomous.ReadLastRelease(project); rec != nil {
		view.LastRelease = &releasePushRelease{
			Tag:         rec.Tag,
			Commit:      rec.PushedCommit,
			ReleasePath: rec.ReleasePath,
			PushOutcome: rec.PushOutcome,
			CIOutcome:   rec.CIOutcome,
		}
	}
	return view, nil
}
