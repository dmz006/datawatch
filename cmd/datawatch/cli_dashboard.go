// cli_dashboard.go — #57/#58 CLI surface for dashboard card layout.
//
//	datawatch dashboard ls                     list cards
//	datawatch dashboard card get <id>          get one card
//	datawatch dashboard card set <id> <cs>     set column span (add if missing)
//	datawatch dashboard card add <id> <cs>     append a card
//	datawatch dashboard card rm <id>           remove a card
//	datawatch dashboard layout get             get raw layout JSON
//	datawatch dashboard layout set             set raw layout JSON from stdin

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newDashboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Manage dashboard card layout (#57)",
	}
	cmd.AddCommand(newDashboardLsCmd())
	cmd.AddCommand(newDashboardCardCmd())
	cmd.AddCommand(newDashboardLayoutCmd())
	cmd.AddCommand(newDashboardConsoleCmd())
	return cmd
}

func newDashboardLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List dashboard cards",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/dashboard/cards") },
	}
}

func newDashboardCardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "card",
		Short: "Card sub-commands (get/set/add/rm)",
	}
	cmd.AddCommand(newDashboardCardGetCmd())
	cmd.AddCommand(newDashboardCardSetCmd())
	cmd.AddCommand(newDashboardCardAddCmd())
	cmd.AddCommand(newDashboardCardRmCmd())
	return cmd
}

func newDashboardCardGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get one dashboard card by id",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/dashboard/cards/" + args[0]) },
	}
}

func newDashboardCardSetCmd() *cobra.Command {
	var rs int
	cmd := &cobra.Command{
		Use:   "set <id> <cs>",
		Short: "Set column span for a card; adds the card if not present",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			cs, err := strconv.Atoi(args[1])
			if err != nil || cs < 1 || cs > 12 {
				return fmt.Errorf("cs must be 1-12")
			}
			body := map[string]any{"id": args[0], "cs": cs}
			if rs > 0 {
				body["rs"] = rs
			}
			return daemonJSON(http.MethodPut, "/api/dashboard/cards/"+args[0], body)
		},
	}
	cmd.Flags().IntVar(&rs, "rs", 0, "row span (optional)")
	return cmd
}

func newDashboardCardAddCmd() *cobra.Command {
	var rs int
	cmd := &cobra.Command{
		Use:   "add <id> <cs>",
		Short: "Append a card to the dashboard layout",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			cs, err := strconv.Atoi(args[1])
			if err != nil || cs < 1 || cs > 12 {
				return fmt.Errorf("cs must be 1-12")
			}
			body := map[string]any{"id": args[0], "cs": cs}
			if rs > 0 {
				body["rs"] = rs
			}
			return daemonJSON(http.MethodPost, "/api/dashboard/cards", body)
		},
	}
	cmd.Flags().IntVar(&rs, "rs", 0, "row span (optional)")
	return cmd
}

func newDashboardCardRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Remove a card from the dashboard layout",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/dashboard/cards/"+args[0], nil)
		},
	}
}

func newDashboardLayoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "layout",
		Short: "Raw dashboard layout (get/set)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Get raw dashboard layout JSON",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/dashboard/layout") },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set",
		Short: "Set raw dashboard layout JSON (reads from stdin)",
		RunE: func(_ *cobra.Command, _ []string) error {
			var body json.RawMessage
			if err := json.NewDecoder(os.Stdin).Decode(&body); err != nil {
				return fmt.Errorf("invalid JSON: %w", err)
			}
			return daemonJSON(http.MethodPut, "/api/dashboard/layout", body)
		},
	})
	return cmd
}

// newDashboardConsoleCmd is the operator console summary over chat-config
// parity: the same GET the PWA console uses, plus a set subcommand that
// reuses the daemon config API path (PUT /api/config) — the exact same
// path the chat `configure dashboard.console.enabled=false` command takes.
//
//	datawatch dashboard console             — operator console summary (GET /api/dashboard/console)
//	datawatch dashboard console --json      — the raw endpoint JSON
//	datawatch dashboard console set <key> <value> — set a console key
func newDashboardConsoleCmd() *cobra.Command {
	var rawJSON bool
	console := &cobra.Command{
		Use:   "console",
		Short: "Operator console summary (GET /api/dashboard/console)",
		RunE: func(*cobra.Command, []string) error {
			body, err := daemonJSONBody(http.MethodGet, "/api/dashboard/console", nil)
			if err != nil {
				return err
			}
			if rawJSON {
				prettyPrint(body)
				return nil
			}
			fmt.Println(renderDashboardConsole(body))
			return nil
		},
	}
	console.Flags().BoolVar(&rawJSON, "json", false, "print the raw endpoint JSON")
	console.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a console key via the daemon config API (dashboard.console.enabled=false, dashboard.console.show_lanes=true, …)",
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("usage: dashboard console set <key> <value>")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runConfigSet(cfg, args[0], args[1])
		},
	})
	return console
}

// dashboardConsoleSection mirrors one section of the console endpoint response.
// Available=false means the console (or that section) is switched off in config.
type dashboardConsoleSection struct {
	Available bool   `json:"available"`
	Count     int    `json:"count"`
	Summary   string `json:"summary"`
	Link      string `json:"link"`
}

// dashboardConsoleSectionIDs is the endpoint's canonical section order.
var dashboardConsoleSectionIDs = []string{"questions_waiting", "held_blocked", "lanes", "usage", "ci_release"}

// renderDashboardConsole is the human view of the endpoint JSON: one line per
// section in the endpoint's order — the same five sections the PWA console
// shows, from the identical data.
func renderDashboardConsole(body []byte) string {
	var c struct {
		GeneratedAt string                             `json:"generated_at"`
		Sections    map[string]dashboardConsoleSection `json:"sections"`
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return fmt.Sprintf("dashboard console: unparseable response: %v", err)
	}
	var b strings.Builder
	if c.GeneratedAt != "" {
		fmt.Fprintf(&b, "Operator console (%s)\n", c.GeneratedAt)
	}
	for _, id := range dashboardConsoleSectionIDs {
		s := c.Sections[id]
		if !s.Available {
			fmt.Fprintf(&b, "  %-18s off   %s\n", id, s.Summary)
			continue
		}
		fmt.Fprintf(&b, "  %-18s %d    %s\n", id, s.Count, s.Summary)
	}
	return strings.TrimSpace(b.String())
}
