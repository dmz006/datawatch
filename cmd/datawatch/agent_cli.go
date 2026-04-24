// F10 sprint 3 — `datawatch agent …` CLI.
//
// Mirrors `datawatch profile …` — all subcommands talk to the running
// daemon via REST. Reuses the profile_cli.go daemonAddressForCLI /
// http helpers rather than duplicating them.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage ephemeral agent workers",
		Long: `Spawn, list, inspect, and terminate ephemeral agent workers.

Each worker is a container running a slim datawatch daemon that works
on a single task, bound to a Project Profile (what) and Cluster Profile
(where). Requires the parent daemon to be running.`,
	}
	cmd.AddCommand(
		newAgentSpawnCmd(),
		newAgentListCmd(),
		newAgentShowCmd(),
		newAgentLogsCmd(),
		newAgentKillCmd(),
	)
	return cmd
}

func newAgentSpawnCmd() *cobra.Command {
	var project, cluster, task string
	var formatFlag string
	cmd := &cobra.Command{
		Use:   "spawn",
		Short: "Spawn a new agent worker",
		Long: `Spawn a new container-based agent bound to a (project, cluster) profile pair.

Example:
  datawatch agent spawn --project datawatch-app --cluster testing-k8s \
                       --task "add loading spinner to watch face"`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if project == "" || cluster == "" {
				return fmt.Errorf("--project and --cluster are required")
			}
			body, _ := json.Marshal(map[string]string{
				"project_profile": project,
				"cluster_profile": cluster,
				"task":            task,
			})
			out, err := profileCLIPost("/api/agents", body)
			if err != nil {
				return err
			}
			return renderProfileOutput(out, formatFlag, "")
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project profile name (required)")
	cmd.Flags().StringVar(&cluster, "cluster", "", "Cluster profile name (required)")
	cmd.Flags().StringVar(&task, "task", "", "Task description injected into worker's env")
	cmd.Flags().StringVarP(&formatFlag, "format", "f", "json", "Output format: json|yaml")
	return cmd
}

func newAgentListCmd() *cobra.Command {
	var formatFlag string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active agent workers",
		RunE: func(_ *cobra.Command, _ []string) error {
			out, err := profileCLIGet("/api/agents")
			if err != nil {
				return err
			}
			if formatFlag == "table" {
				return renderAgentTable(out)
			}
			return renderProfileOutput(out, formatFlag, "agents")
		},
	}
	cmd.Flags().StringVarP(&formatFlag, "format", "f", "table", "Output format: table|json|yaml")
	return cmd
}

func newAgentShowCmd() *cobra.Command {
	var formatFlag string
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show one agent by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			out, err := profileCLIGet("/api/agents/" + args[0])
			if err != nil {
				return err
			}
			return renderProfileOutput(out, formatFlag, "")
		},
	}
	cmd.Flags().StringVarP(&formatFlag, "format", "f", "json", "Output format: json|yaml")
	return cmd
}

func newAgentLogsCmd() *cobra.Command {
	var lines int
	cmd := &cobra.Command{
		Use:   "logs <id>",
		Short: "Fetch recent container logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path := "/api/agents/" + args[0] + "/logs?lines=" + strconv.Itoa(lines)
			out, err := profileCLIGet(path)
			if err != nil {
				return err
			}
			// Logs are plain text, not JSON — just dump.
			fmt.Print(string(out))
			return nil
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 200, "Max tail lines")
	return cmd
}

func newAgentKillCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kill <id>",
		Short: "Terminate an agent worker",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if _, err := profileCLIDelete("/api/agents/" + args[0]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Terminated agent %s\n", args[0])
			return nil
		},
	}
}

// renderAgentTable pretty-prints the list response as ID/STATE/
// PROFILE pair columns — enough to scan at a glance.
func renderAgentTable(body []byte) error {
	var wrapper struct {
		Agents []map[string]interface{} `json:"agents"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return err
	}
	if len(wrapper.Agents) == 0 {
		fmt.Println("(no agents)")
		return nil
	}
	fmt.Printf("%-32s  %-10s  %-40s  %s\n", "ID", "STATE", "PROFILES", "TASK")
	for _, a := range wrapper.Agents {
		id, _ := a["id"].(string)
		state, _ := a["state"].(string)
		proj, _ := a["project_profile"].(string)
		cluster, _ := a["cluster_profile"].(string)
		task, _ := a["task"].(string)
		if len(task) > 40 {
			task = task[:37] + "…"
		}
		fmt.Printf("%-32s  %-10s  %-40s  %s\n",
			id, state, proj+" / "+cluster, task)
	}
	return nil
}
