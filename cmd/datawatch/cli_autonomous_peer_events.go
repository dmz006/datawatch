package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

// newAutonomousPeerEventsCmd builds `autonomous peer-events`: read, start and
// stop the peer event subscriber (REST /api/autonomous/peer-events).
func newAutonomousPeerEventsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "peer-events",
		Short: "Read a federation peer's lifecycle events into this daemon (status, start, stop)",
		Long: `The peer event subscriber reads the lifecycle events of a registered federation
peer and feeds them into this daemon's local dispatch, so a local subscription
sees a peer's events like any other.

  peer-events status               whether the feature is on and which peers are being read
  peer-events start <peer> <queue> [--since CURSOR]
                                   start reading <peer>'s named pending-events queue
  peer-events stop <peer>          stop reading <peer>

It needs autonomous.notify.forwarding.enabled and autonomous.notify.forwarding.peer_events
both true, and the peer must be a federation peer that grants autonomous:read.
A read lasts until you stop it or the daemon restarts.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	c.AddCommand(newPeerEventsStatusCmd(), newPeerEventsStartCmd(), newPeerEventsStopCmd())
	return c
}

func newPeerEventsStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether peer event reading is on and which peers are read",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := daemonJSONBody(http.MethodGet, "/api/autonomous/peer-events", nil)
			if err != nil {
				return err
			}
			var v struct {
				Enabled bool     `json:"enabled"`
				Running []string `json:"running"`
			}
			if err := json.Unmarshal(body, &v); err != nil {
				return fmt.Errorf("unexpected response: %w", err)
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "peer event reading: %s\n", peerEventsOnOff(v.Enabled))
			if len(v.Running) == 0 {
				_, _ = fmt.Fprintln(out, "reading: no peers")
				return nil
			}
			_, _ = fmt.Fprintf(out, "reading: %s\n", strings.Join(v.Running, ", "))
			return nil
		},
	}
}

func newPeerEventsStartCmd() *cobra.Command {
	var since uint64
	c := &cobra.Command{
		Use:   "start <peer> <queue>",
		Short: "Start reading a peer's pending-events queue",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := daemonJSONBody(http.MethodPost, "/api/autonomous/peer-events/start",
				map[string]any{"peer": args[0], "queue": args[1], "since": since})
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "reading events from peer %s (queue %s)\n", args[0], args[1])
			return nil
		},
	}
	c.Flags().Uint64Var(&since, "since", 0, "resume after this queue cursor (0 reads the whole retained backlog)")
	return c
}

func newPeerEventsStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <peer>",
		Short: "Stop reading a peer's events",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := daemonJSONBody(http.MethodPost, "/api/autonomous/peer-events/stop", map[string]any{"peer": args[0]})
			if err != nil {
				return err
			}
			var v struct {
				Stopped bool `json:"stopped"`
			}
			if err := json.Unmarshal(body, &v); err != nil {
				return fmt.Errorf("unexpected response: %w", err)
			}
			if !v.Stopped {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "no read was running for peer %s\n", args[0])
				return nil
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "stopped reading events from peer %s\n", args[0])
			return nil
		},
	}
}

func peerEventsOnOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
