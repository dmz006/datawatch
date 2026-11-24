// `datawatch pause` and `datawatch resume`: the operator pause service. Same
// names and arguments as the REST, MCP and comm-channel surfaces:
//
//	datawatch pause --reason "release freeze"                 pause all new work
//	datawatch pause --project web --reason "reviewing"        pause one project
//	datawatch pause status [--peers]                           show the state
//	datawatch resume                                           clear the global pause
//	datawatch resume --project web                             clear one project
//	datawatch resume --all                                     clear every pause
//
// Add --peers (or --peer name, repeatable) to send the same action to the
// federation peers; each peer reports its own state.

package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

// pauseActorCLI labels the operator's own command line in the audit log.
const pauseActorCLI = "cli"

func pausePeersFields(body map[string]any, peers []string, allPeers bool) {
	if allPeers {
		body["all_peers"] = true
	}
	if len(peers) > 0 {
		body["peers"] = peers
	}
}

// pauseBody builds the POST /api/pause body. A project makes it a project
// pause; no project is the global pause. A reason is required.
func pauseBody(project, reason string, peers []string, allPeers bool) (map[string]any, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("--reason is required; say why work is being paused")
	}
	body := map[string]any{"scope": "global", "reason": reason, "actor": pauseActorCLI}
	if p := strings.TrimSpace(project); p != "" {
		body["scope"], body["project"] = "project", p
	}
	pausePeersFields(body, peers, allPeers)
	return body, nil
}

// resumeBody builds the POST /api/pause/resume body: --all clears everything,
// --project one project, neither the global pause.
func resumeBody(project string, all bool, note string, peers []string, allPeers bool) (map[string]any, error) {
	project = strings.TrimSpace(project)
	if all && project != "" {
		return nil, fmt.Errorf("--all clears every pause; it cannot be combined with --project")
	}
	body := map[string]any{"actor": pauseActorCLI}
	switch {
	case all:
		body["all"] = true
	case project != "":
		body["scope"], body["project"] = "project", project
	default:
		body["scope"] = "global"
	}
	if n := strings.TrimSpace(note); n != "" {
		body["note"] = n
	}
	pausePeersFields(body, peers, allPeers)
	return body, nil
}

func newPauseCmd() *cobra.Command {
	var project, reason string
	var peers []string
	var allPeers bool
	cmd := &cobra.Command{
		Use:   "pause",
		Short: "Pause new work (all, or one project); running tasks finish",
		Long: `Pause stops new work from being admitted: no new Automaton start, no restart,
no new worker admission, no lane fill. Tasks already running finish.
Schedules, the daemon, the autonomous manager, tabs, history, dashboards and
reports stay up. Only the operator can resume (datawatch resume); a worker,
watcher, supervisor or model cannot.`,
		Example: `  datawatch pause --reason "release freeze"
  datawatch pause --project web --reason "reviewing the plan"
  datawatch pause --reason "weekly limit" --peers`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := pauseBody(project, reason, peers, allPeers)
			if err != nil {
				return err
			}
			return daemonJSON(http.MethodPost, "/api/pause", body)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Pause only this project (a registered name or a directory); omit for a global pause")
	cmd.Flags().StringVar(&reason, "reason", "", "Why work is paused (required; recorded in the audit log)")
	cmd.Flags().StringSliceVar(&peers, "peer", nil, "Also send the pause to this federation peer (repeatable)")
	cmd.Flags().BoolVar(&allPeers, "peers", false, "Also send the pause to every federation peer")
	var statusPeers bool
	status := &cobra.Command{
		Use:   "status",
		Short: "Show the pause state (global and per project)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			path := "/api/pause"
			if statusPeers {
				path += "?peers=1"
			}
			return daemonGet(path)
		},
	}
	status.Flags().BoolVar(&statusPeers, "peers", false, "Also read every federation peer's own state")
	cmd.AddCommand(status)
	return cmd
}

func newResumeCmd() *cobra.Command {
	var project, note string
	var all, allPeers bool
	var peers []string
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume paused work (the global pause, one project, or everything)",
		Long: `Resume clears a pause so new work is admitted again. Only the operator can
resume: the daemon refuses it from a session, a worker, a watcher, a
supervisor or a model.`,
		Example: `  datawatch resume
  datawatch resume --project web
  datawatch resume --all --peers`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := resumeBody(project, all, note, peers, allPeers)
			if err != nil {
				return err
			}
			return daemonJSON(http.MethodPost, "/api/pause/resume", body)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Resume only this project; omit for the global pause")
	cmd.Flags().BoolVar(&all, "all", false, "Clear the global pause and every project pause in one action")
	cmd.Flags().StringVar(&note, "note", "", "Optional note recorded in the audit log")
	cmd.Flags().StringSliceVar(&peers, "peer", nil, "Also resume this federation peer (repeatable)")
	cmd.Flags().BoolVar(&allPeers, "peers", false, "Also resume every federation peer")
	return cmd
}
